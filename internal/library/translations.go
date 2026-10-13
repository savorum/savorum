package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/recipe"
)

// Translation errors distinguish a missing precondition from a refused link.
var (
	ErrTranslationUnsupported      = errors.New("this library cannot relate translations")
	ErrTranslationLanguageRequired = errors.New("both recipes need a language before they can be related as translations")
	ErrTranslationSelf             = errors.New("a recipe cannot be its own translation")
	ErrTranslationNotLinked        = errors.New("the recipes do not name each other as translations")
)

// TranslationRepository reads the recipes that take part in translation
// relationships and applies several revisions atomically.
type TranslationRepository interface {
	// TranslationMembers returns every active recipe that declares a
	// relationship by recipe ID or is named by one.
	TranslationMembers(ctx context.Context) ([]TranslationMember, error)
	ReviseMany(ctx context.Context, commands []RevisionCommand) ([]Entry, error)
}

// TranslationMember is one active library recipe in a translation relationship.
type TranslationMember struct {
	ID         EntryID
	DocumentID recipe.RecipeID
	Title      string
	Language   string
	CreatedAt  time.Time
	Links      []TranslationLink
}

// Rendition is another recipe in a translation set, described from the
// viewpoint of the recipe being read.
type Rendition struct {
	ID       EntryID
	Title    string
	Language string
	// Relation is how the rendition relates to the recipe being read.
	// Renditions reached only through another rendition are alternates.
	Relation recipe.TranslationRelation
	// Linked reports that one of the two recipes names the other, so the
	// relationship can be removed from either.
	Linked bool
}

// TranslationSet is what a library knows about the other renditions of one recipe.
type TranslationSet struct {
	Renditions []Rendition
	// Original is the rendition the set declares as its original, or empty.
	Original EntryID
	// Unresolved are the recipe's own declarations that name nothing in this library.
	Unresolved []recipe.Translation
}

// TranslationSets groups members into connected sets of two or more recipes.
// Declarations from either side connect two recipes.
func TranslationSets(members []TranslationMember) [][]TranslationMember {
	byDocument := make(map[recipe.RecipeID]int, len(members))
	for index, member := range members {
		byDocument[member.DocumentID] = index
	}

	parent := make([]int, len(members))
	for index := range parent {
		parent[index] = index
	}

	var find func(int) int

	find = func(index int) int {
		if parent[index] != index {
			parent[index] = find(parent[index])
		}

		return parent[index]
	}

	for index, member := range members {
		for _, link := range member.Links {
			if target, ok := byDocument[link.RecipeID]; ok && target != index {
				parent[find(target)] = find(index)
			}
		}
	}

	grouped := map[int][]TranslationMember{}
	order := []int{}

	for index, member := range members {
		root := find(index)
		if _, seen := grouped[root]; !seen {
			order = append(order, root)
		}

		grouped[root] = append(grouped[root], member)
	}

	sets := make([][]TranslationMember, 0, len(order))
	for _, root := range order {
		if len(grouped[root]) > 1 {
			sets = append(sets, grouped[root])
		}
	}

	return sets
}

// SetOriginal returns the member a set declares as its original: the one
// every declaration of direction agrees on. Otherwise it returns empty.
func SetOriginal(set []TranslationMember) EntryID {
	byDocument := make(map[recipe.RecipeID]EntryID, len(set))
	for _, member := range set {
		byDocument[member.DocumentID] = member.ID
	}

	originals := map[EntryID]bool{}

	for _, member := range set {
		for _, link := range member.Links {
			target, ok := byDocument[link.RecipeID]
			if !ok {
				continue
			}

			switch link.Relation {
			case recipe.TranslationOriginal:
				originals[target] = true
			case recipe.TranslationOf:
				originals[member.ID] = true
			case recipe.TranslationAlternate:
			}
		}
	}

	if len(originals) != 1 {
		return ""
	}

	for id := range originals {
		return id
	}

	return ""
}

// PreferredRendition chooses which member of a set to present first. The
// person's content languages are tried in order, allowing the closest
// language match; without one, the set's original, and then its earliest
// recipe, is chosen. Explicit selection of a recipe never passes through here.
func PreferredRendition(set []TranslationMember, preferences []string) EntryID {
	if len(set) == 0 {
		return ""
	}

	supported := make([]language.Tag, 0, len(set))
	candidates := make([]int, 0, len(set))

	for index, member := range set {
		tag, err := language.Parse(member.Language)
		if err != nil {
			continue
		}

		supported = append(supported, tag)
		candidates = append(candidates, index)
	}

	desired := make([]language.Tag, 0, len(preferences))

	for _, preference := range preferences {
		if tag, err := language.Parse(preference); err == nil {
			desired = append(desired, tag)
		}
	}

	if len(supported) > 0 && len(desired) > 0 {
		_, index, confidence := language.NewMatcher(supported).Match(desired...)
		if confidence != language.No {
			return set[candidates[index]].ID
		}
	}

	if original := SetOriginal(set); original != "" {
		return original
	}

	earliest := set[0]
	for _, member := range set[1:] {
		if member.CreatedAt.Before(earliest.CreatedAt) || (member.CreatedAt.Equal(earliest.CreatedAt) && member.ID < earliest.ID) {
			earliest = member
		}
	}

	return earliest.ID
}

// renditionsOf describes a set from one member's viewpoint.
func renditionsOf(set []TranslationMember, id EntryID) []Rendition {
	var self TranslationMember

	for _, member := range set {
		if member.ID == id {
			self = member
		}
	}

	renditions := make([]Rendition, 0, len(set)-1)

	for _, member := range set {
		if member.ID == id {
			continue
		}

		rendition := Rendition{ID: member.ID, Title: member.Title, Language: member.Language, Relation: recipe.TranslationAlternate}

		for _, link := range self.Links {
			if link.RecipeID == member.DocumentID {
				rendition.Linked, rendition.Relation = true, link.Relation
			}
		}

		for _, link := range member.Links {
			if link.RecipeID == self.DocumentID {
				rendition.Linked = true
				if rendition.Relation == recipe.TranslationAlternate {
					rendition.Relation = inverseRelation(link.Relation)
				}
			}
		}

		renditions = append(renditions, rendition)
	}

	return renditions
}

func inverseRelation(relation recipe.TranslationRelation) recipe.TranslationRelation {
	switch relation {
	case recipe.TranslationOriginal:
		return recipe.TranslationOf
	case recipe.TranslationOf:
		return recipe.TranslationOriginal
	case recipe.TranslationAlternate:
		return recipe.TranslationAlternate
	default:
		return recipe.TranslationAlternate
	}
}

func (s *Service) translationRepository() (TranslationRepository, error) {
	repository, ok := s.repository.(TranslationRepository)
	if !ok {
		return nil, ErrTranslationUnsupported
	}

	return repository, nil
}

// Translations reports the other renditions of an active recipe.
func (s *Service) Translations(ctx context.Context, id EntryID) (TranslationSet, error) {
	entry, err := s.Get(ctx, id)
	if err != nil {
		return TranslationSet{}, err
	}

	repository, err := s.translationRepository()
	if err != nil {
		return TranslationSet{}, err
	}

	members, err := repository.TranslationMembers(ctx)
	if err != nil {
		return TranslationSet{}, fmt.Errorf("read translation members: %w", err)
	}

	result := TranslationSet{}
	resolved := map[recipe.RecipeID]bool{}

	for _, set := range TranslationSets(members) {
		if !slices.ContainsFunc(set, func(member TranslationMember) bool { return member.ID == entry.ID }) {
			continue
		}

		result.Renditions = renditionsOf(set, entry.ID)
		result.Original = SetOriginal(set)

		for _, member := range set {
			resolved[member.DocumentID] = true
		}
	}

	for _, declared := range entry.Recipe.Translations {
		if declared.RecipeID == "" || !resolved[declared.RecipeID] {
			result.Unresolved = append(result.Unresolved, declared)
		}
	}

	return result, nil
}

// LinkTranslation records that two library recipes are renditions of each
// other. relation says how the counterpart relates to the recipe named by id.
// Both documents declare the relationship; neither's content changes.
func (s *Service) LinkTranslation(ctx context.Context, id, counterpart EntryID, relation recipe.TranslationRelation) ([]Entry, error) {
	if id == counterpart {
		return nil, ErrTranslationSelf
	}

	repository, err := s.translationRepository()
	if err != nil {
		return nil, err
	}

	entry, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	other, err := s.Get(ctx, counterpart)
	if err != nil {
		return nil, err
	}

	if entry.Recipe.Language == "" || other.Recipe.Language == "" {
		return nil, ErrTranslationLanguageRequired
	}

	commands := make([]RevisionCommand, 0, 2)

	for _, change := range []struct {
		entry     Entry
		declared  recipe.Translation
		otherSide Entry
	}{
		{entry, declaration(other, relation), other},
		{other, declaration(entry, inverseRelation(relation)), entry},
	} {
		document := withDeclaration(change.entry.Recipe, change.declared)
		if equalTranslations(document.Translations, change.entry.Recipe.Translations) {
			continue
		}

		if err := s.validate(document, s.units); err != nil {
			return nil, err
		}

		commands = append(commands, RevisionCommand{
			ID: change.entry.ID, ExpectedRevision: change.entry.Revision, Recipe: document,
			CurrentState: StateActive, TargetState: StateActive,
			Metadata: RevisionMetadata{Cause: RevisionCauseManualEdit},
		})
	}

	return s.reviseMany(ctx, repository, commands)
}

// UnlinkTranslation removes the declarations two recipes make about each
// other. Renditions related only through a third recipe are not linked.
func (s *Service) UnlinkTranslation(ctx context.Context, id, counterpart EntryID) ([]Entry, error) {
	repository, err := s.translationRepository()
	if err != nil {
		return nil, err
	}

	entry, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	other, err := s.Get(ctx, counterpart)
	if err != nil {
		return nil, err
	}

	commands := make([]RevisionCommand, 0, 2)

	for _, pair := range [][2]Entry{{entry, other}, {other, entry}} {
		document := pair[0].Recipe
		document.Translations = slices.DeleteFunc(slices.Clone(document.Translations), func(declared recipe.Translation) bool {
			return declared.RecipeID == pair[1].Recipe.ID
		})

		if len(document.Translations) == 0 {
			document.Translations = nil
		}

		if equalTranslations(document.Translations, pair[0].Recipe.Translations) {
			continue
		}

		commands = append(commands, RevisionCommand{
			ID: pair[0].ID, ExpectedRevision: pair[0].Revision, Recipe: document,
			CurrentState: StateActive, TargetState: StateActive,
			Metadata: RevisionMetadata{Cause: RevisionCauseManualEdit},
		})
	}

	if len(commands) == 0 {
		return nil, ErrTranslationNotLinked
	}

	return s.reviseMany(ctx, repository, commands)
}

// TranslationDraft describes the unsaved start of a new translation.
type TranslationDraft struct {
	Language string
	Title    string
	// CopyContent starts the draft from a copy of the source recipe's portable
	// content. The copy is ordinary text in the source language, with no
	// binding to the source afterwards.
	CopyContent bool
}

// DraftTranslation returns an unsaved document that starts a translation of an
// active recipe. Nothing is stored until CreateTranslation.
func (s *Service) DraftTranslation(ctx context.Context, id EntryID, request TranslationDraft) (recipe.Document, error) {
	source, err := s.Get(ctx, id)
	if err != nil {
		return recipe.Document{}, err
	}

	documentID, err := s.newID()
	if err != nil {
		return recipe.Document{}, fmt.Errorf("generate recipe document ID: %w", err)
	}

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            recipe.RecipeID(documentID),
		Title:         strings.TrimSpace(request.Title),
		Language:      strings.TrimSpace(request.Language),
	}

	if request.CopyContent {
		copied, err := deepCopy(source.Recipe)
		if err != nil {
			return recipe.Document{}, err
		}
		// Library state, source attribution, and relationships belong to the
		// source recipe and are not copied.
		document.Description = copied.Description
		document.Yields = copied.Yields
		document.Nutrition = copied.Nutrition
		document.DietaryClaims = copied.DietaryClaims
		document.AllergenDeclarations = copied.AllergenDeclarations
		document.Times = copied.Times
		document.Notes = copied.Notes
		document.Variants = copied.Variants
		document.Equipment = copied.Equipment
		document.IngredientSections = copied.IngredientSections
		document.InstructionSections = copied.InstructionSections
		document.Assets = copied.Assets
		document.ImageRefs = copied.ImageRefs
		document.PrimaryImage = copied.PrimaryImage
	}

	if source.Recipe.Language != "" {
		document.Translations = []recipe.Translation{declaration(source, recipe.TranslationOriginal)}
	}

	return document, nil
}

// CreateTranslation stores a new recipe written as a translation of an active
// recipe and links the two. A relationship the new document already declares
// with the source is kept; otherwise the source is recorded as its original.
func (s *Service) CreateTranslation(ctx context.Context, id EntryID, document recipe.Document) (Entry, error) {
	repository, err := s.translationRepository()
	if err != nil {
		return Entry{}, err
	}

	source, err := s.Get(ctx, id)
	if err != nil {
		return Entry{}, err
	}

	if source.Recipe.Language == "" || document.Language == "" {
		return Entry{}, ErrTranslationLanguageRequired
	}

	if document.ID == source.Recipe.ID {
		return Entry{}, ErrTranslationSelf
	}

	relation := recipe.TranslationOriginal

	for _, declared := range document.Translations {
		if declared.RecipeID == source.Recipe.ID {
			relation = declared.Relation
		}
	}

	document = withDeclaration(document, declaration(source, relation))

	created, err := s.Create(ctx, document)
	if err != nil {
		return Entry{}, err
	}
	// The new recipe already names its source, which relates the two on its
	// own, so a source that changed meanwhile keeps the relationship one-sided
	// rather than failing the translation that was just saved.
	_ = s.declareReverse(ctx, repository, id, created, inverseRelation(relation))

	return created, nil
}

func (s *Service) declareReverse(ctx context.Context, repository TranslationRepository, id EntryID, created Entry, relation recipe.TranslationRelation) error {
	source, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	updated := withDeclaration(source.Recipe, declaration(created, relation))
	if err := s.validate(updated, s.units); err != nil {
		return err
	}

	_, err = s.reviseMany(ctx, repository, []RevisionCommand{{
		ID: source.ID, ExpectedRevision: source.Revision, Recipe: updated,
		CurrentState: StateActive, TargetState: StateActive,
		Metadata: RevisionMetadata{Cause: RevisionCauseManualEdit},
	}})

	return err
}

func deepCopy(document recipe.Document) (recipe.Document, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return recipe.Document{}, fmt.Errorf("copy recipe content: %w", err)
	}

	var copied recipe.Document
	if err := json.Unmarshal(encoded, &copied); err != nil {
		return recipe.Document{}, fmt.Errorf("copy recipe content: %w", err)
	}

	return copied, nil
}

func declaration(counterpart Entry, relation recipe.TranslationRelation) recipe.Translation {
	return recipe.Translation{
		Relation: relation, Language: counterpart.Recipe.Language,
		Title: counterpart.Recipe.Title, RecipeID: counterpart.Recipe.ID,
	}
}

// withDeclaration replaces any declaration naming the same recipe, keeping
// the position of the first one.
func withDeclaration(document recipe.Document, declared recipe.Translation) recipe.Document {
	translations := make([]recipe.Translation, 0, len(document.Translations)+1)
	placed := false

	for _, existing := range document.Translations {
		if existing.RecipeID != declared.RecipeID {
			translations = append(translations, existing)
			continue
		}

		if !placed {
			translations = append(translations, declared)
			placed = true
		}
	}

	if !placed {
		translations = append(translations, declared)
	}

	document.Translations = translations

	return document
}

func equalTranslations(left, right []recipe.Translation) bool {
	return slices.Equal(left, right)
}

func (s *Service) reviseMany(ctx context.Context, repository TranslationRepository, commands []RevisionCommand) ([]Entry, error) {
	if len(commands) == 0 {
		return []Entry{}, nil
	}

	timestamp := s.now().UTC()
	for index := range commands {
		commands[index].Timestamp = timestamp
	}

	entries, err := repository.ReviseMany(ctx, commands)
	if err != nil {
		return nil, fmt.Errorf("revise translation relationship: %w", err)
	}

	return entries, nil
}
