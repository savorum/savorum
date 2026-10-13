package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
)

// TranslationMembers returns the active recipes that declare a translation by
// recipe ID and the active recipes those declarations name.
func (s *Store) TranslationMembers(ctx context.Context) ([]library.TranslationMember, error) {
	if err := s.authorizeScope(ctx, access.Viewer); err != nil {
		return nil, fmt.Errorf("library access: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH declaring AS (
			SELECT document_id, translations_json FROM library_recipes
			WHERE library_id = ? AND deleted_at IS NULL AND translations_json <> '[]'
		), named AS (
			SELECT DISTINCT json_extract(link.value, '$.recipe_id') AS document_id
			FROM declaring, json_each(declaring.translations_json) AS link
		)
		SELECT id, document_id, title, language, translations_json, created_at
		FROM library_recipes
		WHERE library_id = ? AND deleted_at IS NULL
		  AND (translations_json <> '[]' OR document_id IN (SELECT document_id FROM named))
		ORDER BY created_at, id`, scopeID(ctx), scopeID(ctx))
	if err != nil {
		return nil, fmt.Errorf("query translation members: %w", err)
	}
	defer rows.Close()

	members := []library.TranslationMember{}

	for rows.Next() {
		var (
			member    library.TranslationMember
			links     string
			createdAt string
		)
		if err := rows.Scan(&member.ID, &member.DocumentID, &member.Title, &member.Language, &links, &createdAt); err != nil {
			return nil, fmt.Errorf("scan translation member: %w", err)
		}

		if err := json.Unmarshal([]byte(links), &member.Links); err != nil {
			return nil, fmt.Errorf("decode translations of %s: %w", member.ID, err)
		}

		member.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse creation time of %s: %w", member.ID, err)
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate translation members: %w", err)
	}

	return members, nil
}

var _ library.TranslationRepository = (*Store)(nil)
