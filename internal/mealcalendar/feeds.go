package mealcalendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/meal"
)

// Feed errors.
var (
	ErrNotFound = errors.New("calendar feed not found")
	ErrInvalid  = errors.New("calendar feed request is invalid")
)

// Bounds for feed names and exported ranges.
const (
	MaximumNameLength = 100
	MaximumRangeDays  = 366
	feedPastDays      = 60
	feedFutureDays    = 366
	maximumMeals      = 2000
	tokenPrefix       = "savorum_cal_" //nolint:gosec // a prefix, not a credential
)

// CalendarFeed is a read-only subscription to one library's plan.
type CalendarFeed struct {
	ID            string     `json:"id"`
	LibraryID     string     `json:"-"`
	Name          string     `json:"name"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     string     `json:"created_by"`
	CreatedByName string     `json:"created_by_name"`
	RotatedAt     *time.Time `json:"rotated_at,omitempty"`
	LastReadAt    *time.Time `json:"last_read_at,omitempty"`
	Mine          bool       `json:"mine"`
}

// CalendarFeedSecret carries a feed's URL, which is only shown on creation and rotation.
type CalendarFeedSecret struct {
	CalendarFeed CalendarFeed `json:"feed"`
	Path         string       `json:"path"`
	URL          string       `json:"url,omitempty"`
}

// Persistence stores feeds by token hash.
type Persistence interface {
	CreateCalendarFeed(ctx context.Context, feed CalendarFeed, tokenHash string) error
	ListCalendarFeeds(ctx context.Context) ([]CalendarFeed, error)
	RotateCalendarFeed(ctx context.Context, id, tokenHash string, at time.Time) (CalendarFeed, error)
	DeleteCalendarFeed(ctx context.Context, id string) error
	ResolveCalendarFeed(ctx context.Context, tokenHash string, at time.Time) (CalendarFeed, identity.Principal, error)
}

// MealLister reads the plan.
type MealLister interface {
	ListMeals(ctx context.Context, options meal.ListOptions) ([]meal.Meal, int, error)
}

// Service creates feeds and writes calendars.
type Service struct {
	store     Persistence
	meals     MealLister
	instance  func(context.Context) (string, error)
	publicURL func(context.Context) string
	now       func() time.Time
}

// NewService creates a feed service.
func NewService(store Persistence, meals MealLister, instance func(context.Context) (string, error), publicURL func(context.Context) string) *Service {
	return &Service{store: store, meals: meals, instance: instance, publicURL: publicURL, now: time.Now}
}

// Create makes a feed for the current library.
func (s *Service) Create(ctx context.Context, name string) (CalendarFeedSecret, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > MaximumNameLength {
		return CalendarFeedSecret{}, ErrInvalid
	}

	id, err := randomText(12)
	if err != nil {
		return CalendarFeedSecret{}, err
	}

	token, err := newToken()
	if err != nil {
		return CalendarFeedSecret{}, err
	}

	feed := CalendarFeed{ID: "feed_" + id, LibraryID: access.LibraryID(ctx), Name: name, CreatedAt: s.now().UTC(), Mine: true}
	if err := s.store.CreateCalendarFeed(ctx, feed, hashToken(token)); err != nil {
		return CalendarFeedSecret{}, err
	}

	if principal, ok := identity.FromContext(ctx); ok {
		feed.CreatedBy, feed.CreatedByName = principal.UserID, principal.DisplayName
	}

	return s.secret(ctx, feed, token), nil
}

// List returns the reader's feeds, or every feed for a library owner.
func (s *Service) List(ctx context.Context) ([]CalendarFeed, error) {
	return s.store.ListCalendarFeeds(ctx)
}

// Rotate replaces a feed's URL.
func (s *Service) Rotate(ctx context.Context, id string) (CalendarFeedSecret, error) {
	token, err := newToken()
	if err != nil {
		return CalendarFeedSecret{}, err
	}

	feed, err := s.store.RotateCalendarFeed(ctx, id, hashToken(token), s.now().UTC())
	if err != nil {
		return CalendarFeedSecret{}, err
	}

	return s.secret(ctx, feed, token), nil
}

// Delete revokes a feed.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.DeleteCalendarFeed(ctx, id)
}

// Export writes the plan between two dates for a one-off download.
func (s *Service) Export(ctx context.Context, from, to library.Date) ([]byte, error) {
	start, err := time.Parse(time.DateOnly, from.String())
	if err != nil {
		return nil, ErrInvalid
	}

	end, err := time.Parse(time.DateOnly, to.String())
	if err != nil || end.Before(start) || end.Sub(start) > MaximumRangeDays*24*time.Hour {
		return nil, ErrInvalid
	}

	return s.encode(ctx, "Savorum meal plan", from, to)
}

// Read serves a subscription with its creator's authority, so a feed stops
// working when they lose access to the library.
func (s *Service) Read(ctx context.Context, token string) ([]byte, error) {
	if !strings.HasPrefix(token, tokenPrefix) || len(token) != len(tokenPrefix)+43 {
		return nil, ErrNotFound
	}

	now := s.now().UTC()

	feed, principal, err := s.store.ResolveCalendarFeed(ctx, hashToken(token), now)
	if err != nil {
		return nil, err
	}

	// Storage skips authorization for calls without a principal.
	if principal.UserID == "" || feed.LibraryID == "" {
		return nil, ErrNotFound
	}

	scoped := access.WithLibrary(identity.WithPrincipal(ctx, principal), feed.LibraryID)
	from := library.Date(now.AddDate(0, 0, -feedPastDays).Format(time.DateOnly))
	to := library.Date(now.AddDate(0, 0, feedFutureDays).Format(time.DateOnly))

	document, err := s.encode(scoped, feed.Name, from, to)
	if errors.Is(err, access.ErrForbidden) || errors.Is(err, library.ErrNotFound) {
		return nil, ErrNotFound
	}

	return document, err
}

func (s *Service) encode(ctx context.Context, name string, from, to library.Date) ([]byte, error) {
	meals := []meal.Meal{}

	for len(meals) < maximumMeals {
		page, total, err := s.meals.ListMeals(ctx, meal.ListOptions{DateFrom: &from, DateTo: &to, Limit: meal.MaximumListLimit, Offset: len(meals)})
		if err != nil {
			return nil, err
		}

		meals = append(meals, page...)
		if len(page) == 0 || len(meals) >= total {
			break
		}
	}

	instance, err := s.instance(ctx)
	if err != nil {
		return nil, fmt.Errorf("read instance identity: %w", err)
	}

	planURL := ""
	if base := s.publicURL(ctx); base != "" {
		planURL = base + "/plan"
	}

	return Encode(Calendar{Name: name, Instance: instance, PlanURL: planURL, Stamp: s.now()}, meals), nil
}

func (s *Service) secret(ctx context.Context, feed CalendarFeed, token string) CalendarFeedSecret {
	result := CalendarFeedSecret{CalendarFeed: feed, Path: "/calendar/" + token + ".ics"}
	if base := s.publicURL(ctx); base != "" {
		result.URL = base + result.Path
	}

	return result
}

func newToken() (string, error) {
	value, err := randomText(32)
	if err != nil {
		return "", err
	}

	return tokenPrefix + value, nil
}

func randomText(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate calendar feed secret: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
