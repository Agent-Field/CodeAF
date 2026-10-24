package appwire

import (
	"encoding/json"
	"testing"
)

func TestDirectCallUsesTheDesktopMethodTable(t *testing.T) {
	result, err := Call(&fakeEngine{}, "asks.list", json.RawMessage(`{"agent":"a1"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil || string(raw) != `{"asks":[]}` {
		t.Fatalf("%s, %v", raw, err)
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`"x"`)} {
		if _, err := Call(&fakeEngine{}, "asks.list", raw); err == nil {
			t.Fatal("accepted non-object params")
		}
	}
}
