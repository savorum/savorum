package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

// MealService supplies library-scoped plans and reusable meal combinations.
type MealService interface {
	CreateMeal(context.Context, meal.Meal) (meal.Meal, error)
	GetMeal(context.Context, string) (meal.Meal, error)
	ListMeals(context.Context, meal.ListOptions) ([]meal.Meal, int, error)
	UpdateMeal(context.Context, string, uint64, meal.Meal) (meal.Meal, error)
	DeleteMeal(context.Context, string, uint64) error
	CopyMeal(context.Context, string, *library.Date, meal.MealType) (meal.Meal, error)
	CreateSavedMeal(context.Context, meal.SavedMeal) (meal.SavedMeal, error)
	GetSavedMeal(context.Context, string) (meal.SavedMeal, error)
	ListSavedMeals(context.Context, int, int) ([]meal.SavedMeal, int, error)
	UpdateSavedMeal(context.Context, string, uint64, meal.SavedMeal) (meal.SavedMeal, error)
	DeleteSavedMeal(context.Context, string, uint64) error
	InstantiateSavedMeal(context.Context, string, *library.Date, meal.MealType) (meal.Meal, error)
}

// Meal is the API representation of one planned meal occurrence.
type Meal struct {
	ID            string     `json:"id"`
	Name          string     `json:"name,omitempty" maxLength:"200"`
	DisplayTitle  string     `json:"display_title" readOnly:"true"`
	Date          *string    `json:"date,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"`
	MealType      string     `json:"meal_type,omitempty" enum:",breakfast,lunch,dinner,snack"`
	Skipped       bool       `json:"skipped"`
	Notes         string     `json:"notes,omitempty" maxLength:"10000"`
	Dishes        []MealDish `json:"dishes" minItems:"1" maxItems:"50"`
	Revision      uint64     `json:"revision" minimum:"1"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by,omitempty" readOnly:"true"`
	CreatedByName string     `json:"created_by_name,omitempty" readOnly:"true"`
	UpdatedBy     string     `json:"updated_by,omitempty" readOnly:"true"`
	UpdatedByName string     `json:"updated_by_name,omitempty" readOnly:"true"`
}

// MealInput is the writable form of a planned meal occurrence.
type MealInput struct {
	Name     string     `json:"name,omitempty" maxLength:"200"`
	Date     *string    `json:"date,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"`
	MealType string     `json:"meal_type,omitempty" enum:",breakfast,lunch,dinner,snack"`
	Skipped  bool       `json:"skipped,omitempty"`
	Notes    string     `json:"notes,omitempty" maxLength:"10000"`
	Dishes   []MealDish `json:"dishes" minItems:"1" maxItems:"50"`
}

// MealDish is one independently scaled recipe or plain-text dish in a meal.
type MealDish struct {
	ID              string          `json:"id,omitempty"`
	EntryID         *string         `json:"entry_id,omitempty"`
	RecipeRevision  *uint64         `json:"recipe_revision,omitempty" minimum:"1"`
	ShoppingChoices []string        `json:"shopping_choices,omitempty" readOnly:"true"`
	LeftoverOf      string          `json:"leftover_of,omitempty" maxLength:"201"`
	DisplayName     string          `json:"display_name" minLength:"1" maxLength:"200"`
	Scale           *string         `json:"scale,omitempty" maxLength:"100"`
	Notes           string          `json:"notes,omitempty" maxLength:"5000"`
	PlainItems      []MealPlainItem `json:"plain_items,omitempty" maxItems:"100" doc:"Only for a dish without entry_id."`
	RecipeStatus    string          `json:"recipe_status,omitempty" enum:"available,trashed,removed" readOnly:"true"`
	RecipeImageURL  string          `json:"recipe_image_url,omitempty" readOnly:"true"`
}

// MealPlainItem is a manual shopping need attached to a plain-text dish.
type MealPlainItem struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name" minLength:"1" maxLength:"200"`
	Quantity string `json:"quantity,omitempty" maxLength:"200"`
}

// SavedMeal is a reusable combination of dishes.
type SavedMeal struct {
	ID        string     `json:"id"`
	Name      string     `json:"name" minLength:"1" maxLength:"200"`
	Notes     string     `json:"notes,omitempty" maxLength:"10000"`
	Dishes    []MealDish `json:"dishes" minItems:"1" maxItems:"50"`
	Revision  uint64     `json:"revision" minimum:"1"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// SavedMealInput is the writable form of a reusable meal combination.
type SavedMealInput struct {
	Name   string     `json:"name" minLength:"1" maxLength:"200"`
	Notes  string     `json:"notes,omitempty" maxLength:"10000"`
	Dishes []MealDish `json:"dishes" minItems:"1" maxItems:"50"`
}

type mealOutput struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag" doc:"Validates this representation, including the recipe and shopping details it reads. If-Match takes the revision instead."`
	Body     Meal
}

type mealsOutput struct {
	Body struct {
		Items  []Meal `json:"items"`
		Total  int    `json:"total"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
}

type savedMealOutput struct {
	Location string `header:"Location"`
	ETag     string `header:"ETag" doc:"Validates this representation, including the recipe details it reads. If-Match takes the revision instead."`
	Body     SavedMeal
}

type savedMealsOutput struct {
	Body struct {
		Items  []SavedMeal `json:"items"`
		Total  int         `json:"total"`
		Limit  int         `json:"limit"`
		Offset int         `json:"offset"`
	}
}

type (
	mealPathInput struct {
		ID string `path:"id"`
	}
	mealWriteInput struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    MealInput
	}
)

type mealDeleteInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
}
type mealPlacementInput struct {
	Date     *string `json:"date,omitempty" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"`
	MealType string  `json:"meal_type,omitempty" enum:",breakfast,lunch,dinner,snack"`
}
type copyMealInput struct {
	ID   string `path:"id"`
	Body mealPlacementInput
}
type listMealsInput struct {
	From               string `query:"from"`
	To                 string `query:"to"`
	IncludeUnscheduled bool   `query:"include_unscheduled" default:"false" doc:"Also return meals without a date"`
	Limit              int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset             int    `query:"offset" default:"0" minimum:"0"`
}
type savedMealWriteInput struct {
	ID      string `path:"id"`
	IfMatch string `header:"If-Match"`
	Body    SavedMealInput
}
type instantiateSavedMealInput struct {
	ID   string `path:"id"`
	Body mealPlacementInput
}
type listSavedMealsInput struct {
	Limit  int `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset int `query:"offset" default:"0" minimum:"0"`
}

func registerMeals(api huma.API, service MealService) {
	huma.Register(api, huma.Operation{OperationID: "create-meal", Method: http.MethodPost, Path: "/api/v1/meals", Summary: "Plan a meal", Tags: []string{"Plan"}, DefaultStatus: 201, Errors: []int{404, 422}}, func(ctx context.Context, input *struct{ Body MealInput }) (*mealOutput, error) {
		mealInput, err := mealFromInput(input.Body)
		if err != nil {
			return nil, mapMealError(err, "meal")
		}

		value, err := retryCreate(ctx, api, "create-meal", input, func(work context.Context) (meal.Meal, error) { return service.CreateMeal(work, mealInput) }, service.GetMeal)

		return mealResponse(value, "/api/v1/meals/"+value.ID, err)
	})
	huma.Register(api, huma.Operation{OperationID: "list-meals", Method: http.MethodGet, Path: "/api/v1/meals", Summary: "List meals", Tags: []string{"Plan"}}, func(ctx context.Context, input *listMealsInput) (*mealsOutput, error) {
		from, err := optionalMealDateString(input.From)
		if err != nil {
			return nil, mapMealError(err, "meal")
		}

		to, err := optionalMealDateString(input.To)
		if err != nil {
			return nil, mapMealError(err, "meal")
		}

		values, total, err := service.ListMeals(ctx, meal.ListOptions{DateFrom: from, DateTo: to, Unscheduled: input.IncludeUnscheduled, Limit: input.Limit, Offset: input.Offset})
		if err != nil {
			return nil, mapMealError(err, "meal")
		}

		output := &mealsOutput{}

		output.Body.Items = make([]Meal, len(values))
		for index := range values {
			output.Body.Items[index] = mealToAPI(values[index])
		}

		output.Body.Total, output.Body.Limit, output.Body.Offset = total, input.Limit, input.Offset

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-meal", Method: http.MethodGet, Path: "/api/v1/meals/{id}", Summary: "Get a meal", Tags: []string{"Plan"}, Errors: []int{404}}, func(ctx context.Context, input *mealPathInput) (*mealOutput, error) {
		value, err := service.GetMeal(ctx, input.ID)
		return mealResponse(value, "", err)
	})
	huma.Register(api, huma.Operation{OperationID: "replace-meal", Method: http.MethodPut, Path: "/api/v1/meals/{id}", Summary: "Replace a meal", Tags: []string{"Plan"}, Errors: []int{400, 404, 412, 422, 428}}, func(ctx context.Context, input *mealWriteInput) (*mealOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "meal")
		if err != nil {
			return nil, err
		}

		mealInput, err := mealFromInput(input.Body)
		if err != nil {
			return nil, mapMealError(err, "meal")
		}

		value, err := service.UpdateMeal(ctx, input.ID, expected, mealInput)

		return mealResponse(value, "", err)
	})
	huma.Register(api, huma.Operation{OperationID: "delete-meal", Method: http.MethodDelete, Path: "/api/v1/meals/{id}", Summary: "Delete a meal", Tags: []string{"Plan"}, DefaultStatus: 204, Errors: []int{400, 404, 412, 428}}, func(ctx context.Context, input *mealDeleteInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "meal")
		if err != nil {
			return nil, err
		}

		if err := service.DeleteMeal(ctx, input.ID, expected); err != nil {
			return nil, mapMealError(err, "meal")
		}

		return &noContentOutput{}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-meal-copy", Method: http.MethodPost, Path: "/api/v1/meals/{id}/copy", Summary: "Copy a meal", Tags: []string{"Plan"}, DefaultStatus: 201, Errors: []int{404, 422}}, func(ctx context.Context, input *copyMealInput) (*mealOutput, error) {
		date, err := optionalMealDate(input.Body.Date)
		if err != nil {
			return nil, mapMealError(err, "meal")
		}

		value, err := retryCreate(ctx, api, "create-meal-copy", input, func(work context.Context) (meal.Meal, error) {
			return service.CopyMeal(work, input.ID, date, meal.MealType(input.Body.MealType))
		}, service.GetMeal)

		return mealResponse(value, "/api/v1/meals/"+value.ID, err)
	})

	registerSavedMeals(api, service)
}

func registerSavedMeals(api huma.API, service MealService) {
	huma.Register(api, huma.Operation{OperationID: "create-saved-meal", Method: http.MethodPost, Path: "/api/v1/saved-meals", Summary: "Save a reusable meal", Tags: []string{"Plan"}, DefaultStatus: 201, Errors: []int{404, 422}}, func(ctx context.Context, input *struct{ Body SavedMealInput }) (*savedMealOutput, error) {
		value, err := retryCreate(ctx, api, "create-saved-meal", input, func(work context.Context) (meal.SavedMeal, error) {
			return service.CreateSavedMeal(work, savedMealFromInput(input.Body))
		}, service.GetSavedMeal)

		return savedMealResponse(value, "/api/v1/saved-meals/"+value.ID, err)
	})
	huma.Register(api, huma.Operation{OperationID: "list-saved-meals", Method: http.MethodGet, Path: "/api/v1/saved-meals", Summary: "List reusable meals", Tags: []string{"Plan"}}, func(ctx context.Context, input *listSavedMealsInput) (*savedMealsOutput, error) {
		values, total, err := service.ListSavedMeals(ctx, input.Limit, input.Offset)
		if err != nil {
			return nil, mapMealError(err, "saved meal")
		}

		output := &savedMealsOutput{}

		output.Body.Items = make([]SavedMeal, len(values))
		for index := range values {
			output.Body.Items[index] = savedMealToAPI(values[index])
		}

		output.Body.Total, output.Body.Limit, output.Body.Offset = total, input.Limit, input.Offset

		return output, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-saved-meal", Method: http.MethodGet, Path: "/api/v1/saved-meals/{id}", Summary: "Get a reusable meal", Tags: []string{"Plan"}, Errors: []int{404}}, func(ctx context.Context, input *mealPathInput) (*savedMealOutput, error) {
		value, err := service.GetSavedMeal(ctx, input.ID)
		return savedMealResponse(value, "", err)
	})
	huma.Register(api, huma.Operation{OperationID: "replace-saved-meal", Method: http.MethodPut, Path: "/api/v1/saved-meals/{id}", Summary: "Replace a reusable meal", Tags: []string{"Plan"}, Errors: []int{400, 404, 412, 422, 428}}, func(ctx context.Context, input *savedMealWriteInput) (*savedMealOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "saved meal")
		if err != nil {
			return nil, err
		}

		value, err := service.UpdateSavedMeal(ctx, input.ID, expected, savedMealFromInput(input.Body))

		return savedMealResponse(value, "", err)
	})
	huma.Register(api, huma.Operation{OperationID: "delete-saved-meal", Method: http.MethodDelete, Path: "/api/v1/saved-meals/{id}", Summary: "Delete a reusable meal", Tags: []string{"Plan"}, DefaultStatus: 204, Errors: []int{400, 404, 412, 428}}, func(ctx context.Context, input *mealDeleteInput) (*noContentOutput, error) {
		expected, err := parseRevisionPrecondition(input.IfMatch, "saved meal")
		if err != nil {
			return nil, err
		}

		if err := service.DeleteSavedMeal(ctx, input.ID, expected); err != nil {
			return nil, mapMealError(err, "saved meal")
		}

		return &noContentOutput{}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-meal-from-saved", Method: http.MethodPost, Path: "/api/v1/saved-meals/{id}/plan", Summary: "Plan a reusable meal", Tags: []string{"Plan"}, DefaultStatus: 201, Errors: []int{404, 422}}, func(ctx context.Context, input *instantiateSavedMealInput) (*mealOutput, error) {
		date, err := optionalMealDate(input.Body.Date)
		if err != nil {
			return nil, mapMealError(err, "saved meal")
		}

		value, err := retryCreate(ctx, api, "create-meal-from-saved", input, func(work context.Context) (meal.Meal, error) {
			return service.InstantiateSavedMeal(work, input.ID, date, meal.MealType(input.Body.MealType))
		}, service.GetMeal)

		return mealResponse(value, "/api/v1/meals/"+value.ID, err)
	})
}

// representationETag validates a meal or saved meal as returned. Its dishes
// read recipe and shopping state that does not advance the meal revision, so
// the revision alone cannot validate the representation.
func representationETag(body any) string {
	data, err := json.Marshal(body)
	if err != nil {
		return ""
	}

	digest := sha256.Sum256(data)

	return `"r-` + hex.EncodeToString(digest[:16]) + `"`
}

func mealResponse(value meal.Meal, location string, err error) (*mealOutput, error) {
	if err != nil {
		return nil, mapMealError(err, "meal")
	}

	body := mealToAPI(value)

	return &mealOutput{Location: location, ETag: representationETag(body), Body: body}, nil
}

func savedMealResponse(value meal.SavedMeal, location string, err error) (*savedMealOutput, error) {
	if err != nil {
		return nil, mapMealError(err, "saved meal")
	}

	body := savedMealToAPI(value)

	return &savedMealOutput{Location: location, ETag: representationETag(body), Body: body}, nil
}

func mapMealError(err error, noun string) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}

	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	switch {
	case errors.Is(err, meal.ErrInvalidMeal):
		return problem(422, "meal-invalid", err.Error())
	case errors.Is(err, meal.ErrUnknownRecipe):
		return problem(404, "recipe-not-found", "A dish names a recipe this library does not hold")
	case errors.Is(err, meal.ErrNotFound), errors.Is(err, library.ErrNotFound):
		return problem(404, strings.ReplaceAll(noun, " ", "-")+"-not-found", "The requested "+noun+" does not exist")
	case errors.Is(err, meal.ErrPreconditionFailed):
		if conflict, ok := errors.AsType[*meal.PreconditionError](err); ok {
			return problem(412, "meal-revision-conflict", fmt.Sprintf("The %s changed since it was loaded. Expected revision %d, current revision is %d", noun, conflict.Expected, conflict.Current))
		}

		return problem(412, "meal-revision-conflict", "The "+noun+" changed since it was loaded")
	default:
		return problem(500, "internal-error", "The request could not be completed")
	}
}

func optionalMealDate(value *string) (*library.Date, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}

	parsed, err := library.ParseDate(*value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", meal.ErrInvalidMeal, err)
	}

	return &parsed, nil
}

func optionalMealDateString(value string) (*library.Date, error) {
	return optionalMealDate(&value)
}

func mealFromInput(input MealInput) (meal.Meal, error) {
	date, err := optionalMealDate(input.Date)
	if err != nil {
		return meal.Meal{}, err
	}

	result := meal.Meal{Skipped: input.Skipped, Name: input.Name, Date: date, MealType: meal.MealType(input.MealType), Notes: input.Notes}
	result.Dishes = dishesFromAPI(input.Dishes)

	return result, nil
}

func mealToAPI(value meal.Meal) Meal {
	result := Meal{
		Skipped: value.Skipped, ID: value.ID, Name: value.Name, DisplayTitle: value.DisplayTitle(), MealType: string(value.MealType), Notes: value.Notes,
		Revision: value.Revision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		CreatedBy: value.CreatedBy, CreatedByName: value.CreatedByName, UpdatedBy: value.UpdatedBy, UpdatedByName: value.UpdatedByName,
	}
	if value.Date != nil {
		date := value.Date.String()
		result.Date = &date
	}

	result.Dishes = dishesToAPI(value.Dishes)

	return result
}

func savedMealFromInput(input SavedMealInput) meal.SavedMeal {
	result := meal.SavedMeal{Name: input.Name, Notes: input.Notes}
	for _, dish := range dishesFromAPI(input.Dishes) {
		result.Dishes = append(result.Dishes, meal.SavedDish{ID: dish.ID, EntryID: dish.EntryID, DisplayName: dish.DisplayName, Scale: dish.Scale, Notes: dish.Notes, PlainItems: dish.PlainItems})
	}

	return result
}

func savedMealToAPI(value meal.SavedMeal) SavedMeal {
	result := SavedMeal{ID: value.ID, Name: value.Name, Notes: value.Notes, Revision: value.Revision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
	for _, dish := range value.Dishes {
		result.Dishes = append(result.Dishes, dishToAPI(meal.Dish{ID: dish.ID, EntryID: dish.EntryID, DisplayName: dish.DisplayName, Scale: dish.Scale, Notes: dish.Notes, PlainItems: dish.PlainItems, RecipeStatus: dish.RecipeStatus, RecipeImage: dish.RecipeImage}))
	}

	if result.Dishes == nil {
		result.Dishes = []MealDish{}
	}

	return result
}

func dishesFromAPI(values []MealDish) []meal.Dish {
	result := make([]meal.Dish, len(values))
	for index, value := range values {
		result[index] = meal.Dish{ID: value.ID, RecipeRevision: value.RecipeRevision, LeftoverOf: value.LeftoverOf, DisplayName: value.DisplayName, Scale: toRational(value.Scale), Notes: value.Notes}
		if value.EntryID != nil {
			entryID := library.EntryID(*value.EntryID)
			result[index].EntryID = &entryID
		}

		for _, item := range value.PlainItems {
			result[index].PlainItems = append(result[index].PlainItems, meal.PlainItem{ID: item.ID, Name: item.Name, Quantity: item.Quantity})
		}
	}

	return result
}

func dishesToAPI(values []meal.Dish) []MealDish {
	result := make([]MealDish, len(values))
	for index, value := range values {
		result[index] = dishToAPI(value)
	}

	return result
}

func dishToAPI(value meal.Dish) MealDish {
	result := MealDish{ID: value.ID, RecipeRevision: value.RecipeRevision, ShoppingChoices: value.ShoppingChoices, LeftoverOf: value.LeftoverOf, DisplayName: value.DisplayName, Scale: fromRational(value.Scale), Notes: value.Notes, RecipeStatus: string(value.RecipeStatus)}
	if value.EntryID != nil {
		entryID := string(*value.EntryID)
		result.EntryID = &entryID

		if value.RecipeImage != "" {
			result.RecipeImageURL = "/api/v1/recipes/" + entryID + "/assets/" + value.RecipeImage
		}
	}

	for _, item := range value.PlainItems {
		result.PlainItems = append(result.PlainItems, MealPlainItem{ID: item.ID, Name: item.Name, Quantity: item.Quantity})
	}

	return result
}
