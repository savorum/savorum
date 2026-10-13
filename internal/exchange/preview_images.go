package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/savorum/savorum/internal/assets"
)

// PreviewImage returns one candidate's leading photograph, retrieving it now if
// nobody has yet.
//
// This is the whole of the preview's outbound image work, driven by what a
// reviewer looks at. Each request uses the guarded transport and its limits,
// and a per-session allowance bounds how many distinct photographs one review
// retrieves; past it a photograph is pending, and the import fetches it.
func (s *Service) PreviewImage(ctx context.Context, previewID, itemID string) (PreviewAsset, error) {
	session, identity, embedded, err := s.previewImageTarget(ctx, previewID, itemID)
	if err != nil {
		return PreviewAsset{}, err
	}

	if embedded != nil {
		return *embedded, nil
	}

	if asset, outcome, known := s.stagedPreviewImage(previewID, session, identity); known {
		switch outcome {
		case remoteStaged:
			return asset, nil
		case remoteDeferred:
			return PreviewAsset{}, ErrPreviewImagePending
		case remoteFailed:
			return PreviewAsset{}, ErrPreviewImageUnavailable
		default:
			return PreviewAsset{}, ErrPreviewImageUnavailable
		}
	}

	if !s.reservePreviewImageFetch(previewID, session) {
		s.recordPreviewImageOutcome(previewID, session, identity, stagedRemoteAsset{}, remoteDeferred)
		return PreviewAsset{}, ErrPreviewImagePending
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.previewLimits.ImageTimeout)
	defer cancel()

	fetched, err := s.fetchRemote(fetchCtx, identity)
	if err == nil {
		var data []byte

		data, err = io.ReadAll(io.LimitReader(fetched.Body, maximumRemoteImageBytes+1))

		closeErr := fetched.Body.Close()
		switch {
		case err == nil && closeErr != nil:
			err = closeErr
		case err == nil && len(data) > maximumRemoteImageBytes:
			err = fmt.Errorf("remote image exceeds its ceiling")
		}

		if err == nil {
			staged := stagedRemoteAsset{bytes: data, mediaType: fetched.MediaType}
			// Keeping the bytes saves the import from fetching them again, when
			// the session has room.
			s.recordPreviewImageOutcome(previewID, session, identity, staged, remoteStaged)

			return PreviewAsset{Bytes: append([]byte(nil), staged.bytes...), MediaType: staged.mediaType}, nil
		}
	}
	// A reader who navigated away has not learned anything about the
	// photograph, so nothing is remembered and the allowance is given back.
	if ctx.Err() != nil {
		s.releasePreviewImageFetch(previewID, session)
		return PreviewAsset{}, fmt.Errorf("fetch preview image: %w", ctx.Err())
	}
	// Running out of time is not the same as an address that does not answer.
	// The import tries this one again, so it is outstanding work.
	if errors.Is(err, context.DeadlineExceeded) {
		s.recordPreviewImageOutcome(previewID, session, identity, stagedRemoteAsset{}, remoteDeferred)
		return PreviewAsset{}, ErrPreviewImagePending
	}

	s.recordPreviewImageOutcome(previewID, session, identity, stagedRemoteAsset{}, remoteFailed)

	return PreviewAsset{}, ErrPreviewImageUnavailable
}

// previewImageTarget resolves which photograph a row is asking for. A package
// or a document that arrived with its bytes answers immediately; everything
// else names an address.
func (s *Service) previewImageTarget(ctx context.Context, previewID, itemID string) (*previewSession, string, *PreviewAsset, error) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session := s.previews[previewID]
	if session == nil || session.scope != s.previewScope(ctx) {
		return nil, "", nil, ErrPreviewNotFound
	}

	candidate := session.candidates[itemID]
	if candidate == nil {
		return nil, "", nil, ErrPreviewNotFound
	}

	if asset := leadingEmbeddedAsset(candidate); asset != nil {
		return session, "", asset, nil
	}

	for _, remote := range candidate.item.RemoteAssets {
		identity, err := canonicalRemoteURL(remote.URL)
		if err != nil {
			continue
		}

		return session, identity, nil, nil
	}

	return nil, "", nil, ErrPreviewImageUnavailable
}

// leadingEmbeddedAsset returns the first inline-safe photograph a package or an
// asset-carrying upload brought with it.
func leadingEmbeddedAsset(candidate *previewCandidate) *PreviewAsset {
	if candidate.item.Opened != nil {
		for _, asset := range candidate.item.Opened.Assets {
			if assets.InlineSafe(asset.MediaType) {
				return &PreviewAsset{Bytes: append([]byte(nil), asset.Bytes...), MediaType: asset.MediaType}
			}
		}
	}

	for _, asset := range candidate.item.EmbeddedAssets {
		if assets.InlineSafe(asset.MediaType) {
			return &PreviewAsset{Bytes: append([]byte(nil), asset.Bytes...), MediaType: asset.MediaType}
		}
	}

	return nil
}

func (s *Service) stagedPreviewImage(previewID string, session *previewSession, identity string) (PreviewAsset, remoteStagingOutcome, bool) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	if s.previews[previewID] != session {
		return PreviewAsset{}, remoteFailed, false
	}

	if staged, ok := session.stagedRemote[identity]; ok {
		return PreviewAsset{Bytes: append([]byte(nil), staged.bytes...), MediaType: staged.mediaType}, remoteStaged, true
	}

	outcome, known := session.imageOutcomes[identity]

	return PreviewAsset{}, outcome, known
}

// reservePreviewImageFetch takes one of the session's photograph allowances.
func (s *Service) reservePreviewImageFetch(previewID string, session *previewSession) bool {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	if s.previews[previewID] != session || session.imageFetches >= s.previewLimits.ImagesPerSession {
		return false
	}

	session.imageFetches++

	return true
}

func (s *Service) releasePreviewImageFetch(previewID string, session *previewSession) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	if s.previews[previewID] == session && session.imageFetches > 0 {
		session.imageFetches--
	}
}

func (s *Service) recordPreviewImageOutcome(previewID string, session *previewSession, identity string, staged stagedRemoteAsset, outcome remoteStagingOutcome) {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	if s.previews[previewID] != session {
		return
	}

	if session.imageOutcomes == nil {
		session.imageOutcomes = map[string]remoteStagingOutcome{}
	}
	// A deferred photograph is not a decision, so it is not remembered: the
	// allowance may free up, and the import will try it regardless.
	if outcome != remoteDeferred {
		session.imageOutcomes[identity] = outcome
	}

	if outcome != remoteStaged {
		return
	}

	grown := int64(len(staged.bytes))
	if exceedsInt64(session.retainedBytes, grown, s.previewLimits.RetainedBytesPerSession) ||
		exceedsInt64(s.previewStats.RetainedBytes+s.pendingPreviewBytes, grown, s.previewLimits.RetainedBytes) {
		return
	}

	session.stagedRemote[identity] = staged
	session.retainedBytes += grown
	s.previewStats.RetainedBytes += grown
}
