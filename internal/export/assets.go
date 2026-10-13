package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/sref"
)

// Assets is the reading half of the asset store an export needs. An export
// never fetches a recipe's original URLs.
type Assets interface {
	Open(ctx context.Context, id assets.ID) (assets.Blob, io.ReadCloser, error)
}

type assetResolver struct{ store Assets }

// resolve turns a recipe's declared assets, whatever their role, into the
// content an SREF package writer can stream.
func (r *assetResolver) resolve(ctx context.Context, snapshot RecipeSnapshot) ([]sref.AssetContent, error) {
	if len(snapshot.Recipe.Assets) == 0 {
		return nil, nil
	}

	if r == nil || r.store == nil {
		return nil, &AssetError{
			RecipeTitle: snapshot.Recipe.Title,
			AssetID:     snapshot.Recipe.Assets[0].ID,
			Reason:      "this instance stores no files",
			ReasonCode:  "storage-disabled",
		}
	}

	contents := make([]sref.AssetContent, 0, len(snapshot.Recipe.Assets))
	for _, asset := range snapshot.Recipe.Assets {
		declared := asset
		contents = append(contents, sref.AssetContent{
			ID: declared.ID, Path: declared.Path, MediaType: declared.MediaType,
			SHA256: declared.SHA256, Size: declared.Size,
			Open: func() (io.ReadCloser, error) {
				_, body, err := r.store.Open(ctx, assets.ID("sha256:"+declared.SHA256))
				if err != nil {
					return nil, r.unavailable(snapshot, declared.ID, err)
				}
				// A reader who navigates away mid-download should not leave
				// the server copying the rest of a photograph to nobody.
				return &cancellableBody{ctx: ctx, body: body}, nil
			},
		})
	}

	return contents, nil
}

// verify reads every declared photograph through a digest before any of the
// artifact is written, so an export is complete or fails before it starts.
func (r *assetResolver) verify(ctx context.Context, snapshot RecipeSnapshot, verified map[string]int64) error {
	if len(snapshot.Recipe.Assets) == 0 {
		return nil
	}

	if r == nil || r.store == nil {
		return &AssetError{
			RecipeTitle: snapshot.Recipe.Title,
			AssetID:     snapshot.Recipe.Assets[0].ID,
			Reason:      "this instance stores no files",
			ReasonCode:  "storage-disabled",
		}
	}

	for _, asset := range snapshot.Recipe.Assets {
		if err := ctx.Err(); err != nil {
			return err
		}

		if size, alreadyVerified := verified[asset.SHA256]; alreadyVerified {
			if size != asset.Size {
				return &AssetError{RecipeTitle: snapshot.Recipe.Title, AssetID: asset.ID, ReasonCode: "size-mismatch", Reason: "is not the size another reference records"}
			}

			continue
		}

		blob, body, err := r.store.Open(ctx, assets.ID("sha256:"+asset.SHA256))
		if err != nil {
			return r.unavailable(snapshot, asset.ID, err)
		}

		digest := sha256.New()
		size, copyErr := io.Copy(digest, body)
		_ = body.Close()

		if copyErr != nil {
			return &AssetError{
				RecipeTitle: snapshot.Recipe.Title, AssetID: asset.ID,
				Reason: "could not be read", ReasonCode: "unreadable",
			}
		}
		// The bytes just read must agree with both the recipe and the store.
		if size != asset.Size || blob.Size != asset.Size {
			return &AssetError{
				RecipeTitle: snapshot.Recipe.Title, AssetID: asset.ID,
				Reason: "is not the size the recipe records", ReasonCode: "size-mismatch",
			}
		}

		if hex.EncodeToString(digest.Sum(nil)) != asset.SHA256 {
			return &AssetError{
				RecipeTitle: snapshot.Recipe.Title, AssetID: asset.ID,
				Reason: "failed its integrity check", ReasonCode: "integrity",
			}
		}

		verified[asset.SHA256] = asset.Size
	}

	return nil
}

func (*assetResolver) unavailable(snapshot RecipeSnapshot, assetID string, err error) error {
	reason := "could not be read"
	code := "unreadable"

	if errors.Is(err, assets.ErrNotFound) {
		reason = "is not in this library"
		code = "missing"
	}

	return &AssetError{RecipeTitle: snapshot.Recipe.Title, AssetID: assetID, Reason: reason, ReasonCode: code}
}

// cancellableBody stops a copy when the request it belongs to is over. Its
// context exists only for the lifetime of the returned request-scoped body.
//
//nolint:containedctx // io.Reader.Read has no context parameter
type cancellableBody struct {
	ctx  context.Context
	body io.ReadCloser
}

func (b *cancellableBody) Read(buffer []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}

	return b.body.Read(buffer) //nolint:wrapcheck // transparent readers must preserve io.EOF exactly
}

func (b *cancellableBody) Close() error {
	return b.body.Close() //nolint:wrapcheck // transparent closers preserve the wrapped body's protocol
}
