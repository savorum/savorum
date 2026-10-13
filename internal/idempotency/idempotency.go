// Package idempotency coordinates bounded retry receipts. Persistence records a
// receipt in the same transaction as the created resource.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

var (
	// ErrConflict refuses reuse of a key for different input.
	ErrConflict = errors.New("idempotency key was used for a different request")
	// ErrInvalid rejects empty or oversized retry keys.
	ErrInvalid = errors.New("idempotency key must contain 1 to 200 characters")
	// ErrCapacity bounds retained retry receipts.
	ErrCapacity = errors.New("too many unexpired idempotency keys")
)

// ReplayError carries the identity found inside a creation transaction.
type ReplayError struct{ ID string }

func (*ReplayError) Error() string { return "creation already committed" }

// Receipt names a request and the durable resource created by it.
type (
	Receipt    struct{ Operation, Key, Digest string }
	contextKey struct{}
)

// FromContext is consumed only inside a resource-creation transaction.
func FromContext(ctx context.Context) (Receipt, bool) {
	r, ok := ctx.Value(contextKey{}).(Receipt)
	return r, ok
}

// Repository reads receipts within the current principal/library scope.
type Repository interface {
	FindIdempotency(context.Context, Receipt) (string, error)
}

// Coordinator uses fixed stripes so callers cannot grow a lock registry.
type Coordinator struct {
	locks      [64]sync.Mutex
	repository Repository
}

// New coordinates bounded retry receipts persisted with their original mutations.
func New(repository Repository) *Coordinator { return &Coordinator{repository: repository} }

// Begin serializes equal keys until release. Empty keys retain existing client
// behavior. A nonempty result is a replay; callers still authorize the resource.
func (c *Coordinator) Begin(ctx context.Context, operation, key string, body []byte) (context.Context, string, func(), error) {
	if key == "" {
		return ctx, "", func() {}, nil
	}

	if len(key) > 200 {
		return ctx, "", func() {}, ErrInvalid
	}

	digest := sha256.Sum256(body)
	hashed := sha256.Sum256([]byte(key))
	r := Receipt{Operation: operation, Key: hex.EncodeToString(hashed[:]), Digest: hex.EncodeToString(digest[:])}
	lock := &c.locks[int(hashed[0])%len(c.locks)]
	lock.Lock()

	id, err := c.repository.FindIdempotency(ctx, r)
	if err != nil {
		lock.Unlock()
		return ctx, "", func() {}, err
	}

	return context.WithValue(ctx, contextKey{}, r), id, lock.Unlock, nil
}
