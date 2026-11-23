package store

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AN UNCHANGED FLOOR IS LISTED WITHOUT DECODING ANYTHING. The fifth live A+B
// run's idle engine read 1.7 GB of job documents in six minutes, because the
// runner's capacity read, the server's settle and the lease desk each decoded
// every document on every look. A listing now decodes only the documents whose
// file changed since the last one, whichever store on the root reads them.
func TestListingAnUnchangedFloorDecodesNothing(t *testing.T) {
	st := open(t)
	var ids []int
	for i := range 40 {
		it, err := st.Create(factory.Item{Title: "backlog", Num: i + 1, Kind: factory.KindIssue})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	if _, err := st.ListSaved(); err != nil {
		t.Fatal(err)
	}
	// Another door's store on the same root shares what was read.
	other, err := Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	before := documentDecodes.Load()
	for range 5 {
		if items, err := other.ListSaved(); err != nil || len(items) != 40 {
			t.Fatalf("ListSaved = %d items, %v", len(items), err)
		}
		if _, err := st.List(); err != nil {
			t.Fatal(err)
		}
	}
	if n := documentDecodes.Load() - before; n != 0 {
		t.Fatalf("listing an unchanged floor ten times decoded %d documents, want 0", n)
	}
	// One write is one document read again, and every reader sees it.
	if err := st.Update(ids[7], func(it *factory.Item) error { it.Title = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	before = documentDecodes.Load()
	got, err := other.Get(ids[7])
	if err != nil || got.Title != "changed" {
		t.Fatalf("after a write another store read %q, %v", got.Title, err)
	}
	if _, err := other.ListSaved(); err != nil {
		t.Fatal(err)
	}
	if n := documentDecodes.Load() - before; n != 1 {
		t.Fatalf("one changed document cost %d decodes, want 1", n)
	}
}

// EVERY ANSWER IS THE CALLER'S OWN. A caller that changes what it was given,
// down to a log line inside a phase or a run in a map, changes nothing the
// next caller reads.
func TestAnswerFromTheCacheSharesNothing(t *testing.T) {
	st := open(t)
	when := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	it, err := st.Create(factory.Item{
		Title: "deep", Kind: factory.KindIssue,
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, On: true, Input: map[string]string{"title": "{title}"}}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {Target: "a.b", Status: "running", Began: when}},
		PullRequest:     &factory.PullRequest{Number: 9, HeadSHA: "abc"},
		Stream:          &factory.Stream{Phases: []factory.Phase{{Name: "build", Rounds: []factory.RoundLine{{Round: 1, Summary: "first"}}}}, Log: []factory.LogLine{{At: when, Text: "said"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := st.Get(it.ID)
	wantJSON, _ := json.Marshal(want)
	a, _ := st.Get(it.ID)
	a.Title = "x"
	a.Stages[0].Input["title"] = "x"
	a.AgentFieldSteps["build"].Status = "x"
	a.PullRequest.HeadSHA = "x"
	a.Stream.Phases[0].Rounds[0].Summary = "x"
	a.Stream.Log[0].Text = "x"
	list, _ := st.ListSaved()
	list[0].Stream.Log[0].Text = "y"
	b, _ := st.Get(it.ID)
	if gotJSON, _ := json.Marshal(b); string(gotJSON) != string(wantJSON) {
		t.Fatalf("a caller's change reached the next read:\n got %s\nwant %s", gotJSON, wantJSON)
	}
	if !b.Stream.Log[0].At.Equal(when) {
		t.Fatalf("a copied time lost its value: %v", b.Stream.Log[0].At)
	}
}

// THE COPY REACHES EVERY FIELD A DOCUMENT HAS, including ones added after this
// test. Every exported field of a document is filled to a few levels deep,
// and no slice, map or pointer of the copy may share its storage with the
// original's.
func TestDeepCopySharesNoStorageWithTheDocument(t *testing.T) {
	var d doc
	fill(reflect.ValueOf(&d).Elem(), 0)
	c := cloneDoc(d)
	if !reflect.DeepEqual(d, c) {
		t.Fatal("the copy differs from the document")
	}
	shared(t, "doc", reflect.ValueOf(d), reflect.ValueOf(c))
}

func fill(v reflect.Value, depth int) {
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fill(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k, depth+1)
		fill(e, depth+1)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf([]any{"x"}))
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), depth+1)
			}
		}
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	}
}

func shared(t *testing.T, path string, a, b reflect.Value) {
	t.Helper()
	switch a.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		if a.IsNil() {
			return
		}
		if a.Kind() != reflect.Slice || a.Len() > 0 {
			if a.UnsafePointer() == b.UnsafePointer() {
				t.Fatalf("%s is shared between the document and its copy", path)
			}
		}
		switch a.Kind() {
		case reflect.Pointer:
			shared(t, path+"*", a.Elem(), b.Elem())
		case reflect.Slice:
			for i := 0; i < a.Len(); i++ {
				shared(t, path+"[]", a.Index(i), b.Index(i))
			}
		case reflect.Map:
			for _, k := range a.MapKeys() {
				shared(t, path+"{}", a.MapIndex(k), b.MapIndex(k))
			}
		}
	case reflect.Interface:
		if !a.IsNil() {
			shared(t, path, a.Elem(), b.Elem())
		}
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if a.Type().Field(i).IsExported() {
				shared(t, path+"."+a.Type().Field(i).Name, a.Field(i), b.Field(i))
			}
		}
	}
}
