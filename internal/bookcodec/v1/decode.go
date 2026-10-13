package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Decode parses a version 1 book document.
func Decode(data []byte) (Document, error) {
	var document Document

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode schema v1 book document: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode schema v1 book document: trailing JSON value")
		}

		return Document{}, fmt.Errorf("decode schema v1 book document trailer: %w", err)
	}

	return document, nil
}
