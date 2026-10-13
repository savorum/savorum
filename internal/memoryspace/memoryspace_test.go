package memoryspace

import (
	"errors"
	"sync"
	"testing"
)

func TestReservationsRefuseUnknownOverflowAnd512MiB(t *testing.T) {
	for _, test := range []struct {
		name         string
		space        Space
		bytes, floor uint64
		err          error
	}{
		{"512MiB", Space{512 << 20, 512 << 20}, 272 << 20, 256 << 20, nil},
		{"unknown", Space{}, 1, 0, nil},
		{"probe error", Space{}, 1, 0, errors.New("unavailable")},
		{"overflow", Space{^uint64(0), ^uint64(0)}, ^uint64(0), 1, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := NewManager()

			m.inspect = func() (Space, error) { return test.space, test.err }
			if _, err := m.Reserve(test.bytes, test.floor); !errors.Is(err, ErrResourceMemory) {
				t.Fatalf("expected resource memory refusal: %v", err)
			}

			if m.reserved != 0 {
				t.Fatal("refusal reserved memory")
			}
		})
	}
}

func TestConcurrentReservationsAndIdempotentRelease(t *testing.T) {
	m := NewManager()
	m.inspect = func() (Space, error) { return Space{1024, 1024}, nil }

	release, err := m.Reserve(600, 100)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := m.Reserve(400, 100); !errors.Is(err, ErrResourceMemory) {
				t.Errorf("concurrent reservation: %v", err)
			}
		})
	}

	wg.Wait()
	release()
	release()

	next, err := m.Reserve(900, 100)
	if err != nil {
		t.Fatal(err)
	}

	next()
}
