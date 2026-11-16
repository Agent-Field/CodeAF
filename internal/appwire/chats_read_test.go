package appwire

import (
	"encoding/json"
	"slices"
	"testing"
)

// reader is an engine that reads remote chats when can says so.
type reader struct {
	fakeEngine
	can  bool
	got  ChatReadParams
	page ChatReadResult
}

func (r *reader) CanReadChats() bool { return r.can }

func (r *reader) ReadChat(p ChatReadParams) (ChatReadResult, error) {
	r.got = p
	return r.page, nil
}

// chats.read is advertised only by an engine that can serve it NOW (a relay and
// a furrow that reads paths), and refused as unknown otherwise: a capability
// that cannot work is absent, not present and failing.
func TestChatsReadIsAbsentUnlessTheEngineCanReadNow(t *testing.T) {
	if !slices.Contains(Methods(), "chats.read") {
		t.Fatal("chats.read is not registered")
	}
	cannot := &reader{}
	if h := withChatsFeatures(Hello{}, cannot); slices.Contains(h.Features, FeatureChatsRead) {
		t.Fatal("an engine that cannot read advertises chats.read")
	}
	if _, err := methods["chats.read"].call(cannot, json.RawMessage(`{"cell":"x"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine that cannot read answered %v", err)
	}
	if _, err := methods["chats.read"].call(&fakeEngine{}, json.RawMessage(`{"cell":"x"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an old engine answered %v", err)
	}
	can := &reader{can: true, page: ChatReadResult{Head: "h", More: true, Before: "c"}}
	if h := withChatsFeatures(Hello{}, can); !slices.Contains(h.Features, FeatureChatsRead) {
		t.Fatal("an engine that can read does not advertise chats.read")
	}
	if _, err := methods["chats.read"].call(can, json.RawMessage(`{}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("a read with no cell answered %v", err)
	}
	out, err := methods["chats.read"].call(can, json.RawMessage(`{"cell":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if can.got.Limit != ChatReadLimit {
		t.Fatalf("limit defaulted to %d, want %d", can.got.Limit, ChatReadLimit)
	}
	raw, _ := json.Marshal(out)
	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	// entries is [] (never null), fetched_bytes is always said, even when 0.
	if e, ok := shape["entries"].([]any); !ok || len(e) != 0 {
		t.Fatalf("entries = %v in %s", shape["entries"], raw)
	}
	for _, key := range []string{"more", "before", "head", "fetched_bytes"} {
		if _, ok := shape[key]; !ok {
			t.Fatalf("%s missing from %s", key, raw)
		}
	}
}
