package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
)

func TestVolumeRecoveryCommandAcceptsBoundedStdinManifestBeyondArgvLimit(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256(nil)

	request := administration.VolumeRequest{Operation: "recovery_a", Tenant: "hh_a", Backup: "backup_a", Image: "registry.invalid/core@sha256:" + strings.Repeat("a", 64), MaximumBytes: 1 << 20, Expected: administration.StateReport{TenantID: "hh_a", InstanceID: "instance", OwnerUserID: "owner", LibraryID: "library", Tables: map[string]administration.TableState{"users": {}}}, Files: map[string]administration.VolumeFile{"instance.savorum-backup": {SHA256: hex.EncodeToString(hash[:])}}}
	for i := range 1400 {
		request.Files[fmt.Sprintf("webhook-secrets/key_%d", i)] = administration.VolumeFile{SHA256: hex.EncodeToString(hash[:])}
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) < 96<<10 {
		t.Fatal("fixture does not exceed base64 argv limit")
	}

	command := newVolumeRecoveryCommand()
	command.SetArgs([]string{"--volume", root, "--action", "status", "--request", "-"})
	command.SetIn(bytes.NewReader(data))

	var output bytes.Buffer
	command.SetOut(&output)

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	var receipt administration.VolumeReceipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt.Operation != request.Operation {
		t.Fatalf("stdin manifest receipt: %v", err)
	}
}

func TestVolumeRecoveryActsAsTheVolumeOwnerOnlyAsRoot(t *testing.T) {
	root := t.TempDir()

	var calls [][2]int

	setter := func(uid, gid int) error {
		calls = append(calls, [2]int{uid, gid})
		return nil
	}

	if err := actAsVolumeOwner(root, 1000, setter); err != nil || len(calls) != 0 {
		t.Fatal("a non-root process acted as the volume owner", err, calls)
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}

	uid, gid, ok := fileOwner(info)
	if !ok {
		t.Skip("the volume owner is unavailable on this platform")
	}

	if err := actAsVolumeOwner(root, 0, setter); err != nil {
		t.Fatal(err)
	}

	if uid == 0 {
		if len(calls) != 0 {
			t.Fatal("a root-owned volume switched users", calls)
		}
	} else if len(calls) != 1 || calls[0] != [2]int{uid, gid} {
		t.Fatalf("owner switches = %v, want one (%d, %d)", calls, uid, gid)
	}

	failing := func(_, _ int) error { return errors.New("denied") }
	if err := actAsVolumeOwner(root, 0, failing); uid != 0 && err == nil {
		t.Fatal("a failing owner switch was accepted")
	}

	if err := actAsVolumeOwner(filepath.Join(root, "missing"), 0, setter); err == nil {
		t.Fatal("a missing volume was accepted")
	}
}

func TestVolumeRecoveryHoldWaitsForCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	command := newVolumeRecoveryCommand()
	command.SetArgs([]string{"--volume", t.TempDir(), "--action", "hold"})
	command.SetContext(ctx)

	finished := make(chan error, 1)
	go func() { finished <- command.Execute() }()

	select {
	case err := <-finished:
		t.Fatalf("hold returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("hold ignores cancellation")
	}
}
