package importer

// Native application adapters. These deliberately retain source wording and
// use the ordinary ingredient parser; vendor IDs are provenance only.
import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/ingredientparser"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const nativeMaxArchive = 64 << 20

type nativeLineGroup struct {
	title string
	lines []string
}

func readNativeZip(data []byte, maxEntries int, fn func(string, []byte) error) error {
	return readNativeZipBudget(data, maxEntries, archivepolicy.NewBudget(nativeMaxArchive), fn)
}

func readNativeZipBudget(data []byte, maxEntries int, budget *archivepolicy.Budget, fn func(string, []byte) error) error {
	if len(data) == 0 || len(data) > nativeMaxArchive {
		return fmt.Errorf("native archive exceeds limit")
	}

	if err := archivepolicy.Preflight(data, maxEntries, 8<<20); err != nil {
		return fmt.Errorf("preflight native archive: %w", err)
	}

	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open native archive: %w", err)
	}

	if len(z.File) > maxEntries {
		return fmt.Errorf("native archive has too many members")
	}

	seen := make(map[string]struct{}, len(z.File))
	for _, f := range z.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() {
			// A directory entry is named with its trailing slash.
			name = strings.TrimSuffix(name, "/")
		}

		clean := filepath.ToSlash(filepath.Clean(name))
		if clean != name || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.ContainsRune(clean, 0) || (len(clean) >= 2 && clean[1] == ':') {
			return fmt.Errorf("unsafe archive member path %q", f.Name)
		}

		// Reject names that become the same path on common case-insensitive or
		// Unicode-normalizing filesystems, even when this importer reads in memory.
		key := norm.NFC.String(cases.Fold().String(norm.NFC.String(clean)))
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate archive member %q", f.Name)
		}

		seen[key] = struct{}{}

		if f.FileInfo().IsDir() {
			continue
		}

		if f.UncompressedSize64 > 8<<20 {
			return fmt.Errorf("member %q exceeds limit", f.Name)
		}

		if err := budget.Spend(int64(f.UncompressedSize64)); err != nil {
			return fmt.Errorf("reserve expanded size for member %q: %w", f.Name, err)
		}

		r, e := f.Open()
		if e != nil {
			return fmt.Errorf("open native archive member %q: %w", f.Name, e)
		}

		b, e := io.ReadAll(io.LimitReader(r, 8<<20+1))
		_ = r.Close()

		if e != nil {
			return fmt.Errorf("read native archive member %q: %w", f.Name, e)
		}

		if len(b) > 8<<20 {
			return fmt.Errorf("member %q expands beyond limit", f.Name)
		}

		if e = fn(filepath.ToSlash(f.Name), b); e != nil {
			return e
		}
	}

	return nil
}

func zipContainsJSONKey(data []byte, key string) bool {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("PK\x03\x04")) || archivepolicy.Preflight(data, 1000, 8<<20) != nil {
		return false
	}

	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}

	remaining := int64(8 << 20)

	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}

		b, ok := readProbeMember(f, &remaining)
		if !ok {
			return false
		}

		if bytes.Contains(b, []byte(key)) {
			return true
		}
	}

	return false
}

// readProbeMember shares one decompression allowance across format detection.
func readProbeMember(file *zip.File, remaining *int64) ([]byte, bool) {
	if *remaining <= 0 || file.UncompressedSize64 > uint64(*remaining) {
		return nil, false
	}

	opened, err := file.Open()
	if err != nil {
		return nil, false
	}

	body, readErr := io.ReadAll(io.LimitReader(opened, *remaining+1))

	closeErr := opened.Close()
	if readErr != nil || closeErr != nil || int64(len(body)) > *remaining {
		return nil, false
	}

	*remaining -= int64(len(body))

	return body, true
}

func sourceDoc(app, id, title string) recipe.Document {
	h := sha256.Sum256([]byte(app + "\x00" + id))
	return recipe.Document{SchemaVersion: recipe.CurrentDocumentSchemaVersion, ID: recipe.RecipeID(strings.ToLower(strings.ReplaceAll(app, " ", "-")) + "." + fmt.Sprintf("%x", h[:8])), Title: title, Source: &recipe.Source{Application: app, SourceID: id}}
}

func addNativeLines(d *recipe.Document, groups []nativeLineGroup, locale string) {
	for gi, g := range groups {
		s := recipe.IngredientSection{ID: fmt.Sprintf("i%d", gi+1), Title: g.title}
		for j, line := range g.lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			s.Ingredients = append(s.Ingredients, ingredientparser.Parse(line, fmt.Sprintf("%s-%d", s.ID, j+1), locale))
		}

		if len(s.Ingredients) > 0 {
			d.IngredientSections = append(d.IngredientSections, s)
		}
	}
}

func addDirections(d *recipe.Document, groups []nativeLineGroup) {
	n := 0

	for gi, g := range groups {
		is := recipe.InstructionSection{ID: fmt.Sprintf("m%d", gi+1), Title: g.title}
		for _, line := range g.lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			n++
			is.Steps = append(is.Steps, recipe.Step{ID: fmt.Sprintf("p%d", n), Text: strings.TrimSpace(line)})
		}

		if len(is.Steps) > 0 {
			d.InstructionSections = append(d.InstructionSections, is)
		}
	}
}

func duration(v any) *recipe.DurationExpression {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" || s == "0" || s == "0 minutes" {
		return nil
	}

	if n, e := strconv.Atoi(s); e == nil {
		return &recipe.DurationExpression{Value: fmt.Sprintf("PT%dM", n), SourceText: s}
	}

	parts := strings.Fields(strings.ToLower(s))
	// Parse a single numeric/unit pair while retaining compound durations
	// verbatim. Parsing only the first pair would silently discard the rest
	// (for example, "1 hour 30 minutes").
	if len(parts) == 2 {
		if n, e := strconv.Atoi(parts[0]); e == nil {
			unit := parts[1]

			var suffix string

			switch unit {
			case "minute", "minutes", "min", "mins":
				suffix = "M"
			case "hour", "hours", "hr", "hrs":
				suffix = "H"
			default:
				return &recipe.DurationExpression{Text: s}
			}

			iso := "PT" + strconv.Itoa(n) + suffix

			return &recipe.DurationExpression{Value: iso, SourceText: s}
		}
	}

	return &recipe.DurationExpression{Text: s}
}

func setTimes(d *recipe.Document, prep, cook, total any) {
	t := &recipe.Times{Prep: duration(prep), Cook: duration(cook), Total: duration(total)}
	if t.Prep != nil || t.Cook != nil || t.Total != nil {
		d.Times = t
	}
}

func addUnsupported(item *ImportedRecipe, field, evidence string) {
	item.Diagnostics = append(item.Diagnostics, Diagnostic{Code: "unsupported_field", Class: DiagnosticInformational, Severity: SeverityInfo, Area: field, Message: "Source field is not represented by Savorum yet; preserved in fidelity diagnostics.", Targets: []DiagnosticTarget{{Field: field, Evidence: fieldEvidence(evidence)}}})
}

// maximumFieldEvidence bounds what a diagnostic quotes back, since a source
// field can hold an embedded image.
const maximumFieldEvidence = 200

func fieldEvidence(value string) string {
	value = strings.TrimSpace(value)
	if runes := []rune(value); len(runes) > maximumFieldEvidence {
		return strings.TrimSpace(string(runes[:maximumFieldEvidence])) + "\u2026"
	}

	return value
}

// absentNativeField reports whether a decoded native field carries nothing,
// including JSON null, which fmt would render as "<nil>".
func absentNativeField(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return strings.TrimSpace(fmt.Sprint(value)) == ""
	}
}

// importedRating keeps a source rating inside the one-to-five range the
// library accepts; Paprika, Mealie and Tandoor spell "not rated" as zero.
func importedRating(item *ImportedRecipe, fields map[string]any, field string) {
	raw, ok := fields[field].(float64)
	if !ok || raw == 0 {
		return
	}

	if raw < 1 || raw > 5 || raw != math.Trunc(raw) {
		addUnsupported(item, field, strconv.FormatFloat(raw, 'f', -1, 64))

		return
	}

	value := uint8(raw)
	item.LibraryMetadata.Rating = &value
}

// addUnknownNativeFields makes the loss boundary of a native adapter visible.
// Native exports are intentionally decoded into maps so fields introduced by
// a newer exporter do not disappear without an import diagnostic. Empty
// optional fields are ignored, and each adapter supplies the fields it maps or
// intentionally handles elsewhere.
func addUnknownNativeFields(item *ImportedRecipe, fields map[string]any, known map[string]struct{}, handled ...string) {
	accounted := make(map[string]struct{}, len(handled))
	for _, key := range handled {
		accounted[key] = struct{}{}
	}

	keys := make([]string, 0, len(fields))

	for key, value := range fields {
		if _, ok := known[key]; ok || absentNativeField(value) {
			continue
		}

		if _, ok := accounted[key]; ok {
			continue
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		addUnsupported(item, "source."+key, fmt.Sprint(fields[key]))
	}
}

// addExportVersionMetadata keeps source format/version markers available to a
// future refresh without treating them as recipe content.
func addExportVersionMetadata(metadata map[string]any, fields map[string]any) {
	for _, key := range []string{"export_version", "format_version", "schema_version", "version"} {
		if value, ok := fields[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			metadata[key] = value
		}
	}
}

func nativeFieldSet(fields ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}

	return set
}

func embeddedFromValue(v any) []EmbeddedAsset {
	var out []EmbeddedAsset

	for _, m := range arrayMaps(v) {
		raw := firstString(m, "data", "base64")
		if raw == "" {
			continue
		}

		b, e := base64.StdEncoding.DecodeString(raw)
		if e != nil {
			continue
		}

		name := firstString(m, "filename", "path", "name")

		media := firstString(m, "media_type", "content_type")
		if media == "" {
			media = sniffImageMedia(b)
		}

		sum := sha256.Sum256(b)
		out = append(out, EmbeddedAsset{ID: name, Path: name, MediaType: media, SHA256: fmt.Sprintf("%x", sum), Bytes: b})
	}

	return out
}

func sniffImageMedia(b []byte) string {
	if bytes.HasPrefix(b, []byte("\xff\xd8\xff")) {
		return "image/jpeg"
	}

	if bytes.HasPrefix(b, []byte("\x89PNG")) {
		return "image/png"
	}

	return "application/octet-stream"
}

// EmbeddedAssetID is the identifier an embedded asset takes once it is part of
// a recipe document. Import review uses it to stand in unattached assets under
// the same name.
func EmbeddedAssetID(asset EmbeddedAsset) string {
	id := strings.ToLower(strings.ReplaceAll(asset.ID, " ", "-"))
	if id == "" && len(asset.SHA256) >= 12 {
		id = asset.SHA256[:12]
	}

	return id
}

func attachEmbedded(d *recipe.Document, assets []EmbeddedAsset) {
	// Two photographs exported under one filename are one asset, since asset
	// identifiers and paths are unique.
	attached := make(map[string]struct{}, len(d.Assets))
	for _, existing := range d.Assets {
		attached[existing.ID] = struct{}{}
		attached[strings.ToLower(existing.Path)] = struct{}{}
	}

	for _, a := range assets {
		id := EmbeddedAssetID(a)
		if id == "" {
			continue
		}

		path := a.Path
		if !strings.HasPrefix(path, "assets/") {
			path = "assets/" + id
		}

		if _, already := attached[id]; already {
			continue
		}

		if _, already := attached[strings.ToLower(path)]; already {
			continue
		}

		attached[id] = struct{}{}
		attached[strings.ToLower(path)] = struct{}{}

		d.Assets = append(d.Assets, recipe.Asset{ID: id, Path: path, MediaType: a.MediaType, SHA256: a.SHA256, Size: int64(len(a.Bytes)), Role: recipe.AssetRoleImage})
		d.ImageRefs = append(d.ImageRefs, id)
	}

	if len(d.ImageRefs) > 0 {
		d.PrimaryImage = d.ImageRefs[0]
	}
}

func stringSlice(v any) []string {
	var r []string

	switch x := v.(type) {
	case []any:
		for _, a := range x {
			if s, ok := a.(string); ok {
				r = append(r, s)
			} else if m, ok := a.(map[string]any); ok {
				if s := firstString(m, "name", "text", "note", "title"); s != "" {
					r = append(r, s)
				}
			}
		}
	case string:
		for line := range strings.SplitSeq(strings.ReplaceAll(x, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				r = append(r, strings.TrimSpace(line))
			}
		}
	}

	return r
}

func firstString(v map[string]any, ks ...string) string {
	for _, k := range ks {
		if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}

	return ""
}

func arrayMaps(v any) []map[string]any {
	var r []map[string]any

	for _, x := range stringSlice(v) {
		_ = x
	}

	if a, ok := v.([]any); ok {
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				r = append(r, m)
			}
		}
	}

	return r
}
