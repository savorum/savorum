package buildinfo

import "testing"

func TestInjectedVersionWins(t *testing.T) {
	original := Version
	Version = "0.1.0-test"

	t.Cleanup(func() { Version = original })

	if got := CurrentVersion(); got != "0.1.0-test" {
		t.Fatalf("CurrentVersion() = %q", got)
	}
}
