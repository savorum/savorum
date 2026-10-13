package backupstream

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/administration"
)

// IncrementalPath collects a coherent native tree through collection-scoped tokens.
const IncrementalPath = Path + "/incremental"

func (h *Handler) incrementalRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == IncrementalPath && r.Method == http.MethodPost {
		h.startIncremental(w, r)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, IncrementalPath+"/"), "/")
	if len(parts) == 1 && r.Method == http.MethodDelete {
		h.mu.Lock()
		defer h.mu.Unlock()

		if h.current != nil && h.current.id == parts[0] {
			h.discard()
		}

		w.WriteHeader(http.StatusNoContent)

		return
	}

	if r.Method != http.MethodGet || (len(parts) != 1 && (len(parts) != 3 || parts[1] != "assets")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}

	h.collectIncremental(w, r, parts)
}

func (h *Handler) startIncremental(w http.ResponseWriter, r *http.Request) {
	maxBytes, err := strconv.ParseInt(r.URL.Query().Get("max_bytes"), 10, 64)
	if err != nil || maxBytes < 1 || maxBytes == 1<<63-1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_bound"})
		return
	}

	timeout := h.retention()

	if text := r.URL.Query().Get("timeout"); text != "" {
		value, err := time.ParseDuration(text)
		if err != nil || value <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_timeout"})
			return
		}

		timeout = value
	}

	limits := administration.Limits{MaximumExpandedBytes: maxBytes}

	if text := r.URL.Query().Get("max_files"); text != "" {
		value, err := strconv.Atoi(text)
		if err != nil || value < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_files"})
			return
		}

		limits.MaximumFiles = value
	}

	if text := r.URL.Query().Get("max_manifest_bytes"); text != "" {
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil || value < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_manifest_budget"})
			return
		}

		limits.MaximumManifestBytes = value
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.cleanAbandoned(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "workspace_cleanup_failed"})
		return
	}

	h.expire()

	if h.current != nil && h.current.state == failed {
		h.discard()
	}

	if h.current != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "collection_busy"})
		return
	}

	directory, err := os.MkdirTemp(h.Workspace, ".savorum-hosting-backup-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "backup_failed"})
		return
	}

	ctx := h.Context
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	current := &job{id: rand.Text(), state: preparing, directory: directory, createdAt: h.now(), retention: timeout, cancel: cancel}

	h.current = current
	go h.prepareIncremental(ctx, current, maxBytes, limits)
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		defer h.mu.Unlock()

		if h.current == current {
			h.discard()
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"id": current.id, "state": string(preparing)})
}

// The serving process owns the instance lock before this handler is installed.
func (h *Handler) cleanAbandoned() error {
	if h.cleaned {
		return nil
	}

	entries, err := os.ReadDir(h.Workspace)
	if err != nil {
		return fmt.Errorf("inspect abandoned backup workspaces: %w", err)
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".savorum-hosting-backup-") {
			continue
		}

		name := filepath.Join(h.Workspace, entry.Name())
		if h.current != nil && name == h.current.directory {
			continue
		}

		if !entry.IsDir() {
			return errors.New("unsafe abandoned backup workspace")
		}

		if err := os.RemoveAll(name); err != nil {
			return fmt.Errorf("remove abandoned backup workspace: %w", err)
		}
	}

	h.cleaned = true

	return nil
}

func (h *Handler) prepareIncremental(ctx context.Context, current *job, maxBytes int64, budgets ...administration.Limits) {
	assets, err := administration.PrepareIncremental(ctx, h.Store, h.AssetRoot, h.WebhookDirectory, current.directory, current.createdAt, maxBytes, budgets...)
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.current != current {
		_ = os.RemoveAll(current.directory)
		return
	}

	if err != nil {
		h.Logger.Error("incremental preparation failed", "error", err)

		current.state = failed
		_ = os.RemoveAll(current.directory)

		return
	}

	current.assets = make(map[string]administration.IncrementalAsset, len(assets))
	for _, asset := range assets {
		current.assets[asset.SHA256] = asset
	}

	current.state, current.readyAt = ready, h.now()
}

func (h *Handler) collectIncremental(w http.ResponseWriter, r *http.Request, parts []string) {
	h.mu.Lock()
	h.expire()

	current := h.current
	if current == nil || current.id != parts[0] {
		h.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})

		return
	}

	if current.state != ready {
		progress := current.state

		status := http.StatusAccepted
		if progress == failed {
			status = http.StatusInternalServerError
		}
		h.mu.Unlock()
		writeJSON(w, status, map[string]string{"id": current.id, "state": string(progress)})

		return
	}

	if len(parts) == 3 {
		asset, ok := current.assets[parts[2]]
		if !ok {
			h.mu.Unlock()
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})

			return
		}

		file, err := os.Open(asset.Path)
		h.mu.Unlock()

		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "backup_failed"})
			return
		}

		defer file.Close()

		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != asset.Size {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "backup_failed"})
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(asset.Size, 10))
		w.Header().Set("X-Savorum-Collection", current.id)
		w.Header().Set("Cache-Control", "no-store")

		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		if count, err := io.CopyN(w, file, asset.Size); err != nil || count != asset.Size {
			panic(http.ErrAbortHandler)
		}

		return
	}

	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("X-Savorum-Collection", current.id)
	w.Header().Set("Cache-Control", "no-store")

	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	if err := administration.WriteIncrementalMetadata(r.Context(), w, current.directory, current.createdAt); err != nil {
		if !errors.Is(err, context.Canceled) {
			h.Logger.ErrorContext(r.Context(), "incremental collection interrupted", "error", err)
		}

		panic(http.ErrAbortHandler)
	}
}
