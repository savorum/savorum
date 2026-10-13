//go:build linux || darwin

package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func renderPageTool(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("testdata/document-scan.png")
	if err != nil {
		t.Fatal(err)
	}

	return "/bin/cat " + shellArgument(path)
}

func shellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestDocumentProcessOutputLimitsStopSilentChild(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			// Each fake writes a small excess, then stops producing output.
			// Waiting for another write or for the extraction deadline is unsafe.
			redirect := ""
			limit := 32

			if stream == "stderr" {
				redirect = " >&2"
				limit = 64 << 10
			}

			script := "printf " + shellArgument(strings.Repeat("x", limit+1)) + redirect + "; exec /bin/sleep 10"
			withTools(t, toolPath(t, map[string]string{"overflow": script}))

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			out := cappedBuffer{max: 32}
			started := time.Now()

			err := runDocumentTool(ctx, &out, "overflow")
			if !errors.Is(err, errDocumentOutputLimit) || time.Since(started) > time.Second || out.b.Len() > out.max {
				t.Fatalf("error=%v elapsed=%v buffered=%d", err, time.Since(started), out.b.Len())
			}
		})
	}
}

func TestDocumentProcessCancellationStopsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "escaped")
	withTools(t, toolPath(t, map[string]string{
		"descendants": "(/bin/sleep 0.4; printf escaped > " + shellArgument(marker) + ") & wait",
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	out := cappedBuffer{max: 32}
	started := time.Now()

	err := runDocumentTool(ctx, &out, "descendants")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("error=%v elapsed=%v", err, time.Since(started))
	}

	time.Sleep(500 * time.Millisecond)

	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled descendant wrote its marker: %v", err)
	}
}

func TestDocumentProcessInheritedPipeHasBoundedWait(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "escaped")
	withTools(t, toolPath(t, map[string]string{
		"retained-pipe": "(/bin/sleep 0.6; printf escaped > " + shellArgument(marker) + ") & exit 0",
	}))

	out := cappedBuffer{max: 32}
	started := time.Now()

	err := runDocumentTool(t.Context(), &out, "retained-pipe")
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("error=%v elapsed=%v", err, time.Since(started))
	}

	time.Sleep(700 * time.Millisecond)

	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant survived pipe cleanup: %v", err)
	}
}

func TestDocumentProcessArgumentsAndChildLimits(t *testing.T) {
	withTools(t, toolPath(t, map[string]string{
		"limits": "printf '%s\\n' \"$1\"; ulimit -H -t; ulimit -S -t; ulimit -H -f; ulimit -S -f; ulimit -H -c; ulimit -S -c; printf '%s %s\\n' \"$LC_ALL\" \"$OMP_THREAD_LIMIT\"",
	}))
	marker := filepath.Join(t.TempDir(), "injected")
	argument := "$(touch " + marker + ")'; echo injected"

	out := cappedBuffer{max: 1024}
	if err := runDocumentTool(t.Context(), &out, "limits", argument); err != nil {
		t.Fatal(err)
	}

	want := argument + "\n15\n15\n8192\n8192\n0\n0\nC 1\n"
	if out.b.String() != want {
		t.Fatalf("child limits/arguments=%q want %q", out.b.String(), want)
	}

	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("argument executed: %v", err)
	}
}

func TestDocumentProcessFileLimitPreventsGrowth(t *testing.T) {
	file := filepath.Join(t.TempDir(), "output")
	withTools(t, toolPath(t, map[string]string{
		"file-limit": "ulimit -S -f 8 && ulimit -H -f 8 || exit 1; exec /bin/dd if=/dev/zero of=\"$1\" bs=1024 count=16",
	}))

	out := cappedBuffer{max: 1024}
	if err := runDocumentTool(t.Context(), &out, "file-limit", file); err == nil {
		t.Fatal("child exceeded file limit successfully")
	}

	info, err := os.Stat(file)
	if err != nil || info.Size() > 8<<10 {
		t.Fatalf("file=%v error=%v", info, err)
	}
}

func TestDocumentProcessLimitSetupFailsBeforeTool(t *testing.T) {
	command, err := documentCommand(t.Context(), "/bin/sh", "-c", "printf executed")
	if err != nil {
		t.Fatal(err)
	}
	// Lower the inherited hard CPU limit before executing the actual wrapper.
	// Its attempt to set 15 seconds must fail before the tool's printf.
	command.Args = append([]string{"/bin/sh", "-c", `ulimit -S -t 1 && ulimit -H -t 1 && exec "$@"`, "restricted"}, command.Args...)
	out := cappedBuffer{max: 1024}

	command.Stdout = &out
	if err := command.Run(); err == nil || out.b.Len() != 0 {
		t.Fatalf("limit setup error=%v tool output=%q", err, out.b.String())
	}
}

func TestDocumentWorkAdmissionAndCancellation(t *testing.T) {
	release, err := acquireDocumentWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := acquireDocumentWork(t.Context()); !errors.Is(err, errDocumentBusy) {
		t.Fatalf("concurrent work error=%v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := acquireDocumentWork(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled work error=%v", err)
	}
}

func TestDocumentPDFPagePreflight(t *testing.T) {
	for _, count := range []string{"9", "0", "garbage", "1\nPages: 1", ""} {
		t.Run(fmt.Sprintf("pages-%q", count), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "extracted")
			withTools(t, toolPath(t, map[string]string{
				"pdfinfo":   "printf " + shellArgument("Pages: "+count+"\n"),
				"pdftotext": "printf extracted > " + shellArgument(marker),
			}))

			if _, err := (localOCR{}).Extract(t.Context(), "input.pdf", []byte("%PDF-1.7")); err == nil {
				t.Fatal("invalid or excess page count accepted")
			}

			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("text extraction ran before page rejection: %v", err)
			}
		})
	}
}

func TestDocumentPDFExtractionUsesBoundedPageArguments(t *testing.T) {
	fixture := shellArgument(bboxReport([][]string{{"Text page"}, {"Instructions", "Mix"}}))
	withTools(t, toolPath(t, map[string]string{
		"pdfinfo":   "printf 'Pages: 2\\n'",
		"pdftotext": `test "$1" = -f && test "$2" = 1 && test "$3" = -l && test "$4" = 2 && test "$5" = -bbox-layout && test "$7" = - || exit 9; printf '%s' ` + fixture,
	}))

	if text, err := (localOCR{}).Extract(t.Context(), "input.pdf", []byte("%PDF-1.7")); err != nil || !strings.Contains(text, "Mix") {
		t.Fatalf("text=%q error=%v", text, err)
	}
}

func TestDocumentPDFRendersOnlyBlankPages(t *testing.T) {
	withTools(t, toolPath(t, map[string]string{
		"pdftotext": bboxTool([]string{"Ingredients", "2 eggs"}, nil),
		"pdftoppm":  `test "$1" = -f && test "$2" = 2 && test "$3" = -l && test "$4" = 2 && test "$5" = -singlefile && test "$6" = -png && test "$7" = -scale-to && test "$8" = 2048 && test "$#" = 9 || exit 9; ` + renderPageTool(t),
		"tesseract": tsvTool("Instructions", "Beat eggs."),
	}))

	text, err := (localOCR{}).Extract(t.Context(), "input.pdf", []byte("%PDF-1.7"))
	if err != nil || !strings.Contains(text, "2 eggs") || !strings.Contains(text, "Beat eggs.") {
		t.Fatalf("text=%q error=%v", text, err)
	}
}
