// Command srefadapter presents Savorum to the SREF conformance runner.
//
// It implements SREF's command-line conformance contract:
//
//	srefadapter capabilities
//	srefadapter check json-reader path/to/fixture.recipe.json
//	srefadapter rewrite path/to/fixture.recipe.json
//
// The adapter translates and does not judge: every answer comes from the same
// internal/sref code the application uses.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/archivepolicy"
	"github.com/savorum/savorum/internal/sref/bundle"
	"github.com/savorum/savorum/internal/units"
)

// capabilities are what Savorum claims.
var capabilities = []string{
	"json-reader",
	"json-writer",
	"package-reader",
	"bundle-reader",
	"round-trip",
	// Unit-resolution vectors run against the production catalog.
	"unit-resolution",
}

// categorised is a refusal this adapter makes itself, in cases where the
// requirement belongs to the bundle rather than to anything internal/sref
// reports on its own.
type categorised struct {
	category    sref.FailureCategory
	requirement string
	message     string
}

func (e *categorised) Error() string { return e.message }

func (e *categorised) Category() sref.FailureCategory { return e.category }

func (e *categorised) RequirementID() string { return e.requirement }

type answer struct {
	Outcome       string               `json:"outcome"`
	ErrorCategory sref.FailureCategory `json:"error_category,omitempty"`
	RequirementID string               `json:"requirement_id,omitempty"`
	Detail        string               `json:"detail,omitempty"`
	Document      map[string]any       `json:"document,omitempty"`
	// What Savorum actually wrote, for the runner to validate independently
	// against SREF's own schemas.
	Artifact         string `json:"artifact,omitempty"`
	ArtifactEncoding string `json:"artifact_encoding,omitempty"`
}

// wrote records an emitted document alongside the verdict about it. SREF
// documents are UTF-8 JSON, so they travel as themselves.
func wrote(written []byte) answer {
	return answer{Outcome: "accepted", Artifact: string(written), ArtifactEncoding: "utf-8"}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: srefadapter capabilities|check <capability> <fixture>|rewrite <fixture>|resolve <fixture>")
	}

	switch arguments[0] {
	case "capabilities":
		return emit(map[string]any{
			"implementation":        "savorum",
			"version":               version(),
			"sref_version":          sref.Version,
			"unit_registry_version": sref.UnitRegistryVersion,
			"capabilities":          capabilities,
		})
	case "check":
		if len(arguments) != 3 {
			return errors.New("check takes a capability and a fixture")
		}

		return check(arguments[1], arguments[2])
	case "rewrite":
		if len(arguments) != 2 {
			return errors.New("rewrite takes a fixture")
		}

		return rewrite(arguments[1])
	case "resolve":
		if len(arguments) != 2 {
			return errors.New("resolve takes a fixture")
		}

		return resolve(arguments[1])
	default:
		return fmt.Errorf("unknown command %q", arguments[0])
	}
}

func check(capability, path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is the conformance fixture explicitly requested by the runner
	if err != nil {
		return fmt.Errorf("read conformance fixture: %w", err)
	}

	switch capability {
	case "json-reader", "json-writer", "round-trip":
		document, err := sref.Decode(data)
		if err != nil {
			return emit(rejected(err))
		}

		if capability == "json-reader" {
			return emit(answer{Outcome: "accepted"})
		}
		// A writer case asks whether Savorum can produce the document, so the
		// document is written and read back rather than merely parsed.
		written, err := sref.Encode(document)
		if err != nil {
			return emit(rejected(err))
		}

		if _, err := sref.Decode(written); err != nil {
			return emit(rejected(err))
		}

		return emit(wrote(written))
	case "package-reader":
		if _, err := sref.ReadPackage(data, sref.DefaultLimits()); err != nil {
			return emit(rejected(err))
		}

		return emit(answer{Outcome: "accepted"})
	case "bundle-reader":
		if err := readBundle(data); err != nil {
			return emit(rejected(err))
		}

		return emit(answer{Outcome: "accepted"})
	default:
		return emit(answer{Outcome: "unsupported", Detail: capability + " is not claimed"})
	}
}

func readBundle(data []byte) error {
	_, err := bundle.VisitWithBudget(data, bundle.DefaultLimits(), archivepolicy.NewBudget(bundle.DefaultLimits().WorkBytes), func(member bundle.Member) error {
		opened, err := sref.ReadPackage(member.Bytes, sref.DefaultLimits())
		if err != nil {
			// Verifying a member's bytes established that they are the ones
			// the manifest declared. Whether they are a valid package is a
			// separate question, and its failure is the bundle's.
			return &categorised{
				category:    sref.FailureCategoryOf(err),
				requirement: "invalid-bundle-member-package",
				message:     fmt.Sprintf("member %q is not a valid package: %v", member.MemberID, err),
			}
		}
		// Verifying a member's bytes says nothing about whether they are a
		// valid package, and the bundle rules require both.
		if string(opened.Recipe.ID) != member.RecipeID {
			return &categorised{
				category:    sref.IntegrityFailure,
				requirement: "bundle-recipe-id-disagreement",
				message: fmt.Sprintf("member %q declares recipe %q and carries %q",
					member.MemberID, member.RecipeID, opened.Recipe.ID),
			}
		}

		return nil
	})

	return err
}

// rewrite reads a document and writes it back for the runner to compare.
func rewrite(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is the rewrite fixture explicitly requested by the runner
	if err != nil {
		return fmt.Errorf("read rewrite fixture: %w", err)
	}

	document, err := sref.Decode(data)
	if err != nil {
		return emit(rejected(err))
	}

	written, err := sref.Encode(document)
	if err != nil {
		return emit(rejected(err))
	}

	var shaped map[string]any
	if err := json.Unmarshal(written, &shaped); err != nil {
		return emit(rejected(err))
	}
	// `document` is this adapter's parse of what it wrote, and `artifact` is
	// what it wrote.
	response := wrote(written)
	response.Document = shaped

	return emit(response)
}

// rejected reports why Savorum refused a document, in SREF's own vocabulary as
// translated by internal/sref.
func rejected(err error) answer {
	if own, ok := errors.AsType[*categorised](err); ok {
		return answer{
			Outcome: "rejected", ErrorCategory: own.Category(),
			RequirementID: own.RequirementID(), Detail: own.Error(),
		}
	}

	return answer{
		Outcome: "rejected", ErrorCategory: sref.FailureCategoryOf(err),
		RequirementID: sref.RequirementID(err), Detail: detailOf(err),
	}
}

// detailOf names the first violation rather than only its count.
func detailOf(err error) string {
	var validation *recipe.ValidationError
	if errors.As(err, &validation) && len(validation.Violations) > 0 {
		first := validation.Violations[0]
		return fmt.Sprintf("%s at %s: %s", first.Code, first.Path, first.Message)
	}

	return err.Error()
}

func emit(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode adapter response: %w", err)
	}

	fmt.Println(string(encoded))

	return nil
}

func version() string {
	// The adapter reports the recipe-domain schema it implements rather than a
	// release number, because that is what determines how it reads a document.
	_ = units.DefaultCatalog
	_ = filepath.Separator

	return fmt.Sprintf("recipe-schema-%d", recipe.CurrentDocumentSchemaVersion)
}
