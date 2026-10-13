package memoryspace

import "testing"

func TestDarwinPhysicalReclaimablePages(t *testing.T) {
	got, err := darwinAvailablePages(1024<<20, 16384, []string{"Pages free: 1024.", "Pages inactive: 32768.", "Pages speculative: 1024.", "File-backed pages: 16384.", "Pages purgeable: 32000.", "Pages wired down: 64000.", "Pages occupied by compressor: 64000."})
	if err != nil || got != (Space{1024 << 20, 272 << 20}) {
		t.Fatalf("got %+v %v", got, err)
	}

	for _, lines := range [][]string{
		{"Pages free: 1."},
		{"Pages free: 1.", "Pages inactive: 2.", "File-backed pages: bad."},
		{"Pages free: 18446744073709551615.", "Pages inactive: 1.", "File-backed pages: 1."},
		{"Pages free: 1.", "Pages inactive: 2.", "File-backed pages: 1.", "Pages free: 1."},
	} {
		if _, err := darwinAvailablePages(1024, 16384, lines); err == nil {
			t.Fatal("invalid counters admitted")
		}
	}
}
