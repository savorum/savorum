package textutil

import "testing"

func TestCapitalize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"ascii lowercase", "hello", "Hello"},
		{"multibyte lowercase", "échec", "Échec"},
		{"already uppercase", "Hello", "Hello"},
		{"leading whitespace preserved", "  hello", "  hello"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Capitalize(testCase.input); got != testCase.want {
				t.Fatalf("Capitalize(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}
