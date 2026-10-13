package v1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/imaging"
)

// Derivatives are an in-memory, bounded convenience cache. They are not part
// of the asset store or backup: deleting them only costs a later regeneration.
const derivativeCacheBytes = 32 << 20

type derivativeCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	order   []string
	bytes   int
	pending map[string]*derivativeFlight
	slots   chan struct{}
}

var presentationImages = derivativeCache{entries: make(map[string][]byte)}

const derivativeVersion = "v4"

type derivativeFlight struct {
	done  chan struct{}
	value []byte
	err   error
}

func derivativeKey(id assets.ID, preset imaging.DerivativePreset) string {
	return string(id) + ":" + derivativeVersion + ":" + string(preset)
}

func (c *derivativeCache) getOrCreate(ctx context.Context, service AssetService, id assets.ID, preset imaging.DerivativePreset) ([]byte, error) {
	key := derivativeKey(id, preset)

	c.mu.Lock()
	if value := c.entries[key]; value != nil {
		c.mu.Unlock()
		return value, nil
	}

	if flight := c.pending[key]; flight != nil {
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-flight.done:
			if ctx.Err() == nil && (errors.Is(flight.err, context.Canceled) || errors.Is(flight.err, context.DeadlineExceeded)) {
				return c.getOrCreate(ctx, service, id, preset)
			}

			return flight.value, flight.err
		}
	}

	if c.pending == nil {
		c.pending = make(map[string]*derivativeFlight)
	}

	if c.slots == nil {
		c.slots = make(chan struct{}, 1)
	}

	flight := &derivativeFlight{done: make(chan struct{})}
	c.pending[key] = flight
	c.mu.Unlock()

	select {
	case c.slots <- struct{}{}:
		flight.value, flight.err = createDerivative(ctx, service, id, preset)

		<-c.slots
	case <-ctx.Done():
		flight.err = ctx.Err()
	}

	c.mu.Lock()
	if flight.err == nil && len(flight.value) <= derivativeCacheBytes {
		for c.bytes+len(flight.value) > derivativeCacheBytes && len(c.order) > 0 {
			old := c.order[0]
			c.order = c.order[1:]
			c.bytes -= len(c.entries[old])
			delete(c.entries, old)
		}

		if c.entries == nil {
			c.entries = make(map[string][]byte)
		}

		c.entries[key] = flight.value
		c.order = append(c.order, key)
		c.bytes += len(flight.value)
	}

	delete(c.pending, key)
	close(flight.done)
	c.mu.Unlock()

	return flight.value, flight.err
}

func createDerivative(ctx context.Context, service AssetService, id assets.ID, preset imaging.DerivativePreset) ([]byte, error) {
	blob, reader, err := service.Open(ctx, id)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	if blob.Size < 0 || blob.Size > 64<<20 {
		return nil, assets.ErrTooLarge
	}

	source, err := io.ReadAll(io.LimitReader(reader, blob.Size+1))
	if err != nil {
		return nil, fmt.Errorf("read derivative source: %w", err)
	}

	if int64(len(source)) > blob.Size {
		return nil, fmt.Errorf("%w: stored bytes exceed their recorded size", assets.ErrIntegrity)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	value, err := imaging.DeriveImage(source, preset)
	if err != nil {
		return nil, fmt.Errorf("derive presentation image: %w", err)
	}

	return value, nil
}
