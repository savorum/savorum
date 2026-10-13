package main

import (
	"context"
	"errors"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/mealcalendar"
	"github.com/savorum/savorum/internal/shopping"
)

type schemaShoppingService struct{}

func (schemaShoppingService) Lists(context.Context) ([]shopping.ShoppingListSummary, error) {
	return nil, errors.New("schema-only service")
}

func (schemaShoppingService) CreateList(context.Context, shopping.ShoppingListInput) (shopping.ShoppingList, error) {
	return shopping.ShoppingList{}, errors.New("schema-only service")
}

func (schemaShoppingService) Get(context.Context) (shopping.ShoppingList, error) {
	return shopping.ShoppingList{}, errors.New("schema-only service")
}

func (schemaShoppingService) Change(context.Context, shopping.ShoppingChange) (shopping.ShoppingList, error) {
	return shopping.ShoppingList{}, errors.New("schema-only service")
}

func (schemaShoppingService) Review(context.Context, shopping.ShoppingReviewInput) (shopping.ShoppingReview, error) {
	return shopping.ShoppingReview{}, errors.New("schema-only service")
}

func (schemaShoppingService) ApplyReview(context.Context, shopping.ShoppingApplyInput) (shopping.ShoppingList, error) {
	return shopping.ShoppingList{}, errors.New("schema-only service")
}

func (schemaShoppingService) RenameList(context.Context, shopping.ShoppingListRename) (shopping.ShoppingList, error) {
	return shopping.ShoppingList{}, errors.New("schema-only service")
}

func (schemaShoppingService) DeleteList(context.Context) error {
	return errors.New("schema-only service")
}

func (schemaShoppingService) Activity(context.Context, int64, int) ([]shopping.ShoppingActivity, error) {
	return nil, errors.New("schema-only service")
}

type schemaMealCalendarService struct{}

func (schemaMealCalendarService) Create(context.Context, string) (mealcalendar.CalendarFeedSecret, error) {
	return mealcalendar.CalendarFeedSecret{}, errors.New("schema-only service")
}

func (schemaMealCalendarService) List(context.Context) ([]mealcalendar.CalendarFeed, error) {
	return nil, errors.New("schema-only service")
}

func (schemaMealCalendarService) Rotate(context.Context, string) (mealcalendar.CalendarFeedSecret, error) {
	return mealcalendar.CalendarFeedSecret{}, errors.New("schema-only service")
}

func (schemaMealCalendarService) Delete(context.Context, string) error {
	return errors.New("schema-only service")
}

func (schemaMealCalendarService) Export(context.Context, library.Date, library.Date) ([]byte, error) {
	return nil, errors.New("schema-only service")
}
