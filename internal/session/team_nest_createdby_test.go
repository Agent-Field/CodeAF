package session

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// A SUB-TEAM team_start MAKES IS THE STARTING AGENT'S: the team records the
// conversation id of the manager that made it.
func TestASubTeamRecordsTheAgentThatMadeIt(t *testing.T) {
	n := newTeamTree(t, "boss", "web")
	n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	boss := n.agent(t, "boss")
	boss.teamBoundary()
	if said, failed := callTool(t, boss.teamStartTool, `{"handle":"api","brief":"Build the API.","kind":"team","name":"backend"}`); failed {
		t.Fatalf("team_start of kind team said %q", said)
	}
	var backend teams.Team
	for _, team := range n.file(t).Teams {
		if team.Name == "backend" {
			backend = team
		}
	}
	if backend.CreatedBy != "boss" {
		t.Fatalf("the sub-team was created by %q, want boss", backend.CreatedBy)
	}
}
