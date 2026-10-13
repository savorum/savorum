package compiler

import (
	"runtime/debug"
	"testing"
)

func TestGuestMemoryGrowsInPlaceAndReturnsItsShareOfTheGoLimit(t *testing.T) {
	const limit = 1 << 30

	previous := debug.SetMemoryLimit(limit)

	t.Cleanup(func() { debug.SetMemoryLimit(previous) })

	memory := allocateMemory(0, 16*MemoryPageBytes)
	if _, mapped := memory.(*mappedMemory); !mapped {
		t.Fatalf("guest memory is %T, want a mapping outside the Go heap", memory)
	}

	first := memory.Reallocate(MemoryPageBytes)
	first[0] = 7

	grown := memory.Reallocate(4 * MemoryPageBytes)
	if &grown[0] != &first[0] || grown[0] != 7 || len(grown) != 4*MemoryPageBytes {
		t.Fatal("growing moved or lost the guest's memory")
	}

	for _, value := range grown[MemoryPageBytes:] {
		if value != 0 {
			t.Fatal("grown guest memory is not zeroed")
		}
	}

	if got := debug.SetMemoryLimit(-1); got != limit-4*MemoryPageBytes {
		t.Fatalf("Go limit with a guest mapped = %d, want %d", got, limit-4*MemoryPageBytes)
	}

	if memory.Reallocate(17*MemoryPageBytes) != nil {
		t.Fatal("guest memory grew past its maximum")
	}

	memory.Free()

	if got := debug.SetMemoryLimit(-1); got != limit {
		t.Fatalf("Go limit after the guest closed = %d, want %d", got, limit)
	}
}
