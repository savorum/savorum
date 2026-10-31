package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/webhook"
)

func TestFinishedWebhookDeliveriesAreRetried(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, scope := range []struct{ path, event string }{{"/api/v1/webhooks", "recipe.created"}, {"/api/v1/instance-webhooks", "user.disabled"}} {
		created := h.send(http.MethodPost, scope.path, map[string]any{"name": "Hook", "url": "https://example.com/hook", "event_types": []string{scope.event}, "enabled": true}, http.StatusCreated)
		path := scope.path + "/" + decode[struct {
			Subscription struct {
				ID string `json:"id"`
			} `json:"subscription"`
		}](t, created).Subscription.ID

		h.send(http.MethodPost, path+"/test", nil, http.StatusAccepted)

		delivery := decode[[]webhook.Delivery](t, h.send(http.MethodGet, path+"/deliveries", nil, http.StatusOK))[0]

		// The receiver is unreachable from a test, so the attempt that would
		// finish this delivery is recorded directly.
		attempt := webhook.Attempt{ID: "attempt-" + delivery.ID, At: time.Now().UTC(), Status: http.StatusNoContent, Code: "delivered"}
		if err := h.store.FinishWebhookAttempt(t.Context(), delivery, attempt, "delivered", nil, 5); err != nil {
			t.Fatal(err)
		}

		h.send(http.MethodPost, path+"/deliveries/"+delivery.ID+"/redeliver", nil, http.StatusAccepted)
	}
}
