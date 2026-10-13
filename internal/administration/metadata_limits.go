package administration

import "github.com/savorum/savorum/internal/storagebudget"

func (l Limits) files() int {
	if l.MaximumFiles > 0 {
		return l.MaximumFiles
	}

	return storagebudget.NativeFiles()
}

func (l Limits) manifestBytes() int64 {
	if l.MaximumManifestBytes > 0 {
		return l.MaximumManifestBytes
	}

	return storagebudget.NativeManifestBytes()
}

func (l Limits) directoryBytes() int64 {
	if l.MaximumDirectoryBytes > 0 {
		return l.MaximumDirectoryBytes
	}

	return int64(l.files()) * 1024
}
