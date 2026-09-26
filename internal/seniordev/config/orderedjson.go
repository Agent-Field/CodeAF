//go:build !windows

package config

import (
	"encoding/json"
	"errors"
)

// orderedJSONField is one member of an object decoded with its source order
// intact. Config keeps source order for the permission and tools blocks,
// where rule order is meaningful.
type orderedJSONField struct {
	key   string
	value any
}

type orderedJSONObject []orderedJSONField

// decodeOrderedJSON decodes the next value from decoder, keeping object
// members in source order. Numbers arrive as json.Number.
func decodeOrderedJSON(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := orderedJSONObject{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return nil, keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("JSON object key is not a string")
			}
			value, valueErr := decodeOrderedJSON(decoder)
			if valueErr != nil {
				return nil, valueErr
			}
			object = append(object, orderedJSONField{key: key, value: value})
		}
		_, err = decoder.Token()
		return object, err
	case '[':
		array := []any{}
		for decoder.More() {
			value, valueErr := decodeOrderedJSON(decoder)
			if valueErr != nil {
				return nil, valueErr
			}
			array = append(array, value)
		}
		_, err = decoder.Token()
		return array, err
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}
