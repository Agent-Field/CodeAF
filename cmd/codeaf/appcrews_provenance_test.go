package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// THE WIRE SAYS WHICH TURN CAUSED A LINE, from the engine's record and from
// nothing else: the person's message, the run, and the app agent that ran it.
func TestAppLogLineProjectsProvenance(t *testing.T) {
	team := teamstore.Team{ID: "aaaaaaaaaaaa", Name: "harbor", Manager: "hm", Members: []teamstore.Member{
		{Key: "hm", Handle: "boss", File: "/conv/manager/transcript.jsonl"},
		{Key: "w1", Handle: "web", File: "/conv/web/transcript.jsonl"},
	}}
	stamped := teamstore.Entry{ID: "000000000001", Kind: teamstore.KindDirective, From: teamstore.FromManager, To: "web", Member: "w1", Text: "go",
		Provenance: teamstore.Provenance{Turn: "msg_x", Run: "RUN1", RunBy: "hm"}}
	line := appLogLine(team, stamped)
	if line.Turn != "msg_x" || line.Run != "RUN1" || line.RunAgent != appAgentID("/conv/manager/transcript.jsonl") {
		t.Fatalf("projected %+v", line)
	}
	// The run's agent is the one that ran it, not the member the line concerns.
	if line.RunAgent == line.Agent {
		t.Errorf("run_agent %q is the line's member agent", line.RunAgent)
	}
	// A conversation the team no longer lists is named by its key, as every
	// other agent in a line is.
	gone := stamped
	gone.RunBy = "/conv/left/transcript.jsonl"
	if got := appLogLine(team, gone).RunAgent; got != appAgentID("/conv/left/transcript.jsonl") {
		t.Errorf("a departed runner is %q", got)
	}
	// A legacy line projects as unknown and is not dropped.
	legacy := appLogLine(team, teamstore.Entry{ID: "000000000002", Kind: teamstore.KindNote, From: "web", To: teamstore.ToManager, Text: "old"})
	if legacy.Turn != "" || legacy.Run != "" || legacy.RunAgent != "" || legacy.Text != "old" {
		t.Errorf("legacy projected %+v", legacy)
	}
	raw, _ := json.Marshal(legacy)
	for _, key := range []string{`"turn"`, `"run"`, `"run_agent"`} {
		if strings.Contains(string(raw), key) {
			t.Errorf("a legacy line writes %s on the wire: %s", key, raw)
		}
	}
	var back appwire.LogLine
	if err := json.Unmarshal(mustJSON(t, line), &back); err != nil || back.Turn != "msg_x" || back.RunAgent != line.RunAgent {
		t.Errorf("round trip %+v %v", back, err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
