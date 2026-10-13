package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory  uint32 = 64 * 1024
	passwordTime    uint32 = 3
	passwordThreads uint8  = 2
)

// ErrPasswordBusy refuses expensive password work when process capacity is full.
var ErrPasswordBusy = errors.New("password work capacity is exhausted")

const (
	passwordConcurrency           = 2
	passwordMemoryEnvelope uint32 = 256 * 1024
)

// Admission reserves active KDF memory, leaving half of a 512 MiB host for
// application work. Retained Go heap and process RSS have separate lifetimes.
var passwordWork struct {
	sync.Mutex
	running int
	memory  uint32
}

func acquirePasswordWork(ctx context.Context, memory uint32) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	passwordWork.Lock()
	defer passwordWork.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if memory == 0 || memory > passwordMemoryEnvelope || passwordWork.running == passwordConcurrency || memory > passwordMemoryEnvelope-passwordWork.memory {
		return nil, ErrPasswordBusy
	}

	passwordWork.running++
	passwordWork.memory += memory

	return func() {
		passwordWork.Lock()
		defer passwordWork.Unlock()

		passwordWork.running--
		passwordWork.memory -= memory
	}, nil
}

// HashPassword encodes the algorithm and parameters alongside a random salt.
func HashPassword(ctx context.Context, password string) (string, error) {
	release, err := acquirePasswordWork(ctx, passwordMemory)
	if err != nil {
		return "", err
	}
	defer release()

	if len(password) < 12 || len(password) > 1024 {
		return "", fmt.Errorf("password must contain between 12 and 1024 bytes")
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, passwordTime, passwordMemory, passwordThreads, 32)

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemory, passwordTime, passwordThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword also indicates whether successful credentials need a policy upgrade.
func VerifyPassword(ctx context.Context, encoded, password string) (valid, upgrade bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}

	if len(password) > 1024 {
		return false, false, nil
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, false, nil
	}

	var (
		memory, iterations uint32
		parallel           uint8
	)
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallel); err != nil || n != 3 {
		return false, false, nil
	}

	if memory < 8*uint32(parallel) || memory > 256*1024 || iterations < 1 || iterations > 10 || parallel < 1 || parallel > 16 {
		return false, false, nil
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, false, nil
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) != 32 {
		return false, false, nil
	}

	release, err := acquirePasswordWork(ctx, memory)
	if err != nil {
		return false, false, err
	}
	defer release()

	if err := ctx.Err(); err != nil {
		return false, false, err
	}

	candidate := argon2.IDKey([]byte(password), salt, iterations, memory, parallel, 32)

	if err := ctx.Err(); err != nil {
		return false, false, err
	}

	return subtle.ConstantTimeCompare(candidate, hash) == 1, memory < passwordMemory || iterations < passwordTime || parallel < passwordThreads, nil
}
