package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/sref/archivepolicy"
)

func nativeSecurityZIP(t *testing.T, names ...string) []byte {
	t.Helper()

	var data bytes.Buffer

	w := zip.NewWriter(&data)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write([]byte("payload")); err != nil {
			t.Fatal(err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return data.Bytes()
}

func TestNativeArchiveRejectsAmbiguousAndUnsafeMembers(t *testing.T) {
	for _, names := range [][]string{
		{"recipe.json", "recipe.json"},
		{"recipe.json", "./recipe.json"},
		{"Recipe.json", "recipe.json"},
		{"caf\u00e9.json", "cafe\u0301.json"},
		{"../recipe.json"},
		{"folder/../recipe.json"},
		{"/recipe.json"},
		{`..\recipe.json`},
		{`C:\recipe.json`},
		{"recipe\x00.json"},
	} {
		t.Run(names[len(names)-1], func(t *testing.T) {
			err := readNativeZip(nativeSecurityZIP(t, names...), 10, func(string, []byte) error { return nil })
			if err == nil {
				t.Fatal("accepted unsafe or ambiguous archive members")
			}
		})
	}
}

func TestNativeArchiveAcceptsDirectoryEntries(t *testing.T) {
	archive := func(directory string) []byte {
		var data bytes.Buffer

		w := zip.NewWriter(&data)
		if _, err := w.Create(directory); err != nil {
			t.Fatal(err)
		}

		f, err := w.Create("images/recipe.json")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write([]byte("{}")); err != nil {
			t.Fatal(err)
		}

		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		return data.Bytes()
	}

	var read []string
	if err := readNativeZip(archive("images/"), 10, func(name string, _ []byte) error {
		read = append(read, name)
		return nil
	}); err != nil || len(read) != 1 || read[0] != "images/recipe.json" {
		t.Fatalf("read=%v err=%v", read, err)
	}

	if err := readNativeZip(archive("../images/"), 10, func(string, []byte) error { return nil }); err == nil {
		t.Fatal("accepted a directory entry outside the archive")
	}
}

func TestNativeProbeHasOneExpandedByteBudget(t *testing.T) {
	var members []nativeZIPMember

	for index := range 9 {
		name := fmt.Sprintf("%02d.json", index)

		body := bytes.Repeat([]byte("x"), 1<<20)
		if index == 8 {
			copy(body, `"recipe_ingredient"`)
		}

		members = append(members, nativeZIPMember{name: name, data: body})
	}

	archive := orderedNativeZIP(t, members...)
	if zipContainsJSONKey(archive, `"recipe_ingredient"`) {
		t.Fatal("probe read beyond its aggregate expanded-byte budget")
	}
}

func TestPlainTextRejectsAdversarialLineCountsAndLengths(t *testing.T) {
	for _, text := range []string{
		"Soup\n" + strings.Repeat("\n", 1_000_000),
		"Soup\n" + strings.Repeat("x", maxPlainTextLineBytes+1),
	} {
		_, err := (plainTextAdapter{}).Import(context.Background(), Input{Name: "recipe.txt", Data: []byte(text)}, Options{})
		if err == nil {
			t.Fatal("accepted text beyond line limits")
		}
	}
}

func TestNativeArchiveSharesExpandedBudgetAcrossMembers(t *testing.T) {
	visited := 0

	err := readNativeZipBudget(nativeSecurityZIP(t, "one", "two"), 10, archivepolicy.NewBudget(10), func(string, []byte) error {
		visited++
		return nil
	})
	if err == nil || visited != 1 {
		t.Fatalf("visited=%d err=%v", visited, err)
	}
}

func TestNativeArchiveRejectsEntryCountAndHighExpansion(t *testing.T) {
	var names []string
	for index := range 1001 {
		names = append(names, fmt.Sprintf("%04d.json", index))
	}

	if err := readNativeZip(nativeSecurityZIP(t, names...), 1000, func(string, []byte) error { return nil }); err == nil {
		t.Fatal("accepted excess central directory entries")
	}

	if err := readNativeZip(fuzzArchive(nativeZIPMember{name: "bomb.json", data: bytes.Repeat([]byte("x"), 9<<20)}), 1000, func(string, []byte) error { return nil }); err == nil {
		t.Fatal("accepted archive member beyond expanded limit")
	}
}

func TestNestedNativeProbeSharesOneExpandedBudget(t *testing.T) {
	first := fuzzArchive(nativeZIPMember{name: "recipe.json", data: bytes.Repeat([]byte("x"), 5<<20)})
	secondBody := bytes.Repeat([]byte("x"), 5<<20)
	copy(secondBody, `"steps"`)
	second := fuzzArchive(nativeZIPMember{name: "recipe.json", data: secondBody})

	outer := fuzzArchive(nativeZIPMember{name: "first.zip", data: first}, nativeZIPMember{name: "second.zip", data: second})
	if zipContainsNestedJSONKey(outer, `"steps"`) {
		t.Fatal("nested probe read beyond the shared expanded-byte budget")
	}
}
