package app_test

import (
	"net/http"
	"testing"
)

func TestWebhookSubscriptionsAreManagedAndTested(t *testing.T) {
	t.Parallel()

	for _, scope := range []struct{ path, event string }{{"/api/v1/webhooks", "recipe.created"}, {"/api/v1/instance-webhooks", "user.disabled"}} {
		t.Run(scope.path, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			subscription := map[string]any{"name": "Hook", "url": "https://example.com/hook", "event_types": []string{scope.event}, "enabled": true}
			created := h.send(http.MethodPost, scope.path, subscription, http.StatusCreated)
			path := scope.path + "/" + decode[struct {
				Subscription struct {
					ID string `json:"id"`
				} `json:"subscription"`
			}](t, created).Subscription.ID

			h.send(http.MethodGet, scope.path, nil, http.StatusOK)
			h.send(http.MethodGet, path, nil, http.StatusOK)

			subscription["name"] = "Renamed hook"
			updated := h.send(http.MethodPut, path, subscription, http.StatusOK, "If-Match", created.Header().Get("ETag"))
			h.send(http.MethodPost, path+"/rotate-secret", nil, http.StatusOK, "If-Match", updated.Header().Get("ETag"))

			h.send(http.MethodPost, path+"/test", nil, http.StatusAccepted)

			deliveries := h.send(http.MethodGet, path+"/deliveries", nil, http.StatusOK)
			delivery := decode[[]struct {
				ID string `json:"id"`
			}](t, deliveries)[0].ID

			h.send(http.MethodGet, path+"/deliveries/"+delivery, nil, http.StatusOK)

			current := h.send(http.MethodGet, path, nil, http.StatusOK)
			h.send(http.MethodDelete, path, nil, http.StatusNoContent, "If-Match", current.Header().Get("ETag"))
		})
	}
}
