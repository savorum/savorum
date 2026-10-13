package importer

import "testing"

func TestDocumentProcessLinuxMemoryLimits(t *testing.T) {
	withTools(t, toolPath(t, map[string]string{"memory-limit": "ulimit -H -v; ulimit -S -v"}))

	out := cappedBuffer{max: 128}
	if err := runDocumentTool(t.Context(), &out, "memory-limit"); err != nil {
		t.Fatal(err)
	}

	if out.b.String() != "262144\n262144\n" {
		t.Fatalf("child memory limit=%q", out.b.String())
	}
}
