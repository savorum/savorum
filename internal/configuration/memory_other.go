//go:build !linux && !darwin

package configuration

func physicalMemory() uint64 { return 0 }
