package teams

import (
	"encoding/json"
	"strings"
	"testing"
)

// THE FACTORY FLAGS ARE ADDITIVE: absent when false, round-tripped when set,
// and a file from before they were kept reads as unmarked and is written back
// without them.
func TestFactoryFlagsRoundTripAndLegacyFilesStayUnmarked(t *testing.T) {
	legacy := `{"id":"a","name":"factory","parent":"","members":[{"key":"k","file":"f","where":"w","word":"x"}],"manager":"","made":"2026-01-01T00:00:00Z","future":1}`
	var old Team
	if err := json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatal(err)
	}
	if old.FactoryManaged || old.Members[0].FactoryStage || old.Members[0].FactoryBrief != "" {
		t.Fatalf("a legacy team read as marked: %+v", old)
	}
	raw, _ := json.Marshal(old)
	if strings.Contains(string(raw), "factory_") || !strings.Contains(string(raw), `"future":1`) {
		t.Fatalf("legacy write-back = %s", raw)
	}

	old.FactoryManaged = true
	old.Members[0].FactoryStage, old.Members[0].FactoryBrief = true, "do the review"
	raw, _ = json.Marshal(old)
	var back Team
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !back.FactoryManaged || !back.Members[0].FactoryStage || back.Members[0].FactoryBrief != "do the review" {
		t.Fatalf("round trip = %s", raw)
	}
	if back.extra["factory_managed"] != nil || back.Members[0].extra["factory_stage"] != nil || back.Members[0].extra["factory_brief"] != nil {
		t.Fatalf("a known flag leaked into the unknown fields: %+v", back)
	}
	if !strings.Contains(string(raw), `"future":1`) {
		t.Fatalf("an unknown field was lost: %s", raw)
	}
}
