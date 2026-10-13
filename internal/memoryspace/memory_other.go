//go:build !linux && !darwin

package memoryspace

import "errors"

func Inspect() (Space, error) {
	return Space{}, errors.New("physical memory probe is unavailable on this platform")
}
