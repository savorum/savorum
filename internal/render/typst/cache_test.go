package typst

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTheCompilerCacheMustBePrivateToThisUser(t *testing.T) {
	root := t.TempDir()

	created := filepath.Join(root, "savorum", "typst")
	if _, err := privateDirectory(created); err != nil {
		t.Fatalf("a new cache directory was refused: %v", err)
	}

	info, err := os.Stat(created)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory mode = %v; want 0700", info.Mode().Perm())
	}

	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(shared, 0o770); err != nil { //nolint:gosec // the test needs a directory other users can write
		t.Fatal(err)
	}

	if _, err := privateDirectory(shared); err == nil {
		t.Fatal("a group-writable cache directory was accepted")
	}

	if _, err := privateDirectory(filepath.Join(shared, "typst")); err == nil {
		t.Fatal("a cache directory inside a group-writable one was accepted")
	}

	linked := filepath.Join(root, "linked")
	if err := os.Symlink(created, linked); err != nil {
		t.Fatal(err)
	}

	if _, err := privateDirectory(linked); err == nil {
		t.Fatal("a symbolic link was accepted as the cache directory")
	}
}

func TestTheCompilerCacheRejectsWritableHigherAncestors(t *testing.T) {
	for _, mode := range []os.FileMode{0o770, 0o777} {
		t.Run(mode.String(), func(t *testing.T) {
			shared := filepath.Join(t.TempDir(), "shared")

			cache := filepath.Join(shared, "private", "savorum", "typst")
			if err := os.MkdirAll(cache, 0o700); err != nil {
				t.Fatal(err)
			}

			if err := os.Chmod(shared, mode); err != nil {
				t.Fatal(err)
			}

			if _, err := privateDirectory(cache); err == nil {
				t.Fatal("a writable higher ancestor was accepted")
			}

			renderer := NewCached(cache)

			t.Cleanup(func() { _ = renderer.Close() })

			if renderer.cache != "" {
				t.Fatal("an unsafe path enabled the machine-code disk cache")
			}
		})
	}
}

func TestTheCompilerCacheDoesNotCreateBeneathUnsafeAncestors(t *testing.T) {
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o777); err != nil { //nolint:gosec // the fixture needs an unsafe ancestor
		t.Fatal(err)
	}

	cache := filepath.Join(shared, "private", "savorum", "typst")
	if _, err := privateDirectory(cache); err == nil {
		t.Fatal("an unsafe ancestor was accepted")
	}

	if _, err := os.Lstat(filepath.Join(shared, "private")); !os.IsNotExist(err) {
		t.Fatalf("directories were created beneath an unsafe ancestor: %v", err)
	}
}

func TestTheCompilerCacheAcceptsPrivateDirectoriesBeneathStickyAncestors(t *testing.T) {
	shared := t.TempDir()
	if err := os.Chmod(shared, os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}

	for _, suffix := range []string{"typst", filepath.Join("home", ".cache", "savorum", "typst")} {
		cache := filepath.Join(shared, suffix)
		if _, err := privateDirectory(cache); err != nil {
			t.Fatalf("a private cache beneath a sticky ancestor was refused: %v", err)
		}
	}

	if _, err := privateDirectory(shared); err == nil {
		t.Fatal("a shared sticky directory was accepted as the cache itself")
	}
}

func TestTheCompilerCacheChecksSymlinkAncestorsAndTheirTargets(t *testing.T) {
	root := t.TempDir()

	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}

	alias := filepath.Join(root, "home")
	if err := os.Symlink("private", alias); err != nil {
		t.Fatal(err)
	}

	renderer := NewCached(filepath.Join(alias, "savorum", "typst"))

	t.Cleanup(func() { _ = renderer.Close() })

	want, err := filepath.EvalSymlinks(filepath.Join(private, "savorum", "typst"))
	if err != nil {
		t.Fatal(err)
	}

	if renderer.cache != want {
		t.Fatalf("cache = %q, want resolved private path %q", renderer.cache, want)
	}

	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}

	unsafeTarget := filepath.Join(shared, "private")
	if err := os.Mkdir(unsafeTarget, 0o700); err != nil {
		t.Fatal(err)
	}

	unsafeAlias := filepath.Join(root, "unsafe-target")
	if err := os.Symlink(unsafeTarget, unsafeAlias); err != nil {
		t.Fatal(err)
	}

	unsafeParentAlias := filepath.Join(shared, "safe-target")
	if err := os.Symlink(private, unsafeParentAlias); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(shared, 0o777); err != nil { //nolint:gosec // the fixture needs an unsafe symlink path and target
		t.Fatal(err)
	}

	for _, path := range []string{unsafeAlias, unsafeParentAlias} {
		if _, err := privateDirectory(filepath.Join(path, "savorum", "typst")); err == nil {
			t.Fatalf("unsafe symbolic-link ancestry was accepted: %s", path)
		}
	}

	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}

	if _, err := privateDirectory(filepath.Join(loop, "typst")); err == nil {
		t.Fatal("a symbolic-link loop was accepted")
	}
}

func TestTheCompilerCacheAcceptsTheSystemTemporaryPath(t *testing.T) {
	roots := []string{os.TempDir()}
	if os.TempDir() != "/tmp" {
		roots = append(roots, "/tmp")
	}

	for _, root := range roots {
		t.Run(root, func(t *testing.T) {
			// macOS temporary paths traverse protected /tmp and /var aliases.
			cache, err := os.MkdirTemp(root, "savorum-cache-*") //nolint:usetesting // exercise a direct child of each system temporary directory
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = os.RemoveAll(cache) })

			resolved, err := privateDirectory(cache)
			if err != nil {
				t.Fatal(err)
			}

			want, err := filepath.EvalSymlinks(cache)
			if err != nil {
				t.Fatal(err)
			}

			if resolved != want {
				t.Fatalf("cache = %q, want %q", resolved, want)
			}
		})
	}
}

type cacheEntryInfo struct {
	os.FileInfo
	owner *syscall.Stat_t
	mode  os.FileMode
}

func (info cacheEntryInfo) Sys() any          { return info.owner }
func (info cacheEntryInfo) Mode() os.FileMode { return info.mode }
func (info cacheEntryInfo) IsDir() bool       { return info.mode.IsDir() }

func TestTheCompilerCacheRequiresTrustedOwnership(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	current := uint32(os.Geteuid()) //nolint:gosec // effective user IDs fit the operating system's uid_t
	foreign := current + 1

	for name, test := range map[string]struct {
		uid   uint32
		mode  os.FileMode
		leaf  bool
		valid bool
	}{
		"current ancestor":        {current, os.ModeDir | 0o755, false, true},
		"root ancestor":           {0, os.ModeDir | 0o755, false, true},
		"foreign ancestor":        {foreign, os.ModeDir | 0o700, false, false},
		"current cache":           {current, os.ModeDir | 0o700, true, true},
		"foreign cache":           {foreign, os.ModeDir | 0o700, true, false},
		"root cache":              {0, os.ModeDir | 0o700, true, current == 0},
		"root sticky ancestor":    {0, os.ModeDir | os.ModeSticky | 0o777, false, true},
		"foreign sticky ancestor": {foreign, os.ModeDir | os.ModeSticky | 0o777, false, false},
		"foreign child of sticky": {foreign, os.ModeDir | 0o755, false, false},
		"current alias":           {current, os.ModeSymlink | 0o777, false, true},
		"root alias":              {0, os.ModeSymlink | 0o777, false, true},
		"foreign alias":           {foreign, os.ModeSymlink | 0o777, false, false},
		"cache alias":             {current, os.ModeSymlink | 0o777, true, false},
		"regular file":            {current, 0o600, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			entry := cacheEntryInfo{FileInfo: info, owner: &syscall.Stat_t{Uid: test.uid}, mode: test.mode}
			if err := validateCacheEntry(entry, test.leaf); (err == nil) != test.valid {
				t.Fatalf("valid = %v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}

	if err := validateCacheEntry(cacheEntryInfo{FileInfo: info, mode: os.ModeDir | 0o700}, true); err == nil {
		t.Fatal("missing ownership metadata was accepted")
	}
}

func TestTheCompilerCacheResolvesTargetsBeforeParentComponents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "target", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(root, "home"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("../target/deep", filepath.Join(root, "home", "jump")); err != nil {
		t.Fatal(err)
	}

	alias := filepath.Join(root, "alias")
	if err := os.Symlink("home/jump/../cache", alias); err != nil {
		t.Fatal(err)
	}

	resolved, err := privateDirectory(filepath.Join(alias, "savorum", "typst"))
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(filepath.Join(root, "target", "cache", "savorum", "typst"))
	if err != nil {
		t.Fatal(err)
	}

	if resolved != want {
		t.Fatalf("cache = %q, want symlink-resolved path %q", resolved, want)
	}

	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(unsafe, 0o777); err != nil { //nolint:gosec // the fixture needs an unsafe traversed target
		t.Fatal(err)
	}

	unsafeAlias := filepath.Join(root, "unsafe-alias")
	if err := os.Symlink("unsafe/../target/cache", unsafeAlias); err != nil {
		t.Fatal(err)
	}

	if _, err := privateDirectory(filepath.Join(unsafeAlias, "typst")); err == nil {
		t.Fatal("an unsafe target component before '..' was accepted")
	}
}
