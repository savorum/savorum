package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/capacity"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// maintenanceFlags are the options the offline database commands share.
type maintenanceFlags struct {
	database string
	assets   string
	timeout  time.Duration
}

func (m *maintenanceFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&m.database, "database", "", "path to the SQLite database")
	cmd.Flags().StringVar(&m.assets, "assets", "", "content-addressed asset storage directory")
	cmd.Flags().DurationVar(&m.timeout, "timeout", 2*time.Minute, "maximum maintenance duration")
	requireFlags(cmd, "database")
	markPathFlags(cmd, "database", "assets")
}

// open opens an existing database under the storage policy the server uses,
// bounded by --timeout. A missing database is an error rather than a new empty
// instance, so a mistyped path cannot look like a working one. The returned
// function closes the database and releases the timeout.
func (m *maintenanceFlags) open(cmd *cobra.Command, savorum *application) (context.Context, *sqlite.Store, func(), error) {
	return m.openInstance(cmd, savorum, false)
}

func (m *maintenanceFlags) openInstance(cmd *cobra.Command, savorum *application, offline bool) (context.Context, *sqlite.Store, func(), error) {
	if m.timeout <= 0 {
		return nil, nil, nil, usageErrorf("timeout must be positive")
	}

	policy, err := savorum.storagePolicy()
	if err != nil {
		return nil, nil, nil, err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), m.timeout)

	instance := administration.Instance{Database: m.database, Assets: resolveAssetPath(m.database, m.assets), Policy: policy}

	open := instance.OpenOnline
	if offline {
		open = instance.OpenExisting
	}

	store, closeStore, err := open(ctx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}

	return ctx, store, func() { closeStore(); cancel() }, nil
}

func newStorageCommand(savorum *application) *cobra.Command {
	var (
		maintenance     maintenanceFlags
		backupDirectory string
	)

	command := &cobra.Command{
		Use:     "storage",
		Short:   "Report database, asset, and backup capacity",
		Long:    "Storage writes a JSON capacity report for the database, its assets, and the\nfilesystem the next backup would be written to.",
		Example: "  savorum storage --database /data/library.db --assets /data/assets --backup-directory /backups",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, store, closeStore, err := maintenance.open(cmd, savorum)
			if err != nil {
				return err
			}
			defer closeStore()

			if backupDirectory == "" {
				backupDirectory = filepath.Dir(maintenance.database)
			}

			report, err := capacity.Inspect(ctx, store, maintenance.database, resolveAssetPath(maintenance.database, maintenance.assets), backupDirectory)
			if err != nil {
				return err
			}

			return writeReport(cmd, report, "storage report")
		},
	}

	maintenance.bind(command)
	command.Flags().StringVar(&backupDirectory, "backup-directory", "", "filesystem used for the next backup")
	markPathFlags(command, "assets", "backup-directory")

	return command
}

func newCheckDatabaseCommand(savorum *application) *cobra.Command {
	var maintenance maintenanceFlags

	command := &cobra.Command{
		Use:     "check-database",
		Short:   "Check database integrity and foreign keys",
		Example: "  savorum check-database --database /data/library.db",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, store, closeStore, err := maintenance.open(cmd, savorum)
			if err != nil {
				return err
			}
			defer closeStore()

			if err := store.IntegrityCheck(ctx); err != nil {
				return err
			}

			savorum.report(cmd, "Database integrity and foreign keys: ok\n")

			return nil
		},
	}

	maintenance.bind(command)

	return command
}

func newRebuildSearchCommand(savorum *application) *cobra.Command {
	var maintenance maintenanceFlags

	command := &cobra.Command{
		Use:     "rebuild-search",
		Short:   "Rebuild the recipe search index",
		Long:    "Rebuild-search recreates the search index from the recipes themselves.\nRecipe history is not touched.",
		Example: "  savorum rebuild-search --database /data/library.db",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, store, closeStore, err := maintenance.open(cmd, savorum)
			if err != nil {
				return err
			}
			defer closeStore()

			if err := store.RebuildSearchIndex(ctx); err != nil {
				return err
			}

			savorum.report(cmd, "Search index rebuilt; recipe history unchanged\n")

			return nil
		},
	}

	maintenance.bind(command)

	return command
}

func newCheckpointCommand(savorum *application) *cobra.Command {
	var maintenance maintenanceFlags

	command := &cobra.Command{
		Use:     "checkpoint",
		Short:   "Checkpoint the write-ahead log",
		Example: "  savorum checkpoint --database /data/library.db",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, store, closeStore, err := maintenance.open(cmd, savorum)
			if err != nil {
				return err
			}
			defer closeStore()

			result, err := store.Checkpoint(ctx)
			if err != nil {
				return err
			}

			return writeReport(cmd, result, "checkpoint report")
		},
	}

	maintenance.bind(command)

	return command
}

// writeReport puts a machine-readable report on standard output.
func writeReport(cmd *cobra.Command, report any, description string) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write %s: %w", description, err)
	}

	return nil
}

func resolveAssetPath(databasePath, configured string) string {
	if configured != "" {
		return configured
	}

	return filepath.Join(filepath.Dir(databasePath), "assets")
}
