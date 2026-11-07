// Package strictjson enforces the lexical JSON rules shared by SREF readers.
package strictjson

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxJSONDepth = 128

// Validate rejects byte sequences that ordinary JSON decoding would silently
// normalize, including duplicate object members and trailing JSON values.
func Validate(data []byte) (err error) {
	defer func() {
		if err != nil && Kind(err) == "" {
			err = &Failure{kind: Malformed, cause: err}
		}
	}()

	// jsontext rejects duplicate names, invalid UTF-8 and a byte-order mark by default.
	decoder := jsontext.NewDecoder(bytes.NewReader(data))

	for {
		_, err := decoder.ReadToken()
		if err != nil {
			return classify(data, err)
		}

		if decoder.StackDepth() > maxJSONDepth {
			return fmt.Errorf("JSON nesting exceeds depth limit %d", maxJSONDepth)
		}

		if decoder.StackDepth() == 0 {
			break
		}
	}

	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			return &Failure{kind: Trailing, cause: errors.New("trailing JSON content")}
		}

		return fmt.Errorf("read content after JSON value: %w", classify(data, err))
	}

	return nil
}

func classify(data []byte, err error) error {
	syntax, ok := errors.AsType[*jsontext.SyntacticError](err)
	if !ok {
		return err
	}

	if errors.Is(syntax.Err, jsontext.ErrDuplicateName) {
		return &Failure{kind: Duplicate, cause: fmt.Errorf("duplicate JSON member %q", syntax.JSONPointer.LastToken())}
	}

	// Decoder errors may point one byte past the rejected token.
	for _, offset := range []int64{syntax.ByteOffset, syntax.ByteOffset - 1} {
		if offset >= 0 && offset < int64(len(data)) && nonfiniteToken(data, int(offset)) {
			return &Failure{kind: Nonfinite, cause: errors.New("nonfinite JSON number")}
		}
	}

	return err
}

// Decode validates the original bytes before decoding them with unknown-field
// rejection. Callers deliberately control the diagnostic exposed at their
// trust boundary.
func Decode(data []byte, target any) error {
	if err := Validate(data); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode validated JSON: %w", err)
	}

	return nil
}

func nonfiniteToken(data []byte, index int) bool {
	if index > 0 && !strings.ContainsRune("[{: ,\t\r\n", rune(data[index-1])) {
		return false
	}

	for _, token := range []string{"NaN", "Infinity", "-Infinity"} {
		rest := data[index:]
		if bytes.HasPrefix(rest, []byte(token)) && (len(rest) == len(token) || strings.ContainsRune("]}, \t\r\n", rune(rest[len(token)]))) {
			return true
		}
	}

	return false
}
