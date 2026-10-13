package webhook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/storagebudget"
)

func TestSignatureExactBytesTimestampAndRotation(t *testing.T) {
	now := time.Unix(1800000000, 0)
	body := []byte(`{"delivery_id":"stable","event":{"version":1}}`)

	signed := Signature("first-secret", now, body)
	if !VerifySignature("first-secret", signed, body, now.Add(299*time.Second)) {
		t.Fatal("valid signature rejected")
	}

	for _, value := range []struct {
		secret, header string
		body           []byte
		at             time.Time
	}{
		{"rotated-secret", signed, body, now},
		{"first-secret", signed, append(bytes.Clone(body), ' '), now},
		{"first-secret", signed, body, now.Add(301 * time.Second)},
		{"first-secret", signed, body, now.Add(-301 * time.Second)},
		{"first-secret", "t=9223372036854775808,v1=00", body, now},
		{"first-secret", "t=1800000000,v1=garbage", body, now},
	} {
		if VerifySignature(value.secret, value.header, value.body, value.at) {
			t.Fatalf("invalid signature accepted: %q", value.header)
		}
	}
}

type controlledResolver struct {
	answers [][]net.IPAddr
	calls   int
}

func (r *controlledResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	answer := r.answers[min(r.calls, len(r.answers)-1)]
	r.calls++

	return answer, nil
}

func TestDestinationPolicyRejectsRebindingAndUnsafeRanges(t *testing.T) {
	policy := DestinationPolicy{}
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "192.168.1.1", "::1", "fc00::1", "fe80::1", "0.0.0.0", "224.0.0.1", "::ffff:127.0.0.1"} {
		if policy.addressAllowed(net.ParseIP(address)) {
			t.Errorf("non-public destination allowed: %s", address)
		}
	}

	for _, raw := range []string{"http://example.org", "https://user:secret@example.org", "https://example.org/#secret", "https://example.org:0/", "https://example.org:99999/"} {
		if _, err := destinationURL(raw); !errors.Is(err, ErrDestination) {
			t.Errorf("invalid destination accepted: %s", raw)
		}
	}

	resolver := &controlledResolver{answers: [][]net.IPAddr{{{IP: net.ParseIP("8.8.8.8")}}, {{IP: net.ParseIP("127.0.0.1")}}}}

	policy.resolver = resolver
	if err := policy.Validate(t.Context(), "https://food.test/hook"); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://food.test/hook", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}

	response, err := policy.client().Do(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if !errors.Is(err, ErrDestination) || resolver.calls != 2 {
		t.Fatalf("DNS change bypassed connection policy: %v, lookups=%d", err, resolver.calls)
	}
}

func TestRedirectsPreservePOSTAndRevalidateDestinations(t *testing.T) {
	calls := 0

	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "/accepted")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/unsafe":
			w.Header().Set("Location", "https://127.0.0.2/private")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/downgrade":
			w.Header().Set("Location", "http://127.0.0.1/private")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/loop":
			w.Header().Set("Location", "/loop")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/accepted":
			calls++

			if r.Method != http.MethodPost {
				t.Error("redirect changed method")
			}

			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer receiver.Close()

	policy := DestinationPolicy{TrustedNetworks: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
	client := policy.client()

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("missing controlled transport")
	}

	trusted, ok := receiver.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("missing test certificate trust")
	}

	transport.TLSClientConfig = trusted.TLSClientConfig.Clone()
	defer transport.CloseIdleConnections()

	for _, path := range []string{"/redirect", "/unsafe", "/downgrade", "/loop"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, receiver.URL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}

		response, err := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}

		switch path {
		case "/redirect":
			if err != nil || response.StatusCode != http.StatusNoContent {
				t.Fatalf("safe redirect: %v", err)
			}
		case "/loop":
			if err != nil || response.StatusCode != http.StatusTemporaryRedirect {
				t.Fatalf("redirect bound: %v", err)
			}
		default:
			if !errors.Is(err, ErrDestination) {
				t.Fatalf("unsafe redirect %s: %v", path, err)
			}
		}
	}

	if calls != 1 {
		t.Fatalf("unexpected receiver calls: %d", calls)
	}
}

type deliveryRepository struct {
	Repository
	subscription Subscription
	attempts     []Attempt
	state        string
	next         *time.Time
}

func (r *deliveryRepository) DeliveryWebhook(context.Context, string) (Subscription, error) {
	return r.subscription, nil
}

func (r *deliveryRepository) FinishWebhookAttempt(_ context.Context, _ Delivery, a Attempt, state string, next *time.Time, _ int) error {
	r.attempts = append(r.attempts, a)
	r.state = state
	r.next = next

	return nil
}

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExpiredDeliveryDoesNotSendAfterRestart(t *testing.T) {
	repo := &deliveryRepository{subscription: Subscription{ID: "hook", Enabled: true}}

	service, err := New(repo, t.TempDir(), DestinationPolicy{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	service.client = &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired delivery reached the network")
		return nil, nil
	})}

	delivery := Delivery{ID: "stable", SubscriptionID: "hook", CycleStartedAt: time.Now().Add(-25 * time.Hour), CycleAttempts: 1}
	if err := service.deliver(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}

	if repo.state != "failed" || repo.next != nil || len(repo.attempts) != 1 || repo.attempts[0].Code != "retry-window-expired" {
		t.Fatalf("expired retry was not finalized: %+v", repo)
	}
}

func TestDeliverySigningRetryBoundsAndSanitizedHistory(t *testing.T) {
	repo := &deliveryRepository{}

	service, err := New(repo, t.TempDir(), DestinationPolicy{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	ref, secret, err := service.secret(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	repo.subscription = Subscription{ID: "hook", URL: "https://receiver.example/private-path", Enabled: true, SecretRef: ref}

	body, err := DeliveryBody("stable", event.Envelope{ID: "fact", Type: "recipe.created", Version: 1})
	if err != nil {
		t.Fatal(err)
	}

	delivery := Delivery{ID: "stable", SubscriptionID: "hook", EventID: "fact", EventType: "recipe.created", Body: body, CycleStartedAt: time.Now()}
	status := 503

	service.client = &http.Client{Transport: responseTransport(func(request *http.Request) (*http.Response, error) {
		sent, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}

		if !bytes.Equal(sent, body) || !VerifySignature(secret, request.Header.Get("Savorum-Signature"), sent, time.Now()) || request.Header.Get("Savorum-Delivery-ID") != "stable" || request.Header.Get("Savorum-Event-Version") != "1" {
			t.Error("delivery identity, bytes, or signature changed")
		}

		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"99999"}}, Body: io.NopCloser(strings.NewReader(secret + repo.subscription.URL + "\x00" + strings.Repeat("reply", 200)))}, nil
	})}
	if err = service.deliver(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}

	if repo.state != "retrying" || repo.next == nil || time.Until(*repo.next) > time.Hour {
		t.Fatalf("bounded retry: %s %v", repo.state, repo.next)
	}

	excerpt := repo.attempts[0].Excerpt
	if strings.Contains(excerpt, secret) || strings.Contains(excerpt, repo.subscription.URL) || strings.ContainsRune(excerpt, 0) || len(excerpt) > 512 {
		t.Fatalf("unsafe history: %q", excerpt)
	}

	delivery.CycleAttempts = 7
	if err = service.deliver(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}

	if repo.state != "failed" || repo.next != nil {
		t.Fatal("attempt budget exceeded")
	}

	delivery.CycleAttempts = 0
	status = 400

	if err = service.deliver(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}

	if repo.state != "failed" {
		t.Fatal("permanent rejection retried")
	}

	status = 204

	if err = service.deliver(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}

	if repo.state != "delivered" || repo.attempts[3].Code != "delivered" {
		t.Fatal("success not recorded")
	}
}

func TestSecretEnvelopeRejectionPublishesNothing(t *testing.T) {
	directory := t.TempDir()
	budget := storagebudget.New(t.TempDir(), testDurableLimit)
	budget.AddRetainedRoot(directory)
	budget.SetDatabaseUsage(func(context.Context) (int64, error) { return 0, nil })

	service, err := New(nil, directory, DestinationPolicy{}, 3, budget)
	if err != nil {
		t.Fatal(err)
	}

	orphan := filepath.Join(directory, "orphan")
	if err := os.WriteFile(orphan, make([]byte, storagebudget.SealedWebhookKeySize+1), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, release, err := budget.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, _, err := service.secret(ctx); !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("publication admitted: %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Name() != "orphan" {
		t.Fatalf("rejected secret was published: %v", entries)
	}

	if err := os.Remove(orphan); err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.secret(ctx); err != nil {
		t.Fatalf("cleanup did not restore admission: %v", err)
	}
}
