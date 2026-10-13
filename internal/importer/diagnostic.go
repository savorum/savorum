package importer

// EffectiveClass supplies the compatibility rule for diagnostics created by
// older adapters. New producers should state the class directly; old errors,
// warnings, and information retain their previous meaning.
func (diagnostic Diagnostic) EffectiveClass() DiagnosticClass {
	if diagnostic.Class != "" {
		return diagnostic.Class
	}

	switch diagnostic.Severity {
	case SeverityError:
		return DiagnosticBlocking
	case SeverityInfo:
		return DiagnosticInformational
	case SeverityWarning:
		return DiagnosticActionable
	default:
		return DiagnosticActionable
	}
}

// Occurrences returns the represented number of affected source locations.
func (diagnostic Diagnostic) Occurrences() int {
	if diagnostic.Count > 0 {
		return diagnostic.Count
	}

	if len(diagnostic.Targets) > 0 {
		return len(diagnostic.Targets)
	}

	return 1
}

// AggregateDiagnostics collapses repeated occurrences without hiding their
// stable targets or technical identity.
//
// Diagnostics are collapsed by the sentence a reader sees, not by area, so
// five unmapped fields read as one sentence listing them.
func AggregateDiagnostics(values []Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(values))
	positions := map[string]int{}

	for _, value := range values {
		value.Class = value.EffectiveClass()
		value.Count = value.Occurrences()

		key := value.Code + "\x00" + string(value.Class) + "\x00" + value.Message
		if position, found := positions[key]; found {
			result[position].Count += value.Count
			result[position].Targets = append(result[position].Targets, value.Targets...)

			// The merged entry keeps an area only while every occurrence agrees.
			if result[position].Area != value.Area {
				result[position].Area = ""
			}

			continue
		}

		positions[key] = len(result)
		result = append(result, value)
	}

	return result
}
