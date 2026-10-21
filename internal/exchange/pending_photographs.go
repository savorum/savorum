package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// pendingImagesExtension holds the photographs an import did not store.
const pendingImagesExtension = "x-org.savorum.pending-images"

// PendingPhotographBatch is how many recipes share one round's remote allowance.
const PendingPhotographBatch = 25

// PendingPhotographLibrary stores photographs fetched after an import.
type PendingPhotographLibrary interface {
	PendingPhotographs(ctx context.Context, after library.EntryID, limit int) ([]library.PendingPhotographs, error)
	CompletePendingPhotographs(ctx context.Context, id library.EntryID, expectedRevision uint64, document recipe.Document) (library.Entry, error)
}

// pendingPhotographs fetches deferred photographs after an import and once at
// startup. It never runs on a timer, so an idle server can stand by.
type pendingPhotographs struct {
	//nolint:containedctx // the service lifetime
	lifetime context.Context
	stop     context.CancelFunc
	mu       sync.Mutex
	queue    []library.PendingPhotographs
	queued   map[library.PendingPhotographs]bool
	running  bool
	done     sync.WaitGroup
}

// FetchingPhotographs reports whether deferred photographs are being fetched.
func (s *Service) FetchingPhotographs() bool {
	s.pending.mu.Lock()
	defer s.pending.mu.Unlock()

	return s.pending.running
}

// FetchPendingPhotographs fetches every library's deferred photographs in the
// background.
func (s *Service) FetchPendingPhotographs(ctx context.Context) error {
	pending, ok := s.library.(PendingPhotographLibrary)
	if !ok {
		return nil
	}

	var after library.EntryID

	for {
		listed, err := pending.PendingPhotographs(ctx, after, PendingPhotographBatch)
		if err != nil {
			return err
		}

		s.queuePendingPhotographs(listed)

		if len(listed) < PendingPhotographBatch {
			return nil
		}

		after = listed[len(listed)-1].EntryID
	}
}

// Close stops fetching deferred photographs and waits for the current round.
func (s *Service) Close() {
	s.pending.stop()
	s.pending.done.Wait()
}

func (s *Service) queuePendingPhotographs(targets []library.PendingPhotographs) {
	s.pending.mu.Lock()
	defer s.pending.mu.Unlock()

	if s.pending.lifetime.Err() != nil {
		return
	}

	for _, target := range targets {
		if !s.pending.queued[target] {
			s.pending.queued[target] = true
			s.pending.queue = append(s.pending.queue, target)
		}
	}

	if s.pending.running || len(s.pending.queue) == 0 {
		return
	}

	s.pending.running = true
	s.pending.done.Add(1)

	go s.drainPendingPhotographs()
}

// drainPendingPhotographs works through the queue a round at a time. A recipe
// that fetched nothing waits for the next start.
func (s *Service) drainPendingPhotographs() {
	defer s.pending.done.Done()

	for {
		s.pending.mu.Lock()

		count := min(len(s.pending.queue), PendingPhotographBatch)
		if count == 0 || s.pending.lifetime.Err() != nil {
			s.pending.running = false
			s.pending.mu.Unlock()

			return
		}

		round := append([]library.PendingPhotographs(nil), s.pending.queue[:count]...)
		s.pending.queue = s.pending.queue[count:]

		for _, target := range round {
			delete(s.pending.queued, target)
		}

		s.pending.mu.Unlock()

		again, err := s.fetchPendingPhotographs(s.pending.lifetime, round)
		if err != nil && s.pending.lifetime.Err() == nil {
			slog.Warn("fetching deferred photographs failed; they stay deferred", "error", err)
		}

		s.pending.mu.Lock()
		for _, target := range again {
			if !s.pending.queued[target] {
				s.pending.queued[target] = true
				s.pending.queue = append(s.pending.queue, target)
			}
		}
		s.pending.mu.Unlock()
	}
}

type pendingCandidate struct {
	target     library.PendingPhotographs
	entry      library.Entry
	unresolved []importer.RemoteAsset
}

// fetchPendingPhotographs fetches one round and returns the recipes that made
// progress but still have photographs deferred. A recipe edited during the
// round is left deferred.
func (s *Service) fetchPendingPhotographs(ctx context.Context, targets []library.PendingPhotographs) ([]library.PendingPhotographs, error) {
	pending, ok := s.library.(PendingPhotographLibrary)
	if !ok {
		return nil, nil
	}

	candidates := make([]pendingCandidate, 0, len(targets))
	items := make([]ImportItem, 0, len(targets))
	documents := make([]recipe.Document, 0, len(targets))

	for _, target := range targets {
		scope := access.WithLibrary(ctx, target.LibraryID)

		entry, err := s.library.Get(scope, target.EntryID)
		if errors.Is(err, library.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("read recipe %s with deferred photographs: %w", target.EntryID, err)
		}

		deferred, unresolved := pendingRemoteAssets(entry.Recipe)
		if len(deferred) == 0 {
			continue
		}

		document := entry.Recipe
		document.Extensions = maps.Clone(entry.Recipe.Extensions)
		document.Assets = append([]recipe.Asset(nil), entry.Recipe.Assets...)
		document.ImageRefs = append([]string(nil), entry.Recipe.ImageRefs...)
		nameUnnamedAssets(&document, deferred)

		candidates = append(candidates, pendingCandidate{target: target, entry: entry, unresolved: unresolved})
		items = append(items, ImportItem{RemoteAssets: deferred})
		documents = append(documents, document)
	}

	if len(items) == 0 {
		return nil, nil
	}

	decisions := make([]bool, len(items))
	for index := range decisions {
		decisions[index] = true
	}

	outcomes := s.stageDirectRemoteAssets(ctx, items, decisions)
	_, deferred, unresolved := s.materializeRemoteAssets(ctx, documents, items, decisions, outcomes)

	var again []library.PendingPhotographs

	for index, candidate := range candidates {
		if len(deferred[index]) == len(items[index].RemoteAssets) {
			continue
		}

		document := documents[index]
		delete(document.Extensions, pendingImagesExtension)
		retainRemoteReferences(&document, deferred[index], append(candidate.unresolved, unresolved[index]...))

		if len(document.Extensions) == 0 {
			document.Extensions = nil
		}

		_, err := pending.CompletePendingPhotographs(access.WithLibrary(ctx, candidate.target.LibraryID), candidate.entry.ID, candidate.entry.Revision, document)
		if _, stale := errors.AsType[*library.PreconditionError](err); stale {
			continue
		}

		if err != nil {
			return again, fmt.Errorf("store fetched photographs for recipe %s: %w", candidate.entry.ID, err)
		}

		if len(deferred[index]) > 0 {
			again = append(again, candidate.target)
		}
	}

	return again, nil
}

// pendingRemoteAssets reads back an import's deferred and unresolved photographs.
func pendingRemoteAssets(document recipe.Document) (deferred, unresolved []importer.RemoteAsset) {
	retained, ok := document.Extensions[pendingImagesExtension].(map[string]any)
	if !ok {
		return nil, nil
	}

	return remoteReferencesFrom(retained["deferred"]), remoteReferencesFrom(retained["unresolved"])
}

func remoteReferencesFrom(value any) []importer.RemoteAsset {
	entries, _ := value.([]any)
	remotes := make([]importer.RemoteAsset, 0, len(entries))

	for _, value := range entries {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}

		text := func(key string) string { value, _ := entry[key].(string); return value }

		remote := importer.RemoteAsset{
			URL: text("url"), LogicalID: text("logical_id"), Role: recipe.AssetRole(text("role")),
			Alt: text("alt"), Caption: text("caption"),
			Creator: text("creator"), Credit: text("credit"), Rights: text("rights"),
		}
		if remote.URL == "" {
			continue
		}

		remote.RecipeImage, _ = entry["recipe_image"].(bool)

		if license, ok := entry["license"].(map[string]any); ok {
			name, _ := license["name"].(string)
			url, _ := license["url"].(string)
			remote.License = &recipe.AssetLicense{Name: name, URL: url}
		}

		remotes = append(remotes, remote)
	}

	return remotes
}

// nameUnnamedAssets gives each unnamed deferred photograph an unused ID.
func nameUnnamedAssets(document *recipe.Document, remotes []importer.RemoteAsset) {
	taken := map[string]bool{}
	for _, asset := range document.Assets {
		taken[asset.ID] = true
	}

	for index := range remotes {
		if remotes[index].LogicalID != "" {
			taken[remotes[index].LogicalID] = true
		}
	}

	next := 1

	for index := range remotes {
		if remotes[index].LogicalID != "" {
			continue
		}

		for taken[fmt.Sprintf("imported-image-%d", next)] {
			next++
		}

		remotes[index].LogicalID = fmt.Sprintf("imported-image-%d", next)
		taken[remotes[index].LogicalID] = true
	}
}
