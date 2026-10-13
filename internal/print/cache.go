package print

import (
	"bytes"
	"container/list"
	"fmt"
	"reflect"
	"sync"
)

// The in-memory cache lets preview, download, and print of one specification
// reuse one set of bytes. Nothing is persisted.
const (
	defaultCacheEntries = 24
	defaultCacheBytes   = 96 << 20
)

type artifactCache struct {
	mutex      sync.Mutex
	order      *list.List
	entries    map[string]*list.Element
	bytes      int
	maxEntries int
	maxBytes   int
}

type cacheEntry struct {
	key      string
	artifact Artifact
}

func newArtifactCache(maxEntries, maxBytes int) *artifactCache {
	if maxEntries <= 0 {
		maxEntries = defaultCacheEntries
	}

	if maxBytes <= 0 {
		maxBytes = defaultCacheBytes
	}

	return &artifactCache{
		order:      list.New(),
		entries:    make(map[string]*list.Element, maxEntries),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
	}
}

func (c *artifactCache) get(key, scope string) (Artifact, bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	element, found := c.entries[key]
	if !found {
		return Artifact{}, false, nil
	}

	entry := cacheEntryOf(element)

	if entry.key != key || entry.artifact.ID != key {
		return Artifact{}, false, fmt.Errorf("%w: held artifact does not match its key", ErrArtifactInvariant)
	}

	if entry.artifact.authorizationScope != scope {
		return Artifact{}, false, nil
	}

	c.order.MoveToFront(element)

	return cloneArtifact(entry.artifact), true, nil
}

func (c *artifactCache) put(key string, artifact Artifact) error {
	if artifact.ID != key || artifact.authorizationScope == "" {
		return fmt.Errorf("%w: generated artifact does not match its key or scope", ErrArtifactInvariant)
	}

	if len(artifact.PDF) > c.maxBytes {
		return nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if element, found := c.entries[key]; found {
		entry := cacheEntryOf(element)

		existing := entry.artifact
		if !sameArtifact(existing, artifact) {
			return fmt.Errorf("%w: one key produced different artifacts", ErrArtifactInvariant)
		}

		c.order.MoveToFront(element)

		return nil
	}

	artifact = cloneArtifact(artifact)
	element := c.order.PushFront(&cacheEntry{key: key, artifact: artifact})
	c.entries[key] = element

	c.bytes += len(artifact.PDF)
	for (len(c.entries) > c.maxEntries || c.bytes > c.maxBytes) && c.order.Len() > 1 {
		c.evictOldest()
	}

	return nil
}

func cloneArtifact(artifact Artifact) Artifact {
	artifact.PDF = bytes.Clone(artifact.PDF)
	artifact.Warnings = append([]string(nil), artifact.Warnings...)
	artifact.Recipes = append([]ArtifactRecipe(nil), artifact.Recipes...)

	return artifact
}

func sameArtifact(left, right Artifact) bool {
	if !bytes.Equal(left.PDF, right.PDF) {
		return false
	}

	left.PDF, right.PDF = nil, nil

	return reflect.DeepEqual(left, right)
}

func (c *artifactCache) evictOldest() {
	oldest := c.order.Back()
	if oldest == nil {
		return
	}

	entry := cacheEntryOf(oldest)
	delete(c.entries, entry.key)
	c.order.Remove(oldest)
	c.bytes -= len(entry.artifact.PDF)
}

func cacheEntryOf(element *list.Element) *cacheEntry {
	entry, ok := element.Value.(*cacheEntry)
	if !ok {
		panic("artifact cache list contains a value other than *cacheEntry")
	}

	return entry
}
