package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// accountFlags are the options the account commands share. There is
// deliberately no password option: a password would then be visible in the
// process table and the shell history.
type accountFlags struct {
	database     string
	login        string
	display      string
	passwordFile string
}

func (a *accountFlags) bindCredentials(cmd *cobra.Command) {
	cmd.Flags().StringVar(&a.database, "database", "", "path to the SQLite database")
	cmd.Flags().StringVar(&a.passwordFile, "password-file", "", "password file, or - to read the password from standard input")
	requireFlags(cmd, "database")
	markPathFlags(cmd, "database", "password-file")
}

func (a *accountFlags) bindAccount(cmd *cobra.Command) {
	a.bindCredentials(cmd)
	cmd.Flags().StringVar(&a.login, "login", "", "local login name")
	cmd.Flags().StringVar(&a.display, "display-name", "", "display name")
}

// requireAnswerable reports the options a command needs when no prompt can
// supply them, so a script is told what is missing rather than left waiting on
// standard input.
func (a *accountFlags) requireAnswerable(savorum *application, needsLogin bool) error {
	if savorum.prompt != nil {
		return nil
	}

	var missing []string

	if needsLogin && strings.TrimSpace(a.login) == "" {
		missing = append(missing, "--login")
	}

	if a.passwordFile == "" {
		missing = append(missing, "--password-file <path|->")
	}

	switch len(missing) {
	case 0:
		return nil
	case 1:
		return usageErrorf("%s is required when standard input is not a terminal", missing[0])
	default:
		return usageErrorf("%s are required when standard input is not a terminal", strings.Join(missing, " and "))
	}
}

func newUserCommand(savorum *application) *cobra.Command {
	command := &cobra.Command{
		Use:   "user",
		Short: "Bootstrap, create, and recover accounts",
		Long: "The account commands run against a stopped or running instance's database.\n" +
			"They are how an instance gets its first administrator and how a locked-out\n" +
			"person gets back in.",
	}

	requireGroup(command)
	command.AddCommand(
		newCreateAdminCommand(savorum),
		newCreateUserCommand(savorum),
		newResetPasswordCommand(savorum),
	)

	return command
}

func newCreateAdminCommand(savorum *application) *cobra.Command {
	var account accountFlags

	command := &cobra.Command{
		Use:   "create-admin",
		Short: "Create the first administrator",
		Long: "Create-admin bootstraps the instance administrator. It writes the new\n" +
			"account's user ID to standard output, which is the identifier the other\n" +
			"account commands take.",
		Example: "  savorum user create-admin --database /data/savorum.db --login chef\n" +
			"  savorum user create-admin --database /data/savorum.db --login chef --password-file -",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAccountCommand(cmd, savorum, &account, func(ctx context.Context, store *sqlite.Store, login, password string) error {
				principal, err := store.BootstrapAdmin(ctx, login, account.display, password)
				if err != nil {
					return err
				}

				fmt.Fprintln(cmd.OutOrStdout(), principal.UserID)

				return nil
			})
		},
	}

	account.bindAccount(command)

	return command
}

func newCreateUserCommand(savorum *application) *cobra.Command {
	var (
		account accountFlags
		actor   string
	)

	command := &cobra.Command{
		Use:   "create",
		Short: "Create a local account",
		Long: "Create adds a local account on behalf of an administrator, whose user ID\n" +
			"is the --actor. The new account's user ID is written to standard output.",
		Example: "  savorum user create --database /data/savorum.db --actor ADMIN_USER_ID --login sam",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAccountCommand(cmd, savorum, &account, func(ctx context.Context, store *sqlite.Store, login, password string) error {
				principal, err := store.CreateLocalUser(ctx, actor, login, account.display, password)
				if err != nil {
					return err
				}

				fmt.Fprintln(cmd.OutOrStdout(), principal.UserID)

				return nil
			})
		},
	}

	account.bindAccount(command)
	command.Flags().StringVar(&actor, "actor", "", "administrator user ID creating the account")
	requireFlags(command, "actor")

	return command
}

func newResetPasswordCommand(savorum *application) *cobra.Command {
	var (
		account accountFlags
		userID  string
	)

	command := &cobra.Command{
		Use:   "reset-password",
		Short: "Set a new password and revoke browser sessions",
		Long: "Reset-password is the recovery path for an account nobody can sign in to.\n" +
			"It replaces the password and revokes that account's browser sessions. For an\n" +
			"account that signs in only through an external provider, --login gives it a\n" +
			"local login and password.",
		Example: "  savorum user reset-password --database /data/savorum.db --user-id ADMIN_USER_ID\n" +
			"  savorum user reset-password --database /data/savorum.db --user-id OWNER_ID --login owner",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := account.requireAnswerable(savorum, false); err != nil {
				return err
			}

			store, closeStore, err := savorum.openAccounts(cmd.Context(), account.database)
			if err != nil {
				return err
			}
			defer closeStore()

			password, err := readAccountPassword(cmd, savorum, account.passwordFile)
			if err != nil {
				return err
			}

			if login := strings.TrimSpace(account.login); login != "" {
				if err := store.EstablishLocalPassword(cmd.Context(), userID, login, password); errors.Is(err, identity.ErrConflict) {
					return errors.New("the account already has a local login; omit --login to reset its password")
				} else if err != nil {
					return err
				}
			} else if err := store.RecoverPassword(cmd.Context(), userID, password); errors.Is(err, identity.ErrConflict) {
				return errors.New("the account has no local login; add --login to give it one")
			} else if err != nil {
				return err
			}

			savorum.report(cmd, "Password reset; browser sessions revoked.\n")

			return nil
		},
	}

	account.bindCredentials(command)
	command.Flags().StringVar(&account.login, "login", "", "local login name for an account that has none")
	command.Flags().StringVar(&userID, "user-id", "", "application user ID to recover")
	requireFlags(command, "user-id")

	return command
}

// runAccountCommand checks the invocation, opens the database, and only then
// asks for what is missing, so nobody types a password twice to learn the
// database path was wrong.
func runAccountCommand(
	cmd *cobra.Command,
	savorum *application,
	account *accountFlags,
	act func(ctx context.Context, store *sqlite.Store, login, password string) error,
) error {
	if err := account.requireAnswerable(savorum, true); err != nil {
		return err
	}

	store, closeStore, err := savorum.openAccounts(cmd.Context(), account.database)
	if err != nil {
		return err
	}
	defer closeStore()

	login := strings.TrimSpace(account.login)
	if login == "" {
		if login, err = savorum.prompt.line("Login: "); err != nil {
			return promptFailure(cmd, "read login", err)
		}
	}

	password, err := readAccountPassword(cmd, savorum, account.passwordFile)
	if err != nil {
		return err
	}

	return act(cmd.Context(), store, login, password)
}

func (a *application) openAccounts(ctx context.Context, databasePath string) (*sqlite.Store, func(), error) {
	policy, err := a.storagePolicy()
	if err != nil {
		return nil, nil, err
	}

	store, err := sqlite.Open(ctx, databasePath, sqlite.WithFilePolicy(policy))
	if err != nil {
		return nil, nil, fmt.Errorf("open account database: %w", err)
	}

	return store, func() { _ = store.Close() }, nil
}

// readAccountPassword reads the password from the named file, from standard
// input for "-", or from a terminal prompt that asks twice and must match. A
// trailing newline is removed.
func readAccountPassword(cmd *cobra.Command, savorum *application, path string) (string, error) {
	if path != "" {
		return readPasswordFile(cmd, path)
	}

	if savorum.prompt == nil {
		return "", usageErrorf("--password-file <path|-> is required when standard input is not a terminal")
	}

	password, err := savorum.prompt.secret("Password: ")
	if err != nil {
		return "", promptFailure(cmd, "read password (use --password-file for containers)", err)
	}

	confirmation, err := savorum.prompt.secret("Confirm password: ")
	if err != nil {
		return "", promptFailure(cmd, "confirm password", err)
	}

	if password != confirmation {
		return "", errors.New("password confirmation does not match")
	}

	return password, nil
}

func readPasswordFile(cmd *cobra.Command, path string) (string, error) {
	reader := cmd.InOrStdin()

	if path != "-" {
		//nolint:gosec // The instance owner explicitly selects a local password file through the CLI.
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open password file: %w", err)
		}

		defer file.Close()

		reader = file
	}

	data, err := io.ReadAll(io.LimitReader(reader, 1026))
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}

// promptFailure keeps an interrupted prompt an interruption: a terminal read
// ends in an ordinary error when the signal arrives, and only the context says
// which one it was.
func promptFailure(cmd *cobra.Command, action string, err error) error {
	if cause := cmd.Context().Err(); cause != nil {
		return fmt.Errorf("%s: %w", action, cause)
	}

	return fmt.Errorf("%s: %w", action, err)
}
