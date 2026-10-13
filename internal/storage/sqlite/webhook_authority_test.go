package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/webhook"
)

func TestWebhookDeliveryRequiresCurrentResponsibleOwner(t *testing.T) {
	for _, change := range []string{"removed", "disabled", "erased"} {
		t.Run(change, func(t *testing.T) {
			s, ctx, recipes := integrationFixture(t)
			owner, _ := identity.FromContext(ctx)

			member, err := s.CreateLocalUser(t.Context(), owner.UserID, "member", "Member", "a long member password phrase")
			if err != nil {
				t.Fatal(err)
			}

			if err := s.SetLibraryMember(ctx, "default", member.UserID, access.Owner, false); err != nil {
				t.Fatal(err)
			}

			memberCtx := identity.WithPrincipal(t.Context(), member)

			hook, err := s.SaveWebhook(memberCtx, "default", webhook.Subscription{ID: "member_hook", Name: "Member endpoint", URL: "https://receiver.example/events", EventTypes: []string{"recipe.created"}, Enabled: true, SecretRef: "key"}, 0, "created")
			if err != nil {
				t.Fatal(err)
			}

			document := testfixture.Document()

			document.ID = "second-recipe"
			if _, err := recipes.Create(ctx, document); err != nil {
				t.Fatal(err)
			}

			if err := s.EnqueueWebhookEvents(ctx); err != nil {
				t.Fatal(err)
			}

			if due, err := s.DueWebhookDeliveries(ctx); err != nil || len(due) != 1 {
				t.Fatalf("initial delivery: %v %v", due, err)
			}

			if _, err := s.db.ExecContext(ctx, `UPDATE webhook_subscriptions SET updated_by='' WHERE id=?`, hook.ID); err != nil {
				t.Fatal(err)
			}

			switch change {
			case "removed":
				err = s.SetLibraryMember(ctx, "default", member.UserID, "", false)
			case "disabled":
				err = s.DisableUser(ctx, owner.UserID, member.UserID, true)
			case "erased":
				err = s.DeleteUser(ctx, owner.UserID, member.UserID)
			}

			if err != nil {
				t.Fatal(err)
			}

			if due, err := s.DueWebhookDeliveries(ctx); err != nil || len(due) != 0 {
				t.Fatalf("queued delivery survived %s: %v %v", change, due, err)
			}

			if _, err := s.DeliveryWebhook(ctx, hook.ID); !errors.Is(err, webhook.ErrNotFound) {
				t.Fatalf("send-time authority survived: %v", err)
			}

			if _, err := recipes.Create(ctx, testfixture.Document()); err != nil {
				t.Fatal(err)
			}

			if err := s.EnqueueWebhookEvents(ctx); err != nil {
				t.Fatal(err)
			}

			if values, err := s.WebhookDeliveries(ctx, "default", hook.ID, 0, 25); err != nil || len(values) != 1 {
				t.Fatalf("new events reached departed owner: %v %v", values, err)
			}

			if _, err := s.SaveWebhook(ctx, "default", hook, hook.Revision, "updated"); err != nil {
				t.Fatal(err)
			}

			if _, err := s.DeliveryWebhook(ctx, hook.ID); err != nil {
				t.Fatalf("successor could not adopt integration: %v", err)
			}
		})
	}
}
