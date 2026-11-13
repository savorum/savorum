package exchange

import (
	"cmp"
	"context"
	"net/url"
	"strings"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/units"
)

// AssumedConvention records a measurement convention inferred from the source domain.
type AssumedConvention struct {
	Convention string `json:"convention"`
	Label      string `json:"label"`
	Host       string `json:"host"`
	PlanID     string `json:"plan_id"`
	Revertible bool   `json:"revertible"`
}

var addressConventions = map[string]units.Convention{
	"com": units.ConventionUS, "us": units.ConventionUS, "gov": units.ConventionUS, "edu": units.ConventionUS,
	"au": units.ConventionAU,
	"uk": units.ConventionMetric, "ie": units.ConventionMetric, "nz": units.ConventionMetric,
	"ca": units.ConventionMetric, "za": units.ConventionMetric,
}

func addressConvention(rawURL string) (units.Convention, string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", ""
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	label := host[strings.LastIndexByte(host, '.')+1:]

	return addressConventions[label], host
}

func englishOrUnstated(preview ImportPreview) bool {
	for _, item := range preview.Items {
		locale := cmp.Or(item.Recipe.SourceLocale, item.Recipe.Language)
		if base, _ := language.Make(locale).Base(); locale != "" && base.String() != "en" {
			return false
		}
	}

	return true
}

func (s *Service) assumeAddressConvention(ctx context.Context, preview ImportPreview, rawURL string) ImportPreview {
	convention, host := addressConvention(rawURL)
	if convention == "" || len(preview.ResolutionDecisions) == 0 || !englishOrUnstated(preview) {
		return preview
	}

	plan, err := s.CreateMeasurementResolutionPlan(ctx, preview.ID, MeasurementResolutionPlanRequest{
		Interpretation:   MeasurementResolutionInterpretation{Kind: ResolutionConvention, Convention: string(convention)},
		ExpectedRevision: preview.Revision, Basis: BasisSourceAddress,
	})
	if err != nil || !applicable(plan) {
		return preview
	}

	applied, err := s.ApplyMeasurementResolutionPlan(ctx, preview.ID, plan.ID, preview.Revision)
	if err != nil || applied.Preview == nil {
		return preview
	}

	label := string(convention)

	for _, profile := range applied.Preview.ResolutionProfiles {
		if profile.ID == string(convention) {
			label = profile.Label
		}
	}

	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()

	session, found := s.previews[preview.ID]
	if !found {
		return *applied.Preview
	}

	session.assumed = &AssumedConvention{Convention: string(convention), Label: label, Host: host, PlanID: plan.ID}

	return previewFromSession(preview.ID, session)
}

// Resolve unique aliases without inferring US, Australian, or imperial conventions.
func (s *Service) settleSoleReadings(ctx context.Context, preview ImportPreview) ImportPreview {
	catalog := units.DefaultCatalog()

	for _, decision := range preview.ResolutionDecisions {
		if len(decision.CandidateUnitIDs) != 1 {
			continue
		}

		if convention, _ := catalog.ConventionOf(decision.CandidateUnitIDs[0]); convention != units.ConventionNone && convention != units.ConventionMetric {
			continue
		}

		plan, err := s.CreateMeasurementResolutionPlan(ctx, preview.ID, MeasurementResolutionPlanRequest{
			GroupIDs:         decision.GroupIDs,
			Interpretation:   MeasurementResolutionInterpretation{Kind: ResolutionCanonicalUnit, UnitID: decision.CandidateUnitIDs[0]},
			ExpectedRevision: preview.Revision, Basis: BasisSoleReading,
		})
		if err != nil || !applicable(plan) {
			continue
		}

		if applied, err := s.ApplyMeasurementResolutionPlan(ctx, preview.ID, plan.ID, preview.Revision); err == nil && applied.Preview != nil {
			preview = *applied.Preview
		}
	}

	return preview
}

func applicable(plan MeasurementResolutionPlan) bool {
	return len(plan.ValidationFailures) == 0 && (len(plan.Occurrences) > 0 || len(plan.PendingOccurrences) > 0)
}
