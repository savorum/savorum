package bundle_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
	"github.com/savorum/savorum/internal/srefcheckout"
)

// validBundleFixtures names the bundle fixtures the corpus expects a reader to
// accept; everything else under tests/bundles/ is hostile. Names do not say
// which is which (`duplicate-recipe-id` is valid), and
// TestTheValidBundleListMatchesTheCorpus keeps this list current.
var validBundleFixtures = map[string]bool{
	"minimum-collections":       true,
	"valid":                     true,
	"duplicate-recipe-id":       true,
	"valid-three-members":       true,
	"valid-linked-translations": true,
}

// TestHostileBundlesAreRefused runs the specification's hostile bundle
// fixtures through Savorum's reader; each must be refused, not half-read.
func TestHostileBundlesAreRefused(t *testing.T) {
	t.Parallel()
	bundleDirectory := srefcheckout.Path(t, "tests/bundles")

	entries, err := os.ReadDir(bundleDirectory)
	if err != nil {
		t.Fatal(err)
	}

	var checked int

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".srefbundle" {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".srefbundle")

		data, err := readBundleFixture(filepath.Join(bundleDirectory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}

		opened, err := bundle.Read(data, bundle.DefaultLimits())
		checked++

		if validBundleFixtures[name] {
			if err != nil {
				t.Errorf("%s: a valid bundle was refused: %v", name, err)
				continue
			}

			if len(opened.Members) != len(opened.Manifest.Recipes) {
				t.Errorf("%s: the reader returned %d members for %d declarations",
					name, len(opened.Members), len(opened.Manifest.Recipes))
			}

			if name == "duplicate-recipe-id" && (len(opened.Members) != 2 ||
				opened.Members[0].RecipeID != opened.Members[1].RecipeID ||
				opened.Members[0].MemberID == opened.Members[1].MemberID) {
				t.Errorf("duplicate recipe IDs did not retain distinct member identities: %#v", opened.Members)
			}

			continue
		}

		if err == nil {
			// A member that verifies and is not a valid package is caught by
			// the package reader rather than by the container reader, which is
			// the division of labour the specification describes.
			if invalidMember(t, opened) {
				continue
			}

			t.Errorf("%s: a hostile bundle was accepted", name)
		}
	}

	if checked == 0 {
		srefcheckout.Unavailable(t, "no bundle fixtures found")
	}

	t.Logf("checked %d bundle fixtures", checked)
}

// invalidMember reports whether some member of an otherwise well-formed bundle
// fails as a package.
func invalidMember(t *testing.T, opened bundle.Bundle) bool {
	t.Helper()

	for _, member := range opened.Members {
		document, err := sref.ReadPackage(member.Bytes, sref.DefaultLimits())
		if err != nil {
			return true
		}

		if string(document.Recipe.ID) != member.RecipeID {
			return true
		}
	}

	return false
}

// TestEveryMemberOfTheSpecificationsBundleStandsAlone is the property the
// whole container exists for: a member pulled out of a bundle is an ordinary
// package, readable without the bundle it came from.
func TestEveryMemberOfTheSpecificationsBundleStandsAlone(t *testing.T) {
	t.Parallel()

	data, err := readBundleFixture(filepath.Join(srefcheckout.Path(t, "tests/bundles"), "valid.srefbundle"))
	if err != nil {
		t.Fatal(err)
	}

	opened, err := bundle.Read(data, bundle.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	if len(opened.Members) == 0 {
		t.Fatal("the valid bundle carries nothing")
	}

	for _, member := range opened.Members {
		document, err := sref.ReadPackage(member.Bytes, sref.DefaultLimits())
		if err != nil {
			t.Fatalf("member %q is not usable on its own: %v", member.RecipeID, err)
		}

		if string(document.Recipe.ID) != member.RecipeID {
			t.Fatalf("member %q carries recipe %q", member.RecipeID, document.Recipe.ID)
		}
	}
}

func TestABundleSavorumWritesIsOneItReads(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	writer := bundle.NewWriter(buffer)

	for _, id := range []string{"second-recipe", "first-recipe"} {
		packaged := packageFor(t, id, id)
		if err := writer.Add(id, func(member io.Writer) error {
			return writeBundleMember(member, packaged)
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := bundle.Read(buffer.Bytes(), bundle.DefaultLimits())
	if err != nil {
		t.Fatalf("Savorum cannot read the bundle it wrote: %v", err)
	}
	// The order written is the order read back. It is the only thing the
	// manifest's array says.
	if opened.Manifest.Recipes[0].RecipeID != "second-recipe" ||
		opened.Manifest.Recipes[1].RecipeID != "first-recipe" {
		t.Fatalf("order changed: %#v", opened.Manifest.Recipes)
	}

	for _, member := range opened.Members {
		packaged, err := sref.ReadPackage(member.Bytes, sref.DefaultLimits())
		if err != nil {
			t.Fatalf("member %q is not a package: %v", member.MemberID, err)
		}

		if string(packaged.Recipe.ID) != member.RecipeID {
			t.Fatalf("member %q declares recipe %q and carries %q", member.MemberID, member.RecipeID, packaged.Recipe.ID)
		}
	}
}

func packageFor(t *testing.T, id, title string) []byte {
	t.Helper()

	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            recipe.RecipeID(id), Title: title,
		IngredientSections: []recipe.IngredientSection{{
			ID: "ingredients", Ingredients: []recipe.Ingredient{{ID: "salt", Name: "salt"}},
		}},
		InstructionSections: []recipe.InstructionSection{{
			ID: "method", Steps: []recipe.Step{{ID: "season", Text: "Season."}},
		}},
	}

	packaged, err := sref.WritePackage(document, nil)
	if err != nil {
		t.Fatal(err)
	}

	return packaged
}

func TestABundleRefusesWhatItCannotNameSafely(t *testing.T) {
	t.Parallel()

	write := func(id string) error {
		writer := bundle.NewWriter(&bytes.Buffer{})

		return writer.Add(id, func(member io.Writer) error {
			return writeBundleMember(member, []byte("not really a package"))
		})
	}
	for _, id := range []string{"", "../escape", "a/b", `a\b`, ".hidden", "with space", strings.Repeat("x", 200)} {
		if err := write(id); !errors.Is(err, bundle.ErrInvalidBundle) {
			t.Fatalf("Add(%q) = %v, want a refusal", id, err)
		}
	}
}

func TestDuplicateRecipeIDsKeepDistinctBundleLocalIdentities(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	writer := bundle.NewWriter(buffer)

	add := func(title string) error {
		packaged := packageFor(t, "apple-pie", title)

		return writer.Add("apple-pie", func(member io.Writer) error {
			return writeBundleMember(member, packaged)
		})
	}
	if err := add("First apple pie"); err != nil {
		t.Fatal(err)
	}

	if err := add("Second apple pie"); err != nil {
		t.Fatalf("a second package with the same recipe ID was refused: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := bundle.Read(buffer.Bytes(), bundle.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	if len(opened.Members) != 2 || opened.Members[0].RecipeID != "apple-pie" ||
		opened.Members[1].RecipeID != "apple-pie" || opened.Members[0].MemberID == opened.Members[1].MemberID {
		t.Fatalf("duplicate recipe ID members = %#v", opened.Members)
	}

	for index, member := range opened.Members {
		packaged, err := sref.ReadPackage(member.Bytes, sref.DefaultLimits())
		if err != nil || packaged.Recipe.ID != "apple-pie" {
			t.Fatalf("member %d changed recipe identity: recipe=%q err=%v", index, packaged.Recipe.ID, err)
		}
	}
}

func TestAnEmptyBundleIsNotWritten(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	if err := bundle.NewWriter(buffer).Close(); !errors.Is(err, bundle.ErrInvalidBundle) {
		t.Fatalf("closing an empty bundle = %v, want a refusal", err)
	}
}

func TestMemberPathsAreBuiltFromTheBundleLocalIdentifier(t *testing.T) {
	t.Parallel()

	if got := bundle.MemberPath("r000001"); got != "recipes/r000001.sref" {
		t.Fatalf("MemberPath = %q", got)
	}

	for _, hostile := range []string{"..", "a/b", `a\b`, "", "-leading-dash"} {
		if bundle.ValidPortableID(hostile) {
			t.Fatalf("%q was accepted as a portable identifier", hostile)
		}
	}

	for _, ordinary := range []string{"apple-pie", "Recipe.1", "a", "1leading-digit", "sourdough_starter"} {
		if !bundle.ValidPortableID(ordinary) {
			t.Fatalf("%q was refused as a portable identifier", ordinary)
		}
	}
}

func TestWriterAvoidsUnnecessaryNondeterminismForExactMembers(t *testing.T) {
	t.Parallel()

	build := func() []byte {
		buffer := &bytes.Buffer{}

		writer := bundle.NewWriter(buffer)
		for _, id := range []string{"apple-pie", "sourdough"} {
			if err := writer.Add(id, func(member io.Writer) error {
				return writeBundleMember(member, []byte("package bytes for "+id))
			}); err != nil {
				t.Fatal(err)
			}
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		return buffer.Bytes()
	}
	if !bytes.Equal(build(), build()) {
		t.Fatal("two identical bundles differ")
	}
}

func TestReaderResourceLimitsAreCallerPolicy(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}

	writer := bundle.NewWriter(buffer)
	for _, id := range []string{"first", "second"} {
		if err := writer.Add(id, func(member io.Writer) error {
			return writeBundleMember(member, []byte("package bytes"))
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	limits := bundle.DefaultLimits()

	limits.Members = 1
	if _, err := bundle.Read(buffer.Bytes(), limits); !errors.Is(err, bundle.ErrUnsafeBundle) {
		t.Fatalf("member limit = %v, want unsafe bundle", err)
	}

	limits = bundle.DefaultLimits()

	limits.BytesPerEntry = 1
	if _, err := bundle.Read(buffer.Bytes(), limits); !errors.Is(err, bundle.ErrUnsafeBundle) {
		t.Fatalf("entry-byte limit = %v, want unsafe bundle", err)
	}
}

func TestWriterHasNoFormatLevelMemberCeiling(t *testing.T) {
	t.Parallel()

	limits := bundle.DefaultLimits()
	count := limits.Members + 1
	buffer := &bytes.Buffer{}
	writer := bundle.NewWriter(buffer)

	for index := range count {
		id := fmt.Sprintf("recipe-%d", index+1)
		if err := writer.Add(id, func(member io.Writer) error {
			return writeBundleMember(member, []byte("package bytes"))
		}); err != nil {
			t.Fatalf("writer treated the reader's policy as a format maximum at member %d: %v", index+1, err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := bundle.Read(buffer.Bytes(), limits); !errors.Is(err, bundle.ErrUnsafeBundle) {
		t.Fatalf("default reader policy = %v, want member-limit refusal", err)
	}

	limits.Members = count

	opened, err := bundle.Read(buffer.Bytes(), limits)
	if err != nil {
		t.Fatalf("raised reader policy refused a structurally valid bundle: %v", err)
	}

	if len(opened.Members) != count {
		t.Fatalf("read %d members, want %d", len(opened.Members), count)
	}
}

func TestReaderRejectsCompressionBombsAndCaseCollisions(t *testing.T) {
	t.Parallel()

	compressed := &bytes.Buffer{}
	archive := zip.NewWriter(compressed)

	entry, err := archive.CreateHeader(&zip.FileHeader{Name: bundle.ManifestPath, Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write([]byte(strings.Repeat("x", 4096))); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	limits := bundle.DefaultLimits()

	limits.CompressionRate = 2
	if _, err := bundle.Read(compressed.Bytes(), limits); !errors.Is(err, bundle.ErrUnsafeBundle) ||
		!strings.Contains(err.Error(), "expands far more") {
		t.Fatalf("compression ratio = %v, want resource-limit refusal", err)
	}

	colliding := &bytes.Buffer{}

	archive = zip.NewWriter(colliding)
	for _, name := range []string{"manifest.json", "Manifest.json"} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write([]byte("{}")); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := bundle.Read(colliding.Bytes(), bundle.DefaultLimits()); !errors.Is(err, bundle.ErrUnsafeBundle) ||
		!strings.Contains(err.Error(), "differ only by case") {
		t.Fatalf("case collision = %v, want refusal", err)
	}
}

// TestTheValidBundleListMatchesTheCorpus compares validBundleFixtures with the
// corpus's own expectations, so a new valid fixture upstream fails clearly.
func TestTheValidBundleListMatchesTheCorpus(t *testing.T) {
	var manifest struct {
		Cases []struct {
			Capabilities []string `json:"capabilities"`
			Fixture      string   `json:"fixture"`
			Expected     string   `json:"expected"`
		} `json:"cases"`
	}

	body, err := readBundleFixture(srefcheckout.Path(t, filepath.Join("conformance", "manifest.json")))
	if err != nil {
		srefcheckout.Unavailable(t, "no conformance manifest")
		return
	}

	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{}

	for _, testCase := range manifest.Cases {
		if testCase.Expected != "valid" || !strings.HasPrefix(testCase.Fixture, "tests/bundles/") {
			continue
		}

		expected[strings.TrimSuffix(filepath.Base(testCase.Fixture), ".srefbundle")] = true
	}

	if len(expected) == 0 {
		srefcheckout.Unavailable(t, "the manifest indexes no valid bundle fixtures")
		return
	}

	for name := range expected {
		if !validBundleFixtures[name] {
			t.Errorf("the corpus expects %s to be accepted; add it to validBundleFixtures", name)
		}
	}

	for name := range validBundleFixtures {
		if !expected[name] {
			t.Errorf("validBundleFixtures lists %s, which the corpus no longer expects to be valid", name)
		}
	}
}

func readBundleFixture(path string) ([]byte, error) {
	// Callers resolve paths from the trusted sibling SREF checkout and fixed
	// conformance fixture names, never from runtime or request input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle fixture: %w", err)
	}

	return data, nil
}

func writeBundleMember(destination io.Writer, data []byte) error {
	if _, err := destination.Write(data); err != nil {
		return fmt.Errorf("write bundle test member: %w", err)
	}

	return nil
}
