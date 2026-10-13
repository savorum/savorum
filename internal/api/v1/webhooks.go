package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/webhook"
)

type (
	webhookPath struct {
		ID string `path:"id"`
	}
	webhookWrite struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
		Body    webhook.Input
	}
	webhookConditional struct {
		ID      string `path:"id"`
		IfMatch string `header:"If-Match"`
	}
	webhookDeliveryPath struct {
		ID         string `path:"id"`
		DeliveryID string `path:"delivery_id"`
	}
	webhookHistoryInput struct {
		ID     string `path:"id"`
		Offset int    `query:"offset" default:"0" minimum:"0" maximum:"1000"`
		Limit  int    `query:"limit" default:"25" minimum:"1" maximum:"100"`
	}
	webhookOutput struct {
		ETag string `header:"ETag"`
		Body webhook.Subscription
	}
	webhookGrantOutput struct {
		ETag string `header:"ETag"`
		Body webhook.Grant
	}
)

func webhookProblem(err error) error {
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
	case errors.Is(err, webhook.ErrNotFound):
		return problem(404, "webhook-not-found", "The subscription or delivery is unavailable")
	case errors.Is(err, webhook.ErrConflict):
		value := problem(412, "webhook-revision-conflict", "This subscription changed. Reload its settings before saving again")
		if conflict, ok := errors.AsType[*webhook.RevisionConflict](err); ok {
			value.Params = map[string]any{"expected_revision": conflict.Expected, "current_revision": conflict.Current}
		}

		return value
	case errors.Is(err, webhook.ErrInvalid):
		return problem(422, "webhook-invalid", "Choose a name and supported events for this scope")
	case errors.Is(err, webhook.ErrDestination):
		return problem(422, "webhook-destination-rejected", "Use an HTTPS destination that resolves to public addresses, or an explicitly trusted deployment network")
	case errors.Is(err, webhook.ErrCapacity):
		return problem(429, "webhook-limit", "The subscription, test, or delivery limit was reached. Wait before retrying")
	case errors.Is(err, webhook.ErrState):
		return problem(409, "webhook-delivery-state", "Enable the subscription before sending a test or retrying a finished delivery. A delivery permits at most 32 attempts")
	default:
		return mapError(err)
	}
}
func webhookETag(revision uint64) string { return revisionETag(revision) }

func registerWebhooks(api huma.API, service *webhook.Service) {
	for _, instance := range []bool{false, true} {
		registerWebhookScope(api, service, instance)
	}
}

func registerWebhookScope(api huma.API, service *webhook.Service, instance bool) {
	base := "/api/v1/webhooks"
	prefix := "webhook"
	scope := access.LibraryID
	label := "library"

	if instance {
		base = "/api/v1/instance-webhooks"
		label = "instance"
		prefix = "instance-webhook"
		scope = func(context.Context) string { return "" }
	}

	description := "Library owners manage this library's subscriptions. Instance administrators manage instance security-event subscriptions through /instance-webhooks. Personal tokens cannot manage either. Destinations require HTTPS and public DNS addresses unless a deployment operator explicitly trusts a private CIDR. DNS is checked on each connection; up to three method-preserving HTTPS redirects are revalidated before following. Events contain IDs and minimal facts, never recipe bodies or credentials. Delivery is at least once. Receivers verify the exact body with HMAC-SHA256 over timestamp + '.' + body using Savorum-Signature: t=<Unix seconds>,v1=<hex>, reject timestamps outside five minutes, then persistently deduplicate delivery_id. Savorum-Delivery-ID and Savorum-Event-ID also occur inside the signed body. Secrets appear once on create or rotate. Rotation immediately invalidates the old key for future attempts, including queued retries. Keep signing-key files with private instance backups. Retries cover network failures, 408, 429, and 5xx: up to eight attempts per cycle within 24 hours, exponential delay from 15 seconds with jitter, capped at one hour, honoring bounded Retry-After. Other statuses end the cycle. History retains up to 1000 finished deliveries per subscription for 30 days and 32 attempts per delivery. Repeated final failures automatically pause delivery; fix the receiver before re-enabling. Paused subscriptions retain queued work but do not subscribe to new events."
	huma.Register(api, huma.Operation{OperationID: "list-" + prefix + "s", Method: http.MethodGet, Path: base, Summary: "List " + label + " webhooks", Description: description, Tags: []string{"Webhooks"}}, func(ctx context.Context, _ *struct{}) (*struct{ Body []webhook.Subscription }, error) {
		v, err := service.List(ctx, scope(ctx))
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &struct{ Body []webhook.Subscription }{v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-" + prefix, Method: http.MethodPost, Path: base, DefaultStatus: 201, Summary: "Create " + label + " webhook", Description: description, Tags: []string{"Webhooks"}}, func(ctx context.Context, in *struct{ Body webhook.Input }) (*webhookGrantOutput, error) {
		v, err := retryCreate(ctx, api, "create-"+prefix, in, func(work context.Context) (webhook.Grant, error) {
			return service.Save(work, scope(work), "", in.Body, 0)
		}, func(work context.Context, id string) (webhook.Grant, error) {
			v, err := service.Get(work, scope(work), id)
			return webhook.Grant{Subscription: v}, err
		})
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &webhookGrantOutput{ETag: webhookETag(v.Subscription.Revision), Body: v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-" + prefix, Method: http.MethodGet, Path: base + "/{id}", Summary: "Read " + label + " webhook", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookPath) (*webhookOutput, error) {
		v, err := service.Get(ctx, scope(ctx), in.ID)
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &webhookOutput{ETag: webhookETag(v.Revision), Body: v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "update-" + prefix, Method: http.MethodPut, Path: base + "/{id}", Summary: "Update " + label + " webhook", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookWrite) (*webhookOutput, error) {
		revision, err := parseRevisionPrecondition(in.IfMatch, "webhook")
		if err != nil {
			return nil, err
		}

		v, err := service.Save(ctx, scope(ctx), in.ID, in.Body, revision)
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &webhookOutput{ETag: webhookETag(v.Subscription.Revision), Body: v.Subscription}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-" + prefix, Method: http.MethodDelete, Path: base + "/{id}", DefaultStatus: 204, Summary: "Delete " + label + " webhook", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookConditional) (*struct{}, error) {
		revision, err := parseRevisionPrecondition(in.IfMatch, "webhook")
		if err != nil {
			return nil, err
		}

		if err = service.Delete(ctx, scope(ctx), in.ID, revision); err != nil {
			return nil, webhookProblem(err)
		}

		return nil, nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-" + prefix + "-secret", Method: http.MethodPost, Path: base + "/{id}/rotate-secret", Summary: "Rotate " + label + " key", Description: "The old secret is retired as soon as rotation completes. All future attempts, including retries of existing deliveries, use the new secret. Install the returned secret before re-enabling a paused subscription. The secret cannot be read later.", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookConditional) (*webhookGrantOutput, error) {
		revision, err := parseRevisionPrecondition(in.IfMatch, "webhook")
		if err != nil {
			return nil, err
		}

		v, err := service.Rotate(ctx, scope(ctx), in.ID, revision)
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &webhookGrantOutput{ETag: webhookETag(v.Subscription.Revision), Body: v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-" + prefix + "-test", Method: http.MethodPost, Path: base + "/{id}/test", DefaultStatus: 202, Summary: "Test " + label + " webhook", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookPath) (*struct{ Body webhook.Delivery }, error) {
		v, err := service.Test(ctx, scope(ctx), in.ID)
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &struct{ Body webhook.Delivery }{v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-" + prefix + "-deliveries", Method: http.MethodGet, Path: base + "/{id}/deliveries", Summary: "List " + label + " deliveries", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookHistoryInput) (*struct{ Body []webhook.Delivery }, error) {
		v, err := service.Deliveries(ctx, scope(ctx), in.ID, in.Offset, in.Limit)
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &struct{ Body []webhook.Delivery }{v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-" + prefix + "-delivery", Method: http.MethodGet, Path: base + "/{id}/deliveries/{delivery_id}", Summary: "Read " + label + " delivery", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookDeliveryPath) (*struct{ Body webhook.Detail }, error) {
		v, err := service.Delivery(ctx, scope(ctx), in.ID, in.DeliveryID)
		if err != nil {
			return nil, webhookProblem(err)
		}

		return &struct{ Body webhook.Detail }{v}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "create-" + prefix + "-redelivery", Method: http.MethodPost, Path: base + "/{id}/deliveries/{delivery_id}/redeliver", DefaultStatus: 202, Summary: "Retry " + label + " delivery", Tags: []string{"Webhooks"}}, func(ctx context.Context, in *webhookDeliveryPath) (*struct {
		Body struct {
			Queued bool `json:"queued"`
		}
	}, error,
	) {
		if err := service.Redeliver(ctx, scope(ctx), in.ID, in.DeliveryID); err != nil {
			return nil, webhookProblem(err)
		}

		return &struct {
			Body struct {
				Queued bool `json:"queued"`
			}
		}{Body: struct {
			Queued bool `json:"queued"`
		}{true}}, nil
	})
}
