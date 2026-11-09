package store

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// THE COPY LAW. A document handed out is a deep copy, so every type a
// document reaches must be one the copier can see into: no struct but
// time.Time may carry unexported fields, because those are copied by
// assignment and would be shared.
func TestEveryDocumentTypeCanBeCopiedDeeply(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, path string) {
		if seen[rt] {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(rt.Elem(), path+"[]")
		case reflect.Map:
			walk(rt.Key(), path+"{key}")
			walk(rt.Elem(), path+"{}")
		case reflect.Chan, reflect.Func, reflect.UnsafePointer:
			t.Errorf("%s is a %s, which a document cannot carry", path, rt.Kind())
		case reflect.Struct:
			if rt == reflect.TypeOf(time.Time{}) {
				return
			}
			if hasUnexported(rt) && !plain(rt) {
				t.Errorf("%s (%s) has unexported fields the copier cannot reach", path, rt)
			}
			for i := range rt.NumField() {
				f := rt.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		}
	}
	walk(reflect.TypeOf(doc{}), "doc")

	// And a copy of a fully filled document equals it and shares nothing.
	var full doc
	fill(reflect.ValueOf(&full).Elem(), 0)
	cp := cloneDoc(full)
	if !reflect.DeepEqual(full, cp) {
		t.Fatal("a copy differs from its document")
	}
	shared(t, reflect.ValueOf(full), reflect.ValueOf(cp), "doc")
}

// fill sets every reachable field to something other than its zero value.
func fill(v reflect.Value, depth int) {
	if depth > 6 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("s")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		n := reflect.New(v.Type().Elem())
		fill(n.Elem(), depth+1)
		v.Set(n)
	case reflect.Slice:
		n := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			fill(n.Index(i), depth+1)
		}
		v.Set(n)
	case reflect.Map:
		n := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k, depth+1)
		fill(e, depth+1)
		n.SetMapIndex(k, e)
		v.Set(n)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(map[string]any{"k": []any{"v", 1.5}}))
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)))
			return
		}
		for i := range v.NumField() {
			fill(v.Field(i), depth+1)
		}
	}
}

// shared fails when a and b reach the same pointer, slice array or map.
func shared(t *testing.T, a, b reflect.Value, path string) {
	t.Helper()
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s is shared", path)
			return
		}
		shared(t, a.Elem(), b.Elem(), path)
	case reflect.Slice:
		if a.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s is shared", path)
			return
		}
		for i := range a.Len() {
			shared(t, a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i))
		}
	case reflect.Map:
		if a.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s is shared", path)
			return
		}
		for it := a.MapRange(); it.Next(); {
			shared(t, it.Value(), b.MapIndex(it.Key()), path+"{}")
		}
	case reflect.Interface:
		if !a.IsNil() {
			shared(t, a.Elem(), b.Elem(), path)
		}
	case reflect.Struct:
		if a.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := range a.NumField() {
			if a.Type().Field(i).IsExported() {
				shared(t, a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name)
			}
		}
	}
}
