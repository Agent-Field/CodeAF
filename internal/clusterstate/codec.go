package clusterstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrSchema = errors.New("clusterstate: unsupported schema")
	ErrCodec  = errors.New("clusterstate: invalid canonical encoding")
)

// Encode is the v2 canonical JSON boundary, separate from legacy json.Marshal.
// All uint64 fields are plain decimal strings, even below 2^53; other integers
// must fit JavaScript exactly. Maps sort by UTF-8 key bytes, sealed strings stay
// opaque, and byte slices use Go's padded standard base64. HTML and U+2028/2029
// escaping follows encoding/json. Hosts must pass these bytes without rewriting.
func Encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, ErrCodec
	}
	if !validStrings(reflect.ValueOf(v)) {
		return nil, ErrCodec
	}
	var tree any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = d.Decode(&tree); err != nil {
		return nil, ErrCodec
	}
	tree, err = integers(tree, reflect.TypeOf(v), true)
	if err != nil {
		return nil, err
	}
	return json.Marshal(tree)
}

// JSON's replacement of invalid UTF-8 would alter an opaque field. Reject it
// before it can become a different canonical request or persisted record.
func validStrings(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || validStrings(v.Elem())
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && !validStrings(v.Field(i)) {
				return false
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if !validStrings(iter.Key()) || !validStrings(iter.Value()) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			for i := 0; i < v.Len(); i++ {
				if !validStrings(v.Index(i)) {
					return false
				}
			}
		}
	}
	return true
}

// Decode accepts only canonical bytes into a typed destination. Re-encoding
// rejects duplicate/unknown/missing fields, alternate integer spellings, lossy
// Unicode, and trailing data. It updates the destination only after validation.
func Decode(b []byte, into any) error {
	v := reflect.ValueOf(into)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ErrCodec
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var tree any
	if err := d.Decode(&tree); err != nil {
		return ErrCodec
	}
	tree, err := integers(tree, v.Type().Elem(), false)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(tree)
	if err != nil {
		return ErrCodec
	}
	candidate := reflect.New(v.Type().Elem())
	d = json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if err = d.Decode(candidate.Interface()); err != nil {
		return ErrCodec
	}
	canonical, err := Encode(candidate.Elem().Interface())
	if err != nil || !bytes.Equal(b, canonical) {
		return ErrCodec
	}
	v.Elem().Set(candidate.Elem())
	return nil
}

// integers uses the declared Go shape rather than field names: a sealed string
// named "fence" in a wrapped-key map must never be parsed as a counter. Floats,
// interfaces, custom marshalers and non-string map keys are outside the schema.
func integers(tree any, typ reflect.Type, encode bool) (any, error) {
	if typ == nil {
		return nil, ErrCodec
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if tree == nil {
		return nil, nil
	}
	switch typ.Kind() {
	case reflect.Uint64:
		if encode {
			n, ok := tree.(json.Number)
			if !ok {
				return nil, ErrCodec
			}
			return string(n), nil
		}
		s, ok := tree.(string)
		if !ok {
			return nil, ErrCodec
		}
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != s {
			return nil, ErrCodec
		}
		return json.Number(s), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		n, ok := tree.(json.Number)
		if !ok {
			return nil, ErrCodec
		}
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil || i < -int64(MaxSafeInteger) || i > int64(MaxSafeInteger) {
			return nil, ErrCodec
		}
	case reflect.Struct:
		m, ok := tree.(map[string]any)
		if !ok {
			return nil, ErrCodec
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			if value, exists := m[name]; exists {
				next, err := integers(value, f.Type, encode)
				if err != nil {
					return nil, err
				}
				m[name] = next
			}
		}
	case reflect.Map:
		m, ok := tree.(map[string]any)
		if !ok || typ.Key().Kind() != reflect.String {
			return nil, ErrCodec
		}
		for k, value := range m {
			next, err := integers(value, typ.Elem(), encode)
			if err != nil {
				return nil, err
			}
			m[k] = next
		}
	case reflect.Slice, reflect.Array:
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
			if _, ok := tree.(string); !ok {
				return nil, ErrCodec
			}
			return tree, nil
		}
		list, ok := tree.([]any)
		if !ok {
			return nil, ErrCodec
		}
		for i, value := range list {
			next, err := integers(value, typ.Elem(), encode)
			if err != nil {
				return nil, err
			}
			list[i] = next
		}
	case reflect.String:
		if _, ok := tree.(string); !ok {
			return nil, ErrCodec
		}
	case reflect.Bool:
		if _, ok := tree.(bool); !ok {
			return nil, ErrCodec
		}
	default:
		return nil, ErrCodec
	}
	return tree, nil
}

// EncodeCommand and DecodeCommand additionally check the versioned union. No
// unsupported operation can decode as an empty successful command.
func EncodeCommand(c Command) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return Encode(c)
}

func DecodeCommand(b []byte) (Command, error) {
	var c Command
	if err := Decode(b, &c); err != nil {
		return Command{}, err
	}
	if err := c.Validate(); err != nil {
		return Command{}, err
	}
	return c, nil
}

func (c Command) Validate() error {
	if c.Schema != SchemaVersion {
		return ErrSchema
	}
	if c.Cluster == "" || c.Actor == "" || c.Session == "" || c.Sequence == 0 || c.RequestDigest == "" {
		return ErrCodec
	}
	return oneOf(c.Change)
}

// oneOf checks closed unions without a second hand-maintained operation table.
func oneOf(v any) error {
	value := reflect.ValueOf(v)
	n := 0
	for i := 0; i < value.NumField(); i++ {
		if !value.Field(i).IsNil() {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("%w: expected one record or operation", ErrCodec)
	}
	return nil
}

func EncodeRow(r Row) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return Encode(r)
}

func DecodeRow(b []byte) (Row, error) {
	var r Row
	if err := Decode(b, &r); err != nil {
		return Row{}, err
	}
	if err := r.Validate(); err != nil {
		return Row{}, err
	}
	return r, nil
}

func (r Row) Validate() error {
	if r.Schema != SchemaVersion {
		return ErrSchema
	}
	if r.Key == "" {
		return ErrCodec
	}
	n := 0
	for _, present := range []bool{r.Identity != nil, r.Device != nil, r.Cell != nil} {
		if present {
			n++
		}
	}
	if n != 1 {
		return ErrCodec
	}
	return nil
}

func EncodeResult(r Result) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return Encode(r)
}

func DecodeResult(b []byte) (Result, error) {
	var r Result
	if err := Decode(b, &r); err != nil {
		return Result{}, err
	}
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	return r, nil
}

func (r Result) Validate() error {
	if r.Schema != SchemaVersion {
		return ErrSchema
	}
	if !r.Code.valid() {
		return ErrCodec
	}
	for _, item := range r.Items {
		if !item.Code.valid() {
			return ErrCodec
		}
	}
	return nil
}

func (c Code) valid() bool {
	switch c {
	case OK, NotFound, Exists, LeaseHeld, FenceStale, HeadMoved, CAS, Revoked,
		SelfRevoke, Unauthorized, Rotated, Conflict, Capacity, UpdateRequired, RotationStep, BadGrace, RequestGone, AlreadyDecided, BadRequest:
		return true
	}
	return false
}
