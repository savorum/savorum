package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

func TestPasswordPolicy(t *testing.T) {
	hash, err := HashPassword(t.Context(), "a long password-manager passphrase")
	if err != nil {
		t.Fatal(err)
	}

	if valid, upgrade, err := VerifyPassword(t.Context(), hash, "a long password-manager passphrase"); err != nil || !valid || upgrade {
		t.Fatal("current password rejected or incorrectly marked old")
	}

	if valid, _, _ := VerifyPassword(t.Context(), hash, "wrong"); valid {
		t.Fatal("wrong password accepted")
	}

	for _, invalid := range []string{"", "$argon2id$v=19$m=0,t=0,p=0$x$x", "$argon2id$v=19$m=999999999,t=3,p=2$x$x"} {
		if valid, _, _ := VerifyPassword(t.Context(), invalid, "password"); valid {
			t.Fatal("invalid hash accepted")
		}
	}
}

func TestOlderArgonParametersRequestUpgrade(t *testing.T) {
	salt := []byte("0123456789abcdef")
	hash := argon2.IDKey([]byte("a long existing passphrase"), salt, 1, 8192, 1, 32)

	encoded := "$argon2id$v=19$m=8192,t=1,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash)
	if valid, upgrade, err := VerifyPassword(t.Context(), encoded, "a long existing passphrase"); err != nil || !valid || !upgrade {
		t.Fatal("older valid password did not request rehash")
	}
}

func TestPasswordAdmission(t *testing.T) {
	ctx := t.Context()

	first, err := acquirePasswordWork(ctx, passwordMemory)
	if err != nil {
		t.Fatal(err)
	}

	second, err := acquirePasswordWork(ctx, passwordMemory)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := HashPassword(ctx, "a long password-manager passphrase"); !errors.Is(err, ErrPasswordBusy) {
		t.Fatalf("hash overload: %v", err)
	}

	if _, _, err := VerifyPassword(ctx, "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "password"); !errors.Is(err, ErrPasswordBusy) {
		t.Fatalf("verify overload: %v", err)
	}

	first()
	second()

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := HashPassword(canceled, "a long password-manager passphrase"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := HashPassword(ctx, "short"); err == nil {
		t.Fatal("invalid password accepted")
	}

	for range passwordConcurrency {
		release, err := acquirePasswordWork(ctx, passwordMemory)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
}

func TestPasswordAdmissionConcurrentCapacity(t *testing.T) {
	var running, maximum atomic.Int32

	start, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}, 20)
	admitted := make(chan struct{}, 20)

	for range 20 {
		go func() {
			<-start

			free, err := acquirePasswordWork(t.Context(), passwordMemory)
			if err == nil {
				current := running.Add(1)

				for {
					old := maximum.Load()
					if current <= old || maximum.CompareAndSwap(old, current) {
						break
					}
				}

				admitted <- struct{}{}

				<-release
				running.Add(-1)
				free()
			} else if !errors.Is(err, ErrPasswordBusy) {
				t.Errorf("admission: %v", err)
			}

			done <- struct{}{}
		}()
	}

	close(start)

	for range passwordConcurrency {
		<-admitted
	}
	// All excess callers finish while the admitted work remains blocked.
	for range 20 - passwordConcurrency {
		<-done
	}

	close(release)

	for range passwordConcurrency {
		<-done
	}

	if maximum.Load() != 2 || passwordWorkRunning() != 0 {
		t.Fatalf("capacity=%d retained=%d", maximum.Load(), passwordWorkRunning())
	}
}

func TestPasswordCancellationReleasesAdmittedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() {
		hash, err := HashPassword(ctx, "a long password-manager passphrase")
		if hash != "" {
			t.Error("canceled hash returned a credential")
		}

		done <- err
	}()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	for passwordWorkRunning() == 0 {
		select {
		case err := <-done:
			t.Fatalf("work completed before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("work was not admitted")
		case <-ticker.C:
		}
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-deadline.C:
		t.Fatal("canceled work did not finish")
	}

	if passwordWorkRunning() != 0 {
		t.Fatal("canceled work retained capacity")
	}
}

func passwordWorkRunning() int {
	passwordWork.Lock()
	defer passwordWork.Unlock()

	return passwordWork.running
}

func TestPasswordWeightedAdmission(t *testing.T) {
	ctx := t.Context()

	legacy, err := acquirePasswordWork(ctx, passwordMemoryEnvelope)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := acquirePasswordWork(ctx, passwordMemoryEnvelope); !errors.Is(err, ErrPasswordBusy) {
		t.Fatalf("second legacy: %v", err)
	}

	if _, err := HashPassword(ctx, "a long password-manager passphrase"); !errors.Is(err, ErrPasswordBusy) {
		t.Fatalf("modern alongside legacy: %v", err)
	}

	legacy()

	first, err := acquirePasswordWork(ctx, passwordMemory)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := acquirePasswordWork(ctx, passwordMemoryEnvelope); !errors.Is(err, ErrPasswordBusy) {
		t.Fatalf("legacy alongside modern: %v", err)
	}

	second, err := acquirePasswordWork(ctx, passwordMemory)
	if err != nil {
		t.Fatal(err)
	}

	first()
	second()

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := acquirePasswordWork(canceled, passwordMemoryEnvelope); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}

	release, err := acquirePasswordWork(ctx, passwordMemoryEnvelope)
	if err != nil {
		t.Fatal(err)
	}

	release()
	passwordWork.Lock()
	defer passwordWork.Unlock()

	if passwordWork.memory != 0 || passwordWork.running != 0 {
		t.Fatalf("retained reservation: %d/%d", passwordWork.memory, passwordWork.running)
	}
}

func TestPasswordWeightedConcurrentCapacity(t *testing.T) {
	start, release := make(chan struct{}), make(chan struct{})
	admitted, done := make(chan struct{}, 20), make(chan struct{}, 20)

	for i := range 20 {
		go func() {
			<-start

			memory := []uint32{256 * 1024, 192 * 1024, 128 * 1024, 64 * 1024}[i%4]

			free, err := acquirePasswordWork(t.Context(), memory)
			if err == nil {
				passwordWork.Lock()
				if passwordWork.memory > passwordMemoryEnvelope || passwordWork.running > passwordConcurrency {
					t.Errorf("over reservation: %d/%d", passwordWork.memory, passwordWork.running)
				}
				passwordWork.Unlock()
			} else if !errors.Is(err, ErrPasswordBusy) {
				t.Errorf("admission: %v", err)
			}

			admitted <- struct{}{}

			if err == nil {
				<-release
				free()
			}

			done <- struct{}{}
		}()
	}

	close(start)

	for range 20 {
		<-admitted
	}

	close(release)

	for range 20 {
		<-done
	}

	passwordWork.Lock()
	defer passwordWork.Unlock()

	if passwordWork.running != 0 || passwordWork.memory != 0 {
		t.Fatalf("retained reservation: %d/%d", passwordWork.memory, passwordWork.running)
	}
}
