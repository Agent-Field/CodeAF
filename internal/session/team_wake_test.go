package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// A MEMBER THE MANAGER STARTED TAKES ITS FIRST TURN ON ITS OWN, AND IS HANDED
// THE BRIEF ONCE. The conversation opens before it is a member, as the
// interface opens it; once it is made one under the handle the start names, it
// wakes with no person's words, its first request carries the brief marked as
// the manager's, and nothing hands it the brief a second time.
func TestAStartedMemberWakesOnItsOwnWithTheBriefOnce(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "lexer", Text: "Rewrite the lexer."})
	dir := filepath.Join(filepath.Dir(filepath.Dir(fixture.parser)), "lexer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, placeTranscript)
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("on it"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("again"), nil },
	}}
	lexer := teamAgent(t, fixture, transcript, completer, nil)
	time.Sleep(3 * teamWakeEvery)
	if completer.requests() != 0 {
		t.Fatal("a conversation that is not a member yet woke")
	}
	if err := teams.Update(fixture.profile, func(file *teams.File) error {
		return file.AddMember(fixture.teamID, teams.Member{Key: convKeyOf(t, transcript), File: transcript, Handle: "lexer"})
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for completer.requests() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if completer.requests() == 0 {
		t.Fatal("the started member never took its first turn")
	}
	first := userTextIn(completer.request(0))
	if !briefIn(first, "Rewrite the lexer.") {
		t.Fatalf("the first request did not carry the brief:\n%s", first)
	}
	for _, entry := range lexer.Transcript() {
		if entry.Role == "user" {
			t.Fatalf("the woken turn carries words as the person's: %+v", entry)
		}
	}
	if news := lexer.teamBoundary(); strings.Contains(news, "Rewrite the lexer") {
		t.Fatalf("the brief would be handed over twice: %q", news)
	}
}

// WITH THE TEAM'S AUTO-WAKE OFF, A START OPENS THE MEMBER AND STARTS NO TURN.
// The brief stays in the Traffic and is handed over on the first turn something
// else starts, and the Traffic says why nothing ran.
func TestAStartedMemberWithWakeOffIsOpenedAndNotWoken(t *testing.T) {
	fixture := newTeamFixture(t, true)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "lexer", Text: "Rewrite the lexer."})
	dir := filepath.Join(filepath.Dir(filepath.Dir(fixture.parser)), "lexer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, placeTranscript)
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("on it"), nil },
	}}
	lexer := teamAgent(t, fixture, transcript, completer, nil)
	if err := teams.Update(fixture.profile, func(file *teams.File) error {
		return file.AddMember(fixture.teamID, teams.Member{Key: convKeyOf(t, transcript), File: transcript, Handle: "lexer"})
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for completer.requests() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if completer.requests() != 0 {
		t.Fatal("a team with auto-wake off started the new member's turn")
	}
	said := waitEvent(t, fixture, "auto-wake is off")
	if said.From != teams.FromSystem || said.To != "lexer" || said.State != teams.StateIdle {
		t.Fatalf("the wake-off line is %+v", said)
	}
	if !strings.Contains(said.Text, "no turn was started") {
		t.Fatalf("the wake-off line does not say no turn was started: %q", said.Text)
	}
	events, err := lexer.Submit(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	first := userTextIn(completer.request(0))
	if !briefIn(first, "Rewrite the lexer.") {
		t.Fatalf("the first turn did not carry the brief:\n%s", first)
	}
}

// briefIn reports whether a request's text hands over the manager's brief
// with text as its words. It reads the line the way a surface does
// ([teamLineParts]) rather than matching its wording, so the line's number
// (" #42" at the end of its head) is the delivery's to write.
func briefIn(request, text string) bool {
	for _, line := range strings.Split(request, "\n") {
		parsed, ok := teamLineParts(strings.TrimSpace(line))
		if ok && parsed.Kind == teams.KindStart && parsed.From == teams.FromManager && parsed.Text == text {
			return true
		}
	}
	return false
}
