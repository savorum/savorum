package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/storage/filepolicy"
)

// application carries what the command tree needs from the process besides its
// flags and streams.
type application struct {
	environ []string
	// prompt is nil when nothing can answer a question: no terminal, or --quiet.
	prompt prompter
	quiet  bool
}

// usageError marks a mistyped invocation rather than a failed operation. It
// selects exitUsage and is the only error that earns a pointer to help.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }

func (u usageError) Unwrap() error { return u.err }

func usageErrorf(format string, values ...any) error {
	return usageError{err: fmt.Errorf(format, values...)}
}

// usageArgs adapts a Cobra argument validator so a rejected argument exits as
// a usage error.
func usageArgs(validator cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, arguments []string) error {
		if err := validator(cmd, arguments); err != nil {
			return usageError{err: err}
		}

		return nil
	}
}

func newRootCommand(savorum *application) *cobra.Command {
	root := &cobra.Command{
		Use:   "savorum",
		Short: "Serve a Savorum library and maintain the instance beside it",
		Long: "Savorum serves one library and the tools that keep it: accounts, storage\n" +
			"maintenance, backup and restore, and recipe exchange. Every command works\n" +
			"against the same database, so an instance is a database, an asset directory,\n" +
			"and this binary.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          usageArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			return usageErrorf("a command is required")
		},
		// Cobra validates required flags and flag groups after this hook and
		// before RunE. Validating here as well classifies those mistakes as
		// usage errors while still preceding any prompt or database open.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if savorum.quiet {
				savorum.prompt = nil
			}

			if err := cmd.ValidateRequiredFlags(); err != nil {
				return usageError{err: err}
			}

			if err := cmd.ValidateFlagGroups(); err != nil {
				return usageError{err: err}
			}

			return nil
		},
	}

	root.PersistentFlags().BoolVar(&savorum.quiet, "quiet", false,
		"suppress confirmations and never prompt; results and reports are still written")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err: err} })

	root.AddCommand(
		newServeCommand(savorum),
		newUserCommand(savorum),
		newHostingCommand(savorum),
		newAdministrationCommand(savorum),
		newHealthcheckCommand(),
		newStorageCommand(savorum),
		newCheckDatabaseCommand(savorum),
		newRebuildSearchCommand(savorum),
		newCheckpointCommand(savorum),
		newCollectAssetsCommand(savorum),
		newScrubAssetsCommand(savorum),
		newBackupCommand(savorum),
		newVerifyCommand(savorum),
		newRestoreCommand(savorum),
		newExportCommand(savorum),
		newImportCommand(savorum),
	)
	restrictCompletion(root)

	return root
}

// requireGroup makes a command that only holds subcommands report a missing or
// unknown subcommand. Cobra skips argument validation for a command it cannot
// run, so a group needs a RunE to reject one.
func requireGroup(cmd *cobra.Command) {
	cmd.Args = usageArgs(cobra.NoArgs)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return usageErrorf("%s requires a subcommand", cmd.CommandPath())
	}
}

func requireFlags(cmd *cobra.Command, names ...string) {
	for _, name := range names {
		_ = cmd.MarkFlagRequired(name)
	}
}

func markPathFlags(cmd *cobra.Command, names ...string) {
	for _, name := range names {
		_ = cmd.MarkFlagFilename(name)
	}
}

// restrictCompletion keeps completion a question about the command tree and the
// filesystem. No command completes a recipe, a book, or an account: answering a
// keystroke must never open the library or read a secret.
func restrictCompletion(cmd *cobra.Command) {
	if cmd.ValidArgsFunction == nil && len(cmd.ValidArgs) == 0 {
		cmd.ValidArgsFunction = cobra.NoFileCompletions
	}

	for _, child := range cmd.Commands() {
		restrictCompletion(child)
	}
}

// report writes an operator-facing confirmation to the diagnostic stream, and
// writes nothing under --quiet. Results and reports go to standard output and
// are not affected.
func (a *application) report(cmd *cobra.Command, format string, values ...any) {
	if a.quiet {
		return
	}

	fmt.Fprintf(cmd.ErrOrStderr(), format, values...)
}

// storagePolicy is the one deployment value the offline commands share with the
// server, rather than each command interpreting SAVORUM_* for itself.
func (a *application) storagePolicy() (filepolicy.Policy, error) {
	return configuration.ParseStoragePolicy(a.environ)
}

// prompter answers an interactive question.
type prompter interface {
	line(question string) (string, error)
	secret(question string) (string, error)
}

// terminalPrompt returns a prompter when input is a terminal, and nil for a
// pipe, a file, or a container without a TTY.
func terminalPrompt(input io.Reader, diagnostics io.Writer) prompter {
	file, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil
	}

	return &terminalPrompter{input: file, output: diagnostics}
}

// terminalPrompter asks on the diagnostic stream and reads from the terminal,
// so a prompt never lands in a redirected result file.
type terminalPrompter struct {
	input  *os.File
	output io.Writer
}

func (t *terminalPrompter) line(question string) (string, error) {
	fmt.Fprint(t.output, question)

	answer, err := bufio.NewReader(t.input).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read from the terminal: %w", err)
	}

	return strings.TrimSpace(answer), nil
}

func (t *terminalPrompter) secret(question string) (string, error) {
	fmt.Fprint(t.output, question)

	answer, err := term.ReadPassword(int(t.input.Fd()))

	fmt.Fprintln(t.output)

	if err != nil {
		return "", fmt.Errorf("read from the terminal: %w", err)
	}

	return string(answer), nil
}
