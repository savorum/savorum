package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// openAssets composes the database and the asset directory, which the asset
// commands need together because a registered asset is a row and a file. The
// returned function closes the database and asset directory handle.
func (a *application) openAssets(ctx context.Context, databasePath, assetPath string) (*assets.Service, func(), error) {
	policy, err := a.storagePolicy()
	if err != nil {
		return nil, nil, err
	}

	store, err := sqlite.Open(ctx, databasePath, sqlite.WithFilePolicy(policy))
	if err != nil {
		return nil, nil, err
	}

	closeStore := func() { _ = store.Close() }

	blobStore, err := files.NewAssetStore(resolveAssetPath(databasePath, assetPath), files.DefaultMaximumAssetSize, files.WithFilePolicy(policy))
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	closeStore = func() {
		_ = blobStore.Close()
		_ = store.Close()
	}

	service, err := assets.NewService(store, blobStore)
	if err != nil {
		closeStore()
		return nil, nil, err
	}

	return service, closeStore, nil
}

func newCollectAssetsCommand(savorum *application) *cobra.Command {
	var (
		databasePath string
		assetPath    string
		grace        time.Duration
	)

	command := &cobra.Command{
		Use:   "gc-assets",
		Short: "Remove orphaned asset files",
		Long: "Gc-assets removes asset files no recipe refers to any more. The grace period\n" +
			"protects an asset that was uploaded but not yet attached.",
		Example: "  savorum gc-assets --database /data/library.db --grace 48h",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, closeStore, err := savorum.openAssets(cmd.Context(), databasePath, assetPath)
			if err != nil {
				return err
			}
			defer closeStore()

			removed, err := service.CollectGarbage(cmd.Context(), grace)
			if err != nil {
				return err
			}

			savorum.report(cmd, "removed %d orphaned asset(s)\n", removed)

			return nil
		},
	}

	command.Flags().StringVar(&databasePath, "database", "", "path to the SQLite database")
	command.Flags().StringVar(&assetPath, "assets", "", "content-addressed asset storage directory")
	command.Flags().DurationVar(&grace, "grace", 24*time.Hour, "minimum age of orphaned assets")
	requireFlags(command, "database")
	markPathFlags(command, "database", "assets")

	return command
}

func newScrubAssetsCommand(savorum *application) *cobra.Command {
	var (
		databasePath string
		assetPath    string
	)

	command := &cobra.Command{
		Use:   "scrub-assets",
		Short: "Verify stored assets against their registered digests",
		Long: "Scrub-assets reads every registered asset and compares it with the digest\n" +
			"recorded for it. Each integrity issue is written to standard output as its\n" +
			"kind and identifier, and the command fails when there is at least one.",
		Example: "  savorum scrub-assets --database /data/library.db --assets /data/assets",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, closeStore, err := savorum.openAssets(cmd.Context(), databasePath, assetPath)
			if err != nil {
				return err
			}
			defer closeStore()

			report, err := service.Scrub(cmd.Context())
			if err != nil {
				return err
			}

			for _, issue := range report.Issues {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", issue.Kind, issue.ID)
			}

			if len(report.Issues) > 0 {
				return fmt.Errorf("asset scrub checked %d registered asset(s) and found %d integrity issue(s)", report.Checked, len(report.Issues))
			}

			savorum.report(cmd, "verified %d registered asset(s); no integrity issues found\n", report.Checked)

			return nil
		},
	}

	command.Flags().StringVar(&databasePath, "database", "", "path to the SQLite database")
	command.Flags().StringVar(&assetPath, "assets", "", "content-addressed asset storage directory")
	requireFlags(command, "database")
	markPathFlags(command, "database", "assets")

	return command
}
