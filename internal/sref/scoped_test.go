package sref_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/srefcheckout"
)

// TestScopedExtensionsSurviveAReadAndWriteCycle is the SREF round-trip
// capability: every unknown extension has to come back on the object it was
// found on, not merely somewhere in the document.
func TestScopedExtensionsSurviveAReadAndWriteCycle(t *testing.T) {
	t.Parallel()
	roundTripDirectory := srefcheckout.Path(t, "tests/roundtrip")

	entries, err := os.ReadDir(roundTripDirectory)
	if err != nil {
		t.Fatal(err)
	}

	extensionsFound := 0

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		original, err := readSREFFixture(filepath.Join(roundTripDirectory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}

		document, err := sref.Decode(original)
		if err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}

		written, err := sref.Encode(document)
		if err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}

		before := extensionsByPath(t, original)
		extensionsFound += len(before)

		after := extensionsByPath(t, written)
		for path, value := range before {
			got, kept := after[path]
			if !kept {
				t.Errorf("%s: extension at %s was lost", entry.Name(), path)
				continue
			}

			if !reflect.DeepEqual(got, value) {
				t.Errorf("%s: extension at %s changed:\n have %v\n want %v", entry.Name(), path, got, value)
			}
		}
	}

	if extensionsFound == 0 {
		t.Error("the round-trip fixture family carries no extensions, so it proves nothing")
	}
}

// extensionsByPath finds every extension member in a document together with the
// structural path it sits at, which is what "at the same object scope" means.
func extensionsByPath(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	found := map[string]any{}

	var walk func(value any, path string)

	walk = func(value any, path string) {
		switch holder := value.(type) {
		case map[string]any:
			// Savorum's own carrier is where lifted extensions rest between a
			// read and a write; it is not itself part of the comparison.
			identity, _ := holder["id"].(string)

			here := path
			if identity != "" {
				here = path + "#" + identity
			}

			for name, child := range holder {
				if name == sref.ScopedExtensionsMember {
					continue
				}

				if len(name) > 2 && name[0] == 'x' && name[1] == '-' {
					found[here+"/"+name] = child
					continue
				}

				walk(child, here+"/"+name)
			}
		case []any:
			for index, item := range holder {
				// Identified objects are compared by identity rather than by
				// position, because order is not what "same scope" means.
				if object, ok := item.(map[string]any); ok {
					if _, named := object["id"].(string); named {
						walk(item, path)
						continue
					}
				}

				walk(item, path+"/"+itoa(index))
			}
		}
	}
	walk(document, "")

	return found
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}

	return digits
}
