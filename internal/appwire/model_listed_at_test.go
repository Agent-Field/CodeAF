package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelListingTimeWireIsOptionalAndRoundTrips(t *testing.T) {
	for _, timestamp := range []int64{0, 1760000000} {
		data, err := json.Marshal(Model{ID: "fixture", ListedAt: timestamp})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "listed_at") != (timestamp > 0) {
			t.Fatalf("optional timestamp: %s", data)
		}
		var back Model
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		if back.ListedAt != timestamp {
			t.Fatalf("lost timestamp: %+v", back)
		}
	}
	var legacy Model
	if err := json.Unmarshal([]byte(`{"id":"legacy-2099-12-31"}`), &legacy); err != nil || legacy.ListedAt != 0 {
		t.Fatalf("legacy invented timestamp: %+v %v", legacy, err)
	}
}
