package account

import (
	"net/url"
	"testing"
)

func TestAssistantOriginAllowed(t *testing.T) {
	for _, tt := range []struct {
		origin      string
		optIn, want bool
	}{
		{"https://recipes.example", false, true},
		{"http://localhost:8080", false, true},
		{"http://192.168.124.158:8080", false, false},
		{"http://192.168.124.158:8080", true, true},
		{"http://10.0.0.1:8080", true, true},
		{"http://[fd00::1]:8080", true, true},
		{"http://8.8.8.8", true, false},
		{"http://recipes.example", true, false},
		{"ftp://192.168.1.1", true, false},
	} {
		u, err := url.Parse(tt.origin)
		if err != nil {
			t.Fatal(err)
		}

		if got := assistantOriginAllowed(u, tt.optIn); got != tt.want {
			t.Errorf("%s opt-in %v: got %v", tt.origin, tt.optIn, got)
		}
	}
}
