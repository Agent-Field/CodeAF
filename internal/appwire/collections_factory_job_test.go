package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// Part A (A2): the wire validator and optional fields match store ref identity.
func TestPartACollectionFactoryJobRefs(t *testing.T) {
	for _, tc := range []struct {
		r  Ref
		ok bool
	}{
		{Ref{Kind: RefFactoryJob, ID: "7", Factory: "f"}, true},
		{Ref{Kind: RefFactoryJob, ID: "7"}, false},
		{Ref{Kind: RefFactoryJob, ID: "0", Factory: "f"}, false},
		{Ref{Kind: RefFactoryJob, ID: "07", Factory: "f"}, false},
		{Ref{Kind: RefFactoryJob, ID: "+7", Factory: "f"}, false},
		{Ref{Kind: RefFactoryJob, ID: "7", Factory: " f"}, false},
		{Ref{Kind: RefFactoryJob, ID: "7", Factory: strings.Repeat("x", 4097)}, false},
		{Ref{Kind: RefFactoryJob, ID: "7", Factory: "f", SessionID: "chat"}, false},
		{Ref{Kind: RefConversation, ID: "chat", Factory: "f"}, false},
	} {
		for _, adding := range []bool{false, true} {
			if err := CheckRef(tc.r, adding); (err == nil) != tc.ok {
				t.Errorf("%+v adding %v: %v", tc.r, adding, err)
			}
		}
	}
	raw, err := json.Marshal(Member{Ref: Ref{Kind: RefFactoryJob, ID: "7", Factory: "f"}})
	if err != nil || !strings.Contains(string(raw), `"factory":"f"`) {
		t.Fatalf("factory: %s %v", raw, err)
	}
	for _, flag := range []bool{false, true} {
		raw, err := json.Marshal(Collection{ID: "c", Name: "Name", Workstream: flag})
		if err != nil || strings.Contains(string(raw), `"workstream":true`) != flag {
			t.Fatalf("flag: %s %v", raw, err)
		}
	}
}
