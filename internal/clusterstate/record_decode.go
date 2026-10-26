package clusterstate

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
)

type recordCounter uint64

func (n *recordCounter) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return ErrCodec
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || strconv.FormatUint(value, 10) != text {
		return ErrCodec
	}
	*n = recordCounter(value)
	return nil
}

// Large retained views decode directly into their typed shape. A shadow type
// admits canonical decimal counters without allocating multiple generic JSON
// trees containing the same complete frame plan.
var cellRecordWire = recordWireType(reflect.TypeOf(Cell{}))
var resultRecordWire = recordWireType(reflect.TypeOf(Result{}))

func recordWireType(original reflect.Type) reflect.Type {
	switch original.Kind() {
	case reflect.Uint64:
		return reflect.TypeOf(recordCounter(0))
	case reflect.Pointer:
		elem := recordWireType(original.Elem())
		if elem != original.Elem() {
			return reflect.PointerTo(elem)
		}
	case reflect.Slice:
		elem := recordWireType(original.Elem())
		if elem != original.Elem() {
			return reflect.SliceOf(elem)
		}
	case reflect.Array:
		elem := recordWireType(original.Elem())
		if elem != original.Elem() {
			return reflect.ArrayOf(original.Len(), elem)
		}
	case reflect.Map:
		elem := recordWireType(original.Elem())
		if elem != original.Elem() {
			return reflect.MapOf(original.Key(), elem)
		}
	case reflect.Struct:
		fields := make([]reflect.StructField, original.NumField())
		changed := false
		for i := range fields {
			fields[i] = original.Field(i)
			next := recordWireType(fields[i].Type)
			changed = changed || next != fields[i].Type
			fields[i].Type = next
		}
		if changed {
			return reflect.StructOf(fields)
		}
	}
	return original
}
func copyRecordValue(dst, src reflect.Value) {
	if dst.Type() == src.Type() {
		dst.Set(src)
		return
	}
	switch dst.Kind() {
	case reflect.Uint64:
		dst.SetUint(src.Uint())
	case reflect.Pointer:
		if !src.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
			copyRecordValue(dst.Elem(), src.Elem())
		}
	case reflect.Struct:
		for i := 0; i < dst.NumField(); i++ {
			copyRecordValue(dst.Field(i), src.Field(i))
		}
	case reflect.Slice:
		if !src.IsNil() {
			dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
			for i := 0; i < src.Len(); i++ {
				copyRecordValue(dst.Index(i), src.Index(i))
			}
		}
	case reflect.Array:
		for i := 0; i < src.Len(); i++ {
			copyRecordValue(dst.Index(i), src.Index(i))
		}
	case reflect.Map:
		if !src.IsNil() {
			dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
			iter := src.MapRange()
			for iter.Next() {
				value := reflect.New(dst.Type().Elem()).Elem()
				copyRecordValue(value, iter.Value())
				dst.SetMapIndex(iter.Key(), value)
			}
		}
	}
}
func decodeRetainedRecord(raw []byte, destination reflect.Value) (bool, error) {
	var wire reflect.Type
	switch destination.Type().Elem() {
	case reflect.TypeOf(Cell{}):
		wire = cellRecordWire
	case reflect.TypeOf(Result{}):
		wire = resultRecordWire
	default:
		return false, nil
	}
	shadow := reflect.New(wire)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(shadow.Interface()) != nil {
		return true, ErrCodec
	}
	candidate := reflect.New(destination.Type().Elem())
	copyRecordValue(candidate.Elem(), shadow.Elem())
	comparison := canonicalComparison{want: raw}
	if encodeCanonicalValueTo(&comparison, candidate.Elem().Interface(), false) != nil || comparison.offset != len(raw) {
		return true, ErrCodec
	}
	destination.Elem().Set(candidate.Elem())
	return true, nil
}
