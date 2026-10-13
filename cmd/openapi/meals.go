package main

import (
	"context"
	"errors"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

type schemaMealService struct{}

func (schemaMealService) CreateMeal(context.Context, meal.Meal) (meal.Meal, error) {
	return meal.Meal{}, errors.New("schema-only service")
}

func (schemaMealService) GetMeal(context.Context, string) (meal.Meal, error) {
	return meal.Meal{}, errors.New("schema-only service")
}

func (schemaMealService) ListMeals(context.Context, meal.ListOptions) ([]meal.Meal, int, error) {
	return nil, 0, errors.New("schema-only service")
}

func (schemaMealService) UpdateMeal(context.Context, string, uint64, meal.Meal) (meal.Meal, error) {
	return meal.Meal{}, errors.New("schema-only service")
}

func (schemaMealService) DeleteMeal(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

func (schemaMealService) CopyMeal(context.Context, string, *library.Date, meal.MealType) (meal.Meal, error) {
	return meal.Meal{}, errors.New("schema-only service")
}

func (schemaMealService) CreateSavedMeal(context.Context, meal.SavedMeal) (meal.SavedMeal, error) {
	return meal.SavedMeal{}, errors.New("schema-only service")
}

func (schemaMealService) GetSavedMeal(context.Context, string) (meal.SavedMeal, error) {
	return meal.SavedMeal{}, errors.New("schema-only service")
}

func (schemaMealService) ListSavedMeals(context.Context, int, int) ([]meal.SavedMeal, int, error) {
	return nil, 0, errors.New("schema-only service")
}

func (schemaMealService) UpdateSavedMeal(context.Context, string, uint64, meal.SavedMeal) (meal.SavedMeal, error) {
	return meal.SavedMeal{}, errors.New("schema-only service")
}

func (schemaMealService) DeleteSavedMeal(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

func (schemaMealService) InstantiateSavedMeal(context.Context, string, *library.Date, meal.MealType) (meal.Meal, error) {
	return meal.Meal{}, errors.New("schema-only service")
}
