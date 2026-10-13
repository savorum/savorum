package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/sref/strictjson"
)

// Limits bound what a reader will do with a bundle it did not create. They are
// applied to the container alone: a member is a package, and a package reader
// applies its own limits to it, so a hostile member cannot spend the
// container's budget and a hostile container cannot relax the member's.
type Limits struct {
	ArchiveBytes    int64
	ExpandedBytes   int64
	BytesPerEntry   int64
	Members         int
	PathLength      int
	CompressionRate int64
	ManifestBytes   int64
	DirectoryBytes  int64
	WorkBytes       int64
}

// DefaultLimits returns conservative resource ceilings for reading untrusted bundles.
func DefaultLimits() Limits {
	return Limits{
		ArchiveBytes:    64 << 20,
		ExpandedBytes:   80 << 20,
		BytesPerEntry:   64 << 20,
		Members:         500,
		PathLength:      512,
		CompressionRate: 200,
		ManifestBytes:   4 << 20,
		DirectoryBytes:  8 << 20,
		WorkBytes:       96 << 20,
	}
}

// Member is one verified package a bundle carries, still packaged; decoding it
// is the package reader's job.
type Member struct {
	MemberID string
	RecipeID string
	Path     string
	SHA256   string
	Bytes    []byte
}

// Bundle is a manifest and the members it declares, in declared order.
type Bundle struct {
	Manifest Manifest
	Members  []Member
}

// ArchiveError is a bundle this reader refused, carrying the precise
// snapshot-scoped requirement it failed under.
type ArchiveError struct {
	code    string
	message string
}

func (e *ArchiveError) Error() string { return e.message }

// Code reports the snapshot-scoped requirement identifier of this refusal.
func (e *ArchiveError) Code() string { return e.code }

func (*ArchiveError) Unwrap() error { return ErrUnsafeBundle }

func unsafeBundle(code, reason string, arguments ...any) error {
	return &ArchiveError{
		code:    code,
		message: fmt.Sprintf("%s: %s", ErrUnsafeBundle, fmt.Sprintf(reason, arguments...)),
	}
}

// Read opens a bundle from an untrusted source. Nothing is written anywhere:
// the container is checked, the manifest is read under a strict limit, and
// each member is verified against what the manifest declares before any of it
// is returned.
func Read(data []byte, limits Limits) (Bundle, error) {
	return ReadWithBudget(data, limits, archivepolicy.NewBudget(limits.WorkBytes))
}

// ReadWithBudget charges the bundle and any subsequently decoded members to
// one caller-owned operation budget.
func ReadWithBudget(data []byte, limits Limits, budget *archivepolicy.Budget) (Bundle, error) {
	members := make([]Member, 0)

	manifest, err := VisitWithBudget(data, limits, budget, func(member Member) error {
		members = append(members, member)
		return nil
	})
	if err != nil {
		return Bundle{}, err
	}

	return Bundle{Manifest: manifest, Members: members}, nil
}

// VisitWithBudget verifies and visits one member at a time. The member bytes
// belong to the callback only, allowing production imports to decode a nested
// package and release its compressed representation before opening the next.
func VisitWithBudget(data []byte, limits Limits, budget *archivepolicy.Budget, visit func(Member) error) (Manifest, error) {
	entries, err := validateBundleArchive(data, limits, budget)
	if err != nil {
		return Manifest{}, err
	}

	manifest, expanded, err := readBundleManifest(entries, limits, budget)
	if err != nil {
		return Manifest{}, err
	}

	if err := validateBundleManifestEntries(manifest, entries); err != nil {
		return Manifest{}, err
	}

	if err := visitBundleMembers(manifest, entries, limits, budget, &expanded, visit); err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

func validateBundleArchive(data []byte, limits Limits, budget *archivepolicy.Budget) (map[string]*zip.File, error) {
	if limits.ArchiveBytes > 0 && int64(len(data)) > limits.ArchiveBytes {
		return nil, unsafeBundle("archive-bytes-limit", "the bundle is larger than this reader accepts")
	}

	if err := budget.Spend(int64(len(data))); err != nil {
		return nil, unsafeBundle("work-bytes-limit", "the bundle exceeds this operation's working-set budget")
	}

	maximumEntries := 0
	if limits.Members > 0 {
		maximumEntries = limits.Members + 1
	}

	if err := archivepolicy.Preflight(data, maximumEntries, limits.DirectoryBytes); err != nil {
		if errors.Is(err, archivepolicy.ErrLimit) {
			return nil, unsafeBundle("archive-directory-limit", "the archive directory exceeds this reader's policy")
		}

		return nil, unsafeBundle("invalid-zip-bundle", "the archive directory is malformed")
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, unsafeBundle("invalid-zip-bundle", "the file is not a readable archive")
	}

	if limits.Members > 0 && len(archive.File) > limits.Members+1 {
		return nil, unsafeBundle("member-count-limit", "the bundle holds more entries than this reader accepts")
	}

	entries := make(map[string]*zip.File, len(archive.File))

	caseNames := make(map[string]string, len(archive.File))
	for _, entry := range archive.File {
		name := entry.Name

		folded := strings.ToLower(name)
		if previous, collision := caseNames[folded]; collision {
			return nil, unsafeBundle("duplicate-archive-entry", "entries %q and %q differ only by case", previous, name)
		}

		caseNames[folded] = name
		if err := safeBundlePath(name, limits); err != nil {
			return nil, err
		}

		if entry.Mode()&fs.ModeSymlink != 0 || !entry.Mode().IsRegular() {
			return nil, unsafeBundle("nonregular-archive-entry", "entry %q is not a regular file", name)
		}

		if entry.Flags&0x1 != 0 {
			return nil, unsafeBundle("nonregular-archive-entry", "entry %q is encrypted", name)
		}

		if _, duplicate := entries[name]; duplicate {
			return nil, unsafeBundle("duplicate-archive-entry", "entry %q appears more than once", name)
		}

		// Compared as unsigned: a declared size at or above 2^63 turns into a
		// negative int64, and a hostile archive could pass the limit with one.
		if limits.BytesPerEntry > 0 && entry.UncompressedSize64 > uint64(limits.BytesPerEntry) {
			return nil, unsafeBundle("entry-bytes-limit", "entry %q is larger than this reader accepts", name)
		}

		if limits.CompressionRate > 0 && entry.CompressedSize64 > 0 &&
			entry.UncompressedSize64/entry.CompressedSize64 > uint64(limits.CompressionRate) {
			return nil, unsafeBundle("compression-ratio-limit", "entry %q expands far more than this reader accepts", name)
		}

		entries[name] = entry
	}

	return entries, nil
}

func readBundleManifest(entries map[string]*zip.File, limits Limits, budget *archivepolicy.Budget) (Manifest, int64, error) {
	manifestEntry, present := entries[ManifestPath]
	if !present {
		return Manifest{}, 0, unsafeBundle("missing-bundle-manifest", "the bundle has no manifest.json")
	}

	var expanded int64

	manifestBytes, err := readEntry(manifestEntry, limits, budget, &expanded)
	if err != nil {
		return Manifest{}, 0, err
	}

	if limits.ManifestBytes > 0 && int64(len(manifestBytes)) > limits.ManifestBytes {
		return Manifest{}, 0, unsafeBundle("json-bytes-limit", "the bundle manifest is larger than this reader accepts")
	}

	var manifest Manifest

	if err := rejectManifestClock(manifestBytes); err != nil {
		return Manifest{}, 0, err
	}

	if err := strictjson.Decode(manifestBytes, &manifest); err != nil {
		return Manifest{}, 0, manifestJSONFailure(err)
	}

	if manifest.Format != Format {
		return Manifest{}, 0, unsafeBundle("malformed-package-json",
			"a bundle manifest declares format %q, not %q", manifest.Format, Format)
	}

	if manifest.Version == 0 {
		return Manifest{}, 0, unsafeBundle("malformed-package-json",
			"a bundle manifest does not declare its version")
	}

	if manifest.Version != Version {
		return Manifest{}, 0, unsafeBundle("unsupported-bundle-version",
			"a bundle manifest declares unsupported version %d", manifest.Version)
	}

	if len(manifest.Recipes) == 0 {
		// A bundle exists to carry recipes. One that declares none is not a
		// small bundle; it is a container that says nothing.
		return Manifest{}, 0, unsafeBundle("empty-bundle", "the bundle declares no recipes")
	}

	return manifest, expanded, nil
}

func validateBundleManifestEntries(manifest Manifest, entries map[string]*zip.File) error {
	declared := map[string]struct{}{ManifestPath: {}}
	seenMembers := make(map[string]struct{}, len(manifest.Recipes))

	seenPaths := make(map[string]struct{}, len(manifest.Recipes))
	for _, entry := range manifest.Recipes {
		if !ValidPortableID(entry.MemberID) {
			return unsafeBundle("bundle-member-id-required", "the manifest declares member %q, which is not a portable identifier", entry.MemberID)
		}

		if !ValidPortableID(entry.RecipeID) {
			return unsafeBundle("bundle-recipe-id-disagreement", "the manifest declares recipe %q, which is not a portable identifier", entry.RecipeID)
		}

		if _, repeated := seenMembers[entry.MemberID]; repeated {
			return unsafeBundle("duplicate-bundle-member-id", "the manifest declares member %q twice", entry.MemberID)
		}

		seenMembers[entry.MemberID] = struct{}{}
		if _, repeated := seenPaths[entry.Path]; repeated {
			return unsafeBundle("duplicate-bundle-member-id", "the manifest declares member path %q twice", entry.Path)
		}

		seenPaths[entry.Path] = struct{}{}
		if entry.Path != MemberPath(entry.MemberID) {
			return unsafeBundle("bundle-member-path-mismatch", "member %q is not at the path member %q requires", entry.Path, entry.MemberID)
		}

		declared[entry.Path] = struct{}{}
	}

	for name := range entries {
		if _, expected := declared[name]; !expected {
			return unsafeBundle("undeclared-bundle-entry", "the bundle carries undeclared file %q", name)
		}
	}

	return nil
}

func visitBundleMembers(manifest Manifest, entries map[string]*zip.File, limits Limits, budget *archivepolicy.Budget, expanded *int64, visit func(Member) error) error {
	for _, entry := range manifest.Recipes {
		carriedEntry, carried := entries[entry.Path]
		if !carried {
			return unsafeBundle("missing-bundle-member", "the bundle declares %q but does not carry it", entry.Path)
		}

		body, err := readEntry(carriedEntry, limits, budget, expanded)
		if err != nil {
			return err
		}

		if int64(len(body)) != entry.Size {
			return unsafeBundle("bundle-member-size-mismatch", "%q is not the size the manifest declares", entry.Path)
		}

		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			return unsafeBundle("bundle-member-digest-mismatch", "%q does not match the digest the manifest declares", entry.Path)
		}

		if err := visit(Member{
			MemberID: entry.MemberID, RecipeID: entry.RecipeID,
			Path: entry.Path, SHA256: entry.SHA256, Bytes: body,
		}); err != nil {
			return err
		}
	}

	return nil
}

func readEntry(entry *zip.File, limits Limits, budget *archivepolicy.Budget, expanded *int64) ([]byte, error) {
	room := limits.BytesPerEntry
	if room <= 0 {
		room = limits.ExpandedBytes
	}

	opened, err := entry.Open()
	if err != nil {
		return nil, unsafeBundle("invalid-zip-bundle", "entry %q could not be read", entry.Name)
	}

	body, readErr := io.ReadAll(io.LimitReader(opened, room+1))
	_ = opened.Close()

	if readErr != nil {
		return nil, unsafeBundle("invalid-zip-bundle", "entry %q could not be read", entry.Name)
	}

	if room > 0 && int64(len(body)) > room {
		return nil, unsafeBundle("entry-bytes-limit", "entry %q is larger than this reader accepts", entry.Name)
	}

	if err := budget.Spend(int64(len(body))); err != nil {
		return nil, unsafeBundle("work-bytes-limit", "the bundle exceeds this operation's working-set budget")
	}

	*expanded += int64(len(body))
	if limits.ExpandedBytes > 0 && *expanded > limits.ExpandedBytes {
		return nil, unsafeBundle("expanded-bytes-limit", "the bundle expands to more than this reader accepts")
	}

	return body, nil
}

// safeBundlePath refuses every name that could escape an extraction directory
// or mean two different things on two different systems.
func safeBundlePath(name string, limits Limits) error {
	if name == "" {
		return unsafeBundle("unsafe-bundle-member-path", "the bundle holds an entry with no name")
	}

	if limits.PathLength > 0 && len(name) > limits.PathLength {
		return unsafeBundle("path-length-limit", "entry name %q is longer than this reader accepts", name)
	}

	if strings.Contains(name, `\`) {
		return unsafeBundle("unsafe-bundle-member-path", "entry %q uses a backslash", name)
	}

	if strings.HasPrefix(name, "/") {
		return unsafeBundle("unsafe-bundle-member-path", "entry %q is an absolute path", name)
	}

	if len(name) > 1 && name[1] == ':' {
		return unsafeBundle("unsafe-bundle-member-path", "entry %q names a drive", name)
	}

	for _, value := range name {
		if value < 0x20 || value == 0x7f {
			return unsafeBundle("unsafe-bundle-member-path", "entry %q contains a control character", name)
		}
	}

	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return unsafeBundle("unsafe-bundle-member-path", "entry %q has an unsafe path segment", name)
		}
	}

	if name == ManifestPath {
		return nil
	}

	rest, inside := strings.CutPrefix(name, memberDirectory)
	if !inside {
		return unsafeBundle("bundle-entry-outside-recipes", "entry %q is not somewhere a bundle may put a file", name)
	}

	memberID, ok := strings.CutSuffix(rest, ".sref")
	if !ok || !ValidPortableID(memberID) {
		return unsafeBundle("bundle-member-path-mismatch", "entry %q is not named after a portable member identifier", name)
	}

	return nil
}

// manifestJSONFailure names which lexical rule a bundle manifest broke; see
// the package reader's equivalent.
func manifestJSONFailure(err error) error {
	message := ""
	if err != nil {
		message = err.Error()
	}

	code := "malformed-package-json"

	switch strictjson.Kind(err) {
	case strictjson.Duplicate:
		code = "duplicate-bundle-manifest-member"
	case strictjson.Trailing:
		code = "trailing-bundle-manifest-value"
	}

	return unsafeBundle(code, "the bundle manifest could not be read: %s", message)
}

// clockMembers are the names a writer reaches for when it records when a
// bundle was made.
var clockMembers = []string{"generated_at", "created_at", "timestamp", "generated", "written_at", "modified_at"}

// rejectManifestClock refuses a bundle manifest that records generation time.
//
// Section 18 excludes it so the same members produce the same bytes. It gets
// its own refusal because writers break this rule by accident.
func rejectManifestClock(data []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil
	}

	for _, name := range clockMembers {
		if _, present := members[name]; present {
			return unsafeBundle("bundle-manifest-timestamp",
				"the bundle manifest records %q, and a bundle states no clock", name)
		}
	}

	return nil
}
