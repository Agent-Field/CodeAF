package appwire

import (
	"encoding/json"
	"reflect"
	"testing"
)

// WP-12 wire capability: both features follow the same hook and remain absent
// for stub/plain engines. Optional fields omit while required false flags stay.
func TestWorkstreamSourcesFeatureAndRequiredBooleans(t *testing.T) {
	for _, serves := range []bool{false, true} {
		fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: serves}
		hello := withWorkstreamsFeature(withWorkstreamsFeature(Hello{}, fake), fake)
		want := []string(nil)
		if serves {
			want = []string{FeatureWorkstreams, FeatureWorkstreamSources}
		}
		if !reflect.DeepEqual(hello.Features, want) {
			t.Fatalf("features: %v", hello.Features)
		}
	}
	raw, err := json.Marshal(WorkstreamSourceItem{ID: "source", WorkstreamID: "stream", Kind: "link", Title: "Example", AddedBy: "you"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"read_by_jobs", "proposed"} {
		if value, exists := fields[key]; !exists || value != false {
			t.Fatalf("required false missing: %s", raw)
		}
	}
	for _, key := range []string{"path", "repo", "number", "url", "ref", "why", "gone"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("absent optional present: %s", raw)
		}
	}
}

// WP-12 (b): typed dispatch rejects missing/null fields before reaching the
// serving engine, including inside a kept ref and create's sources array.
func TestWorkstreamSourcesRequiredFieldsOnWire(t *testing.T) {
	fake := &fakeWorkstreamer{fakeEngine: &fakeEngine{}, serves: true}
	call := serveOver(t, fake)
	for _, tc := range []struct{ method, params string }{
		{"source.list", `{}`},
		{"source.add", `{"workstream_id":"w","kind":"link","url":null}`},
		{"source.add", `{"workstream_id":"w","kind":"kept","ref":{"kind":"message","chat_id":"c"}}`},
		{"source.update", `{"id":"s","keep":null}`},
		{"source.remove", `{"id":null}`},
		{"workstream.create", `{"name":"Sources","sources":[{"path":"x"}]}`},
		{"workstream.create", `{"name":"Sources","sources":null}`},
	} {
		frame := call(`{"id":1,"method":"` + tc.method + `","params":` + tc.params + `}`)
		refusal, _ := frame["error"].(map[string]any)
		if refusal["code"] != CodeBadParams {
			t.Fatalf("%s: %v", tc.method, frame)
		}
	}
}
