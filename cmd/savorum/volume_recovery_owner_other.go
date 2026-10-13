//go:build !unix

package main

import (
	"errors"
	"os"
)

func fileOwner(_ os.FileInfo) (int, int, bool) {
	return 0, 0, false
}

func setVolumeOwner(_, _ int) error {
	return errors.New("changing the process owner is unsupported on this platform")
}
