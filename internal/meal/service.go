package meal

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

// Bounds for meal fields and paged lists.
const (
	MaximumMealName    = 200
	MaximumMealNotes   = 10000
	MaximumDishName    = 200
	MaximumDishNotes   = 5000
	MaximumPlainItem   = 200
	MaximumDishesCount = 50

	DefaultListLimit = 50
	MaximumListLimit = 200
)

// IDGenerator supplies a new identity.
type IDGenerator func(prefix string) (string, error)

// Clock supplies timestamps.
type Clock func() time.Time

// Service validates meal mutations and coordinates their persistence.
type Service struct {
	meals      Persistence
	savedMeals SavedMealPersistence
	newID      IDGenerator
	now        Clock
}

// Option customizes a meal service at construction time.
type Option func(*Service)

// WithIDGenerator supplies the generator used for new identities.
func WithIDGenerator(gen IDGenerator) Option {
	return func(s *Service) { s.newID = gen }
}

// WithClock supplies the clock used for timestamps.
func WithClock(clock Clock) Option {
	return func(s *Service) { s.now = clock }
}

// NewService constructs a meal service over its persistence dependencies.
func NewService(meals Persistence, savedMeals SavedMealPersistence, options ...Option) (*Service, error) {
	if meals == nil {
		return nil, errors.New("meal persistence is required")
	}

	if savedMeals == nil {
		return nil, errors.New("saved meal persistence is required")
	}

	s := &Service{
		meals:      meals,
		savedMeals: savedMeals,
		newID:      defaultIDGenerator,
		now:        time.Now,
	}
	for _, option := range options {
		option(s)
	}

	return s, nil
}

// CreateMeal validates and persists a new meal occurrence.
func (s *Service) CreateMeal(ctx context.Context, input Meal) (Meal, error) {
	if err := validateMeal(&input); err != nil {
		return Meal{}, err
	}

	id, err := s.newID("m")
	if err != nil {
		return Meal{}, fmt.Errorf("generate meal ID: %w", err)
	}

	input.ID = id
	input.LibraryID = access.LibraryID(ctx)

	now := s.now().UTC()
	input.CreatedAt = now
	input.UpdatedAt = now
	input.Revision = 1

	if err := s.prepareDishes(input.ID, input.Dishes, false); err != nil {
		return Meal{}, err
	}

	if err := s.meals.CreateMeal(ctx, input.LibraryID, input); err != nil {
		return Meal{}, fmt.Errorf("create meal: %w", err)
	}

	return input, nil
}

// GetMeal retrieves a meal by ID.
func (s *Service) GetMeal(ctx context.Context, id string) (Meal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Meal{}, ErrNotFound
	}

	meal, err := s.meals.GetMeal(ctx, access.LibraryID(ctx), id)
	if err != nil {
		return Meal{}, fmt.Errorf("get meal: %w", err)
	}

	return meal, nil
}

// ListMeals retrieves meals matching the given options.
func (s *Service) ListMeals(ctx context.Context, options ListOptions) ([]Meal, int, error) {
	if options.Limit <= 0 {
		options.Limit = DefaultListLimit
	}

	if options.Limit > MaximumListLimit {
		options.Limit = MaximumListLimit
	}

	if options.Offset < 0 {
		options.Offset = 0
	}

	meals, total, err := s.meals.ListMeals(ctx, access.LibraryID(ctx), options)
	if err != nil {
		return nil, 0, fmt.Errorf("list meals: %w", err)
	}

	return meals, total, nil
}

// UpdateMeal validates and replaces a meal's metadata.
func (s *Service) UpdateMeal(ctx context.Context, id string, expectedRevision uint64, input Meal) (Meal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Meal{}, ErrNotFound
	}

	if err := validateMeal(&input); err != nil {
		return Meal{}, err
	}

	input.ID = id
	input.LibraryID = access.LibraryID(ctx)

	input.UpdatedAt = s.now().UTC()
	if err := s.prepareDishes(input.ID, input.Dishes, true); err != nil {
		return Meal{}, err
	}

	if err := s.meals.UpdateMeal(ctx, input.LibraryID, input, expectedRevision); err != nil {
		return Meal{}, fmt.Errorf("update meal: %w", err)
	}

	return s.meals.GetMeal(ctx, input.LibraryID, id)
}

func (s *Service) prepareDishes(mealID string, dishes []Dish, preserveIDs bool) error {
	for i := range dishes {
		if !preserveIDs || strings.TrimSpace(dishes[i].ID) == "" {
			dishID, err := s.newID("d")
			if err != nil {
				return fmt.Errorf("generate dish ID: %w", err)
			}

			dishes[i].ID = dishID
		}

		dishes[i].MealID = mealID
		dishes[i].Position = i

		for j := range dishes[i].PlainItems {
			if preserveIDs && strings.TrimSpace(dishes[i].PlainItems[j].ID) != "" {
				continue
			}

			itemID, err := s.newID("pi")
			if err != nil {
				return fmt.Errorf("generate plain item ID: %w", err)
			}

			dishes[i].PlainItems[j].ID = itemID
		}
	}

	return nil
}

// DeleteMeal removes a meal and its dishes.
func (s *Service) DeleteMeal(ctx context.Context, id string, expectedRevision uint64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}

	if err := s.meals.DeleteMeal(ctx, access.LibraryID(ctx), id, expectedRevision); err != nil {
		return fmt.Errorf("delete meal: %w", err)
	}

	return nil
}

// AddDish adds a dish to an existing meal.
func (s *Service) AddDish(ctx context.Context, mealID string, dish Dish) (Dish, error) {
	mealID = strings.TrimSpace(mealID)
	if mealID == "" {
		return Dish{}, ErrNotFound
	}

	if err := validateDish(&dish); err != nil {
		return Dish{}, err
	}

	dishID, err := s.newID("d")
	if err != nil {
		return Dish{}, fmt.Errorf("generate dish ID: %w", err)
	}

	dish.ID = dishID
	dish.MealID = mealID

	for i := range dish.PlainItems {
		itemID, err := s.newID("pi")
		if err != nil {
			return Dish{}, fmt.Errorf("generate plain item ID: %w", err)
		}

		dish.PlainItems[i].ID = itemID
	}

	if err := s.meals.AddDish(ctx, access.LibraryID(ctx), mealID, dish); err != nil {
		return Dish{}, fmt.Errorf("add dish: %w", err)
	}

	return dish, nil
}

// UpdateDish replaces a dish's metadata within its meal.
func (s *Service) UpdateDish(ctx context.Context, mealID string, dish Dish) (Dish, error) {
	mealID = strings.TrimSpace(mealID)
	if mealID == "" {
		return Dish{}, ErrNotFound
	}

	if err := validateDish(&dish); err != nil {
		return Dish{}, err
	}

	dish.MealID = mealID

	if err := s.meals.UpdateDish(ctx, access.LibraryID(ctx), mealID, dish); err != nil {
		return Dish{}, fmt.Errorf("update dish: %w", err)
	}

	return dish, nil
}

// RemoveDish removes a single dish from a meal.
func (s *Service) RemoveDish(ctx context.Context, mealID, dishID string) error {
	mealID = strings.TrimSpace(mealID)
	dishID = strings.TrimSpace(dishID)

	if mealID == "" || dishID == "" {
		return ErrNotFound
	}

	if err := s.meals.RemoveDish(ctx, access.LibraryID(ctx), mealID, dishID); err != nil {
		return fmt.Errorf("remove dish: %w", err)
	}

	return nil
}

// MoveMeal changes a meal's date, preserving its identity and state.
func (s *Service) MoveMeal(ctx context.Context, id string, expectedRevision uint64, date *library.Date, mealType MealType) (Meal, error) {
	meal, err := s.GetMeal(ctx, id)
	if err != nil {
		return Meal{}, err
	}

	meal.Date = date
	if mealType != "" {
		meal.MealType = mealType
	}

	return s.UpdateMeal(ctx, id, expectedRevision, meal)
}

// CopyMeal creates a fresh meal occurrence with the same dishes but a new date.
func (s *Service) CopyMeal(ctx context.Context, sourceID string, date *library.Date, mealType MealType) (Meal, error) {
	source, err := s.GetMeal(ctx, sourceID)
	if err != nil {
		return Meal{}, err
	}

	// Create a fresh occurrence; do not carry over the source ID.
	newMeal := Meal{
		Name:     source.Name,
		Date:     date,
		MealType: mealType,
		Notes:    source.Notes,
	}

	// Copy dishes without recipe revision pins or other per-occurrence state.
	for _, d := range source.Dishes {
		newDish := Dish{
			EntryID:     d.EntryID,
			LeftoverOf:  d.LeftoverOf,
			DisplayName: d.DisplayName,
			Scale:       d.Scale,
			Notes:       d.Notes,
		}
		for _, pi := range d.PlainItems {
			newDish.PlainItems = append(newDish.PlainItems, PlainItem{
				Name:     pi.Name,
				Quantity: pi.Quantity,
			})
		}

		newMeal.Dishes = append(newMeal.Dishes, newDish)
	}

	return s.CreateMeal(ctx, newMeal)
}

// CreateSavedMeal saves a reusable meal combination.
func (s *Service) CreateSavedMeal(ctx context.Context, input SavedMeal) (SavedMeal, error) {
	if err := validateSavedMeal(&input); err != nil {
		return SavedMeal{}, err
	}

	id, err := s.newID("sm")
	if err != nil {
		return SavedMeal{}, fmt.Errorf("generate saved meal ID: %w", err)
	}

	input.ID = id
	input.LibraryID = access.LibraryID(ctx)

	now := s.now().UTC()
	input.CreatedAt = now
	input.UpdatedAt = now
	input.Revision = 1

	for i := range input.Dishes {
		dishID, err := s.newID("sd")
		if err != nil {
			return SavedMeal{}, fmt.Errorf("generate saved dish ID: %w", err)
		}

		input.Dishes[i].ID = dishID
		input.Dishes[i].SavedMealID = input.ID
		input.Dishes[i].Position = i

		for j := range input.Dishes[i].PlainItems {
			itemID, err := s.newID("pi")
			if err != nil {
				return SavedMeal{}, fmt.Errorf("generate plain item ID: %w", err)
			}

			input.Dishes[i].PlainItems[j].ID = itemID
		}
	}

	if err := s.savedMeals.CreateSavedMeal(ctx, input.LibraryID, input); err != nil {
		return SavedMeal{}, fmt.Errorf("create saved meal: %w", err)
	}

	return input, nil
}

// GetSavedMeal retrieves a saved meal by ID.
func (s *Service) GetSavedMeal(ctx context.Context, id string) (SavedMeal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SavedMeal{}, ErrNotFound
	}

	meal, err := s.savedMeals.GetSavedMeal(ctx, access.LibraryID(ctx), id)
	if err != nil {
		return SavedMeal{}, fmt.Errorf("get saved meal: %w", err)
	}

	return meal, nil
}

// ListSavedMeals retrieves saved meals.
func (s *Service) ListSavedMeals(ctx context.Context, limit, offset int) ([]SavedMeal, int, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}

	if limit > MaximumListLimit {
		limit = MaximumListLimit
	}

	if offset < 0 {
		offset = 0
	}

	meals, total, err := s.savedMeals.ListSavedMeals(ctx, access.LibraryID(ctx), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list saved meals: %w", err)
	}

	return meals, total, nil
}

// UpdateSavedMeal replaces a saved meal's content.
func (s *Service) UpdateSavedMeal(ctx context.Context, id string, expectedRevision uint64, input SavedMeal) (SavedMeal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SavedMeal{}, ErrNotFound
	}

	if err := validateSavedMeal(&input); err != nil {
		return SavedMeal{}, err
	}

	input.ID = id
	input.LibraryID = access.LibraryID(ctx)
	input.UpdatedAt = s.now().UTC()

	for i := range input.Dishes {
		if input.Dishes[i].ID == "" {
			dishID, err := s.newID("sd")
			if err != nil {
				return SavedMeal{}, fmt.Errorf("generate saved dish ID: %w", err)
			}

			input.Dishes[i].ID = dishID
		}

		input.Dishes[i].SavedMealID = input.ID
		input.Dishes[i].Position = i

		for j := range input.Dishes[i].PlainItems {
			if input.Dishes[i].PlainItems[j].ID == "" {
				itemID, err := s.newID("pi")
				if err != nil {
					return SavedMeal{}, fmt.Errorf("generate plain item ID: %w", err)
				}

				input.Dishes[i].PlainItems[j].ID = itemID
			}
		}
	}

	if err := s.savedMeals.UpdateSavedMeal(ctx, input.LibraryID, input, expectedRevision); err != nil {
		return SavedMeal{}, fmt.Errorf("update saved meal: %w", err)
	}

	return s.savedMeals.GetSavedMeal(ctx, input.LibraryID, id)
}

// DeleteSavedMeal removes a saved meal.
func (s *Service) DeleteSavedMeal(ctx context.Context, id string, expectedRevision uint64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}

	if err := s.savedMeals.DeleteSavedMeal(ctx, access.LibraryID(ctx), id, expectedRevision); err != nil {
		return fmt.Errorf("delete saved meal: %w", err)
	}

	return nil
}

// InstantiateSavedMeal creates a fresh meal occurrence from a saved meal template.
func (s *Service) InstantiateSavedMeal(ctx context.Context, savedMealID string, date *library.Date, mealType MealType) (Meal, error) {
	saved, err := s.GetSavedMeal(ctx, savedMealID)
	if err != nil {
		return Meal{}, err
	}

	newMeal := Meal{
		Name:     saved.Name,
		Date:     date,
		MealType: mealType,
		Notes:    saved.Notes,
	}

	for _, d := range saved.Dishes {
		dish := Dish{
			EntryID:     d.EntryID,
			DisplayName: d.DisplayName,
			Scale:       d.Scale,
			Notes:       d.Notes,
		}
		for _, pi := range d.PlainItems {
			dish.PlainItems = append(dish.PlainItems, PlainItem{
				Name:     pi.Name,
				Quantity: pi.Quantity,
			})
		}

		newMeal.Dishes = append(newMeal.Dishes, dish)
	}

	return s.CreateMeal(ctx, newMeal)
}

func validateMeal(m *Meal) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Notes = strings.TrimSpace(m.Notes)

	if len([]rune(m.Name)) > MaximumMealName {
		return fmt.Errorf("%w: meal name is too long", ErrInvalidMeal)
	}

	if len([]rune(m.Notes)) > MaximumMealNotes {
		return fmt.Errorf("%w: meal notes are too long", ErrInvalidMeal)
	}

	if m.MealType != "" && m.MealType != MealTypeBreakfast && m.MealType != MealTypeLunch && m.MealType != MealTypeDinner && m.MealType != MealTypeSnack {
		return fmt.Errorf("%w: unrecognized meal type", ErrInvalidMeal)
	}

	if len(m.Dishes) > MaximumDishesCount {
		return fmt.Errorf("%w: too many dishes", ErrInvalidMeal)
	}

	if len(m.Dishes) == 0 {
		return fmt.Errorf("%w: a meal needs at least one dish", ErrInvalidMeal)
	}

	for i := range m.Dishes {
		if err := validateDish(&m.Dishes[i]); err != nil {
			return err
		}
	}

	dishIDs, itemIDs := []string{}, []string{}
	for _, dish := range m.Dishes {
		dishIDs = append(dishIDs, dish.ID)
		for _, item := range dish.PlainItems {
			itemIDs = append(itemIDs, item.ID)
		}
	}

	return distinctIdentities(dishIDs, itemIDs)
}

// distinctIdentities rejects a client-supplied dish or shopping item ID used
// twice in one request. Empty IDs are generated later and never collide.
func distinctIdentities(dishIDs, itemIDs []string) error {
	for noun, ids := range map[string][]string{"dish": dishIDs, "shopping item": itemIDs} {
		seen := map[string]bool{}

		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" && seen[id] {
				return fmt.Errorf("%w: %s ID %q is used more than once", ErrInvalidMeal, noun, id)
			}

			seen[id] = true
		}
	}

	return nil
}

func validateDish(d *Dish) error {
	if len(d.LeftoverOf) > 201 || (d.LeftoverOf != "" && d.EntryID != nil) {
		return fmt.Errorf("%w: leftovers cannot repeat a recipe requirement", ErrInvalidMeal)
	}

	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.Notes = strings.TrimSpace(d.Notes)

	if d.DisplayName == "" {
		return fmt.Errorf("%w: dish name is required", ErrInvalidMeal)
	}

	if len([]rune(d.DisplayName)) > MaximumDishName {
		return fmt.Errorf("%w: dish name is too long", ErrInvalidMeal)
	}

	if len([]rune(d.Notes)) > MaximumDishNotes {
		return fmt.Errorf("%w: dish notes are too long", ErrInvalidMeal)
	}

	if d.Scale != nil {
		if err := validatePositiveRational(*d.Scale); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidMeal, err)
		}
	}

	return validatePlainItems(d.EntryID, d.PlainItems)
}

func validatePlainItems(entryID *library.EntryID, items []PlainItem) error {
	if entryID != nil && len(items) > 0 {
		return fmt.Errorf("%w: a recipe dish cannot carry shopping items", ErrInvalidMeal)
	}

	for i := range items {
		items[i].Name = strings.TrimSpace(items[i].Name)

		items[i].Quantity = strings.TrimSpace(items[i].Quantity)
		if items[i].Name == "" {
			return fmt.Errorf("%w: shopping item name is required", ErrInvalidMeal)
		}

		if len([]rune(items[i].Name)) > MaximumPlainItem || len([]rune(items[i].Quantity)) > MaximumPlainItem {
			return fmt.Errorf("%w: shopping item is too long", ErrInvalidMeal)
		}
	}

	return nil
}

func validateSavedMeal(m *SavedMeal) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Notes = strings.TrimSpace(m.Notes)

	if m.Name == "" {
		return fmt.Errorf("%w: saved meal name is required", ErrInvalidMeal)
	}

	if len([]rune(m.Name)) > MaximumMealName {
		return fmt.Errorf("%w: saved meal name is too long", ErrInvalidMeal)
	}

	if len([]rune(m.Notes)) > MaximumMealNotes {
		return fmt.Errorf("%w: saved meal notes are too long", ErrInvalidMeal)
	}

	if len(m.Dishes) > MaximumDishesCount {
		return fmt.Errorf("%w: too many dishes", ErrInvalidMeal)
	}

	if len(m.Dishes) == 0 {
		return fmt.Errorf("%w: a saved meal needs at least one dish", ErrInvalidMeal)
	}

	for i := range m.Dishes {
		d := &m.Dishes[i]
		d.DisplayName = strings.TrimSpace(d.DisplayName)
		d.Notes = strings.TrimSpace(d.Notes)

		if d.DisplayName == "" {
			return fmt.Errorf("%w: dish name is required", ErrInvalidMeal)
		}

		if len([]rune(d.DisplayName)) > MaximumDishName {
			return fmt.Errorf("%w: dish name is too long", ErrInvalidMeal)
		}

		if len([]rune(d.Notes)) > MaximumDishNotes {
			return fmt.Errorf("%w: dish notes are too long", ErrInvalidMeal)
		}

		if d.Scale != nil {
			if err := validatePositiveRational(*d.Scale); err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidMeal, err)
			}
		}

		// Planning a saved meal validates its dishes as a planned meal does, so
		// anything saved must also be plannable.
		if err := validatePlainItems(d.EntryID, d.PlainItems); err != nil {
			return err
		}
	}

	dishIDs, itemIDs := []string{}, []string{}
	for _, dish := range m.Dishes {
		dishIDs = append(dishIDs, dish.ID)
		for _, item := range dish.PlainItems {
			itemIDs = append(itemIDs, item.ID)
		}
	}

	return distinctIdentities(dishIDs, itemIDs)
}

func validatePositiveRational(r recipe.Rational) error {
	if r == "" {
		return nil
	}

	v, err := r.Rat()
	if err != nil {
		return fmt.Errorf("invalid batch size: %w", err)
	}

	if v.Sign() <= 0 {
		return fmt.Errorf("batch size must be positive")
	}

	return nil
}

func defaultIDGenerator(prefix string) (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("read ID entropy: %w", err)
	}

	return prefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(entropy[:])), nil
}
