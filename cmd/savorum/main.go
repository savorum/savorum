// Command savorum is the Savorum server and its maintenance tools: serving the
// API and web interface, and the storage, backup, restore, and exchange
// subcommands an operator runs against the same database beside it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/savorum/savorum/internal/hosting"
)

// Exit codes are part of the command contract.
const (
	exitSuccess = 0
	// exitFailure is an operation that was understood and did not succeed.
	exitFailure = 1
	// exitUsage is an unknown command, a rejected flag, or a missing argument.
	exitUsage = 2
	// exitReconcile is a hosting bootstrap that disagrees with the instance.
	exitReconcile = 3
	// exitCanceled follows an interrupt or termination signal during work.
	exitCanceled = 130
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Environ()))
}

// run executes one command against explicit streams and returns its exit code.
// It installs the interrupt and termination handler the whole tree runs under.
func run(arguments []string, input io.Reader, output, diagnostics io.Writer, environ []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	savorum := &application{environ: environ, prompt: terminalPrompt(input, diagnostics)}

	return execute(ctx, savorum, arguments, input, output, diagnostics)
}

// execute runs the command tree and turns its outcome into an exit code. It
// takes the application rather than building one so that a test can supply a
// terminal that answers prompts and a context it can cancel.
func execute(ctx context.Context, savorum *application, arguments []string, input io.Reader, output, diagnostics io.Writer) int {
	root := newRootCommand(savorum)
	root.SetArgs(arguments)
	root.SetIn(input)
	root.SetOut(output)
	root.SetErr(diagnostics)

	command, err := root.ExecuteContextC(ctx)
	if err == nil {
		return exitSuccess
	}

	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(diagnostics, "savorum: canceled")

		return exitCanceled
	}

	// One line for one failure, with no log preamble and no usage dump.
	fmt.Fprintf(diagnostics, "savorum: %v\n", err)

	if _, ok := errors.AsType[usageError](err); ok {
		fmt.Fprintf(diagnostics, "Run '%s --help' for usage.\n", command.CommandPath())

		return exitUsage
	}

	if _, ok := errors.AsType[*hosting.MismatchError](err); ok {
		return exitReconcile
	}

	return exitFailure
}
