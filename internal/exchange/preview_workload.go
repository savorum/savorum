package exchange

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
)

const (
	previewSessionOverhead   = int64(4 << 10)
	previewCandidateOverhead = int64(1 << 10)
)

var (
	// ErrPreviewBusy reports import preview capacity is exhausted.
	ErrPreviewBusy = errors.New("import preview capacity is exhausted")
	// ErrPreviewTooLarge reports import preview exceeds its retained-memory limit.
	ErrPreviewTooLarge = errors.New("import preview exceeds its retained-memory limit")
)

// PreviewLimits bounds transient work and retained sessions across one Service,
// with additional retained-memory and image limits for each session.
type PreviewLimits struct {
	Sessions                int
	RetainedBytes           int64
	RetainedBytesPerSession int64
	ConcurrentPreviews      int
	Lifetime                time.Duration
	// ImagesPerSession bounds the distinct photographs one review will
	// retrieve on demand: one per selectable recipe.
	ImagesPerSession int
	// ImageTimeout bounds one such retrieval; a late photograph is left for
	// the import to fetch.
	ImageTimeout time.Duration
}

// DefaultPreviewLimits supplies the service budgets also used to replace
// nonpositive fields in WithPreviewLimits.
func DefaultPreviewLimits() PreviewLimits {
	return PreviewLimits{
		Sessions: 16, RetainedBytes: 256 << 20, RetainedBytesPerSession: 96 << 20,
		ConcurrentPreviews: 2, Lifetime: 30 * time.Minute,
		ImagesPerSession: library.MaximumImportRecipes, ImageTimeout: 15 * time.Second,
	}
}

func (limits PreviewLimits) normalized() PreviewLimits {
	defaults := DefaultPreviewLimits()
	if limits.Sessions <= 0 {
		limits.Sessions = defaults.Sessions
	}

	if limits.RetainedBytes <= 0 {
		limits.RetainedBytes = defaults.RetainedBytes
	}

	if limits.RetainedBytesPerSession <= 0 {
		limits.RetainedBytesPerSession = defaults.RetainedBytesPerSession
	}

	if limits.ConcurrentPreviews <= 0 {
		limits.ConcurrentPreviews = defaults.ConcurrentPreviews
	}

	if limits.Lifetime <= 0 {
		limits.Lifetime = defaults.Lifetime
	}

	if limits.ImagesPerSession <= 0 {
		limits.ImagesPerSession = defaults.ImagesPerSession
	}

	if limits.ImageTimeout <= 0 {
		limits.ImageTimeout = defaults.ImageTimeout
	}

	return limits
}

// PreviewStats is a point-in-time resource view plus monotonic outcome
// counters suitable for logs or a future metrics endpoint.
type PreviewStats struct {
	LiveSessions   int
	RetainedBytes  int64
	PendingBytes   int64
	ActivePreviews int
	Accepted       uint64
	Rejected       uint64
	Expired        uint64
	Canceled       uint64
	Completed      uint64
}

// PreviewStats reports resource use without exposing staged content.
func (s *Service) PreviewStats() PreviewStats {
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	stats := s.previewStats
	stats.LiveSessions = len(s.previews)
	stats.PendingBytes = s.pendingPreviewBytes

	return stats
}

type previewReservation struct {
	service *Service
	bytes   int64
	done    bool
}

func (s *Service) beginPreview(bytes int64) (*previewReservation, error) {
	if bytes < 0 || bytes > s.previewLimits.RetainedBytesPerSession {
		s.recordPreviewRejection()
		return nil, ErrPreviewTooLarge
	}

	select {
	case s.previewSlots <- struct{}{}:
	default:
		s.recordPreviewRejection()
		return nil, ErrPreviewBusy
	}

	s.previewsMu.Lock()

	used := s.previewStats.RetainedBytes
	if exceedsInt64(used, s.pendingPreviewBytes, s.previewLimits.RetainedBytes) ||
		exceedsInt64(used+s.pendingPreviewBytes, bytes, s.previewLimits.RetainedBytes) {
		s.previewStats.Rejected++
		s.previewsMu.Unlock()
		<-s.previewSlots

		return nil, ErrPreviewBusy
	}

	s.pendingPreviewBytes += bytes
	s.previewStats.ActivePreviews++
	s.previewsMu.Unlock()

	return &previewReservation{service: s, bytes: bytes}, nil
}

func (s *Service) recordPreviewRejection() {
	s.previewsMu.Lock()
	s.previewStats.Rejected++
	s.previewsMu.Unlock()
}

func (reservation *previewReservation) release() {
	if reservation == nil || reservation.done {
		return
	}

	reservation.done = true
	s := reservation.service
	s.previewsMu.Lock()
	s.pendingPreviewBytes -= reservation.bytes
	s.previewStats.ActivePreviews--
	s.previewsMu.Unlock()
	<-s.previewSlots
}

func (reservation *previewReservation) admit(id string, session *previewSession) error {
	s := reservation.service
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	if session.retainedBytes > s.previewLimits.RetainedBytesPerSession {
		s.previewStats.Rejected++
		return ErrPreviewTooLarge
	}

	retainedWithoutReservation := s.previewStats.RetainedBytes + s.pendingPreviewBytes - reservation.bytes
	if len(s.previews) >= s.previewLimits.Sessions || exceedsInt64(retainedWithoutReservation, session.retainedBytes, s.previewLimits.RetainedBytes) {
		s.previewStats.Rejected++
		return ErrPreviewBusy
	}

	reservation.done = true
	s.pendingPreviewBytes -= reservation.bytes
	s.previewStats.ActivePreviews--
	s.previewStats.RetainedBytes += session.retainedBytes
	s.previewStats.Accepted++
	s.previews[id] = session
	s.schedulePreviewExpiryLocked(id, session)
	<-s.previewSlots

	return nil
}

func exceedsInt64(current, added, maximum int64) bool {
	return added > maximum || current > maximum-added
}

func retainedCandidateBytes(candidate *previewCandidate) int64 {
	// Only the encoded length is read, so the field names are irrelevant and the
	// domain types this embeds stay free of tags they would otherwise carry
	// solely for an accounting measurement.
	encoded, err := json.Marshal(struct {
		View                    ImportPreviewItem     `json:"view"`
		Document                any                   `json:"document"`
		Opened                  any                   `json:"opened"`
		RemoteAssets            any                   `json:"remote_assets"`
		EmbeddedAssets          any                   `json:"embedded_assets"`
		PortableAssetReferences bool                  `json:"portable_asset_references"`
		Metadata                any                   `json:"metadata"`
		ImporterMetadata        any                   `json:"importer_metadata"`
		Diagnostics             []importer.Diagnostic `json:"diagnostics"`
	}{
		candidate.view, candidate.item.Document, candidate.item.Opened,
		candidate.item.RemoteAssets, candidate.item.EmbeddedAssets, candidate.item.PortableAssetReferences,
		candidate.metadata, candidate.importerMetadata, candidate.diagnostics,
	})
	if err != nil || int64(len(encoded)) > (math.MaxInt64-previewCandidateOverhead)/2 {
		return math.MaxInt64
	}

	charged := previewCandidateOverhead + 2*int64(len(encoded))
	if int64(len(candidate.item.Data))+int64(len(candidate.item.SourceBytes)) > math.MaxInt64-charged {
		return math.MaxInt64
	}

	return charged + int64(len(candidate.item.Data)) + int64(len(candidate.item.SourceBytes))
}

func retainedSessionBytes(session *previewSession) int64 {
	total := previewSessionOverhead
	for _, candidate := range session.candidates {
		if candidate.retainedBytes > math.MaxInt64-total {
			return math.MaxInt64
		}

		total += candidate.retainedBytes
	}

	for _, staged := range session.stagedRemote {
		if int64(len(staged.bytes)) > math.MaxInt64-total {
			return math.MaxInt64
		}

		total += int64(len(staged.bytes))
	}

	for _, plan := range session.resolutionPlans {
		charged := retainedResolutionPlanBytes(plan)
		if charged > math.MaxInt64-total {
			return math.MaxInt64
		}

		total += charged
	}

	return total
}

func retainedResolutionPlanBytes(plan *storedResolutionPlan) int64 {
	if plan == nil {
		return 0
	}

	publicPlan := plan.plan
	publicPlan.Preview = nil

	encoded, err := json.Marshal(struct { //nolint:musttag // measured for size only, never transmitted
		Request       MeasurementResolutionPlanRequest       `json:"request"`
		Plan          MeasurementResolutionPlan              `json:"plan"`
		Fingerprints  map[string]string                      `json:"fingerprints"`
		SelectedUnits map[string]string                      `json:"selected_units"`
		Snapshots     map[string]candidateResolutionSnapshot `json:"snapshots"`
	}{plan.request, publicPlan, plan.fingerprints, plan.selectedUnits, plan.snapshots})
	if err != nil || int64(len(encoded)) > math.MaxInt64/2 {
		return math.MaxInt64
	}

	return 2 * int64(len(encoded))
}

func (s *Service) schedulePreviewExpiryLocked(id string, session *previewSession) {
	delay := max(time.Until(session.expiresAt), 0)
	session.timer = time.AfterFunc(delay, func() {
		s.previewsMu.Lock()
		defer s.previewsMu.Unlock()

		if s.previews[id] != session {
			return
		}

		if session.busy {
			session.expired = true
			return
		}

		s.removePreviewLocked(id, session)
		s.previewStats.Expired++
	})
}

func (s *Service) removePreviewLocked(id string, session *previewSession) {
	if current := s.previews[id]; current != session {
		return
	}

	delete(s.previews, id)

	if session.timer != nil {
		session.timer.Stop()
	}

	s.previewStats.RetainedBytes -= session.retainedBytes
}
