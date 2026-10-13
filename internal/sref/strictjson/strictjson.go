// Package strictjson enforces the lexical JSON rules shared by SREF readers.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
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

	if !utf8.Valid(data) {
		return errors.New("JSON is not valid UTF-8")
	}

	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return errors.New("JSON begins with a byte-order mark")
	}

	if err := checkDepth(data); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if err := validateValue(decoder); err != nil {
		return fmt.Errorf("read trailing JSON token: %w", err)
	}

	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return &Failure{kind: Trailing, cause: errors.New("trailing JSON content")}
		}

		return fmt.Errorf("read content after JSON value: %w", err)
	}

	return nil
}

func checkDepth(data []byte) error {
	depth := 0
	inString := false
	escaped := false

	for index, c := range data {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}

			continue
		}

		if (c == 'N' || c == 'I' || c == '-') && nonfiniteToken(data, index) {
			return &Failure{kind: Nonfinite, cause: errors.New("nonfinite JSON number")}
		}

		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxJSONDepth {
				return fmt.Errorf("JSON nesting exceeds depth limit %d", maxJSONDepth)
			}
		case '}', ']':
			depth--
		}
	}

	return nil
}

func validateValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read JSON token: %w", err)
	}

	delimiter, nested := token.(json.Delim)
	if !nested {
		return nil
	}

	switch delimiter {
	case '{':
		return validateObject(decoder)
	case '[':
		return validateArray(decoder)
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func validateObject(decoder *json.Decoder) error {
	seen := map[string]struct{}{}

	for decoder.More() {
		memberToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("read JSON object member: %w", err)
		}

		member, ok := memberToken.(string)
		if !ok {
			return errors.New("object member name is not a string")
		}

		if _, repeated := seen[member]; repeated {
			return &Failure{kind: Duplicate, cause: fmt.Errorf("duplicate JSON member %q", member)}
		}

		seen[member] = struct{}{}

		if err := validateValue(decoder); err != nil {
			return err
		}
	}

	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("read JSON object terminator: %w", err)
	}

	return nil
}

func validateArray(decoder *json.Decoder) error {
	for decoder.More() {
		if err := validateValue(decoder); err != nil {
			return err
		}
	}

	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("read JSON array terminator: %w", err)
	}

	return nil
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
