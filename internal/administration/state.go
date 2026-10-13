package administration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// StateReport describes the migrated snapshot without exposing application rows or credentials.
type StateReport struct {
	PublicURL      string                `json:"public_url"`
	ProviderID     string                `json:"provider_id"`
	Issuer         string                `json:"issuer"`
	TenantID       string                `json:"tenant_id"`
	InstanceID     string                `json:"instance_id"`
	OwnerUserID    string                `json:"owner_user_id"`
	LibraryID      string                `json:"library_id"`
	Tables         map[string]TableState `json:"tables"`
	WebhookSecrets []string              `json:"webhook_secrets"`
}

// TableState compares complete persisted rows, including historical and disabled records.
type TableState struct {
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}

func inspectState(ctx context.Context, path string) (StateReport, error) {
	state := StateReport{Tables: map[string]TableState{}, WebhookSecrets: []string{}}

	db, err := openReadOnlyDatabase(path)
	if err != nil {
		return state, err
	}
	defer db.Close()

	err = db.QueryRowContext(ctx, `SELECT instance_id FROM instance_identity WHERE singleton=1`).Scan(&state.InstanceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, fmt.Errorf("inspect instance identity: %w", err)
	}

	err = db.QueryRowContext(ctx, `SELECT tenant_id,owner_user_id,library_id,public_url,provider_id,issuer FROM hosting_tenant WHERE singleton=1`).Scan(&state.TenantID, &state.OwnerUserID, &state.LibraryID, &state.PublicURL, &state.ProviderID, &state.Issuer)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, fmt.Errorf("inspect tenant identity: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT DISTINCT secret_ref FROM webhook_subscriptions WHERE deleted_at=0 ORDER BY secret_ref`)
	if err != nil {
		return state, fmt.Errorf("inspect webhook key references: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return state, fmt.Errorf("read webhook key reference: %w", err)
		}

		state.WebhookSecrets = append(state.WebhookSecrets, ref)
	}

	err = rows.Err()
	_ = rows.Close()

	if err != nil {
		return state, fmt.Errorf("read webhook references: %w", err)
	}
	// Application tables exclude derived search indexes and the migration ledger.
	for _, table := range []string{
		"users", "identities", "auth_providers", "libraries", "library_memberships", "library_recipes", "library_assets", "user_preferences", "instance_settings", "instance_identity", "hosting_tenant", "hosting_member_operations", "hosting_member_revocations",
		"recipe_revisions", "recipe_head_revisions", "recipe_revision_assets", "recipe_organization_state", "recipe_facets", "recipe_tags", "recipe_user_state", "recipe_cookings", "recipe_imported_cook_count_baselines", "tags", "facets", "saved_views", "assets",
		"books", "book_sections", "book_entries", "recipe_sources", "book_revisions", "book_head_revisions",
		"meals", "meal_dishes", "meal_dish_plain_items", "saved_meals", "saved_meal_dishes", "saved_meal_dish_plain_items", "shopping_lists", "shopping_operations", "shopping_activity", "calendar_feeds", "pantry_entries",
		"import_jobs", "import_job_items", "import_events", "recipe_source_attachments", "recipe_source_import_baselines", "recipe_source_import_baseline_assets",
		"webhook_subscriptions", "webhook_deliveries", "webhook_attempts", "automation_jobs", "automation_results", "domain_events", "event_consumers", "event_horizons",
	} {
		value, err := inspectTable(ctx, db, table)
		if err != nil {
			return state, err
		}

		state.Tables[table] = value
	}

	return state, nil
}

func inspectTable(ctx context.Context, db *sql.DB, table string) (TableState, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM "`+table+`"`) //nolint:gosec,unqueryvet // Hash every column of these application-owned tables.
	if err != nil {
		return TableState{}, fmt.Errorf("inspect recovery table %s: %w", table, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return TableState{}, fmt.Errorf("inspect columns: %w", err)
	}

	hashes := []string{}

	for rows.Next() {
		values := make([]any, len(columns))

		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}

		if err := rows.Scan(pointers...); err != nil {
			return TableState{}, fmt.Errorf("inspect recovery row: %w", err)
		}

		data, err := json.Marshal(values)
		if err != nil {
			return TableState{}, fmt.Errorf("encode recovery row: %w", err)
		}

		digest := sha256.Sum256(data)
		hashes = append(hashes, hex.EncodeToString(digest[:]))
	}

	if err := rows.Err(); err != nil {
		return TableState{}, fmt.Errorf("inspect recovery rows: %w", err)
	}

	slices.Sort(hashes)
	digest := sha256.Sum256([]byte(strings.Join(hashes, "\n")))

	return TableState{Rows: int64(len(hashes)), SHA256: hex.EncodeToString(digest[:])}, nil
}

// VerifyWebhookSecrets requires each snapshot reference to have a protected regular key file.
func VerifyWebhookSecrets(state StateReport, directory string) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open webhook signing keys: %w", err)
	}
	defer root.Close()

	for _, ref := range state.WebhookSecrets {
		if ref == "" || filepath.Base(ref) != ref || ref == "." || ref == ".." {
			return errors.New("invalid webhook key reference")
		}

		info, err := root.Lstat(ref)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return errors.New("required webhook signing key is missing or unprotected; restore the key or rotate the subscription")
		}

		key, err := root.ReadFile(ref)
		if err != nil || !strings.HasPrefix(string(key), "whsec_") || len(key) < 32 {
			return errors.New("required webhook signing key is invalid; restore the key or rotate the subscription")
		}
	}

	return nil
}
