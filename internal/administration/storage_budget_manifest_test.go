package administration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/storagebudget"
)

func TestHostedManifestAdmissionCoversNativeEncoding(t *testing.T) {
	content := manifest{Format: Format, Version: Version, SavorumVersion: "test", DatabaseSchemaVersion: 99, CreatedAt: time.Unix(1, 0).UTC(), Database: fileRecord{Path: "savorum.db", SHA256: strings.Repeat("a", 64), Size: 1 << 30}, Assets: []assetRecord{}}
	bound := storagebudget.NativeManifestHeader

	for _, media := range []string{"image/jpeg", "application/vnd.escaped<&>\"\\\n", strings.Repeat("x", 4096)} {
		asset := assetRecord{ID: "sha256:" + strings.Repeat("b", 64), Path: "assets/sha256/bb/" + strings.Repeat("b", 64), SHA256: strings.Repeat("b", 64), Size: 12345, MediaType: media}
		record := storagebudget.NativeAssetRecord{ID: asset.ID, Path: asset.Path, SHA256: asset.SHA256, Size: asset.Size, MediaType: asset.MediaType}

		native, err := json.Marshal(asset)
		if err != nil {
			t.Fatal(err)
		}

		admitted, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}

		if string(native) != string(admitted) {
			t.Fatal("hosted admission and native asset metadata differ")
		}

		bytes, err := storagebudget.NativeAssetEncodedBytes(record)
		if err != nil {
			t.Fatal(err)
		}

		bound += bytes

		content.Assets = append(content.Assets, asset)
	}

	encoded, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if int64(len(encoded)+1) > bound {
		t.Fatalf("native manifest %d exceeds admission %d", len(encoded)+1, bound)
	}

	if storagebudget.NativeManifestBytes() <= 0 || storagebudget.NativeFiles() <= 0 {
		t.Fatal("hosted admission differs from native format safety bounds")
	}
}
