package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

type scopePrintRenderer struct{}

func (scopePrintRenderer) Version() string { return "scope-test" }
func (scopePrintRenderer) Render(context.Context, print.PrintDocument) ([]byte, error) {
	return []byte("%PDF-1.4\n%%EOF"), nil
}

type scopePrintSource struct{}

func (scopePrintSource) Title() string { return "Private recipe" }
func (scopePrintSource) Resolve(context.Context, int) ([]print.RecipeSnapshot, error) {
	return []print.RecipeSnapshot{{EntryID: "private", Revision: 1, Recipe: recipe.Document{Title: "Private recipe"}}}, nil
}

func TestPrintDocumentGETAndHEADRequireOriginatingUserAndLibrary(t *testing.T) {
	service, err := print.NewService(scopePrintRenderer{}, units.DefaultCatalog(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	owner := access.WithLibrary(identity.WithPrincipal(t.Context(), identity.Principal{UserID: "alice"}), "one")

	artifact, err := service.Render(owner, print.Request{Source: scopePrintSource{}, Options: print.DefaultOptions()})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerPrint(humago.New(mux, huma.DefaultConfig("test", "1")), service)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, test := range []struct {
			user, libraryID string
			status          int
		}{
			{"alice", "one", http.StatusOK},
			{"bob", "one", http.StatusNotFound},
			{"alice", "two", http.StatusNotFound},
			{"", "", http.StatusNotFound},
		} {
			ctx := t.Context()
			if test.user != "" {
				ctx = access.WithLibrary(identity.WithPrincipal(ctx, identity.Principal{UserID: test.user}), test.libraryID)
			}

			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequestWithContext(ctx, method, "/api/v1/print/documents/"+artifact.ID, nil))

			if response.Code != test.status {
				t.Fatalf("%s: got %d want %d: %s", method, response.Code, test.status, response.Body.String())
			}

			if test.status == http.StatusNotFound && response.Header().Get("Content-Type") == "application/pdf" {
				t.Fatal("foreign scope exposed PDF metadata")
			}
		}
	}
}
