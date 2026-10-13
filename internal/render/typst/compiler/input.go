package compiler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

type inputPart struct {
	data   []byte
	binary bool
}

type compilerInput struct {
	parts []inputPart
	size  uint64
}

// Binary assets are encoded directly into guest memory, without retaining a
// second complete base64 envelope on the Go heap.
func prepareInput(value envelope, maximum uint64) (compilerInput, error) {
	files, fonts := value.BinaryFiles, value.Fonts
	value.BinaryFiles, value.Fonts = nil, nil

	header, err := json.Marshal(value)
	if err != nil {
		return compilerInput{}, fmt.Errorf("encode compiler metadata: %w", err)
	}

	input := compilerInput{}
	input.add(header[:len(header)-1], false)

	if len(files) > 0 {
		input.add([]byte(`,"binary_files":{`), false)

		for index, name := range slices.Sorted(maps.Keys(files)) {
			if index > 0 {
				input.add([]byte(","), false)
			}

			key, err := json.Marshal(name)
			if err != nil {
				return compilerInput{}, fmt.Errorf("encode binary filename: %w", err)
			}

			input.add(key, false)
			input.add([]byte(":"), false)
			input.addBinary(files[name])
		}

		input.add([]byte("}"), false)
	}

	if len(fonts) > 0 {
		input.add([]byte(`,"fonts":[`), false)

		for index, font := range fonts {
			if index > 0 {
				input.add([]byte(","), false)
			}

			input.addBinary(font)
		}

		input.add([]byte("]"), false)
	}

	input.add([]byte("}"), false)

	if input.size > maximum || input.size > uint64(^uint32(0)) {
		return compilerInput{}, ErrMemoryLimit
	}

	return input, nil
}

func (input *compilerInput) add(data []byte, binary bool) {
	size := uint64(len(data))
	if binary {
		size = (size + 2) / 3 * 4
	}

	input.parts = append(input.parts, inputPart{data: data, binary: binary})
	input.size += size
}

func (input *compilerInput) addBinary(data []byte) {
	if data == nil {
		input.add([]byte("null"), false)
		return
	}

	input.add([]byte(`"`), false)
	input.add(data, true)
	input.add([]byte(`"`), false)
}

func (input *compilerInput) write(destination []byte) {
	offset := 0

	for _, part := range input.parts {
		if part.binary {
			length := base64.StdEncoding.EncodedLen(len(part.data))
			base64.StdEncoding.Encode(destination[offset:offset+length], part.data)
			offset += length
		} else {
			offset += copy(destination[offset:], part.data)
		}
	}
}
