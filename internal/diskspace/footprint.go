package diskspace

import "math"

// Footprint counts allocated database bytes and all stored original assets.
type Footprint struct {
	DatabaseBytes uint64
	OriginalBytes uint64
	OriginalFiles uint64
}

// BackupRequirements includes an uncompressed archive and a simultaneous
// coherent database snapshot. Arithmetic saturates instead of wrapping.
func (f Footprint) BackupRequirements() (archive, workspace uint64) {
	payload := saturatedSum(f.DatabaseBytes, f.OriginalBytes)

	metadata := uint64(math.MaxUint64)
	if f.OriginalFiles <= math.MaxUint64/1024 {
		metadata = f.OriginalFiles * 1024
	}

	archive = saturatedSum(payload, payload/100, 8<<20, metadata)

	return archive, saturatedSum(f.DatabaseBytes, archive)
}

// Headroom preserves the filesystem reserve alongside maintenance workspace.
func (f Footprint) Headroom() (bytes, inodes uint64) {
	_, workspace := f.BackupRequirements()
	return saturatedSum(workspace, ReserveBytes), saturatedSum(f.OriginalFiles, 8, ReserveInodes)
}

func saturatedSum(values ...uint64) uint64 {
	var sum uint64
	for _, value := range values {
		if value > math.MaxUint64-sum {
			return math.MaxUint64
		}

		sum += value
	}

	return sum
}
