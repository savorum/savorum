package library

// This file contains the source refresh model. Import history owns the base
// snapshot; recipe revisions remain ordinary user-visible revisions.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// ErrNoSourceBaseline reports that an entry has no imported snapshot to compare against.
var ErrNoSourceBaseline = errors.New("recipe has no source-import baseline")

// RefreshConflict locates a value changed differently in the local and upstream recipes.
type RefreshConflict struct {
	Path   string `json:"path"`
	Area   string `json:"area"`
	Base   any    `json:"base,omitempty"`
	Local  any    `json:"local,omitempty"`
	Remote any    `json:"remote,omitempty"`
}

// SourceRefreshPreview holds a three-way comparison against the last imported snapshot.
type SourceRefreshPreview struct {
	ID               string            `json:"id"`
	EntryID          EntryID           `json:"entry_id"`
	ExpectedRevision uint64            `json:"expected_revision"`
	BaseRevision     uint64            `json:"base_revision"`
	Base             recipe.Document   `json:"base"`
	Local            recipe.Document   `json:"local"`
	Remote           recipe.Document   `json:"remote"`
	Merged           recipe.Document   `json:"merged"`
	Conflicts        []RefreshConflict `json:"conflicts"`
	Summary          []string          `json:"summary"`
}

// RefreshChoice selects which side of a source-refresh conflict to retain.
type RefreshChoice string

// Choices available when resolving an upstream-refresh conflict.
const (
	RefreshUseLocal  RefreshChoice = "local"
	RefreshUseRemote RefreshChoice = "remote"
)

// RefreshResolution selects the local or upstream value at one conflicting path.
type RefreshResolution struct {
	Path   string        `json:"path"`
	Choice RefreshChoice `json:"choice"`
}

// CheckSourceUpdates previews upstream changes against the expected local revision.
func (s *Service) CheckSourceUpdates(ctx context.Context, id EntryID, remote recipe.Document, expectedRevision uint64) (SourceRefreshPreview, error) {
	entry, err := s.repository.Get(ctx, id, StateActive)
	if err != nil {
		return SourceRefreshPreview{}, err
	}

	if expectedRevision == 0 {
		expectedRevision = entry.Revision
	}

	if entry.Revision != expectedRevision {
		return SourceRefreshPreview{}, &PreconditionError{EntryID: id, Expected: expectedRevision, Current: entry.Revision}
	}

	if remote.ID != entry.Recipe.ID {
		return SourceRefreshPreview{}, ErrDocumentIdentityChange
	}

	var (
		baseline     *recipe.Document
		baseRevision uint64
	)

	if repository, ok := s.repository.(SourceImportBaselineRepository); ok {
		stored, baselineErr := repository.GetSourceImportBaseline(ctx, id)
		if baselineErr == nil {
			document := stored.Document
			baseline = &document
			baseRevision = stored.Revision
		} else if !errors.Is(baselineErr, ErrNoSourceBaseline) {
			return SourceRefreshPreview{}, baselineErr
		}
	}

	if baseline == nil {
		events, err := s.ListImportEvents(ctx, id, RevisionListOptions{Limit: 100})
		if err != nil {
			return SourceRefreshPreview{}, err
		}

		for _, event := range events {
			if event.SourceSnapshot != nil {
				snapshot := *event.SourceSnapshot
				baseline = &snapshot
				baseRevision = event.BaselineRevision

				break
			}
		}
	}

	if baseline == nil {
		return SourceRefreshPreview{}, ErrNoSourceBaseline
	}

	if err := validateDocumentEncoding(*baseline, entry.Recipe, remote); err != nil {
		return SourceRefreshPreview{}, err
	}

	merged, conflicts, summary := threeWayDocument(*baseline, entry.Recipe, remote)

	digest, err := json.Marshal(remote)
	if err != nil {
		return SourceRefreshPreview{}, fmt.Errorf("encode remote recipe for refresh identity: %w", err)
	}

	hash := sha256.Sum256(digest)

	return SourceRefreshPreview{ID: string(id) + ":" + fmt.Sprint(expectedRevision) + ":" + hex.EncodeToString(hash[:8]), EntryID: id, ExpectedRevision: expectedRevision, BaseRevision: baseRevision, Base: *baseline, Local: entry.Recipe, Remote: remote, Merged: merged, Conflicts: conflicts, Summary: summary}, nil
}

// ApplySourceRefresh commits resolved upstream changes after checking the preview's revision.
func (s *Service) ApplySourceRefresh(ctx context.Context, preview SourceRefreshPreview, resolutions []RefreshResolution) (Entry, error) {
	entry, err := s.repository.Get(ctx, preview.EntryID, StateActive)
	if err != nil {
		return Entry{}, err
	}

	if err := validateDocumentEncoding(entry.Recipe, preview.Local, preview.Remote, preview.Merged); err != nil {
		return Entry{}, err
	}

	if entry.Revision != preview.ExpectedRevision || !semanticEqual(entry.Recipe, preview.Local) {
		return Entry{}, fmt.Errorf("%w: refresh preview is stale", ErrImportTargetChanged)
	}

	choices := make(map[string]RefreshChoice, len(resolutions))
	for _, resolution := range resolutions {
		choices[resolution.Path] = resolution.Choice
	}

	document := preview.Merged
	for _, conflict := range preview.Conflicts {
		choice, ok := choices[conflict.Path]
		if !ok {
			return Entry{}, fmt.Errorf("%w: conflict %s requires a choice", ErrConflict, conflict.Path)
		}

		if choice != RefreshUseLocal && choice != RefreshUseRemote {
			return Entry{}, fmt.Errorf("%w: invalid refresh choice for %s", ErrInvalidOrganization, conflict.Path)
		}

		if err := setDocumentPath(&document, conflict.Path, choice == RefreshUseRemote, preview.Local, preview.Remote); err != nil {
			return Entry{}, err
		}
	}

	if err := s.validate(document, s.units); err != nil {
		return Entry{}, err
	}

	snapshot := preview.Remote
	expected := preview.Local
	mut := ImportMutation{ID: preview.EntryID, ExpectedRevision: preview.ExpectedRevision, ExpectedRecipe: &expected, Recipe: document, SourceSnapshot: &snapshot, Timestamp: s.now().UTC()}
	mut.History = ImportContext{InputKind: "source-refresh", DisplayName: string(preview.EntryID), Importer: "savorum-refresh", ImporterVersion: "1"}

	changed, err := s.repository.ImportMany(ctx, []ImportMutation{mut})
	if err != nil {
		return Entry{}, err
	}

	if len(changed) != 1 {
		return Entry{}, errors.New("refresh did not produce one revision")
	}

	return changed[0], nil
}

func setDocumentPath(document *recipe.Document, path string, useRemote bool, local, remote recipe.Document) error {
	if !strings.ContainsAny(path, "[.") {
		return setDocumentField(document, path, useRemote, local, remote)
	}

	current, source := rawDocumentFields(*document), rawDocumentFields(local)
	if useRemote {
		source = rawDocumentFields(remote)
	}

	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '.' || r == '[' || r == ']' })
	if len(parts) < 2 {
		return fmt.Errorf("%w: refresh path %q is invalid", ErrInvalidOrganization, path)
	}

	var walk func(map[string]any, []string) (any, bool)

	walk = func(value map[string]any, tokens []string) (any, bool) {
		if len(tokens) == 0 {
			return value, true
		}

		next, ok := value[tokens[0]]
		if !ok {
			return nil, false
		}

		if len(tokens) == 1 {
			return next, true
		}

		if child, ok := next.(map[string]any); ok {
			return walk(child, tokens[1:])
		}

		if list, ok := next.([]any); ok {
			for _, member := range list {
				item, ok := member.(map[string]any)
				if ok && fmt.Sprint(item["id"]) == tokens[1] {
					return walk(item, tokens[2:])
				}
			}

			return nil, false
		}

		return nil, false
	}

	value, ok := walk(source, parts)
	if !ok {
		if removeDocumentPath(current, parts) {
			// current came from this document and goes straight back into it,
			// so the round trip cannot fail on shape.
			_ = replaceDocumentFromMap(document, current)

			return nil
		}

		return fmt.Errorf("%w: refresh path %q is unavailable", ErrInvalidOrganization, path)
	}

	if value == nil {
		if removeDocumentPath(current, parts) {
			// current came from this document and goes straight back into it,
			// so the round trip cannot fail on shape.
			_ = replaceDocumentFromMap(document, current)

			return nil
		}

		return fmt.Errorf("%w: refresh path %q is unavailable", ErrInvalidOrganization, path)
	}

	if !assignDocumentPath(current, parts, value) {
		return fmt.Errorf("%w: refresh path %q is unavailable", ErrInvalidOrganization, path)
	}

	return replaceDocumentFromMap(document, current)
}

func replaceDocumentFromMap(document *recipe.Document, value map[string]any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode refreshed recipe fields: %w", err)
	}

	var replacement recipe.Document
	if err := json.Unmarshal(encoded, &replacement); err != nil {
		return fmt.Errorf("decode refreshed recipe fields: %w", err)
	}

	*document = replacement

	return nil
}

func assignDocumentPath(root map[string]any, parts []string, value any) bool {
	if len(parts) == 1 {
		root[parts[0]] = value
		return true
	}

	next, ok := root[parts[0]]
	if !ok {
		return false
	}

	if child, ok := next.(map[string]any); ok {
		return assignDocumentPath(child, parts[1:], value)
	}

	if list, ok := next.([]any); ok {
		for index, member := range list {
			item, ok := member.(map[string]any)
			if !ok || fmt.Sprint(item["id"]) != parts[1] {
				continue
			}

			if len(parts) == 2 {
				list[index] = value
				return true
			}

			return assignDocumentPath(item, parts[2:], value)
		}

		// The chosen side kept an element this document no longer has.
		if len(parts) == 2 {
			root[parts[0]] = append(list, value)
			return true
		}
	}

	return false
}

// removeDocumentPath removes a missing remote/local leaf, or its containing
// stable-ID object when the object itself was deleted on that side.
func removeDocumentPath(root map[string]any, parts []string) bool {
	if len(parts) == 0 {
		return false
	}

	if len(parts) == 1 {
		if _, ok := root[parts[0]]; ok {
			delete(root, parts[0])
			return true
		}

		return false
	}

	next, ok := root[parts[0]]
	if !ok {
		return false
	}

	if child, ok := next.(map[string]any); ok {
		return removeDocumentPath(child, parts[1:])
	}

	if list, ok := next.([]any); ok {
		for index, member := range list {
			item, ok := member.(map[string]any)
			if !ok || fmt.Sprint(item["id"]) != parts[1] {
				continue
			}

			if len(parts) == 2 {
				root[parts[0]] = append(list[:index], list[index+1:]...)
				return true
			}

			return removeDocumentPath(item, parts[2:])
		}
	}

	return false
}

func threeWayDocument(base, local, remote recipe.Document) (recipe.Document, []RefreshConflict, []string) {
	merged := local
	baseFields, localFields, remoteFields := semanticFields(base), semanticFields(local), semanticFields(remote)
	rawBase, rawLocal, rawRemote := rawDocumentFields(base), rawDocumentFields(local), rawDocumentFields(remote)
	keys := make([]string, 0, len(remoteFields))

	seen := map[string]bool{}
	for key := range baseFields {
		seen[key] = true
		keys = append(keys, key)
	}

	for key := range localFields {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}

	for key := range remoteFields {
		if !seen[key] {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	var (
		conflicts []RefreshConflict
		summary   []string
	)

	for _, key := range keys {
		if key == "id" || key == "schema_version" {
			continue
		}

		b, l, r := baseFields[key], localFields[key], remoteFields[key]
		if key == "ingredient_sections" || key == "instruction_sections" || key == "assets" {
			value, nested := mergeStableJSON(rawBase[key], rawLocal[key], rawRemote[key], key)
			// Every value here was decomposed from a recipe.Document and is
			// written back to the same member of one, so it cannot fail to fit.
			_ = setDocumentFieldValue(&merged, key, value)

			conflicts = append(conflicts, nested...)
			if !semanticAnyEqual(l, r) {
				summary = append(summary, "reconciled "+key)
			}

			continue
		}

		lc, rc := !semanticAnyEqual(l, b), !semanticAnyEqual(r, b)
		switch {
		case !lc && rc:
			_ = setDocumentFieldValue(&merged, key, r)

			summary = append(summary, "source changed "+key)
		case lc && !rc:
			summary = append(summary, "kept local "+key)
		case lc && rc && !semanticAnyEqual(l, r):
			conflicts = append(conflicts, RefreshConflict{Path: key, Area: fieldArea(key), Base: b, Local: l, Remote: r})
		case lc && rc:
			_ = setDocumentFieldValue(&merged, key, r)
		}
	}

	return merged, conflicts, summary
}

func rawDocumentFields(document recipe.Document) map[string]any {
	// Public merge and refresh boundaries validate that this lossless generic
	// decode succeeds before calling helpers that compare or copy these fields.
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil
	}

	result := map[string]any{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil
	}

	return result
}

// mergeStableJSON recursively applies three-way rules to maps and arrays whose
// members carry stable IDs. It keeps the chosen side's order, while matching
// content by identity so reordering does not turn unrelated edits into a
// conflict.
func mergeStableJSON(base, local, remote any, path string) (any, []RefreshConflict) {
	if semanticAnyEqual(local, base) {
		return remote, nil
	}

	if semanticAnyEqual(remote, base) || semanticAnyEqual(local, remote) {
		return local, nil
	}

	if bm, ok := base.(map[string]any); ok {
		lm, lok := local.(map[string]any)

		rm, rok := remote.(map[string]any)
		if !lok || !rok {
			return local, []RefreshConflict{{Path: path, Area: fieldArea(path), Base: base, Local: local, Remote: remote}}
		}

		result := map[string]any{}
		maps.Copy(result, lm)

		var conflicts []RefreshConflict

		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}

		for k := range lm {
			keys[k] = true
		}

		for k := range rm {
			keys[k] = true
		}

		ordered := make([]string, 0, len(keys))
		for k := range keys {
			ordered = append(ordered, k)
		}

		sort.Strings(ordered)

		for _, k := range ordered {
			value, c := mergeStableJSON(bm[k], lm[k], rm[k], path+"."+k)
			result[k] = value

			conflicts = append(conflicts, c...)
		}

		return result, conflicts
	}

	if ba, ok := base.([]any); ok {
		la, lok := local.([]any)

		ra, rok := remote.([]any)
		if !lok || !rok {
			return local, []RefreshConflict{{Path: path, Area: fieldArea(path), Base: base, Local: local, Remote: remote}}
		}

		if stableArray(ba) && stableArray(la) && stableArray(ra) {
			return mergeStableArray(ba, la, ra, path)
		}

		return local, []RefreshConflict{{Path: path, Area: fieldArea(path), Base: base, Local: local, Remote: remote}}
	}

	return local, []RefreshConflict{{Path: path, Area: fieldArea(path), Base: base, Local: local, Remote: remote}}
}

func stableArray(values []any) bool {
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok || item["id"] == nil {
			return false
		}
	}

	return true
}

func mergeStableArray(base, local, remote []any, path string) ([]any, []RefreshConflict) {
	bm, lm, rm := indexStableArray(base), indexStableArray(local), indexStableArray(remote)

	ids := map[string]bool{}
	for id := range bm {
		ids[id] = true
	}

	for id := range lm {
		ids[id] = true
	}

	for id := range rm {
		ids[id] = true
	}

	localChanged, remoteChanged := !semanticAnyEqual(local, base), !semanticAnyEqual(remote, base)

	chosenOrder := local
	if !localChanged && remoteChanged {
		chosenOrder = remote
	}

	result := make([]any, 0, len(ids))
	used := map[string]bool{}

	var conflicts []RefreshConflict
	if localChanged && remoteChanged && !stableOrderEqual(local, remote) && stableMembersEqual(local, remote) {
		conflicts = append(conflicts, RefreshConflict{Path: path, Area: fieldArea(path), Base: base, Local: local, Remote: remote})
	}

	for _, value := range chosenOrder {
		item, isObject := value.(map[string]any)
		if !isObject {
			continue
		}

		id := fmt.Sprint(item["id"])

		merged, c := mergeStableArrayItem(id, path, bm, lm, rm)

		if merged != nil {
			result = append(result, merged)
		}

		conflicts = append(conflicts, c...)
		used[id] = true
	}

	for id := range ids {
		if used[id] {
			continue
		}

		merged, c := mergeStableArrayItem(id, path, bm, lm, rm)

		if merged != nil {
			result = append(result, merged)
		}

		conflicts = append(conflicts, c...)
	}

	return result, conflicts
}

func indexStableArray(values []any) map[string]any {
	result := map[string]any{}

	for _, value := range values {
		item, isObject := value.(map[string]any)
		if isObject {
			result[fmt.Sprint(item["id"])] = item
		}
	}

	return result
}

func mergeStableArrayItem(id, path string, base, local, remote map[string]any) (any, []RefreshConflict) {
	merged, conflicts := mergeStableJSON(base[id], local[id], remote[id], path+"["+id+"]")
	// Without a conflict, nil means the member was deleted.
	if merged != nil || len(conflicts) == 0 {
		return merged, conflicts
	}

	if local[id] != nil {
		return local[id], conflicts
	}

	if remote[id] != nil {
		return remote[id], conflicts
	}

	return base[id], conflicts
}

func stableOrderEqual(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		lm, lok := left[index].(map[string]any)

		rm, rok := right[index].(map[string]any)
		if !lok || !rok || fmt.Sprint(lm["id"]) != fmt.Sprint(rm["id"]) {
			return false
		}
	}

	return true
}

func stableMembersEqual(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}

	byID := func(values []any) map[string]any {
		result := make(map[string]any, len(values))
		for _, value := range values {
			member, ok := value.(map[string]any)
			if !ok {
				return nil
			}

			result[fmt.Sprint(member["id"])] = member
		}

		return result
	}

	leftByID, rightByID := byID(left), byID(right)
	if leftByID == nil || rightByID == nil {
		return false
	}

	for id, member := range leftByID {
		other, ok := rightByID[id]
		if !ok || !semanticAnyEqual(member, other) {
			return false
		}
	}

	return true
}

func semanticFields(document recipe.Document) map[string]any {
	// Public merge and refresh boundaries validate the document's generic JSON
	// representation before semantic comparison reaches this private helper.
	canonical := canonicalDocument(document)

	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil
	}

	result := map[string]any{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil
	}

	return result
}

func semanticAnyEqual(a, b any) bool {
	ea, err := json.Marshal(a)
	if err != nil {
		return false
	}

	eb, err := json.Marshal(b)
	if err != nil {
		return false
	}

	return string(ea) == string(eb)
}

func semanticEqual(a, b recipe.Document) bool {
	aFields, bFields := semanticFields(a), semanticFields(b)
	if aFields == nil || bFields == nil {
		return false
	}

	return semanticAnyEqual(aFields, bFields)
}

func validateDocumentEncoding(documents ...recipe.Document) error {
	for index, document := range documents {
		encoded, err := json.Marshal(document)
		if err != nil {
			return fmt.Errorf("encode recipe %d for field comparison: %w", index+1, err)
		}

		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return fmt.Errorf("decode recipe %d for field comparison: %w", index+1, err)
		}
	}

	return nil
}

func canonicalDocument(document recipe.Document) recipe.Document {
	encoded, err := json.Marshal(document)
	if err != nil {
		return document
	}

	var out recipe.Document

	if err := json.Unmarshal(encoded, &out); err != nil {
		return document
	}

	sort.SliceStable(out.IngredientSections, func(i, j int) bool { return out.IngredientSections[i].ID < out.IngredientSections[j].ID })

	for i := range out.IngredientSections {
		sort.SliceStable(out.IngredientSections[i].Ingredients, func(a, b int) bool {
			return out.IngredientSections[i].Ingredients[a].ID < out.IngredientSections[i].Ingredients[b].ID
		})
	}

	sort.SliceStable(out.InstructionSections, func(i, j int) bool { return out.InstructionSections[i].ID < out.InstructionSections[j].ID })

	for i := range out.InstructionSections {
		sort.SliceStable(out.InstructionSections[i].Steps, func(a, b int) bool {
			return out.InstructionSections[i].Steps[a].ID < out.InstructionSections[i].Steps[b].ID
		})
	}

	sort.SliceStable(out.Assets, func(i, j int) bool { return out.Assets[i].ID < out.Assets[j].ID })

	for _, values := range [][]string{out.Categories, out.Courses, out.Keywords, out.Cuisines, out.CookingMethods} {
		sort.Strings(values)
	}

	return out
}

// documentAreas names the part of a recipe each portable document field
// belongs to, in a stable vocabulary clients label for readers.
var documentAreas = map[string]string{
	"assets":                AreaPhotos,
	"authors":               AreaSource,
	"categories":            AreaLabels,
	"cooking_methods":       AreaLabels,
	"courses":               AreaLabels,
	"cuisines":              AreaLabels,
	"description":           AreaDescription,
	"equipment":             AreaEquipment,
	"image_refs":            AreaPhotos,
	"ingredient_sections":   AreaIngredients,
	"instruction_sections":  AreaInstructions,
	"keywords":              AreaLabels,
	"language":              AreaLanguage,
	"modified":              AreaSource,
	"notes":                 AreaNotes,
	"primary_image":         AreaPhotos,
	"published":             AreaSource,
	"source":                AreaSource,
	"source_locale":         AreaLanguage,
	"times":                 AreaTimes,
	"title":                 AreaTitle,
	"translations":          AreaTranslations,
	"variants":              AreaVariants,
	"yields":                AreaYield,
	"nutrition":             AreaNutrition,
	"dietary_claims":        AreaClaims,
	"allergen_declarations": AreaClaims,
}

// The recipe areas a conflict or a change can belong to.
const (
	AreaTitle        = "title"
	AreaDescription  = "description"
	AreaYield        = "yield"
	AreaNutrition    = "nutrition"
	AreaClaims       = "claims"
	AreaTimes        = "times"
	AreaIngredients  = "ingredients"
	AreaInstructions = "instructions"
	AreaNotes        = "notes"
	AreaVariants     = "variants"
	AreaEquipment    = "equipment"
	AreaPhotos       = "photos"
	AreaSource       = "source"
	AreaLabels       = "labels"
	AreaLanguage     = "language"
	AreaTranslations = "translations"
	AreaOther        = "other"
)

// DocumentArea names the recipe area a document field path belongs to. Nested
// paths are named by the field they descend from, so an ingredient's note is
// still an ingredients change.
func DocumentArea(path string) string {
	root := path
	if index := strings.IndexAny(root, ".["); index >= 0 {
		root = root[:index]
	}

	if area, ok := documentAreas[root]; ok {
		return area
	}

	return AreaOther
}

func fieldArea(path string) string {
	return DocumentArea(path)
}

func setDocumentFieldValue(document *recipe.Document, path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode refreshed field %q: %w", path, err)
	}

	return setDocumentFieldJSON(document, path, encoded)
}

func setDocumentField(document *recipe.Document, path string, remote bool, local, rem recipe.Document) error {
	fields := rawDocumentFields(local)
	if !remote {
		return setDocumentFieldValue(document, path, fields[path])
	}

	return setDocumentFieldValue(document, path, rawDocumentFields(rem)[path])
}

func setDocumentFieldJSON(document *recipe.Document, path string, encoded []byte) error {
	v := reflect.ValueOf(document).Elem()

	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if tag == path {
			target := v.Field(i)

			value := reflect.New(target.Type())
			if err := json.Unmarshal(encoded, value.Interface()); err != nil {
				return fmt.Errorf("decode refreshed field %q: %w", path, err)
			}

			target.Set(value.Elem())

			return nil
		}
	}

	return fmt.Errorf("%w: refresh field %q is unsupported", ErrInvalidOrganization, path)
}
