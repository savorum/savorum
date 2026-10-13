package sqlite

import (
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/storagebudget"
)

// operationError adds the storage boundary while preserving typed failures and
// successful nil results from transaction completion and row iteration.
func operationError(operation string, err error) error {
	if err == nil {
		return nil
	}

	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&255 == 7 && !errors.Is(err, ErrMemoryCapacity) {
		err = fmt.Errorf("%w: %w", ErrMemoryCapacity, err)
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func isCapacityError(err error) bool {
	if errors.Is(err, storagebudget.ErrCapacity) || errors.Is(err, diskspace.ErrCapacity) {
		return true
	}

	var coded interface{ Code() int }

	return errors.As(err, &coded) && coded.Code()&255 == 13
}
