package plandb

import (
	"reflect"
	"testing"
	"time"
)

func TestCloneTaskCopiesEveryMutableField(t *testing.T) {
	var source Task
	planTaskFields(t, reflect.ValueOf(&source).Elem(), false)
	cloned := cloneTask(&source)
	if !reflect.DeepEqual(cloned, &source) {
		t.Fatal("clone changed task values")
	}
	planAssertTaskCopy(t, reflect.ValueOf(&source), reflect.ValueOf(cloned), "Task")
	planTaskFields(t, reflect.ValueOf(cloned).Elem(), true)
	var want Task
	planTaskFields(t, reflect.ValueOf(&want).Elem(), false)
	if !reflect.DeepEqual(source, want) {
		t.Fatal("mutating clone changed source task")
	}
}

func TestTaskReadIsolatesEveryNestedField(t *testing.T) {
	store := planOpen(t, "")
	t.Cleanup(func() { _ = store.Close() })
	planAdd(t, store, planSpec("upstream", "Upstream"), TaskSpec{
		ID: "leaf", Title: "Leaf",
		Dependencies:  []Dependency{{TaskID: "upstream", Kind: DepSuggests}},
		Capabilities:  []string{"read", "write", "verify"},
		Resources:     []ResourceClaim{{URI: "repo/file", Mode: "read"}},
		ContextInputs: []string{"context"}, Deliverables: []string{"result"},
		EvidenceRequirements: []string{"proof"}, Checks: []string{"go test ./internal/plandb"},
	})
	if _, err := store.Claim("leaf", "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Done("leaf", "worker", "done", []string{"artifact"}, []string{"evidence"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetVerdictBasis("leaf", VerdictBasis{
		Kind: "run", Runs: []VerdictRun{{Command: "go test ./internal/plandb", ExitCode: 0}},
		Unobserved: []string{"go vet ./internal/plandb"},
	}); err != nil {
		t.Fatal(err)
	}
	// A second handle supplies an independent expectation, without sharing a clone's arrays.
	reopened := planReopen(t, store.Path())
	t.Cleanup(func() { _ = reopened.Close() })
	want := reopened.Task("leaf")
	got := store.Task("leaf")
	if got == nil || !reflect.DeepEqual(got, want) {
		t.Fatal("initial task read differs from persisted task")
	}
	planTaskFields(t, reflect.ValueOf(got).Elem(), true)
	for range 3 {
		if next := store.Task("leaf"); !reflect.DeepEqual(next, want) {
			t.Fatalf("caller mutation changed unchanged task read:\n got: %#v\nwant: %#v", next, want)
		}
	}
	got = store.Task("leaf")
	mutated := make(chan struct{})
	go func() {
		defer close(mutated)
		for range 50 {
			planTaskFields(t, reflect.ValueOf(got).Elem(), true)
		}
	}()
	for range 50 {
		if next := store.Task("leaf"); !reflect.DeepEqual(next, want) {
			t.Error("concurrent caller mutation changed unchanged task read")
			break
		}
	}
	<-mutated
}

// Reflection populates new fields automatically, so an omitted clone cannot hide behind a nil fixture.
// time.Time is an immutable value; its unexported location pointer is safe to share.
func planTaskFields(t *testing.T, value reflect.Value, mutate bool) {
	t.Helper()
	if value.Type() == reflect.TypeFor[time.Time]() {
		moment := time.Unix(1, 0).UTC()
		if mutate {
			moment = moment.Add(time.Second)
		}
		value.Set(reflect.ValueOf(moment))
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			planTaskFields(t, value.Field(i), mutate)
		}
	case reflect.Slice:
		if !mutate {
			value.Set(reflect.MakeSlice(value.Type(), 3, 4))
		}
		for i := 0; i < value.Len(); i++ {
			planTaskFields(t, value.Index(i), mutate)
		}
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			planTaskFields(t, value.Index(i), mutate)
		}
	case reflect.Map:
		if !mutate {
			value.Set(reflect.MakeMap(value.Type()))
			key := reflect.New(value.Type().Key()).Elem()
			planTaskFields(t, key, false)
			entry := reflect.New(value.Type().Elem()).Elem()
			planTaskFields(t, entry, false)
			value.SetMapIndex(key, entry)
		} else {
			for _, key := range value.MapKeys() {
				entry := reflect.New(value.Type().Elem()).Elem()
				entry.Set(value.MapIndex(key))
				planTaskFields(t, entry, true)
				value.SetMapIndex(key, entry)
			}
		}
	case reflect.Pointer:
		if !mutate {
			value.Set(reflect.New(value.Type().Elem()))
		}
		if !value.IsNil() {
			planTaskFields(t, value.Elem(), mutate)
		}
	case reflect.String:
		if mutate {
			value.SetString(value.String() + " changed")
		} else {
			value.SetString("original")
		}
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(value.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value.SetUint(value.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(value.Float() + 1)
	default:
		t.Fatalf("add task field coverage for %s", value.Type())
	}
}

func planAssertTaskCopy(t *testing.T, source, cloned reflect.Value, path string) {
	t.Helper()
	if source.Type() == reflect.TypeFor[time.Time]() {
		return
	}
	switch source.Kind() {
	case reflect.Struct:
		for i := 0; i < source.NumField(); i++ {
			planAssertTaskCopy(t, source.Field(i), cloned.Field(i), path+"."+source.Type().Field(i).Name)
		}
	case reflect.Slice:
		if cloned.Cap() != cloned.Len() {
			t.Errorf("%s: clone capacity = %d, want length %d", path, cloned.Cap(), cloned.Len())
		}
		if source.Pointer() == cloned.Pointer() {
			t.Errorf("%s: clone shares slice backing array", path)
		}
		for i := 0; i < source.Len(); i++ {
			planAssertTaskCopy(t, source.Index(i), cloned.Index(i), path+"[]")
		}
	case reflect.Array:
		for i := 0; i < source.Len(); i++ {
			planAssertTaskCopy(t, source.Index(i), cloned.Index(i), path+"[]")
		}
	case reflect.Pointer, reflect.Map:
		if source.UnsafePointer() == cloned.UnsafePointer() {
			t.Errorf("%s: clone shares %s", path, source.Kind())
		}
		if source.Kind() == reflect.Pointer {
			planAssertTaskCopy(t, source.Elem(), cloned.Elem(), path+"*")
		} else {
			for _, key := range source.MapKeys() {
				// Mutable map keys need explicit coverage rather than silently sharing their state.
				planAssertTaskCopy(t, key, key, path+"[key]")
				planAssertTaskCopy(t, source.MapIndex(key), cloned.MapIndex(key), path+"[value]")
			}
		}
	}
}
