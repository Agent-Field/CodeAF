package teams

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMemberRoundTripPreservesFutureFieldsAndAbsentJoinBoundary(t *testing.T) {
	var member Member
	raw := `{"key":"conversation","handle":"picker","independent":true,"future_authority":{"mode":"explicit"}}`
	must(t, json.Unmarshal([]byte(raw), &member))
	for i := 0; i < 2; i++ {
		data, err := json.Marshal(member)
		must(t, err)
		if !strings.Contains(string(data), `"future_authority":{"mode":"explicit"}`) || strings.Contains(string(data), "joined_at") || !member.Independent || member.Handle != "picker" {
			t.Fatalf("round trip: %s", data)
		}
		must(t, json.Unmarshal(data, &member))
	}
	member.JoinedAt = time.Unix(42, 0).UTC()
	data, err := json.Marshal(member)
	must(t, err)
	var back Member
	must(t, json.Unmarshal(data, &back))
	if !back.JoinedAt.Equal(member.JoinedAt) {
		t.Fatalf("join boundary: %s", data)
	}
}
