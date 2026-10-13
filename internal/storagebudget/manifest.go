package storagebudget

import (
	"encoding/json"
	"fmt"
)

// Native recovery bounds retain a bounded manifest and archive directory.
const (
	NativeManifestHeader int64 = 64 << 10
)

// NativeAssetRecord matches the indented native-backup manifest asset encoding.
type NativeAssetRecord struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
}

// NativeAssetEncodedBytes includes indentation and the array member delimiter.
func NativeAssetEncodedBytes(record NativeAssetRecord) (int64, error) {
	data, err := json.MarshalIndent(record, "    ", "  ")
	if err != nil {
		return 0, fmt.Errorf("encode native asset metadata: %w", err)
	}

	return int64(len(data)) + 6, nil
}

// SealedWebhookKeySize bounds an individual sealed envelope key.
const (
	SealedWebhookKeySize = 4 << 10
)
