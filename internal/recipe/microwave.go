package recipe

import (
	"math"
	"strconv"
	"time"
)

// MicrowaveEstimate explains a display/timer estimate without replacing the
// authored assertion that supplied its basis.
type MicrowaveEstimate struct {
	Original       DurationExpression `json:"original"`
	Estimated      DurationExpression `json:"estimated,omitzero"`
	SourceWatts    int                `json:"source_watts,omitempty"`
	TargetWatts    int                `json:"target_watts,omitempty"`
	PowerPercent   int                `json:"power_percent,omitempty"`
	AuthoredChoice bool               `json:"authored_choice,omitempty"`
	Applicable     bool               `json:"applicable"`
	Reason         string             `json:"reason,omitempty"`
}

// EstimateMicrowaveTime applies Savorum's deliberately narrow policy. It
// prefers a matching authored choice and otherwise scales only numeric manual
// microwave timings. Opaque modes and restrictions are withheld.
func EstimateMicrowaveTime(assertion TimingAssertion, targetWatts int) MicrowaveEstimate {
	result := MicrowaveEstimate{Original: assertion.Duration, TargetWatts: targetWatts}

	conditions := assertion.Microwave
	if conditions == nil {
		result.Reason = "not_microwave"
		return result
	}

	if targetWatts <= 0 {
		result.Reason = "target_power_missing"
		return result
	}

	if len(conditions.Restrictions) > 0 {
		result.Reason = "source_restriction"
		return result
	}

	for _, choice := range conditions.Choices {
		if microwaveEffectiveWatts(choice.RatedOutputWatts, choice.Power) == targetWatts {
			result.Estimated = choice.Duration
			result.SourceWatts = targetWatts
			result.AuthoredChoice = true
			result.Applicable = true

			return result
		}
	}

	if conditions.ModeText != "" || conditions.Power != nil && conditions.Power.Text != "" {
		result.Reason = "power_unresolved"
		return result
	}

	sourceWatts := microwaveEffectiveWatts(conditions.RatedOutputWatts, conditions.Power)
	if sourceWatts <= 0 {
		result.Reason = "source_power_missing"
		return result
	}

	result.SourceWatts = sourceWatts
	if conditions.Power != nil && conditions.Power.Percent != "" {
		result.PowerPercent, _ = strconv.Atoi(conditions.Power.Percent)
	}

	estimated, ok := scaleDurationExpression(assertion.Duration, sourceWatts, targetWatts)
	if !ok {
		result.Reason = "duration_not_numeric"
		return result
	}

	result.Estimated = estimated
	result.Applicable = true

	return result
}

func microwaveEffectiveWatts(rated int, power *MicrowavePower) int {
	if power != nil {
		if power.Watts > 0 {
			return power.Watts
		}

		if percent, err := strconv.Atoi(power.Percent); err == nil && percent > 0 && rated > 0 {
			return int(math.Round(float64(rated*percent) / 100))
		}
	}

	return rated
}

func scaleDurationExpression(value DurationExpression, sourceWatts, targetWatts int) (DurationExpression, bool) {
	result := value
	result.Approximate = true
	result.SourceText = ""

	if value.Value != "" {
		duration, ok := ParseDuration(value.Value)
		if !ok {
			return DurationExpression{}, false
		}

		result.Value = FormatDuration(scaleMicrowaveDuration(duration, sourceWatts, targetWatts))

		return result, result.Value != ""
	}

	if value.Min != "" && value.Max != "" {
		minimum, minOK := ParseDuration(value.Min)

		maximum, maxOK := ParseDuration(value.Max)
		if !minOK || !maxOK {
			return DurationExpression{}, false
		}

		result.Min = FormatDuration(scaleMicrowaveDuration(minimum, sourceWatts, targetWatts))
		result.Max = FormatDuration(scaleMicrowaveDuration(maximum, sourceWatts, targetWatts))

		return result, result.Min != "" && result.Max != ""
	}

	return DurationExpression{}, false
}

func scaleMicrowaveDuration(value time.Duration, sourceWatts, targetWatts int) time.Duration {
	seconds := float64(value/time.Second) * float64(sourceWatts) / float64(targetWatts)
	if seconds < 1 || seconds > float64(math.MaxInt64/int64(time.Second)) {
		return 0
	}

	return time.Duration(math.Round(seconds)) * time.Second
}
