//go:build !linux

package administration

import "errors"

// ProbeGeometry requires Linux block-device metadata.
func ProbeGeometry(mount string) (Geometry, error) {
	return Geometry{}, errors.New("selected block-device geometry requires Linux")
}
