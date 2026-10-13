package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func newBackupCommand(savorum *application) *cobra.Command {
	var (
		databasePath string
		assetPath    string
		output       string
		passwordFile string
		encrypt      bool
	)

	command := &cobra.Command{
		Use:   "backup",
		Short: "Write a restorable archive of the instance",
		Long: "Backup writes one archive holding the database and its assets. It is the\n" +
			"complete copy of an instance; a folder of exported recipes is not. With\n" +
			"--encrypt, the archive and the webhook signing keys are sealed with a password.",
		Example: "  savorum backup --database /data/library.db --output /var/backups/instance.savorum-backup\n" +
			"  savorum backup --database /data/library.db --encrypt --output /var/backups/library.savorum-backup.age",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			policy, err := savorum.storagePolicy()
			if err != nil {
				return err
			}

			store, err := sqlite.Open(cmd.Context(), databasePath, sqlite.WithFilePolicy(policy))
			if err != nil {
				return err
			}
			defer store.Close()

			assets := resolveAssetPath(databasePath, assetPath)
			if !encrypt && passwordFile == "" {
				if err := administration.Backup(cmd.Context(), store, assets, output, time.Now(), policy); err != nil {
					return err
				}

				savorum.report(cmd, "created backup %s\n", output)

				return nil
			}

			password, err := readAccountPassword(cmd, savorum, passwordFile)
			if err != nil {
				return err
			}

			if err := sealedBackup(cmd, store, assets, databasePath, output, password, policy); err != nil {
				return err
			}

			savorum.report(cmd, "created encrypted backup %s\n", output)

			return nil
		},
	}

	command.Flags().StringVar(&databasePath, "database", "", "path to the SQLite database")
	command.Flags().StringVar(&assetPath, "assets", "", "content-addressed asset storage directory")
	command.Flags().StringVar(&output, "output", "", "new backup archive path")
	command.Flags().BoolVar(&encrypt, "encrypt", false, "seal the backup and webhook signing keys with a password")
	command.Flags().StringVar(&passwordFile, "password-file", "", "backup password file, or - for standard input; implies --encrypt")
	requireFlags(command, "database", "output")
	markPathFlags(command, "database", "assets", "output", "password-file")

	return command
}

func newVerifyCommand(savorum *application) *cobra.Command {
	var (
		input, tenant, secrets, passwordFile string
		jsonOutput                           bool
		maximumExpandedBytes                 int64
		metadataFiles                        int
		metadataManifest, metadataDirectory  int64
	)

	command := &cobra.Command{
		Use:   "verify",
		Short: "Report what a backup archive holds",
		Long: "Verify reads an archive without writing anything and reports what it would\n" +
			"restore. Run it before a restore, and after a backup you intend to keep.",
		Example: "  savorum verify --input /var/backups/instance.savorum-backup",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if maximumExpandedBytes <= 0 || maximumExpandedBytes == 1<<63-1 {
				return usageErrorf("--max-expanded-bytes must be between 1 and 9223372036854775806")
			}

			limits := administration.Limits{MaximumExpandedBytes: maximumExpandedBytes, MaximumFiles: metadataFiles, MaximumManifestBytes: metadataManifest, MaximumDirectoryBytes: metadataDirectory}

			sealed, err := administration.IsSealed(input)
			if err != nil {
				return err
			}

			var report administration.Report

			if sealed {
				password, err := readBackupPassword(cmd, savorum, passwordFile)
				if err != nil {
					return err
				}

				report, err = administration.VerifySealed(cmd.Context(), input, password, limits)
				if err != nil {
					return err
				}
			} else if report, err = administration.VerifyWithLimits(cmd.Context(), input, limits); err != nil {
				return err
			}

			if tenant != "" && report.State.TenantID != tenant {
				return fmt.Errorf("backup tenant identity mismatch")
			}

			if secrets != "" && !sealed {
				if err := administration.VerifyWebhookSecrets(report.State, secrets); err != nil {
					return err
				}
			}

			if jsonOutput {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
					return fmt.Errorf("write verification report: %w", err)
				}

				return nil
			}
			// The report is the command's result, not a confirmation of one,
			// so it stays on standard output even when quiet.
			fmt.Fprintf(cmd.OutOrStdout(), "verified %s backup version %d created by Savorum %s at %s (%d files, %d bytes, database schema %d)\n",
				report.Format, report.Version, report.SavorumVersion, report.CreatedAt.Format(time.RFC3339), report.Files, report.Bytes, report.DatabaseSchemaVersion)

			return nil
		},
	}

	command.Flags().StringVar(&input, "input", "", "backup archive path")
	command.Flags().StringVar(&tenant, "tenant-id", "", "require the exact managed tenant identity")
	command.Flags().StringVar(&secrets, "webhook-secrets", "", "require all referenced webhook signing keys in this directory")
	command.Flags().IntVar(&metadataFiles, "max-files", 0, "inventory budget; defaults from memory capacity")
	command.Flags().Int64Var(&metadataManifest, "max-manifest-bytes", 0, "manifest decode budget; defaults from memory capacity")
	command.Flags().Int64Var(&metadataDirectory, "max-directory-bytes", 0, "ZIP directory budget; defaults from inventory budget")
	command.Flags().Int64Var(&maximumExpandedBytes, "max-expanded-bytes", administration.DefaultMaximumExpandedBytes, "maximum combined uncompressed backup bytes")
	command.Flags().BoolVar(&jsonOutput, "json", false, "write a structured verification report")
	command.Flags().StringVar(&passwordFile, "password-file", "", "password file for an encrypted backup, or - for standard input")
	requireFlags(command, "input")
	markPathFlags(command, "input", "password-file")

	return command
}

func newRestoreCommand(savorum *application) *cobra.Command {
	var (
		input                               string
		databasePath                        string
		assetPath                           string
		passwordFile                        string
		maximumExpandedBytes                int64
		metadataFiles                       int
		metadataManifest, metadataDirectory int64
	)

	command := &cobra.Command{
		Use:   "restore",
		Short: "Restore an archive into a new database and asset directory",
		Long: "Restore is an offline operation and writes a new instance. Stop the server\n" +
			"first, and verify the archive before restoring from it.",
		Example: "  savorum restore --input /var/backups/instance.savorum-backup \\\n" +
			"    --database /data/library.db --assets /data/assets",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			policy, err := savorum.storagePolicy()
			if err != nil {
				return err
			}

			if maximumExpandedBytes <= 0 || maximumExpandedBytes == 1<<63-1 {
				return usageErrorf("--max-expanded-bytes must be between 1 and 9223372036854775806")
			}

			limits := administration.Limits{MaximumExpandedBytes: maximumExpandedBytes, MaximumFiles: metadataFiles, MaximumManifestBytes: metadataManifest, MaximumDirectoryBytes: metadataDirectory}
			assets := resolveAssetPath(databasePath, assetPath)

			sealed, err := administration.IsSealed(input)
			if err != nil {
				return err
			}

			if sealed {
				password, err := readBackupPassword(cmd, savorum, passwordFile)
				if err != nil {
					return err
				}

				err = administration.RestoreSealed(cmd.Context(), input, password, databasePath, assets, limits, policy)
				if err != nil {
					return err
				}
			} else if err := administration.RestoreWithLimits(cmd.Context(), input, databasePath, assets, limits, policy); err != nil {
				return err
			}

			savorum.report(cmd, "restored backup to %s\n", databasePath)

			return nil
		},
	}

	command.Flags().StringVar(&input, "input", "", "backup archive path")
	command.Flags().StringVar(&databasePath, "database", "", "new SQLite database path")
	command.Flags().StringVar(&assetPath, "assets", "", "new content-addressed asset storage directory")
	command.Flags().IntVar(&metadataFiles, "max-files", 0, "inventory budget; defaults from memory capacity")
	command.Flags().Int64Var(&metadataManifest, "max-manifest-bytes", 0, "manifest decode budget; defaults from memory capacity")
	command.Flags().Int64Var(&metadataDirectory, "max-directory-bytes", 0, "ZIP directory budget; defaults from inventory budget")
	command.Flags().Int64Var(&maximumExpandedBytes, "max-expanded-bytes", administration.DefaultMaximumExpandedBytes, "maximum combined uncompressed backup bytes")
	command.Flags().StringVar(&passwordFile, "password-file", "", "password file for an encrypted backup, or - for standard input")
	requireFlags(command, "input", "database")
	markPathFlags(command, "input", "database", "assets", "password-file")

	return command
}

// sealedBackup writes a plain archive beside the output, then seals it with the
// webhook signing keys the server keeps beside its database.
func sealedBackup(cmd *cobra.Command, store *sqlite.Store, assets, databasePath, output, password string, policy filepolicy.Policy) error {
	absolute, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve backup output: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absolute), policy.DirectoryMode()); err != nil {
		return fmt.Errorf("create backup output directory: %w", err)
	}

	staging, err := os.MkdirTemp(filepath.Dir(absolute), ".savorum-backup-*")
	if err != nil {
		return fmt.Errorf("create backup staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	archive := filepath.Join(staging, "instance.savorum-backup")
	now := time.Now()

	if err := administration.Backup(cmd.Context(), store, assets, archive, now, policy); err != nil {
		return err
	}

	return administration.Seal(cmd.Context(), archive, databasePath+".webhook-secrets", absolute, password, now)
}

// readBackupPassword reads an existing backup's password from a file, from
// standard input for "-", or from one terminal prompt.
func readBackupPassword(cmd *cobra.Command, savorum *application, path string) (string, error) {
	if path != "" {
		return readPasswordFile(cmd, path)
	}

	if savorum.prompt == nil {
		return "", usageErrorf("--password-file <path|-> is required for an encrypted backup when standard input is not a terminal")
	}

	password, err := savorum.prompt.secret("Backup password: ")
	if err != nil {
		return "", promptFailure(cmd, "read backup password", err)
	}

	return password, nil
}
