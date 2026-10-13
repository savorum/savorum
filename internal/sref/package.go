package sref

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
	"path"
	"sort"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/sref/strictjson"
)

// archiveEpoch is the timestamp every entry Savorum writes carries. The
// specification does not require canonical bytes, but the writer avoids adding
// clock variation when given the same exact document and asset bytes.
var archiveEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// PackageVersion is the package layout this build reads and writes.
const PackageVersion = "0.1.0"

// Limits bound what a reader will do with a package it did not create. The
// specification requires finite limits and leaves the values to policy, so
// these are the defaults rather than the definition.
type Limits struct {
	ArchiveBytes    int64
	ExpandedBytes   int64
	BytesPerEntry   int64
	Entries         int
	PathLength      int
	CompressionRate int64
	ManifestBytes   int64
	DirectoryBytes  int64
	WorkBytes       int64
}

// DefaultLimits returns conservative resource ceilings for reading untrusted packages.
func DefaultLimits() Limits {
	return Limits{
		ArchiveBytes:    64 << 20,
		ExpandedBytes:   80 << 20,
		BytesPerEntry:   64 << 20,
		Entries:         10_002,
		PathLength:      512,
		CompressionRate: 200,
		ManifestBytes:   4 << 20,
		DirectoryBytes:  8 << 20,
		WorkBytes:       96 << 20,
	}
}

// ErrUnsafePackage reports a package that exceeds caller limits or violates archive safety policy.
var ErrUnsafePackage = errors.New("unsafe SREF package")

type manifestFile struct {
	Package struct {
		Version string `json:"version"`
	} `json:"sref_package"`
	Recipe manifestEntry `json:"recipe"`
	// A pointer, because the schema requires the member and a plain slice
	// cannot tell an absent `assets` from an empty one.
	Assets *[]manifestEntry `json:"assets"`
}

type manifestEntry struct {
	ID        string `json:"id,omitempty"`
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
}

// PackageAsset is one file a package carries, already verified against what the
// manifest and the recipe both declare.
type PackageAsset struct {
	ID        string
	Path      string
	MediaType string
	SHA256    string
	Bytes     []byte
}

// Package is a recipe together with the files it needs. A recipe with assets is
// only self-contained as a package; as bare JSON it names files nobody has.
type Package struct {
	Recipe recipe.Document
	Assets []PackageAsset
}

// ArchiveError is a package this reader refused, carrying the precise
// snapshot-scoped requirement it failed under. FailureCategoryOf projects that
// detail onto SREF's portable vocabulary, which names each as a separate
// requirement.
type ArchiveError struct {
	code    string
	message string
}

func (e *ArchiveError) Error() string { return e.message }

// Code reports the snapshot-scoped requirement identifier of this refusal.
func (e *ArchiveError) Code() string { return e.code }

func (*ArchiveError) Unwrap() error { return ErrUnsafePackage }

func unsafePackage(code, reason string, arguments ...any) error {
	return &ArchiveError{
		code:    code,
		message: fmt.Sprintf("%s: %s", ErrUnsafePackage, fmt.Sprintf(reason, arguments...)),
	}
}

// AssetContent is one declared asset whose bytes are opened only when the
// package writer reaches them. A recipe's photographs can be larger than the
// rest of the recipe put together, so they stream through rather than being
// gathered first.
type AssetContent struct {
	ID        string
	Path      string
	MediaType string
	SHA256    string
	Size      int64
	// Open supplies the bytes. It is called at most once.
	Open func() (io.ReadCloser, error)
}

// WritePackage builds a `.sref` archive in memory. It is the convenient form of
// WritePackageTo for callers that already hold every asset's bytes.
func WritePackage(document recipe.Document, assets []PackageAsset) ([]byte, error) {
	contents := make([]AssetContent, 0, len(assets))
	for _, asset := range assets {
		body := asset.Bytes
		contents = append(contents, AssetContent{
			ID: asset.ID, Path: asset.Path, MediaType: asset.MediaType,
			SHA256: asset.SHA256, Size: int64(len(body)),
			Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil },
		})
	}

	buffer := &bytes.Buffer{}
	if err := WritePackageTo(buffer, document, contents); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

// WritePackageTo streams a `.sref` archive. The manifest records the digest and
// size of the recipe and of every asset, and the recipe and manifest agree on
// every asset by construction. Each asset's bytes are verified against what the
// recipe declares while they are copied, so a package cannot be written around
// a photograph that is not the one the recipe names.
func WritePackageTo(destination io.Writer, document recipe.Document, contents []AssetContent) error {
	encoded, err := Encode(document)
	if err != nil {
		return err
	}

	byID := make(map[string]AssetContent, len(contents))
	for _, asset := range contents {
		byID[asset.ID] = asset
	}

	assets := []manifestEntry{}
	manifest := manifestFile{Assets: &assets}
	manifest.Package.Version = PackageVersion
	manifest.Recipe = manifestEntry{
		Path: "recipe.json", MediaType: "application/json",
		SHA256: digestOf(encoded), Size: int64(len(encoded)),
	}

	ordered := make([]AssetContent, 0, len(document.Assets))
	for _, declared := range document.Assets {
		asset, held := byID[declared.ID]
		if !held {
			return fmt.Errorf("recipe declares asset %q but its bytes were not supplied", declared.ID)
		}

		asset.Path, asset.MediaType, asset.SHA256, asset.Size = declared.Path, declared.MediaType, declared.SHA256, declared.Size
		ordered = append(ordered, asset)
		assets = append(assets, manifestEntry{
			ID: declared.ID, Path: declared.Path, MediaType: declared.MediaType,
			SHA256: declared.SHA256, Size: declared.Size,
		})
	}

	// Reassigned because `append` may have moved the backing array, and the
	// manifest holds a pointer to the slice header rather than a copy of it.
	manifest.Assets = &assets

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode package manifest: %w", err)
	}

	manifestBytes = append(manifestBytes, '\n')

	archive := zip.NewWriter(destination)
	if err := writeArchiveEntry(archive, "recipe.json", zip.Deflate, bytes.NewReader(encoded)); err != nil {
		return err
	}

	if err := writeArchiveEntry(archive, "manifest.json", zip.Deflate, bytes.NewReader(manifestBytes)); err != nil {
		return err
	}

	for _, asset := range ordered {
		if err := writeVerifiedAsset(archive, asset); err != nil {
			return err
		}
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("close package: %w", err)
	}

	return nil
}

// writeVerifiedAsset copies one asset into the archive while measuring it. The
// digest and size are checked after the copy rather than before, because the
// bytes on the way through are the only evidence that matters.
func writeVerifiedAsset(archive *zip.Writer, asset AssetContent) error {
	if asset.Open == nil {
		return fmt.Errorf("recipe declares asset %q but its bytes were not supplied", asset.ID)
	}

	body, err := asset.Open()
	if err != nil {
		return fmt.Errorf("read the bytes of %q: %w", asset.ID, err)
	}
	defer body.Close()

	digest := sha256.New()
	counter := &countingWriter{}

	entry, err := archive.CreateHeader(&zip.FileHeader{
		Name: asset.Path, Method: zip.Deflate, Modified: archiveEpoch,
	})
	if err != nil {
		return fmt.Errorf("write %q into package: %w", asset.Path, err)
	}

	if _, err := io.Copy(io.MultiWriter(entry, digest, counter), body); err != nil {
		return fmt.Errorf("read the bytes of %q: %w", asset.ID, err)
	}

	if counter.count != asset.Size {
		return fmt.Errorf("asset %q is %d bytes, and the recipe declares %d",
			asset.ID, counter.count, asset.Size)
	}

	if got := hex.EncodeToString(digest.Sum(nil)); got != asset.SHA256 {
		return fmt.Errorf("asset %q does not match the digest the recipe declares", asset.ID)
	}

	return nil
}

func writeArchiveEntry(archive *zip.Writer, name string, method uint16, source io.Reader) error {
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: archiveEpoch})
	if err != nil {
		return fmt.Errorf("write %q into package: %w", name, err)
	}

	if _, err := io.Copy(entry, source); err != nil {
		return fmt.Errorf("write %q into package: %w", name, err)
	}

	return nil
}

type countingWriter struct{ count int64 }

func (w *countingWriter) Write(data []byte) (int, error) {
	w.count += int64(len(data))
	return len(data), nil
}

// ReadPackage opens a `.sref` archive from an untrusted source. Nothing is
// written anywhere: entries are read into memory, checked against the manifest
// and the recipe, and returned only once everything agrees.
func ReadPackage(data []byte, limits Limits) (Package, error) {
	return ReadPackageWithBudget(data, limits, archivepolicy.NewBudget(limits.WorkBytes))
}

// ReadPackageWithBudget applies the package policy and charges compressed
// input, expanded entries, and decoded recipe state to a budget that may be
// shared with an enclosing bundle operation.
func ReadPackageWithBudget(data []byte, limits Limits, budget *archivepolicy.Budget) (Package, error) {
	contents, err := readPackageEntries(data, limits, budget)
	if err != nil {
		return Package{}, err
	}

	_, recipeBytes, declared, err := readPackageManifest(contents, limits)
	if err != nil {
		return Package{}, err
	}

	document, err := Decode(recipeBytes)
	if err != nil {
		if isMalformedJSON(err) {
			return Package{}, unsafePackage("malformed-package-json", "recipe.json is not readable JSON: %v", err)
		}

		return Package{}, err
	}

	if err := budget.Spend(int64(len(recipeBytes))); err != nil {
		return Package{}, unsafePackage("work-bytes-limit", "the package exceeds this operation's working-set budget")
	}

	assets, err := collectPackageAssets(document, declared, contents)
	if err != nil {
		return Package{}, err
	}

	return Package{Recipe: document, Assets: assets}, nil
}

func readPackageEntries(data []byte, limits Limits, budget *archivepolicy.Budget) (map[string][]byte, error) {
	if limits.ArchiveBytes > 0 && int64(len(data)) > limits.ArchiveBytes {
		return nil, unsafePackage("archive-bytes-limit", "the archive is larger than this reader accepts")
	}

	if err := budget.Spend(int64(len(data))); err != nil {
		return nil, unsafePackage("work-bytes-limit", "the package exceeds this operation's working-set budget")
	}

	if err := archivepolicy.Preflight(data, limits.Entries, limits.DirectoryBytes); err != nil {
		if errors.Is(err, archivepolicy.ErrLimit) {
			return nil, unsafePackage("archive-directory-limit", "the archive directory exceeds this reader's policy")
		}

		return nil, unsafePackage("invalid-zip", "the archive directory is malformed")
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, unsafePackage("invalid-zip", "the file is not a readable archive")
	}

	if limits.Entries > 0 && len(archive.File) > limits.Entries {
		return nil, unsafePackage("entry-count-limit", "the archive holds more entries than this reader accepts")
	}

	contents := make(map[string][]byte, len(archive.File))
	caseNames := make(map[string]string, len(archive.File))

	var expanded int64

	for _, entry := range archive.File {
		name := entry.Name

		folded := strings.ToLower(name)
		if previous, collision := caseNames[folded]; collision {
			return nil, unsafePackage("duplicate-archive-entry", "entries %q and %q differ only by case", previous, name)
		}

		caseNames[folded] = name
		if _, duplicate := contents[name]; duplicate {
			return nil, unsafePackage("duplicate-archive-entry", "entry %q appears more than once", name)
		}

		body, err := readPackageEntry(entry, limits, budget, &expanded)
		if err != nil {
			return nil, err
		}

		contents[name] = body
	}

	return contents, nil
}

func readPackageEntry(entry *zip.File, limits Limits, budget *archivepolicy.Budget, expanded *int64) ([]byte, error) {
	name := entry.Name
	if err := safeEntryPath(name, limits); err != nil {
		return nil, err
	}

	if entry.Mode()&fs.ModeSymlink != 0 || !entry.Mode().IsRegular() || entry.Flags&0x1 != 0 {
		return nil, unsafePackage("nonregular-archive-entry", "entry %q is not a regular unencrypted file", name)
	}

	if limits.BytesPerEntry > 0 && entry.UncompressedSize64 > uint64(limits.BytesPerEntry) {
		return nil, unsafePackage("entry-bytes-limit", "entry %q is larger than this reader accepts", name)
	}

	if limits.CompressionRate > 0 && entry.CompressedSize64 > 0 && entry.UncompressedSize64/entry.CompressedSize64 > uint64(limits.CompressionRate) {
		return nil, unsafePackage("compression-ratio-limit", "entry %q expands far more than this reader accepts", name)
	}

	opened, err := entry.Open()
	if err != nil {
		return nil, unsafePackage("invalid-zip", "entry %q could not be read", name)
	}

	room := limits.BytesPerEntry
	if room <= 0 {
		room = limits.ExpandedBytes
	}

	body, readErr := io.ReadAll(io.LimitReader(opened, room+1))
	_ = opened.Close()

	if readErr != nil {
		return nil, unsafePackage("invalid-zip", "entry %q could not be read", name)
	}

	if room > 0 && int64(len(body)) > room {
		return nil, unsafePackage("entry-bytes-limit", "entry %q is larger than this reader accepts", name)
	}

	if err := budget.Spend(int64(len(body))); err != nil {
		return nil, unsafePackage("work-bytes-limit", "the package exceeds this operation's working-set budget")
	}

	*expanded += int64(len(body))
	if limits.ExpandedBytes > 0 && *expanded > limits.ExpandedBytes {
		return nil, unsafePackage("expanded-bytes-limit", "the archive expands to more than this reader accepts")
	}

	return body, nil
}

func readPackageManifest(contents map[string][]byte, limits Limits) (manifestFile, []byte, map[string]manifestEntry, error) {
	manifestBytes, present := contents["manifest.json"]
	if !present {
		return manifestFile{}, nil, nil, unsafePackage("missing-manifest", "the package has no manifest.json")
	}

	if limits.ManifestBytes > 0 && int64(len(manifestBytes)) > limits.ManifestBytes {
		return manifestFile{}, nil, nil, unsafePackage("json-bytes-limit", "the manifest is larger than this reader accepts")
	}

	var manifest manifestFile
	if err := strictjson.Decode(manifestBytes, &manifest); err != nil {
		return manifestFile{}, nil, nil, manifestJSONFailure(err, "package")
	}

	if manifest.Package.Version == "" {
		return manifestFile{}, nil, nil, unsafePackage("malformed-package-json",
			"a package manifest does not declare sref_package.version")
	}

	if manifest.Package.Version != PackageVersion {
		return manifestFile{}, nil, nil, unsafePackage("unsupported-package-version",
			"a package manifest declares unsupported sref_package.version %q",
			manifest.Package.Version)
	}

	if manifest.Assets == nil {
		return manifestFile{}, nil, nil, unsafePackage("malformed-package-json",
			"a package manifest declares assets, as an empty array when the recipe has none")
	}

	recipeBytes, present := contents["recipe.json"]
	if !present {
		return manifestFile{}, nil, nil, unsafePackage("missing-recipe", "the package has no recipe.json")
	}
	// Both constants in the schema, so both are the manifest failing to be one
	// rather than the manifest disagreeing with something.
	if manifest.Recipe.Path != "recipe.json" {
		return manifestFile{}, nil, nil, unsafePackage("malformed-package-json",
			"a package manifest's recipe entry names recipe.json, not %q", manifest.Recipe.Path)
	}

	if manifest.Recipe.MediaType != "application/json" {
		return manifestFile{}, nil, nil, unsafePackage("malformed-package-json",
			"a package manifest's recipe entry declares application/json, not %q", manifest.Recipe.MediaType)
	}

	if err := verify(manifest.Recipe, recipeBytes); err != nil {
		return manifestFile{}, nil, nil, err
	}

	declared := map[string]manifestEntry{}
	paths := map[string]struct{}{"recipe.json": {}, "manifest.json": {}}

	for _, entry := range *manifest.Assets {
		if _, repeated := declared[entry.ID]; repeated {
			return manifestFile{}, nil, nil, unsafePackage("duplicate-manifest-asset", "the manifest declares asset %q twice", entry.ID)
		}

		if _, repeated := paths[entry.Path]; repeated {
			return manifestFile{}, nil, nil, unsafePackage("duplicate-manifest-asset", "the manifest declares path %q twice", entry.Path)
		}

		declared[entry.ID] = entry
		paths[entry.Path] = struct{}{}
	}

	for name := range contents {
		if _, expected := paths[name]; !expected {
			return manifestFile{}, nil, nil, unsafePackage("undeclared-archive-entry", "the package carries undeclared file %q", name)
		}
	}

	return manifest, recipeBytes, declared, nil
}

func collectPackageAssets(document recipe.Document, declared map[string]manifestEntry, contents map[string][]byte) ([]PackageAsset, error) {
	if len(document.Assets) != len(declared) {
		return nil, unsafePackage("recipe-manifest-disagreement", "the recipe and the manifest declare different assets")
	}

	assets := make([]PackageAsset, 0, len(document.Assets))
	for _, asset := range document.Assets {
		entry, agreed := declared[asset.ID]
		if !agreed {
			return nil, unsafePackage("recipe-manifest-disagreement", "the manifest does not declare asset %q", asset.ID)
		}

		if entry.Path != asset.Path || entry.MediaType != asset.MediaType || entry.SHA256 != asset.SHA256 {
			return nil, unsafePackage("recipe-manifest-disagreement", "the recipe and the manifest disagree about asset %q", asset.ID)
		}

		body, carried := contents[asset.Path]
		if !carried {
			return nil, unsafePackage("missing-asset-entry", "the package declares asset %q but does not carry it", asset.ID)
		}

		if err := verify(entry, body); err != nil {
			return nil, err
		}

		assets = append(assets, PackageAsset{
			ID: asset.ID, Path: asset.Path, MediaType: asset.MediaType,
			SHA256: asset.SHA256, Bytes: body,
		})
	}

	sort.Slice(assets, func(left, right int) bool { return assets[left].Path < assets[right].Path })

	return assets, nil
}

// safeEntryPath refuses every name that could escape an extraction directory or
// mean two different things on two different systems.
func safeEntryPath(name string, limits Limits) error {
	if name == "" {
		return unsafePackage("empty-archive-path-segment", "the archive holds an entry with no name")
	}

	if limits.PathLength > 0 && len(name) > limits.PathLength {
		return unsafePackage("path-length-limit", "entry name %q is longer than this reader accepts", name)
	}

	if strings.ContainsAny(name, "\\") {
		return unsafePackage("backslash-archive-path", "entry %q uses a backslash", name)
	}

	if strings.HasPrefix(name, "/") {
		return unsafePackage("absolute-archive-path", "entry %q is an absolute path", name)
	}

	if len(name) > 1 && name[1] == ':' {
		return unsafePackage("drive-letter-archive-path", "entry %q names a drive", name)
	}

	for _, value := range name {
		if value < 0x20 || value == 0x7f {
			return unsafePackage("control-character-archive-path", "entry %q contains a control character", name)
		}
	}

	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" {
			// An empty segment is a doubled separator, which several path
			// libraries collapse. SREF names it separately from a dot segment
			// because they are refused for different reasons: one is
			// ambiguous, the other is an escape.
			return unsafePackage("empty-archive-path-segment", "entry %q has an empty path segment", name)
		}

		if segment == "." || segment == ".." {
			return unsafePackage("unsafe-archive-path", "entry %q has an unsafe path segment", name)
		}
	}

	if name != "recipe.json" && name != "manifest.json" && !strings.HasPrefix(name, "assets/") {
		return unsafePackage("undeclared-archive-entry", "entry %q is not somewhere a package may put a file", name)
	}

	if cleaned := path.Clean(name); cleaned != name {
		return unsafePackage("unsafe-archive-path", "entry %q is not a normalized path", name)
	}

	return nil
}

func verify(entry manifestEntry, body []byte) error {
	// The recipe and an asset fail under different categories. A recipe whose
	// bytes do not match the manifest is a broken package; an asset that does
	// not is a broken photograph, and a consumer may reasonably do different
	// things about them.
	sizeCode, digestCode := "asset-size-mismatch", "asset-digest-mismatch"
	if entry.Path == "recipe.json" {
		sizeCode, digestCode = "recipe-digest-mismatch", "recipe-digest-mismatch"
	}

	if int64(len(body)) != entry.Size {
		return unsafePackage(sizeCode, "%q is not the size the manifest declares", entry.Path)
	}

	if digestOf(body) != entry.SHA256 {
		return unsafePackage(digestCode, "%q does not match the digest the manifest declares", entry.Path)
	}

	return nil
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// manifestJSONFailure names which lexical rule a manifest broke. Duplicate
// members and trailing values are separate SREF requirements from invalid JSON,
// because either lets two readers disagree about what a package declares.
func manifestJSONFailure(err error, container string) error {
	message := ""
	if err != nil {
		message = err.Error()
	}

	code := "malformed-package-json"

	switch strictjson.Kind(err) {
	case strictjson.Duplicate:
		code = "duplicate-package-manifest-member"
		if container == "bundle" {
			code = "duplicate-bundle-manifest-member"
		}
	case strictjson.Trailing:
		code = "trailing-package-manifest-value"
		if container == "bundle" {
			code = "trailing-bundle-manifest-value"
		}
	}

	return unsafePackage(code, "the %s manifest could not be read: %s", container, message)
}

// isMalformedJSON reports a failure that happened before the bytes became a
// document, as opposed to one a document failed once it existed.
func isMalformedJSON(err error) bool {
	var (
		syntax    *json.SyntaxError
		unmarshal *json.UnmarshalTypeError
	)
	if errors.As(err, &syntax) || errors.As(err, &unmarshal) {
		return true
	}
	// Truncated input reaches a streaming decoder as an end of file rather
	// than as a syntax error, which is the same defect arriving by a
	// different route.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	return strictjson.Kind(err) == strictjson.Malformed
}
