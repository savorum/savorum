package interop_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/semanticdiff"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/srefcheckout"
	"github.com/savorum/savorum/internal/testfixture"
)

// This file checks a round trip through an independent implementation:
//
//	Savorum → SREF → implementation B → SREF → Savorum
//
// which catches a specification misread consistently by Savorum's reader and
// writer. Implementation B is sref-reader paired with sref-writer, driven
// through the conformance adapter protocol, from a sibling checkout.

const (
	defaultReaderDirectory = "../sref-reader"
	defaultWriterDirectory = "../sref-writer"
)

// Where the other implementation is. CI clones both at the revisions
// integration/sref.json declares and points these at them, so the chain runs
// against a pin rather than against whatever is beside the checkout.
func readerDirectory() string {
	return checkoutOr("SREF_READER_CHECKOUT", defaultReaderDirectory)
}

func writerDirectory() string {
	return checkoutOr("SREF_WRITER_CHECKOUT", defaultWriterDirectory)
}

func checkoutOr(variable, fallback string) string {
	directory := os.Getenv(variable)
	if directory == "" {
		directory = fallback
	}

	if resolved, err := srefcheckout.Resolve(directory); err == nil {
		return resolved
	}

	return directory
}

// implementationB is the other implementation, addressed only through the
// published adapter contract.
type implementationB struct{ t *testing.T }

func newImplementationB(t *testing.T) implementationB {
	t.Helper()

	for _, directory := range []string{readerDirectory(), writerDirectory()} {
		if _, err := os.Stat(directory); err != nil {
			if srefcheckout.Required() {
				t.Fatalf("SREF_REQUIRED is set and %s is not beside this repository: %v", directory, err)
			}

			t.Skipf("the independent SREF implementation is not beside this repository: %v", err)
		}
	}

	if _, err := exec.LookPath("python3"); err != nil {
		if srefcheckout.Required() {
			t.Fatalf("SREF_REQUIRED is set and python3 is needed to run the independent implementation: %v", err)
		}

		t.Skipf("python3 is needed to run the independent implementation: %v", err)
	}

	return implementationB{t: t}
}

// rewrite asks the other implementation to read a document and write it back.
func (b implementationB) rewrite(document []byte) []byte {
	b.t.Helper()

	source := filepath.Join(b.t.TempDir(), "handoff.recipe.json")
	if err := os.WriteFile(source, document, 0o600); err != nil {
		b.t.Fatal(err)
	}

	command := exec.CommandContext(b.t.Context(), //nolint:gosec // a sibling checkout path chosen by the test, not request input
		"python3", filepath.Join(writerDirectory(), "tools", "adapter.py"),
		"--reader", readerDirectory(), "rewrite", source,
	)

	var out, errors bytes.Buffer

	command.Stdout, command.Stderr = &out, &errors
	if err := command.Run(); err != nil {
		b.t.Fatalf("the independent implementation failed: %v: %s", err, errors.String())
	}

	var answer struct {
		Outcome       string          `json:"outcome"`
		ErrorCategory string          `json:"error_category"`
		RequirementID string          `json:"requirement_id"`
		Document      json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		b.t.Fatalf("the independent implementation did not answer in the adapter protocol: %v", err)
	}

	if answer.Outcome != "accepted" {
		b.t.Fatalf("the independent implementation refused what Savorum wrote: %s/%s", answer.ErrorCategory, answer.RequirementID)
	}

	return answer.Document
}

func TestASavorumRecipeSurvivesAnIndependentImplementation(t *testing.T) {
	t.Parallel()
	other := newImplementationB(t)

	corpus := append([]recipe.Document{preservationSurface(), difficultQuantities()}, testfixture.PrintCorpus()...)
	for _, document := range corpus {
		t.Run(string(document.ID), func(t *testing.T) {
			t.Parallel()

			document, contents := withRealPhotographs(document)

			archive, err := sref.WritePackage(document, contents)
			if err != nil {
				t.Fatalf("the fixture does not write as a package: %v", err)
			}

			instance := newSavorum(t)
			stored := instance.importOne(t, importer.Input{Name: string(document.ID) + ".sref", Data: archive})
			_, exported := instance.exportPackage(t, stored.EntryID)

			// Savorum → SREF
			written, err := sref.Encode(exported)
			if err != nil {
				t.Fatal(err)
			}

			// → implementation B → SREF
			returned := other.rewrite(written)

			// → Savorum
			back, err := sref.Decode(returned)
			if err != nil {
				t.Fatalf("Savorum cannot read what the independent implementation wrote: %v", err)
			}

			same(t, "a round trip through an independent implementation changed the recipe", exported, back)
		})
	}
}

// Uninterpreted standard members and extensions survive only if the format
// obliges the other implementation to keep them.
func TestPreservedFutureDataSurvivesAnIndependentImplementation(t *testing.T) {
	t.Parallel()
	other := newImplementationB(t)

	instance := newSavorum(t)
	result := instance.importOne(t, importer.Input{
		Name: "future.json", MediaType: "application/json", Data: []byte(futureDocument),
	})
	_, exported := instance.exportPackage(t, result.EntryID)

	written, err := sref.Encode(exported)
	if err != nil {
		t.Fatal(err)
	}

	returned := other.rewrite(written)

	back, err := sref.Decode(returned)
	if err != nil {
		t.Fatalf("Savorum cannot read what the independent implementation wrote: %v", err)
	}

	same(t, "preserved future data did not survive the other implementation", exported, back)

	if len(back.SREFOpaque) == 0 || len(back.Extensions) == 0 {
		t.Fatalf("opaque=%#v extensions=%#v", back.SREFOpaque, back.Extensions)
	}
	// An integer larger than float64 can represent is the case where a
	// re-serializing implementation loses data without noticing.
	if !bytes.Contains(returned, []byte("184467440737095516160")) {
		t.Fatalf("a preserved integer beyond float64 was rewritten:\n%s", returned)
	}
}

// The corpus the specification publishes is the strongest input for this,
// because neither implementation wrote it.
func TestTheSpecificationsExamplesSurviveBothImplementations(t *testing.T) {
	t.Parallel()
	other := newImplementationB(t)

	for name, data := range specimens(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instance := newSavorum(t)
			result := instance.importOne(t, importer.Input{Name: name, MediaType: "application/json", Data: data})
			_, exported := instance.exportPackage(t, result.EntryID)

			written, err := sref.Encode(exported)
			if err != nil {
				t.Fatal(err)
			}

			back, err := sref.Decode(other.rewrite(written))
			if err != nil {
				t.Fatalf("Savorum cannot read what the independent implementation wrote: %v", err)
			}

			if lost := semanticdiff.Lost(exported, back); len(lost) != 0 {
				t.Fatalf("the other implementation dropped what Savorum wrote:\n%s", semanticdiff.Report(lost))
			}
		})
	}
}
