package main

import (
	"context"
	"time"

	"github.com/savorum/savorum/internal/administration"
	"github.com/spf13/cobra"
)

func newAdministrationCommand(app *application) *cobra.Command {
	command := &cobra.Command{Use: "admin", Short: "Snapshot, recover, inspect, and maintain an instance"}
	requireGroup(command)
	command.AddCommand(newCompatibilityCommand(), newBackupCommand(app), newVerifyCommand(app), newRestoreCommand(app), newStorageCommand(app), newCheckDatabaseCommand(app), newRebuildSearchCommand(app), newCheckpointCommand(app), newCollectAssetsCommand(app), newScrubAssetsCommand(app), newVolumeRecoveryCommand(), newMaintenanceCommand(), newDiskCapacityCommand(), newMigrateCommand(app), newSnapshotCommand(app), newHostingAuthorityCommand(false), newHostingAuthorityCommand(true), newHostingEraseCommand())

	return command
}

func newDiskCapacityCommand() *cobra.Command {
	var (
		database, mount    string
		geometry, distinct bool
	)

	command := &cobra.Command{Use: "disk-capacity", Short: "Report an explicitly selected filesystem", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		var (
			report administration.DiskCapacity
			err    error
		)
		if geometry {
			report, err = administration.ReadDiskGeometry(cmd.Context(), database, mount, func() (administration.Geometry, error) { return administration.ProbeGeometry(mount) })
		} else {
			report, err = administration.ReadDiskCapacity(cmd.Context(), database, mount, func() (uint64, error) { return administration.ProbeFilesystem(mount, distinct) })
		}

		if err != nil {
			return err
		}

		return writeReport(cmd, report, "filesystem report")
	}}
	command.Flags().StringVar(&database, "database", "", "optional database containing the required hosting identity")
	command.Flags().StringVar(&mount, "mount", "", "filesystem directory to inspect")
	command.Flags().BoolVar(&distinct, "distinct-mount", false, "require a filesystem separate from the root filesystem")
	command.Flags().BoolVar(&geometry, "geometry", false, "read exact mounted ext4 block-device geometry on Linux")
	requireFlags(command, "mount")
	markPathFlags(command, "database", "mount")

	return command
}

func newMigrateCommand(app *application) *cobra.Command {
	var maintenance maintenanceFlags

	command := &cobra.Command{Use: "migrate", Short: "Apply database migrations to a stopped instance", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		_, _, closeStore, err := maintenance.openInstance(cmd, app, true)
		if err != nil {
			return err
		}

		closeStore()
		app.report(cmd, "Database migrations applied\n")

		return nil
	}}
	maintenance.bind(command)

	return command
}

func newSnapshotCommand(app *application) *cobra.Command {
	var (
		database, assets, secrets, output string
		maximum                           int64
		timeout                           time.Duration
	)

	command := &cobra.Command{Use: "snapshot", Short: "Prepare a native snapshot tree of a stopped instance", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		policy, err := app.storagePolicy()
		if err != nil {
			return err
		}

		if maximum <= 0 || maximum == 1<<63-1 || timeout <= 0 {
			return usageErrorf("snapshot requires positive byte and duration budgets")
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()

		instance := administration.Instance{Database: database, Assets: resolveAssetPath(database, assets), WebhookSecrets: secrets, Policy: policy}

		pinned, err := instance.Snapshot(ctx, output, time.Now(), maximum)
		if err != nil {
			return err
		}

		return writeReport(cmd, pinned, "snapshot assets")
	}}
	command.Flags().StringVar(&database, "database", "", "existing SQLite database")
	command.Flags().StringVar(&assets, "assets", "", "content-addressed asset directory")
	command.Flags().StringVar(&secrets, "webhook-secrets", "", "referenced key directory; defaults beside the database")
	command.Flags().StringVar(&output, "output", "", "new private snapshot directory on the asset filesystem")
	command.Flags().Int64Var(&maximum, "max-bytes", administration.DefaultMaximumExpandedBytes, "maximum combined snapshot bytes")
	command.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "maximum snapshot duration")
	requireFlags(command, "database", "output")
	markPathFlags(command, "database", "assets", "webhook-secrets", "output")

	return command
}

func newCompatibilityCommand() *cobra.Command {
	return &cobra.Command{Use: "compatibility", Short: "Report this binary's database and backup formats", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		report, err := administration.Compatibility()
		if err != nil {
			return err
		}

		return writeReport(cmd, report, "binary compatibility")
	}}
}
