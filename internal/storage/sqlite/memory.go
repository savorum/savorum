package sqlite

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/savorum/savorum/internal/memoryspace"
	"modernc.org/libc"
	"modernc.org/libc/sys/types"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrMemoryCapacity is a recoverable refusal by the process SQLite allocator.
var ErrMemoryCapacity = memoryspace.ErrResourceMemory

type allocatorState struct {
	sync.Mutex
	accountingError error
	limit           int64
}

func enableMemoryAccounting() error {
	tls := libc.NewTLS()
	defer tls.Close()

	args := libc.Xmalloc(tls, types.Size_t(unsafe.Sizeof(uintptr(0))))
	if args == 0 {
		return errors.New("allocate SQLite memory configuration")
	}
	defer libc.Xfree(tls, args)

	if code := sqlite3.Xsqlite3_config(tls, sqlite3.SQLITE_CONFIG_MEMSTATUS, libc.VaList(args, int32(1))); code != sqlite3.SQLITE_OK {
		return fmt.Errorf("configure SQLite memory accounting before initialization: code %d", code)
	}

	return nil
}

// Memory accounting must be configured before any connection initializes SQLite.
var allocator = allocatorState{accountingError: enableMemoryAccounting()}

// ConfigureMemoryBudget installs one process-lifetime allocator limit for
// all SQLite connections. Configure it before instance recovery begins.
func ConfigureMemoryBudget(bytes int64) error {
	allocator.Lock()
	defer allocator.Unlock()

	if bytes <= 0 {
		return errors.New("SQLite memory budget must be positive")
	}

	if allocator.accountingError != nil {
		return allocator.accountingError
	}

	if allocator.limit != 0 {
		if allocator.limit != bytes {
			return fmt.Errorf("SQLite process memory budget already configured: %d", allocator.limit)
		}

		return nil
	}

	tls := libc.NewTLS()
	defer tls.Close()

	if code := sqlite3.Xsqlite3_initialize(tls); code != sqlite3.SQLITE_OK {
		return fmt.Errorf("initialize SQLite allocator: code %d", code)
	}

	sqlite3.Xsqlite3_hard_heap_limit64(tls, bytes)

	if actual := sqlite3.Xsqlite3_hard_heap_limit64(tls, -1); actual != bytes {
		return fmt.Errorf("SQLite allocator limit %d differs from budget %d", actual, bytes)
	}

	allocator.limit = bytes

	return nil
}
