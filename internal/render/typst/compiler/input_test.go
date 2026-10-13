package compiler

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCompilerInputPreservesJSONEnvelope(t *testing.T) {
	for _, data := range [][]byte{nil, {}, {0}, {0, 255}, {0, 128, 255}, {1, 2, 3, 4}, bytes.Repeat([]byte{0, 128, 255}, 1024)} {
		value := envelope{
			Main:        "#text(\"Unicode 日本語\")",
			Files:       map[string]string{"quoted\".typ": "<>&\n"},
			Data:        map[string]any{"number": 123, "text": "שלום\u2028\n"},
			BinaryFiles: map[string][]byte{"photo\"\n.jpg": data, "empty": {}},
			Fonts:       [][]byte{data, {}},
			PDFOptions:  &wirePDFOptions{Ident: "test"},
		}
		assertInputMatchesJSON(t, value)
	}

	assertInputMatchesJSON(t, envelope{Main: "no binary files", Data: nil})
}

func assertInputMatchesJSON(t *testing.T, value envelope) {
	t.Helper()

	input, err := prepareInput(value, 64<<20)
	if err != nil {
		t.Fatal(err)
	}

	actual := make([]byte, input.size)
	input.write(actual)

	expected, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatalf("invalid compiler input: %v", err)
	}

	if err := json.Unmarshal(expected, &want); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatal("compiler input differs from JSON envelope")
	}

	if _, err := prepareInput(value, input.size); err != nil {
		t.Fatalf("exact input boundary rejected: %v", err)
	}

	if _, err := prepareInput(value, input.size-1); err == nil {
		t.Fatal("oversized input accepted")
	}
}

func TestCompilerInputRejectsUnserializableData(t *testing.T) {
	if _, err := prepareInput(envelope{Data: make(chan int)}, 64<<20); err == nil {
		t.Fatal("unserializable input accepted")
	}
}

func BenchmarkCompilerInput(b *testing.B) {
	value := envelope{Main: "#image(\"photo.jpg\")", BinaryFiles: map[string][]byte{"photo.jpg": make([]byte, 24<<20)}}

	input, err := prepareInput(value, 64<<20)
	if err != nil {
		b.Fatal(err)
	}

	destination := make([]byte, input.size)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		input, err := prepareInput(value, 64<<20)
		if err != nil {
			b.Fatal(err)
		}

		input.write(destination)
	}
}
