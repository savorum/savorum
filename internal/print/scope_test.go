package print

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

func TestHeldArtifactsAreIsolatedByRequestUserAndLibrary(t *testing.T) {
	values := identityFixture()
	service := identityService(t, "instance", values)
	request := Request{Source: NewRecipeSource(values, values.entry.ID), Options: DefaultOptions()}
	owner := access.WithLibrary(identity.WithPrincipal(t.Context(), identity.Principal{UserID: "alice"}), "one")

	artifact, err := service.Render(owner, request)
	if err != nil {
		t.Fatal(err)
	}

	if held, found, err := service.HeldFor(owner, artifact.ID); err != nil || !found || held.ID != artifact.ID {
		t.Fatalf("owner lost artifact: %v %v", found, err)
	}

	for _, ctx := range []context.Context{
		access.WithLibrary(identity.WithPrincipal(t.Context(), identity.Principal{UserID: "bob"}), "one"),
		access.WithLibrary(identity.WithPrincipal(t.Context(), identity.Principal{UserID: "alice"}), "two"),
		t.Context(),
	} {
		if _, found, err := service.HeldFor(ctx, artifact.ID); err != nil || found {
			t.Fatalf("foreign scope read held PDF: %v %v", found, err)
		}

		other, err := service.Render(ctx, request)
		if err != nil {
			t.Fatal(err)
		}

		if other.ID == artifact.ID {
			t.Fatal("distinct request scopes coalesced a print artifact")
		}
	}

	if _, found, err := service.Held(artifact.ID); err != nil || found {
		t.Fatalf("offline interface disclosed account PDF: %v %v", found, err)
	}
}

type (
	privatePrintRequestKey struct{}
	scopedPrintAssets      struct {
		data     []byte
		observed chan bool
	}
)

func (s scopedPrintAssets) Open(ctx context.Context, _ assets.ID) (assets.Blob, io.ReadCloser, error) {
	principal, present := identity.PrincipalFromContext(ctx)

	valid := present && principal.UserID == "alice" && access.LibraryID(ctx) == "one" && principal.CredentialID == "" && ctx.Value(privatePrintRequestKey{}) == nil
	s.observed <- valid

	if !valid {
		return assets.Blob{}, nil, assets.ErrNotFound
	}

	return assets.Blob{}, io.NopCloser(bytes.NewReader(s.data)), nil
}

type scopedImageRenderer struct{ observed chan bool }

func (scopedImageRenderer) Version() string { return "scoped-image" }
func (s scopedImageRenderer) Render(_ context.Context, document PrintDocument) ([]byte, error) {
	s.observed <- len(document.Assets) == 1
	return []byte("%PDF-1.4\n%%EOF"), nil
}

func TestRenderWorkerRetainsOnlyPrincipalAndLibraryForAssetAuthorization(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}

	values := identityFixture()
	values.entry.Recipe.PrimaryImage = "photo"
	values.entry.Recipe.Assets = []recipe.Asset{{ID: "photo", SHA256: "photo-hash", MediaType: "image/png"}}
	opened := make(chan bool, 1)
	rendered := make(chan bool, 1)

	service, err := NewService(scopedImageRenderer{observed: rendered}, units.DefaultCatalog(), scopedPrintAssets{data: data.Bytes(), observed: opened}, values, values)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(t.Context(), privatePrintRequestKey{}, "private browser request")
	ctx = access.WithLibrary(identity.WithPrincipal(ctx, identity.Principal{UserID: "alice", CredentialID: "private-credential"}), "one")
	options := DefaultOptions()
	options.Image = ImageInclude

	artifact, err := service.Render(ctx, Request{Source: NewRecipeSource(values, values.entry.ID), Options: options})
	if err != nil {
		t.Fatal(err)
	}

	if len(artifact.Warnings) > 0 || !<-opened || !<-rendered {
		t.Fatalf("worker dropped asset authority or retained request data: %#v", artifact.Warnings)
	}
}
