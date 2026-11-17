package appwire

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// E-P1/E-C1/E-T1/E-R1: the types, registrations and advertisements are additive.
func TestRelayAppContractShapesAndOldEngines(t *testing.T) {
	for _, name := range []string{"pair.start", "pair.get", "pair.answer", "pair.cancel", "chats.list", "agents.take"} {
		if !slices.Contains(Methods(), name) || Schema()["methods"].(map[string]any)[name] == nil {
			t.Fatalf("missing %s", name)
		}
		if _, err := methods[name].call(&fakeEngine{}, json.RawMessage(`{}`)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("old %s: %v", name, err)
		}
	}
	h := withPairFeatures(withChatsFeatures(Hello{}, &fakeEngine{}), &fakeEngine{})
	for _, f := range []string{FeatureDevicesPair, FeatureChatsTake, FeatureSyncRelay} {
		if slices.Contains(h.Features, f) {
			t.Fatalf("old advertises %s", f)
		}
	}
	for name, want := range map[string]reflect.Type{EventPairChanged: reflect.TypeFor[PairResult](), EventChatChanged: reflect.TypeFor[ChatChanged](), EventChatRemoved: reflect.TypeFor[ChatRemoved]()} {
		if EventShape(name) != want {
			t.Fatalf("%s", name)
		}
	}
	raw, err := json.Marshal(ChatChanged{Chat: ChatRow{Cell: "chat", Title: "title", Status: "idle", Seen: Seen{Device: "device", Fence: 9007199254740993}}, Version: 9})
	if err != nil || !json.Valid(raw) {
		t.Fatal(err)
	}
	// A uint64 must survive the Go wire exactly, including values above 2^53.
	var decoded ChatChanged
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Chat.Seen.Fence != 9007199254740993 {
		t.Fatalf("%s: %v", raw, err)
	}
	defs := Schema()["$defs"].(map[string]any)
	for _, name := range []string{"Pairing", "PairQuestion", "ChatRow", "Seen", "Relay", "Location"} {
		if defs[name] == nil {
			t.Fatalf("missing schema %s", name)
		}
	}
}
