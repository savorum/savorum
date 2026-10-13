package storagebudget

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// AdmitWebhookKey checks the sealed envelope before publishing a new key. The
// shared lease must cover this check, publication, and database registration.
// Retained orphan keys count too, including during rotation.
func (c *Coordinator) AdmitWebhookKey(ctx context.Context, size int64) error {
	if c == nil {
		return nil
	}

	if size < 0 || size > SealedWebhookKeySize {
		return ErrCapacity
	}

	count := 0

	for _, root := range c.extraRoots {
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return fmt.Errorf("inspect retained webhook keys: %w", err)
		}

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect retained webhook key: %w", err)
			}

			if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > SealedWebhookKeySize {
				return ErrCapacity
			}

			count++
			if count >= NativeWebhookKeys() {
				return ErrCapacity
			}
		}
	}

	return c.AdmitOriginal(ctx, size)
}
