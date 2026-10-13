// Package archivepolicy contains resource controls shared by SREF package and
// bundle readers. It deliberately has no knowledge of either archive format.
package archivepolicy

import (
	"encoding/binary"
	"errors"
)

var (
	// ErrInvalid reports archive directory is malformed.
	ErrInvalid = errors.New("archive directory is malformed")
	// ErrLimit reports archive exceeds reader resource policy.
	ErrLimit = errors.New("archive exceeds reader resource policy")
)

// Budget is a monotonic operation-wide allocation budget. Charging is
// intentionally conservative: bytes are not refunded when a temporary value
// is released, so nested archives cannot reuse the same allowance repeatedly.
type Budget struct {
	parent    *Budget
	remaining int64
	limited   bool
}

// NewBudget creates a shared byte allowance; a nonpositive maximum is unlimited.
func NewBudget(maximum int64) *Budget { return &Budget{remaining: maximum, limited: maximum > 0} }

// NewChildBudget limits one source and charges its operation-wide parent.
func NewChildBudget(maximum int64, parent *Budget) *Budget {
	budget := NewBudget(maximum)
	budget.parent = parent

	return budget
}

// Spend charges bytes against the archive budget.
func (b *Budget) Spend(bytes int64) error {
	if b == nil || bytes <= 0 {
		return nil
	}

	if b.limited && bytes > b.remaining {
		return ErrLimit
	}

	if err := b.parent.Spend(bytes); err != nil {
		return err
	}

	if b.limited {
		b.remaining -= bytes
	}

	return nil
}

// Preflight reads only the fixed-size ZIP end records. archive/zip builds one
// File value per central-directory record; checking the declared count and
// directory size first prevents an attacker from causing proportional
// allocation before caller policy is applied. ZIP64 counts are supported.
func Preflight(data []byte, maximumEntries int, maximumDirectoryBytes int64) error {
	const (
		endSize        = 22
		maximumComment = 1<<16 - 1
		zip64Locator   = 20
		zip64EndMin    = 56
	)

	start := len(data) - endSize
	if start < 0 {
		return ErrInvalid
	}

	lower := max(start-maximumComment, 0)
	end := -1

	for index := start; index >= lower; index-- {
		if binary.LittleEndian.Uint32(data[index:index+4]) == 0x06054b50 {
			comment := int(binary.LittleEndian.Uint16(data[index+20 : index+22]))
			if index+endSize+comment == len(data) {
				end = index
				break
			}
		}
	}

	if end < 0 {
		return ErrInvalid
	}

	entries := uint64(binary.LittleEndian.Uint16(data[end+10 : end+12]))

	directoryBytes := uint64(binary.LittleEndian.Uint32(data[end+12 : end+16]))
	if entries == 0xffff || directoryBytes == 0xffffffff {
		if end < zip64Locator || binary.LittleEndian.Uint32(data[end-zip64Locator:end-zip64Locator+4]) != 0x07064b50 {
			return ErrInvalid
		}

		offset := binary.LittleEndian.Uint64(data[end-zip64Locator+8 : end-zip64Locator+16])
		//nolint:gosec // the length test to its left makes the subtraction non-negative
		if len(data) < zip64EndMin || offset > uint64(len(data)-zip64EndMin) || binary.LittleEndian.Uint32(data[offset:offset+4]) != 0x06064b50 {
			return ErrInvalid
		}

		entries = binary.LittleEndian.Uint64(data[offset+32 : offset+40])
		directoryBytes = binary.LittleEndian.Uint64(data[offset+40 : offset+48])
	}

	if maximumEntries > 0 && entries > uint64(maximumEntries) {
		return ErrLimit
	}

	if maximumDirectoryBytes > 0 && directoryBytes > uint64(maximumDirectoryBytes) {
		return ErrLimit
	}

	return nil
}
