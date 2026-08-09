// Ordered JSON support for projector row values. The source projectors rely on
// JSON.parse/object-rest/JSON.stringify insertion order, so map[string]any is
// not a parity-safe representation.
package projectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type jsonKind uint8

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

type jsonObjectValue struct {
	keys []string
	vals map[string]jsonValue
}

type jsonValue struct {
	kind jsonKind
	b    bool
	n    float64
	s    string
	a    []jsonValue
	o    *jsonObjectValue
}

func nullJSON() jsonValue               { return jsonValue{kind: jsonNull} }
func boolJSON(v bool) jsonValue         { return jsonValue{kind: jsonBool, b: v} }
func numberJSON(v float64) jsonValue    { return jsonValue{kind: jsonNumber, n: v} }
func stringJSON(v string) jsonValue     { return jsonValue{kind: jsonString, s: v} }
func arrayJSON(v []jsonValue) jsonValue { return jsonValue{kind: jsonArray, a: v} }
func objectJSON(v *jsonObjectValue) jsonValue {
	return jsonValue{kind: jsonObject, o: v}
}

func newJSONObject() *jsonObjectValue {
	return &jsonObjectValue{vals: map[string]jsonValue{}}
}

func (o *jsonObjectValue) get(key string) (jsonValue, bool) {
	if o == nil {
		return jsonValue{}, false
	}
	value, ok := o.vals[key]
	return value, ok
}

func (o *jsonObjectValue) set(key string, value jsonValue) {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

func (o *jsonObjectValue) delete(key string) {
	if _, exists := o.vals[key]; !exists {
		return
	}
	delete(o.vals, key)
	for index, current := range o.keys {
		if current == key {
			o.keys = append(o.keys[:index], o.keys[index+1:]...)
			return
		}
	}
}

func (o *jsonObjectValue) clone() *jsonObjectValue {
	out := newJSONObject()
	for _, key := range o.keys {
		out.set(key, o.vals[key].clone())
	}
	return out
}

func (v jsonValue) clone() jsonValue {
	switch v.kind {
	case jsonArray:
		out := make([]jsonValue, len(v.a))
		for index, item := range v.a {
			out[index] = item.clone()
		}
		v.a = out
	case jsonObject:
		v.o = v.o.clone()
	}
	return v
}

func parseOrderedJSON(data []byte) (jsonValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := parseJSONToken(decoder)
	if err != nil {
		return jsonValue{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return jsonValue{}, errors.New("projectors: trailing JSON value")
		}
		return jsonValue{}, err
	}
	return value, nil
}

func parseJSONToken(decoder *json.Decoder) (jsonValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return jsonValue{}, err
	}
	switch value := token.(type) {
	case nil:
		return nullJSON(), nil
	case bool:
		return boolJSON(value), nil
	case string:
		return stringJSON(value), nil
	case json.Number:
		number, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return jsonValue{}, err
		}
		return numberJSON(number), nil
	case json.Delim:
		switch value {
		case '[':
			items := []jsonValue{}
			for decoder.More() {
				item, err := parseJSONToken(decoder)
				if err != nil {
					return jsonValue{}, err
				}
				items = append(items, item)
			}
			if _, err := decoder.Token(); err != nil {
				return jsonValue{}, err
			}
			return arrayJSON(items), nil
		case '{':
			object := newJSONObject()
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return jsonValue{}, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return jsonValue{}, errors.New("projectors: malformed object key")
				}
				item, err := parseJSONToken(decoder)
				if err != nil {
					return jsonValue{}, err
				}
				object.set(key, item)
			}
			if _, err := decoder.Token(); err != nil {
				return jsonValue{}, err
			}
			return objectJSON(reorderJSONObject(object)), nil
		}
	}
	return jsonValue{}, errors.New("projectors: malformed JSON value")
}

func reorderJSONObject(object *jsonObjectValue) *jsonObjectValue {
	indices := []string{}
	rest := []string{}
	for _, key := range object.keys {
		if _, ok := arrayIndex(key); ok {
			indices = append(indices, key)
		} else {
			rest = append(rest, key)
		}
	}
	if len(indices) == 0 {
		return object
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, _ := arrayIndex(indices[i])
		right, _ := arrayIndex(indices[j])
		return left < right
	})
	out := newJSONObject()
	for _, key := range append(indices, rest...) {
		out.set(key, object.vals[key])
	}
	return out
}

func arrayIndex(key string) (uint32, bool) {
	if key == "0" {
		return 0, true
	}
	if key == "" || len(key) > 10 || key[0] == '0' {
		return 0, false
	}
	for index := range len(key) {
		if key[index] < '0' || key[index] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(key, 10, 32)
	if err != nil || value == 4294967295 {
		return 0, false
	}
	return uint32(value), true
}

func (v jsonValue) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	if err := writeOrderedJSON(&out, v); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeOrderedJSON(out *bytes.Buffer, value jsonValue) error {
	switch value.kind {
	case jsonNull:
		out.WriteString("null")
	case jsonBool:
		if value.b {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case jsonNumber:
		number, err := jscompat.JSNumber(value.n).MarshalJSON()
		if err != nil {
			return err
		}
		out.Write(number)
	case jsonString:
		encoded, err := jscompat.Stringify(value.s)
		if err != nil {
			return err
		}
		out.Write(encoded)
	case jsonArray:
		out.WriteByte('[')
		for index, item := range value.a {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := writeOrderedJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case jsonObject:
		out.WriteByte('{')
		for index, key := range value.o.keys {
			if index > 0 {
				out.WriteByte(',')
			}
			encoded, err := jscompat.Stringify(key)
			if err != nil {
				return err
			}
			out.Write(encoded)
			out.WriteByte(':')
			if err := writeOrderedJSON(out, value.o.vals[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return errors.New("projectors: invalid JSON kind")
	}
	return nil
}

func (v jsonValue) compactString() (string, error) {
	encoded, err := v.MarshalJSON()
	return string(encoded), err
}

func objectField(value jsonValue, key string) (jsonValue, bool) {
	if value.kind != jsonObject {
		return jsonValue{}, false
	}
	return value.o.get(key)
}

func nestedField(value jsonValue, keys ...string) (jsonValue, bool) {
	current := value
	for _, key := range keys {
		var ok bool
		current, ok = objectField(current, key)
		if !ok {
			return jsonValue{}, false
		}
	}
	return current, true
}

func stringField(value jsonValue, key string) (string, bool) {
	field, ok := objectField(value, key)
	return field.s, ok && field.kind == jsonString
}

func sqlValue(value jsonValue, asJSON bool) (any, error) {
	if value.kind == jsonNull {
		return nil, nil
	}
	if asJSON {
		return value.compactString()
	}
	switch value.kind {
	case jsonBool:
		return value.b, nil
	case jsonNumber:
		return value.n, nil
	case jsonString:
		return value.s, nil
	default:
		return value.compactString()
	}
}

// PartialRow is the insertion-ordered row returned by ToPartialRow.
type PartialRow struct {
	value jsonValue
}

func (r PartialRow) MarshalJSON() ([]byte, error) {
	return r.value.MarshalJSON()
}

func (r PartialRow) object() *jsonObjectValue {
	return r.value.o
}
