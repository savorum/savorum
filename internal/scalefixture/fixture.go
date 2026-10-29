// Package scalefixture owns the versioned workload shared by performance,
// capacity and release stress checks. All content and images are synthetic.
package scalefixture

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

// ContractJSON is the embedded scale-fixture contract.
//
//go:embed contract.json
var ContractJSON []byte

// Contract defines the deterministic history and workload generated for scale checks.
type Contract struct {
	Version int   `json:"version"`
	Sizes   []int `json:"sizes"`
	Samples int   `json:"samples"`
	History struct {
		Ordinary      int `json:"ordinary_revisions"`
		Tenth         int `json:"every_tenth_revisions"`
		Hundredth     int `json:"every_hundredth_revisions"`
		BookMembers   int `json:"book_members"`
		BookRevisions int `json:"book_revisions"`
		ImageEvery    int `json:"image_every"`
		ImageVersions int `json:"image_versions"`
		Cookings      int `json:"cookings_per_recipe"`
	} `json:"history"`
	Taxonomy struct {
		Tags          int `json:"tags"`
		FacetsPerKind int `json:"facets_per_kind"`
	} `json:"taxonomy"`
	Budgets        map[string]float64 `json:"budgets_ms"`
	StorageBudgets map[string]int64   `json:"storage_budgets_bytes_per_recipe"`
}

var policy = readPolicy()

// Policy returns the validated embedded scale-fixture contract.
func Policy() Contract { return policy }

func readPolicy() Contract {
	var c Contract
	if err := json.Unmarshal(ContractJSON, &c); err != nil {
		panic(err)
	}

	return c
}

// Depth returns the number of retained revisions for a fixture recipe.
func Depth(i int) int {
	c := Policy()
	if i%100 == 0 {
		return c.History.Hundredth
	}

	if i%10 == 0 {
		return c.History.Tenth
	}

	return c.History.Ordinary
}

// EntryID returns the deterministic library identity for a fixture recipe.
func EntryID(i int) library.EntryID { return library.EntryID(fmt.Sprintf("rscale%06d", i)) }

// Document returns one deterministic recipe for a scale fixture.
func Document(i int) recipe.Document {
	amount := recipe.Rational("100")

	d := recipe.Document{SchemaVersion: recipe.CurrentDocumentSchemaVersion, ID: recipe.RecipeID(fmt.Sprintf("scale-recipe-%06d", i)), Title: fmt.Sprintf("Saffron vegetable supper %06d", i), Description: "A generous family supper with seasonal vegetables, warm spices and a crisp topping.", Language: "en-US", Authors: []recipe.Author{{Name: "Family Kitchen"}}, Cuisines: []string{"Mediterranean"}, Courses: []string{"Dinner"}, Keywords: []string{"weeknight", "vegetables"}, Source: &recipe.Source{Name: "Family notebook", URL: fmt.Sprintf("https://recipes.example/supper/%d", i)}, Notes: []string{"Prepare the vegetables ahead and keep them covered in the refrigerator."}}
	for n, name := range []string{"brown rice", "saffron", "carrots", "onion", "olive oil", "tomatoes", "chickpeas", "parsley"} {
		if len(d.IngredientSections) == 0 {
			d.IngredientSections = []recipe.IngredientSection{{ID: "main", Title: "For the supper"}}
		}

		d.IngredientSections[0].Ingredients = append(d.IngredientSections[0].Ingredients, recipe.Ingredient{ID: fmt.Sprintf("ingredient-%d", n), Name: name, SourceText: "100 g " + name, Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &amount}, Unit: "mass.gram"}})
	}

	d.InstructionSections = []recipe.InstructionSection{{ID: "method", Title: "Method"}}
	for n, text := range []string{"Rinse the rice thoroughly and drain in a sieve.", "Warm the oil and soften the onion, stirring often.", "Add the carrots, tomatoes and chickpeas, then stir in the saffron.", "Cover and simmer gently until the vegetables are tender.", "Fold through the cooked rice and finish with parsley."} {
		d.InstructionSections[0].Steps = append(d.InstructionSections[0].Steps, recipe.Step{ID: fmt.Sprintf("step-%d", n), Text: text})
	}

	return d
}

// ImageBytes returns deterministic image bytes for a fixture recipe.
func ImageBytes(seed int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 512, 384))
	// Seeded on purpose: the capacity fixture has to produce the same bytes on
	// every run for a measurement to be comparable.
	random := rand.New(rand.NewPCG(uint64(seed+1), 42)) //nolint:gosec // reproducible fixture data, not a secret

	for y := range 384 {
		for x := range 512 {
			noise := random.IntN(32)
			//nolint:gosec // every channel is taken modulo 256, or is 80 plus noise under 32
			img.SetRGBA(x, y, color.RGBA{uint8((x/3 + noise) % 256), uint8((y/2 + noise) % 256), uint8(80 + noise), 255})
		}
	}

	var output bytes.Buffer
	if err := jpeg.Encode(&output, img, &jpeg.Options{Quality: 75}); err != nil {
		panic(err)
	}

	return output.Bytes()
}

// Seed writes the deterministic recipe, history, organization, and asset corpus.
func Seed(ctx context.Context, store *sqlite.Store, files assets.BlobStore, count int) error {
	principal, err := store.LocalPrincipal(ctx)
	if err != nil {
		return fmt.Errorf("resolve scale fixture user: %w", err)
	}

	ctx = access.WithLibrary(identity.WithPrincipal(ctx, principal), "default")

	// Synthetic setup verifies and registers bytes directly. Public upload
	// admission is intentionally not timed as part of a prepared-library fixture.
	ingest := func(data []byte) (assets.Blob, error) {
		blob, err := files.Put(ctx, bytes.NewReader(data), assets.IngestOptions{MediaType: "image/jpeg"})
		if err != nil {
			return blob, err
		}

		blob.CreatedAt = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

		return store.RegisterStoredAsset(ctx, blob, blob.CreatedAt)
	}
	c := Policy()
	stamp := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	docs, err := seedDocuments(count, c, ingest)
	if err != nil {
		return err
	}

	if err := seedRecipeHistory(ctx, store, docs, count, c, stamp, ingest); err != nil {
		return err
	}

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		return fmt.Errorf("create scale fixture library service: %w", err)
	}

	if err := seedTaxonomy(ctx, store, stamp); err != nil {
		return err
	}

	for i := range docs {
		state := library.UserState{Favorite: i%10 == 0}
		if i%5 == 0 {
			rating := uint8(4)
			state.Rating = &rating
		}

		tags, facets := Labels(i)

		_, err := store.ReplaceOrganization(ctx, string(EntryID(i)), principal.UserID, 1, library.Organization{EntryID: EntryID(i), TagIDs: tags, FacetIDs: facets, State: state, Sources: []library.RecipeSource{{ID: fmt.Sprintf("source%d", i), Kind: library.SourceURL, URL: fmt.Sprintf("https://recipes.example/supper/%d", i)}}}, nil, stamp)
		if err != nil {
			return err
		}

		for n := 0; n < c.History.Cookings; n++ {
			_, err := store.RecordCooking(ctx, string(EntryID(i)), principal.UserID, library.CookingRecord{ID: fmt.Sprintf("cscale%d-%d", i, n), CookedOn: library.Date(fmt.Sprintf("2026-01-%02d", n+1)), CookedTime: "18:30", Note: "A good family supper.", CreatedAt: stamp})
			if err != nil {
				return err
			}
		}
	}

	for start := 0; start < count; start += c.History.BookMembers {
		book := library.Book{Kind: library.BookPersonal, Title: fmt.Sprintf("Family book %d", start/c.History.BookMembers)}
		for i := start; i < min(start+c.History.BookMembers, count); i++ {
			book.Entries = append(book.Entries, library.BookEntry{EntryID: EntryID(i), Position: i - start + 1, Note: "Family favorite"})
		}

		created, err := service.CreateBook(ctx, book)
		if err != nil {
			return err
		}

		for revision := 2; revision <= c.History.BookRevisions; revision++ {
			created.Description = fmt.Sprintf("Collection notes, edition %d.", revision)

			created, err = service.ReplaceBook(ctx, created.ID, created.Revision, created)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

type blobIngest func([]byte) (assets.Blob, error)

func seedDocuments(count int, contract Contract, ingest blobIngest) ([]recipe.Document, error) {
	documents := make([]recipe.Document, count)
	for index := range documents {
		documents[index] = Document(index)
		if index%contract.History.ImageEvery == 0 {
			blob, err := ingest(ImageBytes(index))
			if err != nil {
				return nil, err
			}

			documents[index].Assets = []recipe.Asset{{ID: "photo", Path: "assets/photo.jpg", SHA256: blob.ContentHash, Size: blob.Size, MediaType: blob.MediaType, Alt: "A bowl of vegetable supper"}}
			documents[index].PrimaryImage = "photo"
			documents[index].ImageRefs = []string{"photo"}
		}

		if err := recipe.ValidateForWrite(documents[index], units.DefaultCatalog()); err != nil {
			return nil, fmt.Errorf("fixture %d: %#w", index, err)
		}
	}

	return documents, nil
}

func seedRecipeHistory(ctx context.Context, store *sqlite.Store, documents []recipe.Document, count int, contract Contract, stamp time.Time, ingest blobIngest) error {
	for revision := 1; revision <= contract.History.Hundredth; revision++ {
		var batch []library.ImportMutation

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}

			_, err := store.ImportMany(ctx, batch)
			batch = nil

			return err
		}

		for index := range documents {
			if revision > Depth(index) {
				continue
			}

			documents[index].Notes = []string{fmt.Sprintf("Cook's adjustment %d: soften the vegetables slowly and taste before serving.", revision)}
			if revision == 2 && index%contract.History.ImageEvery == 0 {
				blob, err := ingest(ImageBytes(count + index))
				if err != nil {
					return err
				}

				documents[index].Assets[0].SHA256 = blob.ContentHash
				documents[index].Assets[0].Size = blob.Size
			}

			snapshot := documents[index]
			batch = append(batch, library.ImportMutation{ID: EntryID(index), Recipe: documents[index], SourceSnapshot: &snapshot, ExpectedRevision: uint64(revision - 1), Timestamp: stamp.Add(time.Duration(revision) * time.Minute), History: library.ImportContext{Importer: "scale-fixture", ImporterVersion: "1", InputKind: "synthetic", DisplayName: "Family notebook", SourceID: string(documents[index].ID)}})

			if len(batch) == 25 {
				if err := flush(); err != nil {
					return err
				}
			}
		}

		if err := flush(); err != nil {
			return err
		}
	}

	return nil
}
