package strictjson

import "errors"

// Lexical failure kinds are independent of diagnostic wording.
const (
	Malformed = "malformed"
	Duplicate = "duplicate"
	Trailing  = "trailing"
	Nonfinite = "nonfinite"
)

// Failure identifies a lexical JSON violation and retains its diagnostic.
type Failure struct {
	kind  string
	cause error
}

func (f *Failure) Error() string { return f.cause.Error() }
func (f *Failure) Unwrap() error { return f.cause }

// RequirementID names a recipe-level lexical requirement.
func (f *Failure) RequirementID() string {
	switch f.kind {
	case Duplicate:
		return "duplicate-json-member"
	case Nonfinite:
		return "nonfinite-json-number"
	default:
		return "invalid-recipe"
	}
}

// Kind returns the lexical classification through wrapping errors.
func Kind(err error) string {
	if failure, ok := errors.AsType[*Failure](err); ok {
		return failure.kind
	}

	return ""
}
