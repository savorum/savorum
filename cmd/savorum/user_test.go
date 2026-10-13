package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPassword = "a-long-enough-password"

// scriptedPrompt stands in for a person at a terminal: it answers in order, and
// can fail the way an interrupted read does.
type scriptedPrompt struct {
	lines   []string
	secrets []string
	fail    func() error
}

func (s *scriptedPrompt) line(string) (string, error) {
	return s.next(&s.lines)
}

func (s *scriptedPrompt) secret(string) (string, error) {
	return s.next(&s.secrets)
}

func (s *scriptedPrompt) next(answers *[]string) (string, error) {
	if s.fail != nil {
		return "", s.fail()
	}

	if len(*answers) == 0 {
		return "", io.EOF
	}

	answer := (*answers)[0]
	*answers = (*answers)[1:]

	return answer, nil
}

func TestAccountCommandsRefuseToWaitForAnAnswerNobodyCanGive(t *testing.T) {
	t.Parallel()

	database := filepath.Join(t.TempDir(), "library.db")

	for name, test := range map[string]struct {
		arguments []string
		contains  string
	}{
		"login and password": {
			arguments: []string{"user", "create-admin", "--database", database},
			contains:  "--login and --password-file <path|-> are required when standard input is not a terminal",
		},
		"password only": {
			arguments: []string{"user", "create-admin", "--database", database, "--login", "chef"},
			contains:  "--password-file <path|-> is required when standard input is not a terminal",
		},
		"recovery password": {
			arguments: []string{"user", "reset-password", "--database", database, "--user-id", "someone"},
			contains:  "--password-file <path|-> is required when standard input is not a terminal",
		},
	} {
		t.Run(name, func(t *testing.T) {
			outcome := savorum(t, test.arguments...)
			if outcome.code != exitUsage {
				t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
			}

			if !strings.Contains(outcome.stderr, test.contains) {
				t.Fatalf("stderr = %q, want to contain %q", outcome.stderr, test.contains)
			}
		})
	}
	// The refusal comes before anything is opened, so a script that forgot an
	// option has not also created an instance.
	if _, err := os.Stat(database); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused command created %s: %v", database, err)
	}
}

func TestTheSecurePasswordPathsCreateAnAdministratorWithoutRevealingTheSecret(t *testing.T) {
	t.Parallel()

	passwordFile := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordFile, []byte(testPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		arguments []string
		input     string
	}{
		"password file":  {arguments: []string{"--password-file", passwordFile}},
		"standard input": {arguments: []string{"--password-file", "-"}, input: testPassword + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			database := filepath.Join(t.TempDir(), "library.db")
			arguments := append([]string{"user", "create-admin", "--database", database, "--login", "chef"}, test.arguments...)

			outcome := invocation{arguments: arguments, input: test.input}.run(t)
			if outcome.code != exitSuccess {
				t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
			}

			if userID := strings.TrimSpace(outcome.stdout); userID == "" || strings.Contains(userID, "\n") {
				t.Fatalf("standard output = %q, want one user ID", outcome.stdout)
			}

			if strings.Contains(outcome.stdout+outcome.stderr, testPassword) {
				t.Fatalf("the password was written back: %q %q", outcome.stdout, outcome.stderr)
			}
		})
	}
}

func TestQuietKeepsResultsAndDropsConfirmations(t *testing.T) {
	t.Parallel()

	database := filepath.Join(t.TempDir(), "library.db")

	created := invocation{
		arguments: []string{"--quiet", "user", "create-admin", "--database", database, "--login", "chef", "--password-file", "-"},
		input:     testPassword,
		prompt:    &scriptedPrompt{},
	}.run(t)
	if created.code != exitSuccess {
		t.Fatalf("exit code = %d, stderr = %q", created.code, created.stderr)
	}

	userID := strings.TrimSpace(created.stdout)
	if userID == "" {
		t.Fatalf("quiet dropped the new user ID: %q", created.stdout)
	}

	if created.stderr != "" {
		t.Fatalf("quiet wrote diagnostics: %q", created.stderr)
	}

	reset := invocation{
		arguments: []string{"user", "reset-password", "--database", database, "--user-id", userID, "--password-file", "-"},
		input:     "another-long-password",
	}.run(t)
	if reset.code != exitSuccess {
		t.Fatalf("reset exit code = %d, stderr = %q", reset.code, reset.stderr)
	}

	if reset.stdout != "" {
		t.Fatalf("a confirmation reached standard output: %q", reset.stdout)
	}

	if !strings.Contains(reset.stderr, "browser sessions revoked") {
		t.Fatalf("reset stderr = %q", reset.stderr)
	}
}

func TestAPromptedPasswordMustBeConfirmed(t *testing.T) {
	t.Parallel()

	outcome := invocation{
		arguments: []string{"user", "create-admin", "--database", filepath.Join(t.TempDir(), "library.db")},
		prompt:    &scriptedPrompt{lines: []string{"chef"}, secrets: []string{testPassword, "typed-it-differently"}},
	}.run(t)
	if outcome.code != exitFailure {
		t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	if !strings.Contains(outcome.stderr, "password confirmation does not match") {
		t.Fatalf("stderr = %q", outcome.stderr)
	}
}

func TestAnInterruptedPromptEndsAsACancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	outcome := invocation{
		ctx:       ctx,
		arguments: []string{"user", "create-admin", "--database", filepath.Join(t.TempDir(), "library.db"), "--login", "chef"},
		// A terminal read ends in an ordinary error when the signal arrives.
		// Only the context says the operator interrupted it.
		prompt: &scriptedPrompt{fail: func() error { cancel(); return io.EOF }},
	}.run(t)

	if outcome.code != exitCanceled {
		t.Fatalf("exit code = %d, want %d (stderr %q)", outcome.code, exitCanceled, outcome.stderr)
	}

	if strings.TrimSpace(outcome.stderr) != "savorum: canceled" {
		t.Fatalf("stderr = %q", outcome.stderr)
	}
}

func TestCreatingAnAccountRequiresTheAdministratorItActsFor(t *testing.T) {
	t.Parallel()

	outcome := savorum(t, "user", "create", "--database", filepath.Join(t.TempDir(), "library.db"), "--login", "sam", "--password-file", "-")
	if outcome.code != exitUsage || !strings.Contains(outcome.stderr, `required flag(s) "actor" not set`) {
		t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}
}
