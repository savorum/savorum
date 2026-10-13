package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// result is one command run through the same entry point the process uses.
type result struct {
	code   int
	stdout string
	stderr string
}

// invocation is a command about to be run, with the process inputs a test
// varies: standard input, the environment, the terminal, and the context.
type invocation struct {
	arguments []string
	input     string
	environ   []string
	prompt    prompter
	ctx       context.Context //nolint:containedctx // the case under test is cancellation during a command
}

var userCommands sync.Mutex

func (i invocation) run(t *testing.T) result {
	t.Helper()

	ctx := i.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	var stdout, stderr bytes.Buffer

	// Commands run in this process share its password-work admission limit.
	if len(i.arguments) > 0 && i.arguments[0] == "user" {
		userCommands.Lock()
		defer userCommands.Unlock()
	}

	savorum := &application{environ: i.environ, prompt: i.prompt}
	code := execute(ctx, savorum, i.arguments, strings.NewReader(i.input), &stdout, &stderr)

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// savorum runs a command the way a container or a script does: no terminal and
// nothing on standard input.
func savorum(t *testing.T, arguments ...string) result {
	t.Helper()

	return invocation{arguments: arguments}.run(t)
}

func TestHelpIsAvailableAtEveryLevel(t *testing.T) {
	t.Parallel()

	for name, arguments := range map[string][]string{
		"root flag":         {"--help"},
		"root command":      {"help"},
		"serve":             {"serve", "--help"},
		"user group":        {"user", "--help"},
		"user subcommand":   {"user", "create-admin", "--help"},
		"export group":      {"export", "--help"},
		"export subcommand": {"export", "recipe", "--help"},
		"import":            {"import", "--help"},
		"backup":            {"backup", "--help"},
		"restore":           {"restore", "--help"},
		"verify":            {"verify", "--help"},
		"storage":           {"storage", "--help"},
		"check-database":    {"check-database", "--help"},
		"rebuild-search":    {"rebuild-search", "--help"},
		"checkpoint":        {"checkpoint", "--help"},
		"gc-assets":         {"gc-assets", "--help"},
		"scrub-assets":      {"scrub-assets", "--help"},
		"healthcheck":       {"healthcheck", "--help"},
		"help for a child":  {"help", "user", "create"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			outcome := savorum(t, arguments...)
			if outcome.code != exitSuccess {
				t.Fatalf("help exit code = %d, stderr = %q", outcome.code, outcome.stderr)
			}

			if !strings.Contains(outcome.stdout, "Usage:") {
				t.Fatalf("help stdout = %q", outcome.stdout)
			}

			if outcome.stderr != "" {
				t.Fatalf("help wrote diagnostics: %q", outcome.stderr)
			}
		})
	}
}

func TestHelpDoesNotTouchTheInstance(t *testing.T) {
	t.Parallel()

	database := filepath.Join(t.TempDir(), "library.db")

	for name, arguments := range map[string][]string{
		"account bootstrap": {"user", "create-admin", "--database", database, "--login", "chef", "--help"},
		"maintenance":       {"check-database", "--database", database, "--help"},
		"export":            {"export", "library", "--database", database, "--output", "-", "--help"},
	} {
		t.Run(name, func(t *testing.T) {
			outcome := savorum(t, arguments...)
			if outcome.code != exitSuccess || outcome.stderr != "" {
				t.Fatalf("help exit code = %d, stderr = %q", outcome.code, outcome.stderr)
			}
		})
	}

	if _, err := os.Stat(database); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("help created %s: %v", database, err)
	}
}

func TestUnknownCommandsAndInvalidFlagsAreUsageErrors(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		arguments []string
		contains  string
	}{
		"no command":         {arguments: nil, contains: "a command is required"},
		"unknown command":    {arguments: []string{"marinate"}, contains: `unknown command "marinate"`},
		"unknown subcommand": {arguments: []string{"user", "promote"}, contains: `unknown command "promote" for "savorum user"`},
		"bare group":         {arguments: []string{"export"}, contains: "requires a subcommand"},
		"unknown flag":       {arguments: []string{"serve", "--databse", "x.db"}, contains: "unknown flag"},
		"invalid flag value": {arguments: []string{"gc-assets", "--database", "x.db", "--grace", "soon"}, contains: "invalid argument"},
		"missing flag":       {arguments: []string{"verify"}, contains: `required flag(s) "input" not set`},
		"two missing flags":  {arguments: []string{"backup"}, contains: `required flag(s) "database", "output" not set`},
		"missing argument":   {arguments: []string{"export", "recipe", "--database", "x.db"}, contains: "accepts 1 arg(s), received 0"},
		"extra argument":     {arguments: []string{"checkpoint", "--database", "x.db", "now"}, contains: `unknown command "now"`},
		"invalid choice":     {arguments: []string{"import", "--database", "x.db", "--input", "x", "--on-duplicate", "merge"}, contains: "not one of reject, skip, or replace"},
		"streamed library":   {arguments: []string{"export", "library", "--database", "x.db", "--output", "-"}, contains: "must be a directory"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			outcome := savorum(t, test.arguments...)
			if outcome.code != exitUsage {
				t.Fatalf("exit code = %d, want %d (stderr %q)", outcome.code, exitUsage, outcome.stderr)
			}

			if !strings.Contains(outcome.stderr, test.contains) {
				t.Fatalf("stderr = %q, want to contain %q", outcome.stderr, test.contains)
			}

			if !strings.Contains(outcome.stderr, "for usage.") {
				t.Fatalf("usage error did not point at help: %q", outcome.stderr)
			}

			if outcome.stdout != "" {
				t.Fatalf("usage error wrote to standard output: %q", outcome.stdout)
			}
		})
	}
}

func TestAFailedOperationIsReportedOnceWithoutUsageOrLogPreamble(t *testing.T) {
	t.Parallel()

	outcome := savorum(t, "check-database", "--database", filepath.Join(t.TempDir(), "absent.db"))
	if outcome.code != exitFailure {
		t.Fatalf("exit code = %d, want %d", outcome.code, exitFailure)
	}

	lines := strings.Split(strings.TrimRight(outcome.stderr, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("failure reported in %d lines: %q", len(lines), outcome.stderr)
	}

	if !strings.HasPrefix(lines[0], "savorum: inspect existing database:") {
		t.Fatalf("failure line = %q", lines[0])
	}

	for _, forbidden := range []string{"savorum stopped", "level=ERROR", "Usage:", "for usage."} {
		if strings.Contains(outcome.stderr, forbidden) {
			t.Fatalf("failure output contains %q: %q", forbidden, outcome.stderr)
		}
	}

	if outcome.stdout != "" {
		t.Fatalf("failure wrote to standard output: %q", outcome.stdout)
	}
}

func TestCompletionAnswersWithoutOpeningTheLibrary(t *testing.T) {
	t.Parallel()

	database := filepath.Join(t.TempDir(), "library.db")

	script := savorum(t, "completion", "bash")
	if script.code != exitSuccess || !strings.Contains(script.stdout, "savorum") {
		t.Fatalf("completion script code = %d, stdout = %q", script.code, truncate(script.stdout))
	}

	for name, arguments := range map[string][]string{
		"entry identifiers": {"__complete", "export", "recipe", "--database", database, ""},
		"account flags":     {"__complete", "user", "create-admin", "--database", database, "--"},
		"password file":     {"__complete", "user", "create-admin", "--password-file", ""},
	} {
		t.Run(name, func(t *testing.T) {
			outcome := savorum(t, arguments...)
			if outcome.code != exitSuccess {
				t.Fatalf("completion exit code = %d, stderr = %q", outcome.code, outcome.stderr)
			}
		})
	}

	if _, err := os.Stat(database); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completion created %s: %v", database, err)
	}
}

func truncate(value string) string {
	if len(value) > 200 {
		return value[:200] + "..."
	}

	return value
}
