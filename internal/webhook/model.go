// Package webhook delivers the shared domain event contract to explicitly
// configured external receivers. It does not execute third-party code.
package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/savorum/savorum/internal/event"
)

var (
	// ErrNotFound deliberately does not distinguish a missing object from an unavailable scope.
	ErrNotFound = errors.New("webhook not found")
	// ErrConflict requires the caller to reload the current settings revision.
	ErrConflict = errors.New("webhook revision changed")
	// ErrInvalid rejects unsupported subscription settings.
	ErrInvalid = errors.New("invalid webhook configuration")
	// ErrDestination rejects unsafe addresses without disclosing network details.
	ErrDestination = errors.New("destination must be an allowed HTTPS address")
	// ErrCapacity bounds subscriptions, test delivery, and queued work.
	ErrCapacity = errors.New("webhook limit reached")
	// ErrState requires a compatible delivery lifecycle for retry.
	ErrState = errors.New("delivery cannot be retried in its current state")
)

// Input configures a fixed event selection and HTTPS destination.
type Input struct {
	Name       string   `json:"name" minLength:"1" maxLength:"100"`
	URL        string   `json:"url" minLength:"1" maxLength:"2048"`
	EventTypes []string `json:"event_types" minItems:"1" maxItems:"50"`
	Enabled    bool     `json:"enabled"`
}

// Subscription exposes settings and failure state, but never the private signing-key reference.
type Subscription struct {
	UpdatedAt           time.Time `json:"updated_at"`
	UpdatedBy           string    `json:"updated_by"`
	ID                  string    `json:"id"`
	LibraryID           string    `json:"library_id,omitempty"`
	Name                string    `json:"name"`
	URL                 string    `json:"url"`
	EventTypes          []string  `json:"event_types"`
	Enabled             bool      `json:"enabled"`
	Revision            uint64    `json:"revision"`
	CreatedAt           time.Time `json:"created_at"`
	SecretRotatedAt     time.Time `json:"secret_rotated_at"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	PauseReason         string    `json:"pause_reason,omitempty"`
	SecretRef           string    `json:"-"`
}

// Grant reveals a newly created signing secret once.
type Grant struct {
	Subscription Subscription `json:"subscription"`
	Secret       string       `json:"secret"`
}

// Delivery retains a stable logical ID across automatic attempts and explicit redelivery.
type Delivery struct {
	ID             string     `json:"id"`
	SubscriptionID string     `json:"subscription_id"`
	EventID        string     `json:"event_id"`
	EventType      string     `json:"event_type"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	Attempts       int        `json:"attempts"`
	CycleAttempts  int        `json:"-"`
	CycleStartedAt time.Time  `json:"-"`
	LastCode       string     `json:"last_code,omitempty"`
	Body           []byte     `json:"-"`
}

// Attempt records bounded, sanitized diagnostics for one receiver request.
type Attempt struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Status     int       `json:"status,omitempty"`
	Code       string    `json:"code"`
	DurationMS int64     `json:"duration_ms"`
	Excerpt    string    `json:"excerpt,omitempty"`
}

// Detail combines delivery lifecycle with its retained attempt history.
type Detail struct {
	Delivery Delivery  `json:"delivery"`
	Attempts []Attempt `json:"attempts"`
}

// Repository separates authorized management from transactional worker operations.
type Repository interface {
	WebhookSubscriptions(context.Context, string) ([]Subscription, error)
	WebhookSubscription(context.Context, string, string) (Subscription, error)
	SaveWebhook(context.Context, string, Subscription, uint64, string) (Subscription, error)
	DeleteWebhook(context.Context, string, string, uint64) error
	WebhookDeliveries(context.Context, string, string, int, int) ([]Delivery, error)
	WebhookDelivery(context.Context, string, string, string) (Detail, error)
	RetryWebhookDelivery(context.Context, string, string, string) error
	CreateWebhookTest(context.Context, string, string, event.Envelope) (Delivery, error)
	// Worker methods atomically fan out committed facts and record outcomes.
	EnqueueWebhookEvents(context.Context) error
	DueWebhookDeliveries(context.Context) ([]Delivery, error)
	DeliveryWebhook(context.Context, string) (Subscription, error)
	FinishWebhookAttempt(context.Context, Delivery, Attempt, string, *time.Time, int) error
}

// RevisionConflict carries the observed revision without requiring prose parsing.
type RevisionConflict struct{ Expected, Current uint64 }

func (*RevisionConflict) Error() string { return ErrConflict.Error() }
func (*RevisionConflict) Unwrap() error { return ErrConflict }
