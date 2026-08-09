// Ordered JSON value support for the ports of src/storage/storage.ts:258-260
// and src/storage/json-migration.ts:77-109 (swe-pro 3b25a1a). JSON.parse
// preserves JavaScript object enumeration order and JSON.stringify observes
// it, so storage migrations cannot safely round-trip through a Go map.
package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Object is a JavaScript plain-object image. Existing keys retain their first
// position when overwritten; integer-index keys are enumerated first.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject constructs an empty ordered object.
func NewObject() *Object {
	return &Object{vals: make(map[string]any)}
}

// Set mirrors JavaScript property assignment.
func (o *Object) Set(key string, value any) {
	if o.vals == nil {
		o.vals = make(map[string]any)
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

// Get returns a property without coercion.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// Delete mirrors JavaScript delete. Re-adding the key appends it at the end.
func (o *Object) Delete(key string) {
	if o == nil {
		return
	}
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Keys returns Object.keys order.
func (o *Object) Keys() []string {
	if o == nil {
		return []string{}
	}
	out := make([]string, len(o.keys))
	copy(out, o.keys)
	return out
}

// MarshalJSON emits compact JSON.stringify-compatible JSON.
func (o *Object) MarshalJSON() ([]byte, error) {
	return marshalValue(o, "", 0)
}

func parseJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("storage: trailing JSON value")
		}
		return nil, err
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObject()
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("storage: malformed object key")
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(key, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return reorderObject(o), nil
		case '[':
			out := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		default:
			return nil, errors.New("storage: malformed JSON delimiter")
		}
	case json.Number:
		n, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			var numErr *strconv.NumError
			if !errors.As(err, &numErr) || !errors.Is(numErr.Err, strconv.ErrRange) {
				return nil, err
			}
		}
		return n, nil
	case string, bool, nil:
		return t, nil
	default:
		return nil, errors.New("storage: malformed JSON token")
	}
}

func reorderObject(o *Object) *Object {
	var indices []string
	var rest []string
	for _, key := range o.keys {
		if _, ok := arrayIndex(key); ok {
			indices = append(indices, key)
		} else {
			rest = append(rest, key)
		}
	}
	if len(indices) == 0 {
		return o
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, _ := arrayIndex(indices[i])
		b, _ := arrayIndex(indices[j])
		return a < b
	})
	out := NewObject()
	for _, key := range append(indices, rest...) {
		v, _ := o.Get(key)
		out.Set(key, v)
	}
	return out
}

func arrayIndex(s string) (uint32, bool) {
	if s == "" || len(s) > 10 {
		return 0, false
	}
	if s == "0" {
		return 0, true
	}
	if s[0] == '0' {
		return 0, false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n >= 4294967295 {
		return 0, false
	}
	return uint32(n), true
}

func marshalValue(v any, indent string, depth int) ([]byte, error) {
	switch value := v.(type) {
	case *Object:
		var b bytes.Buffer
		b.WriteByte('{')
		for i, key := range value.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteByte('\n')
				b.WriteString(strings.Repeat(indent, depth+1))
			}
			keyJSON, _ := jscompat.Stringify(key)
			b.Write(keyJSON)
			if indent == "" {
				b.WriteByte(':')
			} else {
				b.WriteString(": ")
			}
			child, err := marshalValue(value.vals[key], indent, depth+1)
			if err != nil {
				return nil, err
			}
			b.Write(child)
		}
		if indent != "" && len(value.keys) > 0 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, depth))
		}
		b.WriteByte('}')
		return b.Bytes(), nil
	case []any:
		var b bytes.Buffer
		b.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteByte('\n')
				b.WriteString(strings.Repeat(indent, depth+1))
			}
			child, err := marshalValue(item, indent, depth+1)
			if err != nil {
				return nil, err
			}
			b.Write(child)
		}
		if indent != "" && len(value) > 0 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, depth))
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	case float64:
		return jscompat.JSNumber(value).MarshalJSON()
	default:
		if indent == "" {
			return jscompat.Stringify(value)
		}
		return jscompat.StringifyIndent(value)
	}
}

func stringifyIndent(v any) ([]byte, error) {
	return marshalValue(v, "  ", 0)
}

func objectString(o *Object, key string) (string, bool) {
	v, ok := o.Get(key)
	s, valid := v.(string)
	return s, ok && valid
}

func objectObject(o *Object, key string) (*Object, bool) {
	v, ok := o.Get(key)
	obj, valid := v.(*Object)
	return obj, ok && valid
}

func objectArray(o *Object, key string) ([]any, bool) {
	v, ok := o.Get(key)
	arr, valid := v.([]any)
	return arr, ok && valid
}
