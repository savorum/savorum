package strictjson

import (
	"errors"
	"fmt"
	"testing"
)

func TestLexicalKindsPreserveStructuredCauses(t *testing.T) {
	for _, example := range []struct{ source, kind string }{
		{`{"a":1,"a":2}`, Duplicate},
		{`{} []`, Trailing},
		{`{"a":NaN}`, Nonfinite},
		{`{"a":Infinity}`, Nonfinite},
		{`{"a":-Infinity}`, Nonfinite},
		{`{"a":--1}`, Malformed},
		{`{"a":`, Malformed},
		{string([]byte{0xff}), Malformed},
	} {
		t.Run(example.source, func(t *testing.T) {
			err := Validate([]byte(example.source))
			if Kind(fmt.Errorf("読み取り: %w", err)) != example.kind {
				t.Fatalf("kind=%q error=%v", Kind(err), err)
			}
		})
	}

	if err := Validate([]byte(`{"text":"NaN Infinity -Infinity duplicate JSON member"}`)); err != nil {
		t.Fatal(err)
	}

	for _, kind := range []string{Duplicate, Trailing, Nonfinite, Malformed} {
		if got := Kind(&Failure{kind: kind, cause: errors.New("文法エラー")}); got != kind {
			t.Fatalf("kind=%q", got)
		}
	}

	if Kind(errors.New("duplicate JSON member")) != "" {
		t.Fatal("classified untyped prose")
	}
}
