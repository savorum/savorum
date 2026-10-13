package memoryspace

import "os"

// Inspect reports physical memory and applicable controller headroom.
func Inspect() (Space, error) {
	return inspectLinux(os.ReadFile)
}
