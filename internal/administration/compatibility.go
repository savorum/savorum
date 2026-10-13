package administration

import (
	"github.com/savorum/savorum/internal/recipecodec"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// CompatibilityReport describes persisted formats supported by this binary.
// It provides no authorization to modify an instance or a service guarantee.
type CompatibilityReport struct {
	Version               int                        `json:"version"`
	Migrations            []sqlite.MigrationIdentity `json:"migrations"`
	RecipeDocumentVersion int                        `json:"recipe_document_version"`
	BackupFormat          string                     `json:"backup_format"`
	BackupVersion         int                        `json:"backup_version"`
}

// Compatibility reports compiled format identities without reading instance paths.
func Compatibility() (CompatibilityReport, error) {
	migrations, err := sqlite.MigrationIdentities()
	if err != nil {
		return CompatibilityReport{}, err
	}

	return CompatibilityReport{Version: 1, Migrations: migrations, RecipeDocumentVersion: recipecodec.CurrentVersion, BackupFormat: Format, BackupVersion: Version}, nil
}
