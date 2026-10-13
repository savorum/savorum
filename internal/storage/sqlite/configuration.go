package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/configuration"
)

// LoadInstanceSettings reads the one ordinary instance-settings record.
func (s *Store) LoadInstanceSettings(ctx context.Context) (configuration.InstanceSettings, error) {
	var (
		settings                         configuration.InstanceSettings
		allowExternalFetch, allowUploads bool
		updatedAt                        string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT display_name, public_url, default_locale, default_measurement,
		       allow_external_fetch, allow_file_uploads,
		       default_print_page_size, default_print_image,
		       revision, updated_at
		FROM instance_settings
		WHERE singleton = 1`).Scan(
		&settings.DisplayName,
		&settings.PublicURL,
		&settings.DefaultLocale,
		&settings.DefaultMeasurement,
		&allowExternalFetch,
		&allowUploads,
		&settings.PrintDefaults.PageSize,
		&settings.PrintDefaults.Image,
		&settings.Revision,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return configuration.InstanceSettings{}, configuration.ErrNotFound
	}

	if err != nil {
		return configuration.InstanceSettings{}, fmt.Errorf("read instance settings: %w", err)
	}

	settings.ImportPolicy.AllowExternalFetch = allowExternalFetch
	settings.ImportPolicy.AllowFileUploads = allowUploads

	settings.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return configuration.InstanceSettings{}, fmt.Errorf("read instance settings timestamp: %w", err)
	}

	return settings, nil
}

// ReplaceInstanceSettings inserts revision one or atomically replaces the
// expected revision. Deployment overrides never call this method.
func (s *Store) ReplaceInstanceSettings(ctx context.Context, expectedRevision uint64, settings configuration.InstanceSettings) (configuration.InstanceSettings, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return configuration.InstanceSettings{}, fmt.Errorf("begin settings update: %w", err)
	}
	defer tx.Rollback()

	updatedAt := settings.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if expectedRevision == 0 {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO instance_settings (
				singleton, display_name, public_url, default_locale, default_measurement,
				allow_external_fetch, allow_file_uploads,
				default_print_page_size, default_print_image, revision, updated_at
			) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			settings.DisplayName,
			settings.PublicURL,
			settings.DefaultLocale,
			settings.DefaultMeasurement,
			settings.ImportPolicy.AllowExternalFetch,
			settings.ImportPolicy.AllowFileUploads,
			settings.PrintDefaults.PageSize,
			settings.PrintDefaults.Image,
			updatedAt,
		)
		if err != nil {
			_ = tx.Rollback()

			if _, loadErr := s.LoadInstanceSettings(ctx); loadErr == nil {
				return configuration.InstanceSettings{}, configuration.ErrConflict
			}

			return configuration.InstanceSettings{}, fmt.Errorf("insert instance settings: %w", err)
		}

		if err := auditIdentity(ctx, tx, attributionActor(ctx), "instance", "settings.changed"); err != nil {
			return configuration.InstanceSettings{}, err
		}

		if err := tx.Commit(); err != nil {
			return configuration.InstanceSettings{}, fmt.Errorf("commit settings update: %w", err)
		}

		return s.LoadInstanceSettings(ctx)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE instance_settings
		SET display_name = ?, public_url = ?, default_locale = ?, default_measurement = ?,
		    allow_external_fetch = ?, allow_file_uploads = ?,
		    default_print_page_size = ?, default_print_image = ?,
		    revision = revision + 1, updated_at = ?
		WHERE singleton = 1 AND revision = ?`,
		settings.DisplayName,
		settings.PublicURL,
		settings.DefaultLocale,
		settings.DefaultMeasurement,
		settings.ImportPolicy.AllowExternalFetch,
		settings.ImportPolicy.AllowFileUploads,
		settings.PrintDefaults.PageSize,
		settings.PrintDefaults.Image,
		updatedAt,
		expectedRevision,
	)
	if err != nil {
		return configuration.InstanceSettings{}, fmt.Errorf("update instance settings: %w", err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return configuration.InstanceSettings{}, fmt.Errorf("count updated instance settings: %w", err)
	}

	if changed != 1 {
		return configuration.InstanceSettings{}, configuration.ErrConflict
	}

	if err := auditIdentity(ctx, tx, attributionActor(ctx), "instance", "settings.changed"); err != nil {
		return configuration.InstanceSettings{}, err
	}

	if err := tx.Commit(); err != nil {
		return configuration.InstanceSettings{}, fmt.Errorf("commit settings update: %w", err)
	}

	return s.LoadInstanceSettings(ctx)
}
