package compiler

import (
	"math"
	"runtime/debug"
	"sync"

	"github.com/tetratelabs/wazero/experimental"
	"golang.org/x/sys/unix"
)

// mappedMemory reserves a guest's whole linear memory outside the Go heap.
// The kernel commits pages only when the guest touches them, growth never
// copies, and closing the module returns every page at once.
//
// The mapping is released when wazero closes the module, so no slice of it
// may be used after Close.
type mappedMemory struct{ buffer []byte }

// heapMemory backs a guest when the address space cannot be reserved.
type heapMemory struct {
	buffer  []byte
	maximum uint64
}

func allocateMemory(_, maximum uint64) experimental.LinearMemory {
	buffer, err := unix.Mmap(-1, 0, int(maximum), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON|unix.MAP_NORESERVE) //nolint:gosec // the maximum is the compiler's wasm32 memory limit
	if err != nil {
		return &heapMemory{maximum: maximum}
	}

	return &mappedMemory{buffer: buffer[:0]}
}

func (m *mappedMemory) Reallocate(size uint64) []byte {
	if size > uint64(cap(m.buffer)) {
		return nil
	}

	goBudget.reserve(int64(size) - int64(len(m.buffer))) //nolint:gosec // both are below the wasm32 memory limit
	m.buffer = m.buffer[:size]

	return m.buffer
}

func (m *mappedMemory) Free() {
	goBudget.reserve(-int64(len(m.buffer)))

	if reserved := m.buffer[:cap(m.buffer)]; len(reserved) > 0 {
		_ = unix.Munmap(reserved)
	}

	m.buffer = nil
}

func (m *heapMemory) Reallocate(size uint64) []byte {
	if size > m.maximum {
		return nil
	}

	if size > uint64(cap(m.buffer)) {
		grown := make([]byte, size, min(max(size, 2*uint64(cap(m.buffer))), m.maximum))
		copy(grown, m.buffer)
		m.buffer = grown
	}

	m.buffer = m.buffer[:size]

	return m.buffer
}

func (m *heapMemory) Free() { m.buffer = nil }

// goBudget lowers the Go runtime's soft memory limit by the guest memory
// mapped outside the heap, so the heap and the guests together stay within
// the limit configured for the process. The limit is never lowered below a
// quarter of its configured value.
var goBudget memoryBudget

type memoryBudget struct {
	mutex      sync.Mutex
	configured int64
	reserved   int64
}

func (b *memoryBudget) reserve(delta int64) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.reserved == 0 {
		b.configured = debug.SetMemoryLimit(-1)
	}

	b.reserved += delta
	if b.configured == math.MaxInt64 {
		return
	}

	debug.SetMemoryLimit(max(b.configured-b.reserved, b.configured/4))
}
