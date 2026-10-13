package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/idempotency"
)

// Addition is one manual row with a stable client identity.
type Addition struct {
	ID       string `json:"id" minLength:"1" maxLength:"100"`
	Name     string `json:"name" minLength:"1" maxLength:"200"`
	Buy      string `json:"buy,omitempty" maxLength:"200"`
	Note     string `json:"note,omitempty" maxLength:"2000"`
	Category string `json:"category,omitempty" maxLength:"100"`
}

// AddItems commits an ordered batch with one list revision and durable receipt.
func (s *Service) AddItems(ctx context.Context, key string, items []Addition) (ShoppingList, error) {
	if key == "" || len(key) > 200 || len(items) == 0 {
		return ShoppingList{}, ErrInvalid
	}

	data, err := json.Marshal(items)
	if err != nil {
		return ShoppingList{}, fmt.Errorf("encode grocery additions: %w", err)
	}

	digest := sha256.Sum256(data)
	hash := sha256.Sum256([]byte(key))

	return s.store.EditShoppingList(WithRetryScope(ctx, "add-items"), selectedList(ctx), Edit{OperationID: "add-items:" + hex.EncodeToString(hash[:]), Digest: hex.EncodeToString(digest[:]), Role: access.Editor, Activity: ActivityAdd, Mutate: func(list *ShoppingList) error {
		if list.Revision == 0 && selectedList(ctx) != "groceries" {
			return ErrNotFound
		}

		seen := map[string]bool{}
		for _, item := range items {
			if seen[item.ID] {
				return ErrInvalid
			}

			seen[item.ID] = true
			for _, existing := range list.Items {
				if existing.ID == item.ID {
					return ErrConflict
				}
			}

			name, buy, note, category := item.Name, item.Buy, item.Note, item.Category
			if err := Apply(list, ShoppingChange{ID: hex.EncodeToString(hash[:]), Kind: "add", ItemID: item.ID, Name: &name, Buy: &buy, Note: &note, Category: &category}); err != nil {
				return err
			}
		}

		return nil
	}})
}

// RetryIdentity accepts the existing operation ID or its matching retry header.
func RetryIdentity(body, key string) (string, error) {
	if key == "" {
		return body, nil
	}

	if len(key) > 100 {
		return "", idempotency.ErrInvalid
	}

	if body != "" && body != key {
		return "", idempotency.ErrConflict
	}

	return key, nil
}

type retryScopeKey struct{}

// WithRetryScope namespaces header-backed receipts by actor and operation.
func WithRetryScope(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, retryScopeKey{}, operation)
}

// RetryScope identifies header-backed operation receipts.
func RetryScope(ctx context.Context) string {
	value, _ := ctx.Value(retryScopeKey{}).(string)
	return value
}
