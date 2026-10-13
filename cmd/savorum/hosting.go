package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/savorum/savorum/internal/standby"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/hosting"
)

func newHostingCommand(savorum *application) *cobra.Command {
	command := &cobra.Command{
		Use:   "hosting",
		Short: "Bootstrap, inspect, and leave a hosting service",
		Long: "The hosting commands are the operator channel a hosting service uses to\n" +
			"prepare a fresh instance, and the way an owner leaves that service. They\n" +
			"run against the instance's database and name no hosting provider.",
	}

	requireGroup(command)
	command.AddCommand(newHostingBootstrapCommand(savorum), newHostingShowCommand(savorum), newHostingReleaseCommand(savorum), newHostingMemberCommand(), newHostingEraseCommand(), newHostingAuthorityCommand(false), newHostingAuthorityCommand(true))

	return command
}

func newHostingBootstrapCommand(savorum *application) *cobra.Command {
	var database, request string

	command := &cobra.Command{
		Use:   "bootstrap",
		Short: "Prepare a fresh instance for a hosting service",
		Long: "Bootstrap links the hosting service's owner account to the instance\n" +
			"administrator, names the initial library, and records the request. The\n" +
			"result is written to standard output as JSON. Repeating the recorded request\n" +
			"changes nothing. A request that differs from the record, or an instance\n" +
			"already in use, changes nothing and exits with status 3.",
		Example: "  savorum hosting bootstrap --database /data/savorum.db --request /run/bootstrap.json",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			var input hosting.Bootstrap
			if err := readHostingRequest(cmd, request, &input); err != nil {
				return err
			}

			store, closeStore, err := savorum.openAccounts(cmd.Context(), database)
			if err != nil {
				return err
			}
			defer closeStore()

			result, err := store.BootstrapHosting(cmd.Context(), input)
			if err != nil {
				return err
			}

			return writeHostingJSON(cmd.OutOrStdout(), result)
		},
	}

	command.Flags().StringVar(&database, "database", "", "path to the SQLite database")
	command.Flags().StringVar(&request, "request", "", "bootstrap request file, or - to read standard input")
	requireFlags(command, "database", "request")
	markPathFlags(command, "database", "request")

	return command
}

func newHostingShowCommand(savorum *application) *cobra.Command {
	var database string

	command := &cobra.Command{
		Use:   "show",
		Short: "Print the hosting bootstrap record",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, closeStore, err := savorum.openAccounts(cmd.Context(), database)
			if err != nil {
				return err
			}
			defer closeStore()

			tenant, err := store.HostingTenant(cmd.Context())
			if err != nil {
				return err
			}

			return writeHostingJSON(cmd.OutOrStdout(), struct {
				hosting.Bootstrap

				InstanceID  string `json:"instance_id"`
				OwnerUserID string `json:"owner_user_id"`
				LibraryID   string `json:"library_id"`
			}{tenant.Bootstrap, tenant.InstanceID, tenant.OwnerUserID, tenant.LibraryID})
		},
	}

	command.Flags().StringVar(&database, "database", "", "path to the SQLite database")
	requireFlags(command, "database")
	markPathFlags(command, "database")

	return command
}

func newHostingReleaseCommand(savorum *application) *cobra.Command {
	var database string

	command := &cobra.Command{
		Use:   "release",
		Short: "Leave the hosting service",
		Long: "Release removes the bootstrap record, disables the hosting service's sign-in\n" +
			"provider, and revokes its sessions and personal tokens. It refuses until an active administrator\n" +
			"owns the initial library and has a local password. Remove the managed provider from the provider\n" +
			"configuration before the next start.",
		Example: "  savorum user reset-password --database /data/savorum.db --user-id OWNER_ID --login owner\n" +
			"  savorum hosting release --database /data/savorum.db",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, closeStore, err := savorum.openAccounts(cmd.Context(), database)
			if err != nil {
				return err
			}
			defer closeStore()

			tenant, err := store.ReleaseHosting(cmd.Context())
			if err != nil {
				return err
			}

			savorum.report(cmd, "Released from hosting; provider %s is disabled and its sessions and personal tokens are revoked.\n", tenant.ProviderID)

			return nil
		},
	}

	command.Flags().StringVar(&database, "database", "", "path to the SQLite database")
	requireFlags(command, "database")
	markPathFlags(command, "database")

	return command
}

func readHostingRequest(cmd *cobra.Command, path string, into interface{ Validate() error }) error {
	reader := cmd.InOrStdin()

	if path != "-" {
		//nolint:gosec // The operator names the request file.
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open bootstrap request: %w", err)
		}
		defer file.Close()

		reader = file
	}

	decoder := json.NewDecoder(io.LimitReader(reader, 65536))
	decoder.DisallowUnknownFields()

	var extra any

	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("decode bootstrap request: %w", err)
	}

	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return errors.New("bootstrap request must contain one JSON object")
	}

	if err := into.Validate(); err != nil {
		return fmt.Errorf("validate hosting request: %w", err)
	}

	return nil
}

func writeHostingJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write result: %w", err)
	}

	return nil
}

func newHostingMemberCommand() *cobra.Command {
	var database, request, encoded, socket string

	command := &cobra.Command{Use: "member", Short: "Accept a library invitation for a managed member", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		if encoded != "" {
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return usageErrorf("invalid base64 member request")
			}

			cmd.SetIn(bytes.NewReader(raw))

			request = "-"
		}

		var input hosting.MemberRequest
		if err := readHostingRequest(cmd, request, &input); err != nil {
			return err
		}

		if database != "" || socket == "" {
			return usageErrorf("hosting member requires --socket for the live guarded instance; --database is unsupported")
		}

		result, err := hosting.RequestMember(cmd.Context(), socket, input)
		if err != nil {
			return err
		}

		return writeHostingJSON(cmd.OutOrStdout(), result)
	}}
	command.Flags().StringVar(&database, "database", "", "path to the SQLite database")
	command.Flags().StringVar(&request, "request", "", "member request file, or - for standard input")
	command.Flags().StringVar(&encoded, "request-base64", "", "base64 member request, for callers that cannot supply a file or standard input")
	command.Flags().StringVar(&socket, "socket", "", "private live instance operator socket")
	command.MarkFlagsOneRequired("request", "request-base64")
	command.MarkFlagsMutuallyExclusive("request", "request-base64")
	markPathFlags(command, "database", "request", "socket")

	return command
}

func newMaintenanceCommand() *cobra.Command {
	var (
		socket, id, expires, expected string
		release                       bool
	)

	command := &cobra.Command{Use: "maintenance", Short: "Acquire, renew, or release an expiring awake lease", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		change := standby.LeaseChange{ID: id, Release: release}

		var err error
		if !release {
			change.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
			if err != nil {
				return usageErrorf("expires must be an absolute RFC3339 time")
			}
		}

		if expected != "" {
			change.Expected, err = time.Parse(time.RFC3339Nano, expected)
			if err != nil {
				return usageErrorf("expected must be an absolute RFC3339 time")
			}
		}

		if err := administration.ChangeMaintenanceLease(cmd.Context(), socket, change); err != nil {
			return err
		}

		return writeHostingJSON(cmd.OutOrStdout(), change)
	}}
	command.Flags().StringVar(&socket, "socket", "", "local maintenance socket")
	command.Flags().StringVar(&id, "lease", "", "unique operation lease ID (24-128 letters, digits, underscores or hyphens)")
	command.Flags().StringVar(&expires, "expires", "", "absolute RFC3339 deadline, at most 15 minutes ahead")
	command.Flags().StringVar(&expected, "expected", "", "previous expiry for renewal or release")
	command.Flags().BoolVar(&release, "release", false, "release the lease with its expected expiry")
	requireFlags(command, "socket", "lease")

	return command
}

func newHostingEraseCommand() *cobra.Command {
	var (
		database, socket string
		input            hosting.Erasure
	)

	command := &cobra.Command{Use: "erase-user", Short: "Erase a managed identity and its private collections", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		if err := input.Validate(); err != nil {
			return err
		}

		if database != "" || socket == "" {
			return usageErrorf("erase-user requires --socket for the live guarded instance; --database is unsupported")
		}

		if err := administration.RequestErasure(cmd.Context(), socket, input); err != nil {
			return err
		}

		return writeHostingJSON(cmd.OutOrStdout(), map[string]string{"user_id": input.UserID, "outcome": "erased"})
	}}
	command.Flags().StringVar(&database, "database", "", "unsupported offline database path; use --socket")
	command.Flags().StringVar(&socket, "socket", "", "private live instance operator socket")
	command.Flags().StringVar(&input.TenantID, "tenant", "", "recorded hosting tenant ID")
	command.Flags().StringVar(&input.BootstrapID, "bootstrap", "", "recorded bootstrap operation ID")
	command.Flags().StringVar(&input.Subject, "subject", "", "recorded hosting account subject")
	command.Flags().StringVar(&input.UserID, "user", "", "exact local user ID")
	command.Flags().StringVar(&input.Successor, "successor", "", "accepted successor's existing hosting account subject")
	requireFlags(command, "socket", "tenant", "bootstrap", "subject", "user")

	return command
}

func newHostingAuthorityCommand(revoke bool) *cobra.Command {
	var (
		database, socket string
		input            hosting.MemberAuthority
	)

	use, short, outcome := "check-owner", "Check local authority for hosting ownership", "authorized"
	if revoke {
		use, short, outcome = "revoke-member", "Revoke a managed member's credentials and household access", "revoked"
	}

	command := &cobra.Command{Use: use, Short: short, Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		if err := input.Validate(); err != nil {
			return err
		}

		if database != "" || socket == "" {
			return usageErrorf("authority requires --socket for the live guarded instance; --database is unsupported")
		}

		if err := administration.RequestAuthority(cmd.Context(), socket, input, revoke); err != nil {
			return err
		}

		return writeHostingJSON(cmd.OutOrStdout(), map[string]string{"user_id": input.UserID, "outcome": outcome})
	}}
	command.Flags().StringVar(&database, "database", "", "unsupported offline database path; use --socket")
	command.Flags().StringVar(&socket, "socket", "", "private live instance operator socket")
	command.Flags().StringVar(&input.OperationID, "operation", "", "unique private operation ID")
	command.Flags().StringVar(&input.TenantID, "tenant", "", "recorded hosting tenant ID")
	command.Flags().StringVar(&input.BootstrapID, "bootstrap", "", "recorded bootstrap operation ID")
	command.Flags().StringVar(&input.Subject, "subject", "", "recorded hosting account subject")
	command.Flags().StringVar(&input.UserID, "user", "", "exact local user ID")
	command.Flags().StringVar(&input.LibraryID, "library", "", "exact initial library ID")
	requireFlags(command, "socket", "operation", "tenant", "bootstrap", "subject", "user", "library")

	return command
}
