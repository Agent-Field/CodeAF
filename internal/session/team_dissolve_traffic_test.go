package session

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamDissolveLeavesAfterTrafficMakesItUnusable(t *testing.T) {
	f := newHelpersFixture(t)
	a := teamAgent(t, f, f.manager, nil, nil)
	a.teamBoundary()
	if !holds(a, teamDissolveToolName) {
		t.Fatal("unused team did not offer dissolve")
	}
	appendTraffic(t, f, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "The plan is ready."})
	a.teamBoundary()
	if holds(a, teamDissolveToolName) {
		t.Fatal("team_dissolve remains offered after traffic permanently makes it refuse")
	}
	if !holds(a, teamSendToolName) {
		t.Fatal("traffic took away the manager's other tools")
	}
	result := a.executeTool(context.Background(), nil, nil, withdrawnCall("c1", teamDissolveToolName, `{}`), "")
	if want := "team_dissolve is no longer one of your tools: " + teamRetiredDissolve + "."; result.text != want {
		t.Fatalf("a remembered call said %q, want %q", result.text, want)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	// Rotating the log does not make its history safe to forget either.
	path := teams.TrafficPath(f.profile, f.teamID)
	if err := os.Rename(path, strings.TrimSuffix(path, ".jsonl")+".1.jsonl"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := teamAgent(t, f, f.manager, nil, nil)
	reopened.teamBoundary()
	if holds(reopened, teamDissolveToolName) {
		t.Fatal("a manager reopened after traffic was offered team_dissolve")
	}
}
