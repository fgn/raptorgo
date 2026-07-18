// Package strictjson detects duplicate JSON object names before typed decoding.
// encoding/json otherwise accepts duplicates with last-value-wins semantics.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Check requires exactly one syntactically valid JSON value and rejects a
// duplicate object name at any nesting depth. Escaped equivalent names compare
// after JSON unescaping, so "v" and "\u0076" are duplicates.
func Check(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := walkValue(decoder); err != nil {
		return fmt.Errorf("strictjson: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("strictjson: trailing data: %w", err)
	}
	return nil
}

func walkValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode value: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode object name: %w", err)
			}
			name, ok := token.(string)
			if !ok {
				return fmt.Errorf("object name has type %T", token)
			}
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("duplicate object name %q", name)
			}
			seen[name] = struct{}{}
			if err := walkValue(decoder); err != nil {
				return err
			}
		}
		return closeDelimiter(decoder, '}')
	case '[':
		for decoder.More() {
			if err := walkValue(decoder); err != nil {
				return err
			}
		}
		return closeDelimiter(decoder, ']')
	default:
		return fmt.Errorf("unexpected opening delimiter %q", delimiter)
	}
}

func closeDelimiter(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode closing delimiter: %w", err)
	}
	if token != want {
		return fmt.Errorf("closing delimiter = %v, want %q", token, want)
	}
	return nil
}
