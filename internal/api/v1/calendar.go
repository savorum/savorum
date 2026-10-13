package v1

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/mealcalendar"
)

// MealCalendarService publishes the plan to calendar applications.
type MealCalendarService interface {
	Create(ctx context.Context, name string) (mealcalendar.CalendarFeedSecret, error)
	List(ctx context.Context) ([]mealcalendar.CalendarFeed, error)
	Rotate(ctx context.Context, id string) (mealcalendar.CalendarFeedSecret, error)
	Delete(ctx context.Context, id string) error
	Export(ctx context.Context, from, to library.Date) ([]byte, error)
}

type calendarExportInput struct {
	From string `query:"from" required:"true" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"First day to include"`
	To   string `query:"to" required:"true" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"Last day to include, at most 366 days after the first"`
}

type calendarFeedInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" doc:"What the feed is called in calendar applications"`
	}
}

type calendarFeedPath struct {
	ID string `path:"id" maxLength:"100"`
}

type calendarFeedsOutput struct {
	Body struct {
		Items []mealcalendar.CalendarFeed `json:"items"`
	}
}

type calendarSecretOutput struct {
	Body mealcalendar.CalendarFeedSecret
}

func registerMealCalendar(api huma.API, service MealCalendarService) {
	huma.Register(api, huma.Operation{
		OperationID: "export-meal-calendar", Method: http.MethodGet, Path: "/api/v1/meals/calendar",
		Summary:     "Download the plan as iCalendar",
		Description: "Writes the dated meals in a range as all-day events that any calendar application can import. An imported file is a copy: later changes to the plan reach a calendar only through a subscription feed.",
		Tags:        []string{"Plan"}, Errors: []int{http.StatusUnprocessableEntity},
		Responses: map[string]*huma.Response{"200": {Description: "iCalendar document", Content: map[string]*huma.MediaType{"text/calendar": {Schema: &huma.Schema{Type: "string"}}}}},
	}, func(ctx context.Context, input *calendarExportInput) (*huma.StreamResponse, error) {
		document, err := service.Export(ctx, library.Date(input.From), library.Date(input.To))
		if err != nil {
			return nil, mapCalendarError(err)
		}

		return &huma.StreamResponse{Body: func(stream huma.Context) {
			stream.SetHeader("Content-Type", "text/calendar; charset=utf-8")
			stream.SetHeader("Content-Disposition", `attachment; filename="savorum-meal-plan-`+input.From+`-to-`+input.To+`.ics"`)
			stream.SetHeader("Content-Length", strconv.Itoa(len(document)))
			stream.SetHeader("X-Content-Type-Options", "nosniff")
			_, _ = stream.BodyWriter().Write(document)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-calendar-feeds", Method: http.MethodGet, Path: "/api/v1/calendar-feeds",
		Summary:     "List calendar feeds",
		Description: "Lists the feeds the reader created for the selected library. A library owner sees every feed publishing the library's plan so they can revoke any of them. Feed URLs are never listed.",
		Tags:        []string{"Plan"},
	}, func(ctx context.Context, _ *struct{}) (*calendarFeedsOutput, error) {
		feeds, err := service.List(ctx)
		if err != nil {
			return nil, mapCalendarError(err)
		}

		output := &calendarFeedsOutput{}
		output.Body.Items = feeds

		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-calendar-feed", Method: http.MethodPost, Path: "/api/v1/calendar-feeds",
		Summary:     "Create a calendar feed",
		Description: "Creates a read-only feed of the selected library's plan. The response is the only time its URL is shown: anyone holding the URL can read the plan until the feed is rotated or revoked. The feed keeps working only while its creator can read the plan.",
		Tags:        []string{"Plan"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusUnprocessableEntity},
	}, func(ctx context.Context, input *calendarFeedInput) (*calendarSecretOutput, error) {
		secret, err := retryCreate(ctx, api, "create-calendar-feed", input, func(work context.Context) (mealcalendar.CalendarFeedSecret, error) {
			return service.Create(work, input.Body.Name)
		}, func(work context.Context, id string) (mealcalendar.CalendarFeedSecret, error) {
			feeds, err := service.List(work)
			if err != nil {
				return mealcalendar.CalendarFeedSecret{}, err
			}

			for _, feed := range feeds {
				if feed.ID == id {
					return mealcalendar.CalendarFeedSecret{CalendarFeed: feed}, nil
				}
			}

			return mealcalendar.CalendarFeedSecret{}, mealcalendar.ErrNotFound
		})
		if err != nil {
			return nil, mapCalendarError(err)
		}

		return &calendarSecretOutput{Body: secret}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rotate-calendar-feed", Method: http.MethodPost, Path: "/api/v1/calendar-feeds/{id}/rotate",
		Summary:     "Replace a calendar feed's URL",
		Description: "Issues a new URL; the previous one stops working immediately. Calendars subscribed to the old URL must subscribe again.",
		Tags:        []string{"Plan"}, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, input *calendarFeedPath) (*calendarSecretOutput, error) {
		secret, err := service.Rotate(ctx, input.ID)
		if err != nil {
			return nil, mapCalendarError(err)
		}

		return &calendarSecretOutput{Body: secret}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-calendar-feed", Method: http.MethodDelete, Path: "/api/v1/calendar-feeds/{id}",
		Summary: "Revoke a calendar feed", Tags: []string{"Plan"}, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, input *calendarFeedPath) (*noContentOutput, error) {
		if err := service.Delete(ctx, input.ID); err != nil {
			return nil, mapCalendarError(err)
		}

		return &noContentOutput{}, nil
	})
}

func mapCalendarError(err error) error {
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
	case errors.Is(err, mealcalendar.ErrNotFound):
		return problem(http.StatusNotFound, "calendar-feed-not-found", "This calendar feed does not exist or was revoked.")
	case errors.Is(err, mealcalendar.ErrInvalid):
		return problem(http.StatusUnprocessableEntity, "calendar-invalid", "Name the feed, or choose a range of at most a year that ends after it starts.")
	default:
		return problem(http.StatusInternalServerError, "internal-error", "The calendar could not be prepared.")
	}
}
