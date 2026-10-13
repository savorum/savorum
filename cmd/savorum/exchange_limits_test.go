package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
	"golang.org/x/sys/unix"
)

func TestExchangeDirectorySkipsSymlinksAndSpecialFiles(t *testing.T) {
	directory := t.TempDir()

	external := filepath.Join(t.TempDir(), "external.json")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"z.json", "a.sref", "ignored.dat"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("recipe"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Symlink(external, filepath.Join(directory, "linked.json")); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(directory, "nested.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := unix.Mkfifo(filepath.Join(directory, "pipe.json"), 0o600); err != nil {
		t.Fatal(err)
	}

	limits := exchangeInputLimits{fileBytes: 100, totalBytes: 100}

	sources, err := exchangeFiles(t.Context(), directory, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer sources.close()

	if !slices.Equal(sources.paths, []string{"a.sref", "z.json"}) {
		t.Fatalf("discovered: %v", sources.paths)
	}

	explicit, err := exchangeFiles(t.Context(), filepath.Join(directory, "linked.json"), limits)
	if err != nil {
		t.Fatalf("explicit symlink: %v", err)
	}
	defer explicit.close()

	data, err := explicit.read(t.Context(), explicit.paths[0], 100)
	if err != nil || string(data) != "external" {
		t.Fatalf("explicit symlink bytes: %q %v", data, err)
	}

	if _, err := exchangeFiles(t.Context(), filepath.Join(directory, "pipe.json"), limits); err == nil {
		t.Fatal("explicit FIFO accepted")
	}

	if err := os.Remove(filepath.Join(directory, "a.sref")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(external, filepath.Join(directory, "a.sref")); err != nil {
		t.Fatal(err)
	}

	if _, err := sources.read(t.Context(), "a.sref", 100); err == nil {
		t.Fatal("discovered file replacement followed a symlink")
	}

	if err := os.Remove(filepath.Join(directory, "z.json")); err != nil {
		t.Fatal(err)
	}

	if err := unix.Mkfifo(filepath.Join(directory, "z.json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := sources.read(t.Context(), "z.json", 100); err == nil {
		t.Fatal("discovered file replacement accepted a FIFO")
	}
}

func TestExchangeInputBudgetsPreflightAndRecheckChangedFiles(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"a.json", "b.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("123456"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, limits := range []exchangeInputLimits{{fileBytes: 5, totalBytes: 100}, {fileBytes: 6, totalBytes: 11}} {
		if _, err := exchangeFiles(t.Context(), directory, limits); err == nil || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("budget %+v: %v", limits, err)
		}
	}

	limits := exchangeInputLimits{fileBytes: 7, totalBytes: 12}

	sources, err := exchangeFiles(t.Context(), directory, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer sources.close()

	if err := os.WriteFile(filepath.Join(directory, "a.json"), []byte("1234567"), 0o600); err != nil {
		t.Fatal(err)
	}

	seen := 0
	for input, err := range sources.inputs(t.Context(), limits) {
		if seen == 0 && (err != nil || len(input.Data) != 7) {
			t.Fatalf("first source: %+v %v", input, err)
		}

		if seen == 1 && (err == nil || !strings.Contains(err.Error(), "budget")) {
			t.Fatalf("aggregate read recheck: %v", err)
		}

		seen++
	}

	if seen != 2 {
		t.Fatalf("read %d inputs", seen)
	}
}

func TestExchangeCancelledReadStopsBeforeFileAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	sources := &exchangeInputFiles{paths: []string{"missing.json"}}
	for _, err := range sources.inputs(ctx, exchangeInputLimits{fileBytes: 10, totalBytes: 10}) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled source: %v", err)
		}
	}

	ctx, cancel = context.WithCancel(t.Context())

	_, err := io.ReadAll(exchangeContextReader{ctx: ctx, reader: cancellingExchangeReader{cancel: cancel}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during read: %v", err)
	}
}

type cancellingExchangeReader struct{ cancel context.CancelFunc }

func (r cancellingExchangeReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "bytes"), nil
}

func TestLazyExchangeImportReadsInOrderAndFailsAtomically(t *testing.T) {
	database := filepath.Join(t.TempDir(), "library.db")

	ctx, services, err := (&application{}).openExchange(t.Context(), database, "")
	if err != nil {
		t.Fatal(err)
	}
	defer services.close()

	document := withoutPhotographs(testfixture.Document())

	data, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	readFailure := errors.New("source failed")

	for _, fail := range []bool{true, false} {
		calls := 0
		sequence := func(yield func(importer.Input, error) bool) {
			calls++

			if !yield(importer.Input{Name: "first.json", Data: data}, nil) {
				return
			}

			calls++

			if fail {
				yield(importer.Input{}, readFailure)
				return
			}

			second := document
			second.ID = "second-hand-pies"
			second.Title = "Second recipe"

			other, err := sref.Encode(second)
			if err != nil {
				t.Fatal(err)
			}

			yield(importer.Input{Name: "second.json", Data: other}, nil)
		}

		results, err := services.exchange.ImportSourceSequence(ctx, sequence, importer.Options{}, library.DuplicateReject)
		if fail {
			if !errors.Is(err, readFailure) {
				t.Fatalf("sequence failure: %v", err)
			}

			page, err := services.recipes.List(ctx, library.ListOptions{Limit: 10})
			if err != nil || len(page) != 0 {
				t.Fatalf("partial commit: %+v %v", page, err)
			}
		} else if err != nil || len(results) != 2 {
			t.Fatalf("sequence import: %+v %v", results, err)
		}

		if calls != 2 {
			t.Fatalf("source calls: %d", calls)
		}
	}
}

func TestCLIRejectsInvalidByteBudgetsBeforeOpeningInstance(t *testing.T) {
	for _, args := range [][]string{
		{"import", "--database", "missing.db", "--input", "missing.json", "--max-file-bytes", "0"},
		{"import", "--database", "missing.db", "--input", "missing.json", "--max-input-bytes", "-1"},
		{"verify", "--input", "missing.zip", "--max-expanded-bytes", "0"},
		{"restore", "--database", "missing.db", "--input", "missing.zip", "--max-expanded-bytes", "-1"},
	} {
		outcome := savorum(t, args...)
		if outcome.code != exitUsage || !strings.Contains(outcome.stderr, "must be between") {
			t.Fatalf("%v: %+v", args, outcome)
		}
	}
}
