// Command corpusrepin reports differences introduced by updating the
// recipe-corpus pin.
//
//	corpusrepin --corpus ../recipe-corpus \
//	    --before 81be44a --before-targets evaluations/sref/1d9c030d.../targets \
//	    --after  d7a40f1 --after-targets  evaluations/sref/d198825.../targets
//
// It reports presence of constructs, not their faithfulness to the source.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/savorum/savorum/internal/corpuscheck"
	"github.com/savorum/savorum/internal/recipe"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("corpusrepin", flag.ContinueOnError)
	corpus := flags.String("corpus", "../recipe-corpus", "recipe-corpus checkout to read revisions from")
	before := flags.String("before", "", "the corpus revision currently pinned")
	after := flags.String("after", "", "the corpus revision being pinned")
	beforeTargets := flags.String("before-targets", "", "target directory within the current revision")
	afterTargets := flags.String("after-targets", "", "target directory within the new revision")
	only := flags.String("cases", "", "comma-separated case IDs to report in full; others are summarized")

	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("read arguments: %w", err)
	}

	if *before == "" || *after == "" || *beforeTargets == "" || *afterTargets == "" {
		flags.Usage()

		return errors.New("both revisions and both target directories are required")
	}

	ctx := context.Background()

	old, err := targetsAt(ctx, *corpus, *before, *beforeTargets)
	if err != nil {
		return err
	}

	current, err := targetsAt(ctx, *corpus, *after, *afterTargets)
	if err != nil {
		return err
	}

	report(*before, *after, old, current, split(*only))

	return nil
}

func split(value string) map[string]bool {
	selected := map[string]bool{}

	for id := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			selected[trimmed] = true
		}
	}

	return selected
}

func report(before, after string, old, current map[string]recipe.Document, only map[string]bool) {
	fmt.Printf("corpus %s -> %s\n", before, after)
	fmt.Printf("cases %d -> %d\n\n", len(old), len(current))

	changes := corpuscheck.Compare(old, current)
	if len(changes) == 0 {
		fmt.Println("no case gained or lost a tracked construct.")

		return
	}

	// Only cases the pin asserts things about are reported in full.
	elsewhere := map[string]int{}

	for _, change := range changes {
		if len(only) != 0 && !only[change.CaseID] {
			elsewhere[change.Status]++

			continue
		}

		fmt.Printf("%-12s %s\n", change.CaseID, change.Status)

		if len(change.Lost) != 0 {
			fmt.Printf("  lost   %s\n", strings.Join(change.Lost, ", "))
		}

		if len(change.Gained) != 0 {
			fmt.Printf("  gained %s\n", strings.Join(change.Gained, ", "))
		}
	}

	if len(elsewhere) != 0 {
		fmt.Println()
		fmt.Printf("elsewhere in the corpus: %d added, %d removed, %d changed\n",
			elsewhere["added"], elsewhere["removed"], elsewhere["changed"])
	}

	fmt.Println()
	fmt.Println("A construct this slice declares and a case no longer carries needs an")
	fmt.Println("explicit coverage replacement, or a reviewed change to the declaration")
	fmt.Println("saying the coverage was dropped on purpose.")
}

// targetsAt reads one revision's derived targets straight out of Git, so
// neither checkout is moved to produce the comparison.
func targetsAt(ctx context.Context, corpus, revision, directory string) (map[string]recipe.Document, error) {
	listing, err := git(ctx, corpus, "ls-tree", "--name-only", revision+":"+directory)
	if err != nil {
		return nil, fmt.Errorf("list %s at %s: %w", directory, revision, err)
	}

	documents := map[string]recipe.Document{}

	for name := range strings.FieldsSeq(listing) {
		if !strings.HasSuffix(name, ".recipe.json") {
			continue
		}

		body, err := git(ctx, corpus, "show", revision+":"+path.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("read %s at %s: %w", name, revision, err)
		}

		var document recipe.Document
		if err := json.Unmarshal([]byte(body), &document); err != nil {
			return nil, fmt.Errorf("parse %s at %s: %w", name, revision, err)
		}

		// A plain decode drops the `x-` members that record a multi-recipe
		// page, so read them explicitly.
		var members map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &members); err != nil {
			return nil, fmt.Errorf("parse %s members at %s: %w", name, revision, err)
		}

		for member := range members {
			if !strings.HasPrefix(member, "x-") {
				continue
			}

			if document.Extensions == nil {
				document.Extensions = map[string]any{}
			}

			document.Extensions[member] = struct{}{}
		}

		documents[strings.TrimSuffix(name, ".recipe.json")] = document
	}

	return documents, nil
}

func git(ctx context.Context, directory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, arguments...)...) //nolint:gosec // flags of a review command run against the reviewer's own checkout, not request input

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}

	return string(output), nil
}
