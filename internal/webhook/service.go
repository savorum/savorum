package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/storagebudget"

	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/identity"
)

// Service manages signing keys and bounded delivery of committed domain facts.
type Service struct {
	budget       *storagebudget.Coordinator
	directory    string
	repository   Repository
	secrets      *os.Root
	policy       DestinationPolicy
	client       *http.Client
	failureLimit int
	locks        [64]sync.Mutex
}

// New opens a private signing-key root and constructs an outbound client with explicit destination policy.
func New(repository Repository, directory string, policy DestinationPolicy, failureLimit int, budgets ...*storagebudget.Coordinator) (*Service, error) {
	if failureLimit < 1 {
		failureLimit = 10
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, operationError("New", err)
	}

	if err := os.Chmod(directory, 0o700); err != nil { //nolint:gosec // Owner-only directory requires search permission.
		return nil, operationError("New", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, operationError("New", err)
	}

	var budget *storagebudget.Coordinator
	if len(budgets) > 0 {
		budget = budgets[0]
	}

	return &Service{repository: repository, budget: budget, directory: directory, secrets: root, policy: policy, client: policy.client(), failureLimit: failureLimit}, nil
}

// Close releases the signing-key root after delivery has stopped.
func (s *Service) Close() error { return operationError("close private root", s.secrets.Close()) }

func (s *Service) lock(id string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	mu := &s.locks[h.Sum32()%64]
	mu.Lock()

	return mu.Unlock
}

// List returns subscriptions visible to the scope’s current administrator.
func (s *Service) List(ctx context.Context, scope string) ([]Subscription, error) {
	return s.repository.WebhookSubscriptions(ctx, scope)
}

// Get rechecks management authority before exposing subscription settings.
func (s *Service) Get(ctx context.Context, scope, id string) (Subscription, error) {
	return s.repository.WebhookSubscription(ctx, scope, id)
}

func validate(input Input, scope string) error {
	if strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > 100 || len(input.EventTypes) == 0 || len(input.EventTypes) > 50 {
		return ErrInvalid
	}

	for i, kind := range input.EventTypes {
		if !event.ValidType(kind) || kind == "webhook.test" || slices.Contains(input.EventTypes[:i], kind) {
			return ErrInvalid
		}

		if scope != "" && event.ReadCapability(kind) == "instance:admin" {
			return ErrInvalid
		}

		if scope == "" && event.ReadCapability(kind) != "instance:admin" {
			return ErrInvalid
		}
	}

	return nil
}

func (s *Service) secret(ctx context.Context) (string, string, error) {
	ref := "key_" + rand.Text()
	secret := "whsec_" + rand.Text() + rand.Text()

	if err := s.budget.AdmitWebhookKey(ctx, int64(len(secret))); err != nil {
		return "", "", err
	}

	release, err := diskspace.Shared.Reserve(s.directory, uint64(len(secret)), 1, diskspace.ReserveBytes)
	if err != nil {
		return "", "", err
	}
	defer release()

	f, err := s.secrets.OpenFile(ref, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", operationError("secret", err)
	}

	_, err = io.WriteString(f, secret)
	if err == nil {
		err = f.Sync()
	}

	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}

	if err != nil {
		_ = s.secrets.Remove(ref)
		return "", "", operationError("secret", err)
	}

	return ref, secret, nil
}

// Save validates destination and event scope before a conditional configuration write.
func (s *Service) Save(ctx context.Context, scope, id string, input Input, expected uint64) (Grant, error) {
	if err := validate(input, scope); err != nil {
		return Grant{}, operationError("Save", err)
	}
	// Check owner/admin authority before resolving a user-supplied hostname.
	if _, err := s.List(ctx, scope); err != nil {
		return Grant{}, operationError("Save", err)
	}

	if err := s.policy.Validate(ctx, input.URL); err != nil {
		return Grant{}, operationError("Save", err)
	}

	if id == "" {
		id = "wh_" + rand.Text()
	}
	defer s.lock(id)()

	ctx, release, err := s.budget.Acquire(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer release()

	value := Subscription{ID: id, LibraryID: scope, Name: strings.TrimSpace(input.Name), URL: input.URL, EventTypes: slices.Clone(input.EventTypes), Enabled: input.Enabled}

	var secret string
	if expected == 0 {
		ref, key, err := s.secret(ctx)
		if err != nil {
			return Grant{}, operationError("Save", err)
		}

		value.SecretRef = ref
		secret = key
	}

	saved, err := s.repository.SaveWebhook(ctx, scope, value, expected, "settings")
	if err != nil {
		if value.SecretRef != "" {
			_ = s.secrets.Remove(value.SecretRef)
		}

		return Grant{}, operationError("Save", err)
	}

	return Grant{Subscription: saved, Secret: secret}, nil
}

// Rotate serializes with delivery so the old key is retired before returning a replacement.
func (s *Service) Rotate(ctx context.Context, scope, id string, expected uint64) (Grant, error) {
	defer s.lock(id)()

	ctx, release, err := s.budget.Acquire(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer release()

	current, err := s.Get(ctx, scope, id)
	if err != nil {
		return Grant{}, operationError("Rotate", err)
	}

	if current.Revision != expected {
		return Grant{}, &RevisionConflict{Expected: expected, Current: current.Revision}
	}

	previous := current.SecretRef

	ref, secret, err := s.secret(ctx)
	if err != nil {
		return Grant{}, operationError("Rotate", err)
	}

	current.SecretRef = ref

	saved, err := s.repository.SaveWebhook(ctx, scope, current, expected, "rotation")
	if err != nil {
		_ = s.secrets.Remove(ref)
		return Grant{}, operationError("Rotate", err)
	}

	_ = s.secrets.Remove(previous)

	return Grant{Subscription: saved, Secret: secret}, nil
}

// Delete cancels pending delivery before removing the private key.
func (s *Service) Delete(ctx context.Context, scope, id string, expected uint64) error {
	defer s.lock(id)()

	ctx, release, err := s.budget.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	current, err := s.Get(ctx, scope, id)
	if err != nil {
		return operationError("Delete", err)
	}

	if err := s.repository.DeleteWebhook(ctx, scope, id, expected); err != nil {
		return operationError("Delete", err)
	}

	if err := s.secrets.Remove(current.SecretRef); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove retired webhook key: %w", err)
	}

	return nil
}

// Deliveries returns bounded history after checking management authority.
func (s *Service) Deliveries(ctx context.Context, scope, id string, offset, limit int) ([]Delivery, error) {
	return s.repository.WebhookDeliveries(ctx, scope, id, offset, limit)
}

// Delivery returns sanitized attempt diagnostics for an authorized subscription.
func (s *Service) Delivery(ctx context.Context, scope, id, deliveryID string) (Detail, error) {
	return s.repository.WebhookDelivery(ctx, scope, id, deliveryID)
}

// Redeliver queues the original signed-body content under the current subscription key.
func (s *Service) Redeliver(ctx context.Context, scope, id, deliveryID string) error {
	defer s.lock(id)()
	return s.repository.RetryWebhookDelivery(ctx, scope, id, deliveryID)
}

// Test creates a minimal test fact through the real signing and retry pipeline.
func (s *Service) Test(ctx context.Context, scope, id string) (Delivery, error) {
	defer s.lock(id)()

	if _, err := s.Get(ctx, scope, id); err != nil {
		return Delivery{}, operationError("Test", err)
	}

	p, _ := identity.FromContext(ctx)
	fact := event.Envelope{ID: "evt_" + rand.Text(), Version: 1, Type: "webhook.test", OccurredAt: time.Now().UTC(), ActorID: p.UserID, LibraryID: scope, Resource: event.Resource{Type: "webhook", ID: id}, CorrelationID: identity.Correlation(ctx), Payload: event.Payload{Test: true}}

	return s.repository.CreateWebhookTest(ctx, scope, id, fact)
}

// Run owns no core mutations. Only committed outbox facts reach this loop.
func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if err := s.repository.EnqueueWebhookEvents(ctx); err != nil {
			return operationError("Run", err)
		}

		due, err := s.repository.DueWebhookDeliveries(ctx)
		if err != nil {
			return operationError("Run", err)
		}

		for _, delivery := range due {
			if err := s.deliver(ctx, delivery); err != nil {
				return operationError("Run", err)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) deliver(ctx context.Context, delivery Delivery) error {
	defer s.lock(delivery.SubscriptionID)()

	subscription, err := s.repository.DeliveryWebhook(ctx, delivery.SubscriptionID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}

	if err != nil {
		return operationError("deliver", err)
	}

	if !subscription.Enabled {
		return nil
	}

	attempt := Attempt{ID: "attempt_" + rand.Text(), At: time.Now().UTC(), Code: "network-error"}
	if attempt.At.Sub(delivery.CycleStartedAt) >= 24*time.Hour {
		attempt.Code = "retry-window-expired"
		return s.repository.FinishWebhookAttempt(ctx, delivery, attempt, "failed", nil, s.failureLimit)
	}

	started := time.Now()
	retry, retryAfter := s.send(ctx, subscription, delivery, &attempt)

	if ctx.Err() != nil {
		return ctx.Err()
	}

	attempt.DurationMS = time.Since(started).Milliseconds()
	state := "failed"

	var next *time.Time

	if attempt.Code == "delivered" {
		state = "delivered"
	} else if retry && delivery.CycleAttempts < 7 && delivery.Attempts < 31 && time.Since(delivery.CycleStartedAt) < 24*time.Hour {
		state = "retrying"
		delay := retryDelay(delivery.CycleAttempts, retryAfter)
		at := time.Now().UTC().Add(delay)
		next = &at
	}

	return s.repository.FinishWebhookAttempt(ctx, delivery, attempt, state, next, s.failureLimit)
}

// send performs one delivery request and records its outcome in attempt. It
// reports whether the delivery may be retried and any server-requested delay.
func (s *Service) send(ctx context.Context, subscription Subscription, delivery Delivery, attempt *Attempt) (bool, time.Duration) {
	secret, err := s.secrets.ReadFile(subscription.SecretRef)
	if err != nil {
		attempt.Code = "signing-key-unavailable"
		return false, 0
	}

	if _, err := destinationURL(subscription.URL); err != nil {
		attempt.Code = "destination-rejected"
		return false, 0
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.URL, bytes.NewReader(delivery.Body))
	if err != nil {
		attempt.Code = "destination-rejected"
		return false, 0
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Savorum-Webhooks/1")
	request.Header.Set("Savorum-Delivery-ID", delivery.ID)
	request.Header.Set("Savorum-Event-ID", delivery.EventID)
	request.Header.Set("Savorum-Event-Type", delivery.EventType)
	request.Header.Set("Savorum-Event-Version", "1")
	request.Header.Set("Savorum-Signature", Signature(string(secret), attempt.At, delivery.Body))

	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, ErrDestination) {
			attempt.Code = "destination-rejected"
			return false, 0
		}

		return true, 0
	}

	attempt.Status = response.StatusCode
	body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	_ = response.Body.Close()
	excerpt := strings.ReplaceAll(string(body), string(secret), "[redacted]")
	attempt.Excerpt = sanitizeExcerpt(strings.ReplaceAll(excerpt, subscription.URL, "[destination]"))

	retry := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500

	attempt.Code = "receiver-rejected"
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		attempt.Code = "delivered"
		retry = false
	}

	if response.StatusCode >= 300 && response.StatusCode < 400 {
		attempt.Code = "redirect-refused"
	}

	return retry, parseRetryAfter(response.Header.Get("Retry-After"), attempt.At)
}

func sanitizeExcerpt(excerpt string) string {
	excerpt = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}

		return r
	}, excerpt)
	for len(excerpt) > 512 {
		_, size := utf8.DecodeLastRuneInString(excerpt)
		excerpt = excerpt[:len(excerpt)-size]
	}

	return excerpt
}

func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	delay := time.Duration(15*(1<<min(attempt, 8))) * time.Second

	var jitter [1]byte

	_, _ = rand.Read(jitter[:])
	delay += time.Duration(jitter[0]) * delay / 512

	return min(time.Hour, max(delay, retryAfter))
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil {
		return min(time.Hour, max(0, time.Duration(seconds)*time.Second))
	}

	if at, err := http.ParseTime(value); err == nil {
		return min(time.Hour, max(0, at.Sub(now)))
	}

	return 0
}

// DeliveryBody is immutable and signed as serialized bytes, not reconstructed by
// receivers. It intentionally contains no recipe or credential content.
func DeliveryBody(id string, fact event.Envelope) ([]byte, error) {
	data, err := json.Marshal(struct {
		ID    string         `json:"delivery_id"`
		Event event.Envelope `json:"event"`
	}{id, fact})
	if err != nil {
		return nil, fmt.Errorf("encode webhook delivery: %w", err)
	}

	return data, nil
}
