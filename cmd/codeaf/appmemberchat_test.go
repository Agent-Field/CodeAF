package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A TURN'S LAST MESSAGE IS ITS ANSWER EVEN WHEN A LATER TURN WORKED. A member
// answered "BIRCH-9304" with no call; the manager's directive then opened a
// turn of its own with no person's line between, and its calls once turned
// that answer into an update. A message is an update only when its own calls
// ran after it.
func TestListingKeepsAnAnswerBeforeADeliveryOpenedTurn(t *testing.T) {
	items := []session.DisplayEntry{
		{Role: "user", Text: "Read the attached file and reply only with its verification token."},
		{Role: "assistant", Text: "Reading it."},
		{Role: "tool", Tool: "read", CallID: "c1", Answered: true},
		{Role: "tool"},
		{Role: "assistant", Text: "BIRCH-9304"},
		{Role: "aside", ForModel: true, Text: "Team traffic in \"Verification\" for you (@review). These are the team's messages, not the person's words:\n◆ directive from manager #8: Run the suite twice.\n(Reply with team_post.)",
			Team: []session.TeamLine{{Team: "Verification", From: "manager", Kind: "directive", Text: "Run the suite twice.", Thread: "000000000008"}}},
		{Role: "assistant", Text: "Run 1 was 62 tests / OK. Running it a second time now."},
		{Role: "tool", Tool: "bash", CallID: "c2", Answered: true},
		{Role: "assistant", Text: "Both runs passed."},
	}
	entries := appEntriesOf("a", items, time.Now(), nil)
	var said []string
	for _, e := range entries {
		said = append(said, e.Kind+":"+e.Text)
	}
	want := []string{
		"user:Read the attached file and reply only with its verification token.",
		"update:Reading it.",
		"tool:",
		"answer:BIRCH-9304",
		"notice:manager: Run the suite twice.",
		"update:Run 1 was 62 tests / OK. Running it a second time now.",
		"tool:",
		"answer:Both runs passed.",
	}
	if strings.Join(said, "|") != strings.Join(want, "|") {
		t.Fatalf("entries\n%v\nwant\n%v", said, want)
	}
	notice := entries[4]
	if len(notice.Team) != 1 || notice.Team[0] != (appwire.TeamLine{CrewName: "Verification", From: "manager", Kind: "directive", Text: "Run the suite twice.", Post: "000000000008"}) {
		t.Fatalf("the delivery's facts = %+v", notice.Team)
	}
}

// A TEAM POST'S CALL CARRIES WHAT IT DELIVERED, read from the engine's own
// answer: the log entry it became and what it answers; a refused post says
// it was not delivered in the engine's words; any other call carries none.
func TestTeamPostCallCarriesItsHandoff(t *testing.T) {
	ok := appHandoff("team_post", `{"to":"manager","text":"LEAF-READY"}`, "Posted to the manager in \"Verification\" as #3, answering #2. It arrives at the start of their next step.")
	if ok == nil || !ok.Delivered || ok.To != "manager" || ok.CrewName != "Verification" || strings.TrimLeft(ok.Post, "0") != "3" || strings.TrimLeft(ok.Answers, "0") != "2" || ok.Reason != "" {
		t.Fatalf("delivered = %+v", ok)
	}
	refused := appHandoff("team_post", `{"to":"@Release","text":"done"}`, "The team \"Release\" is closed.")
	if refused == nil || refused.Delivered || refused.To != "release" || refused.Reason != "The team \"Release\" is closed." {
		t.Fatalf("refused = %+v", refused)
	}
	if h := appHandoff("bash", `{"to":"manager"}`, "Posted to the manager in \"V\". It arrives at the start of their next step."); h != nil {
		t.Fatalf("a bash call carried %+v", h)
	}
	if h := appHandoff("team_post", `{"to":"manager"}`, ""); h != nil {
		t.Fatalf("a running post carried %+v", h)
	}
}
