package importer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestPaprikaProbeAggregateAndCancellation(t *testing.T) {
	members := []nativeZIPMember{}
	for _, name := range []string{"0", "1", "2", "3", "4", "5"} {
		members = append(members, nativeZIPMember{name, gzipBytes(t, bytes.Repeat([]byte("x"), 256<<10))})
	}

	members = append(members, nativeZIPMember{"recipe", gzipBytes(t, []byte(`{"uid":"recipe"}`))})
	data := orderedNativeZIP(t, members...)
	// The matching member sorts after four full chunks; the probe never reaches it.
	if zipContainsPaprika(t.Context(), data) {
		t.Fatal("aggregate probe limit bypass")
	}

	normal := zipBytes(t, map[string][]byte{"recipe": gzipBytes(t, []byte(`{"uid":"recipe"}`))})
	if !zipContainsPaprika(t.Context(), normal) {
		t.Fatal("normal archive not recognized")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if got := (paprikaAdapter{}).Probe(ctx, Input{Name: "x.paprikarecipes", Data: normal}); got.Confidence != 0 {
		t.Fatal("cancelled probe returned confidence", got)
	}

	remaining := int64(17)
	r := &paprikaProbeReader{ctx: t.Context(), reader: bytes.NewReader(bytes.Repeat([]byte("x"), 100)), remaining: &remaining}

	b, err := io.ReadAll(r)
	if err == nil {
		t.Fatal("budget not enforced")
	}

	if len(b) != 17 || remaining != 0 {
		t.Fatal(len(b), remaining, err)
	}
}

// Declared compressed ranges may overlap or extend beyond the real body. They
// consume work allowance even when inflation returns no usable output.
func TestPaprikaProbeCompressedPrecharge(t *testing.T) {
	members := []nativeZIPMember{}
	for _, name := range []string{"0", "1", "2", "3", "4"} {
		members = append(members, nativeZIPMember{name, gzipBytes(t, []byte("no recipe"))})
	}

	members = append(members, nativeZIPMember{"recipe", gzipBytes(t, []byte(`{"uid":"recipe"}`))})
	data := orderedNativeZIP(t, members...)

	central := bytes.Index(data, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("missing central directory")
	}

	binary.LittleEndian.PutUint32(data[central+20:central+24], (1<<20)+1)

	if zipContainsPaprika(t.Context(), data) {
		t.Fatal("oversized compressed member was inflated")
	}

	data = orderedNativeZIP(t, members...)
	for offset := 0; offset < len(data); {
		next := bytes.Index(data[offset:], []byte{'P', 'K', 1, 2})
		if next < 0 {
			break
		}

		offset += next
		binary.LittleEndian.PutUint32(data[offset+20:offset+24], 1<<20)
		offset += 46 + int(binary.LittleEndian.Uint16(data[offset+28:offset+30])) + int(binary.LittleEndian.Uint16(data[offset+30:offset+32])) + int(binary.LittleEndian.Uint16(data[offset+32:offset+34]))
	}

	if zipContainsPaprika(t.Context(), data) {
		t.Fatal("aggregate compressed precharge bypass")
	}
}

type paprikaReadFunc func([]byte) (int, error)

func (f paprikaReadFunc) Read(p []byte) (int, error) { return f(p) }

func TestPaprikaProbeCompressedReaderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	remaining := int64(1 << 20)
	calls := 0
	r := &paprikaProbeReader{ctx: ctx, remaining: &remaining, reader: paprikaReadFunc(func(p []byte) (int, error) {
		calls++

		if len(p) > 32<<10 {
			t.Fatal("unbounded read")
		}

		cancel()

		return len(p), nil
	})}

	n, err := r.Read(make([]byte, 64<<10))
	if n != 32<<10 || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(n, err, calls)
	}

	if n, err = r.Read(make([]byte, 1)); n != 0 || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("read after cancellation", n, err, calls)
	}
}
