package session

import (
	"strings"
	"testing"
)

// Contract (g) validates byte bounds and JSON shapes before any read or write.
func TestWorkstreamToolBoundsRefuseBeforeTheDoor(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"propose_decision", `{"text":""}`},
		{"propose_decision", `{"text":"` + strings.Repeat("é", 1001) + `"}`},
		{"propose_question", `{"text":"Words","source":{"kind":"url","id":"https://example.com","label":"` + strings.Repeat("x", 201) + `"}}`},
		{"propose_jobs", `{"jobs":[]}`},
		{"propose_jobs", `{"jobs":[{"title":"` + strings.Repeat("x", 201) + `"}]}`},
		{"propose_jobs", `{"jobs":[{"title":"Draft","repo":"two\nlines"}]}`},
		{"propose_jobs", `{"jobs":[{"title":"Draft","text":"` + strings.Repeat("x", 4001) + `"}]}`},
		{"propose_jobs", `{"jobs":[{"title":"Draft"}],"wave":"1"}`},
		{"propose_goahead", `{"scope":"job","factory":"f","job":7,"cap_usd":"1","ends_on":"2026-11-04","why":"Words"}`},
		{"write_agent_page", `{"markdown":"Words","updated_from":"Discussion"}`},
		{"write_agent_page", `{"title":"Notes","markdown":"Words","updated_from":"two\nlines"}`},
		{"workstream_read", `[]`},
		{"workstream_read", `null`},
		{"workstream_read", `{"what":null}`},
		{"workstream_read", `{"what":"page"}`},
	}
	for _, tc := range cases {
		d := &workstreamFakeDoor{}
		workstreamCallForTest(t, d, tc.name, tc.args, true)
		if d.otherCalls+d.reads+len(d.saves) != 0 {
			t.Fatalf("invalid %s reached the door: %+v", tc.name, d)
		}
	}
}
