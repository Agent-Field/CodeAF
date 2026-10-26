package appwire

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

type syncEngine struct {
	fakeEngine
	ack string
}

func (*syncEngine) SyncStatus() (Sync, error)         { return Sync{Mode: SyncQuiet}, nil }
func (e *syncEngine) AcknowledgeSync(id string) error { e.ack = id; return nil }
func (*syncEngine) ChatLocationsKnown() bool          { return true }

// C6: optional capabilities leave older engines' hello and refusals alone.
func TestC6SyncFeaturesAndMethodsAreOptional(t *testing.T) {
	old := withSyncFeatures(Hello{}, &fakeEngine{})
	if len(old.Features) != 0 {
		t.Fatalf("old engine advertised sync: %v", old.Features)
	}
	for name, params := range map[string]string{"sync.get": `{}`, "sync.acknowledge": `{"id":"disclosure"}`} {
		_, err := methods[name].call(&fakeEngine{}, json.RawMessage(params))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Fatalf("%s without capability: %v", name, err)
		}
	}
	e := &syncEngine{}
	hello := withSyncFeatures(withSyncFeatures(Hello{}, e), e)
	if !slices.Equal(hello.Features, []string{FeatureSyncStatus, FeatureChatsLocation}) {
		t.Fatalf("features = %v", hello.Features)
	}
	r, err := methods["sync.get"].call(e, json.RawMessage(`{}`))
	if err != nil || r.(*SyncResult).Sync.Mode != SyncQuiet {
		t.Fatalf("sync.get = %+v, %v", r, err)
	}
	if _, err := methods["sync.acknowledge"].call(e, json.RawMessage(`{"id":"disclosure"}`)); err != nil || e.ack != "disclosure" {
		t.Fatalf("acknowledge = %q, %v", e.ack, err)
	}
	if _, err := methods["sync.acknowledge"].call(e, json.RawMessage(`{}`)); wireCode(t, err) != CodeBadParams {
		t.Fatalf("empty acknowledgement = %v", err)
	}
}

// C1/C2/C3: the schema and JSON preserve the additive facts, including here:false.
func TestC1C2C3SyncWireShapes(t *testing.T) {
	raw, err := json.Marshal(Agent{Location: &Location{Here: false, Holder: &LocationHolder{ID: "device", Name: "laptop"}, Line: "continued"}})
	if err != nil {
		t.Fatal(err)
	}
	var agent map[string]json.RawMessage
	if err := json.Unmarshal(raw, &agent); err != nil || string(agent["location"]) != `{"here":false,"holder":{"id":"device","name":"laptop"},"line":"continued"}` {
		t.Fatalf("location JSON = %s, %v", raw, err)
	}
	raw, _ = json.Marshal(Agent{})
	var absent map[string]any
	_ = json.Unmarshal(raw, &absent)
	if _, present := absent["location"]; present {
		t.Fatal("unknown location must be absent")
	}
	schema := Schema()
	for _, method := range []string{"sync.get", "sync.acknowledge"} {
		if _, registered := methods[method]; !registered {
			t.Fatalf("method is not registered: %s", method)
		}
		if schema["methods"].(map[string]any)[method] == nil {
			t.Fatalf("schema lacks %s", method)
		}
	}
	if EventShape(EventSyncChanged) != reflect.TypeFor[SyncChanged]() {
		t.Fatal("sync.changed has no typed shape")
	}
	defs := schema["$defs"].(map[string]any)
	properties := defs["Sync"].(map[string]any)["properties"].(map[string]any)
	if got := properties["mode"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"quiet", "on", "off", "unavailable"}) {
		t.Fatalf("sync modes = %v", got)
	}
	properties = defs["Entry"].(map[string]any)["properties"].(map[string]any)
	kinds := properties["notice_kind"].(map[string]any)["enum"].([]string)
	for _, kind := range []string{NoticeSync, NoticeMoved, NoticeInterrupted} {
		if !slices.Contains(kinds, kind) {
			t.Fatalf("schema lacks notice_kind %q", kind)
		}
	}
	raw, err = json.Marshal(Entry{Kind: EntryNotice, NoticeKind: NoticeSync, Quiet: false})
	var notice map[string]any
	if err != nil || json.Unmarshal(raw, &notice) != nil || notice["quiet"] != false {
		t.Fatalf("visible notice must explicitly carry quiet:false: %s, %v", raw, err)
	}
	raw, err = json.Marshal(SyncChanged{Sync: Sync{Mode: SyncOn, Disclosure: &SyncDisclosure{ID: "owed", Text: "disclosure"}}})
	if err != nil || string(raw) != `{"sync":{"mode":"on","disclosure":{"id":"owed","text":"disclosure"}}}` {
		t.Fatalf("sync.changed JSON = %s, %v", raw, err)
	}
}
