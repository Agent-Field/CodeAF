package session

// THE TEAM, AS TESTS: who is offered which verb, what the verbs write, what a
// conversation is told and how often, and what only a manager carries.
//
// Every fixture here builds a real teams.json and a real Traffic log through
// internal/teams, the one store the interface writes too, so what is asserted is
// the contract between the two sides and not a second copy of it.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamFixture is one profile with one team in it: a manager conversation, a
// member called web, and a member called parser, each with its own journal.
type teamFixture struct {
	profile string
	teamID  string
	// manager, web and parser are the three transcripts.
	manager, web, parser string
}

// convKeyOf is the interface's key for a transcript (tui3's convKey): the path
// cleaned, with its symlinks resolved once the file exists.
func convKeyOf(t *testing.T, path string) string {
	t.Helper()
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(path)
}

// newTeamFixture makes the three transcripts and the team. managed says whether
// the manager conversation is recorded as the team's manager.
//
// ITS TEAM HAS THE AUTO-WAKE OFF, so every test here reads delivery at the
// turns it starts itself, with no turn started behind it by the traffic
// watch; the wake's own tests build theirs with [newWakingTeamFixture].
func newTeamFixture(t *testing.T, managed bool) teamFixture {
	t.Helper()
	return makeTeamFixture(t, managed, false)
}

// newWakingTeamFixture is [newTeamFixture] with the team's auto-wake on, as a
// team is made in the product.
func newWakingTeamFixture(t *testing.T) teamFixture {
	t.Helper()
	return makeTeamFixture(t, true, true)
}

func makeTeamFixture(t *testing.T, managed, wakes bool) teamFixture {
	t.Helper()
	root := t.TempDir()
	fixture := teamFixture{profile: filepath.Join(root, "profile")}
	for _, name := range []string{"manager", "web", "parser"} {
		dir := filepath.Join(root, "sessions", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, placeTranscript)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "manager":
			fixture.manager = path
		case "web":
			fixture.web = path
		case "parser":
			fixture.parser = path
		}
	}
	fixture.teamID = teams.NewID()
	err := teams.Update(fixture.profile, func(file *teams.File) error {
		file.Teams = append(file.Teams, teams.Team{ID: fixture.teamID, Name: "harbor", WakeOff: !wakes})
		for _, member := range []teams.Member{
			{Key: convKeyOf(t, fixture.manager), File: fixture.manager, Word: "Harbor manager", Handle: "boss"},
			{Key: convKeyOf(t, fixture.web), File: fixture.web, Word: "web frontend", Handle: "web"},
			{Key: convKeyOf(t, fixture.parser), File: fixture.parser, Word: "the parser", Handle: "parser"},
		} {
			if err := file.AddMember(fixture.teamID, member); err != nil {
				return err
			}
		}
		if managed {
			return file.SetManager(fixture.teamID, convKeyOf(t, fixture.manager))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("make the team: %v", err)
	}
	return fixture
}

// teamAgent is a conversation on one of the fixture's transcripts, with a
// session folder of its own beside it so its cursor has somewhere to live.
func teamAgent(t *testing.T, fixture teamFixture, transcript string, completer Completer, mutate func(*Config)) *Agent {
	t.Helper()
	if completer == nil {
		completer = &scriptedCompleter{}
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ProfileDir = fixture.profile
		config.SessionFile = transcript
		config.Place = Place{Dir: filepath.Dir(transcript)}
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

func appendTraffic(t *testing.T, fixture teamFixture, entry teams.Entry) {
	t.Helper()
	if err := teams.AppendTraffic(fixture.profile, fixture.teamID, entry); err != nil {
		t.Fatalf("append traffic: %v", err)
	}
}

func holds(agent *Agent, name string) bool { return agent.hasTool(name) }

// ── gating ──────────────────────────────────────────────────────────────────

func TestTheManagerIsOfferedTheManagersVerbsAndNotThePost(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	for _, name := range teamToolNames {
		if holds(manager, name) {
			t.Fatalf("%s is on the belt before any boundary found the team", name)
		}
	}
	manager.teamBoundary()
	for _, name := range []string{teamStatusToolName, teamReadToolName, teamSendToolName, teamStopToolName, teamStartToolName} {
		if !holds(manager, name) {
			t.Errorf("the manager was not offered %s", name)
		}
	}
	if holds(manager, teamPostToolName) {
		t.Error("the manager was offered team_post, which is a member's verb")
	}
}

func TestAMemberIsOfferedThePostAndNoManagersVerb(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()
	if !holds(web, teamPostToolName) {
		t.Error("a member of a managed team was not offered team_post")
	}
	for _, name := range []string{teamStatusToolName, teamReadToolName, teamSendToolName, teamStopToolName, teamStartToolName} {
		if holds(web, name) {
			t.Errorf("a member was offered the manager's %s", name)
		}
	}
}

func TestNoTeamNoManagerAnEmptyProfileAndATaskAreOfferedNothing(t *testing.T) {
	cases := map[string]func(t *testing.T) *Agent{
		"a conversation in no team": func(t *testing.T) *Agent {
			fixture := newTeamFixture(t, true)
			other := filepath.Join(t.TempDir(), "elsewhere", placeTranscript)
			_ = os.MkdirAll(filepath.Dir(other), 0o700)
			return teamAgent(t, fixture, other, nil, nil)
		},
		"a member of a team with no manager": func(t *testing.T) *Agent {
			fixture := newTeamFixture(t, false)
			return teamAgent(t, fixture, fixture.web, nil, nil)
		},
		// AN EMPTY PROFILE IS THE ORDINARY LAUNCH AND READS THE STATE ROOT'S
		// teams.json, which here is a root of this test's own holding no team.
		// The fixture's team is in another directory, so the manager's journal
		// is nobody's manager on this launch.
		"a conversation whose own profile holds no team": func(t *testing.T) *Agent {
			t.Setenv(home.EnvVar, t.TempDir())
			fixture := newTeamFixture(t, true)
			return teamAgent(t, fixture, fixture.manager, nil, func(config *Config) { config.ProfileDir = "" })
		},
		"a task node on the manager's own journal": func(t *testing.T) *Agent {
			fixture := newTeamFixture(t, true)
			return teamAgent(t, fixture, fixture.manager, nil, func(config *Config) { config.InTask = true })
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			agent := build(t)
			if news := agent.teamBoundary(); news != "" {
				t.Errorf("it was told %q", news)
			}
			for _, tool := range teamToolNames {
				if holds(agent, tool) {
					t.Errorf("it was offered %s", tool)
				}
			}
		})
	}
}

// THE FIXED PREFIX IS NOT A TEAM'S TO PAY FOR. The verbs are armed at a
// boundary, never built into the belt a conversation is constructed with, so
// every shape prefixbudget_test.go weighs is the shape it was.
func TestTheConstructedBeltCarriesNoTeamVerbEvenForAManager(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	for _, tool := range manager.belt() {
		for _, name := range teamToolNames {
			if tool.Name == name {
				t.Fatalf("belt() built %s; team verbs must arrive by the arming door", name)
			}
		}
	}
}

// A MANAGER REMOVED IS TOLD SO BY THE VERB. The verb stays on the belt (the
// append law), and the file is asked again on the call.
func TestAVerbKeptAfterTheManagerWasRemovedRefuses(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	if err := teams.Update(fixture.profile, func(file *teams.File) error { return file.ClearManager(fixture.teamID) }); err != nil {
		t.Fatal(err)
	}
	text, failed, _ := manager.teamSendTool(context.Background(), json.RawMessage(`{"to":"web","text":"hi"}`))
	if !failed || !strings.Contains(text, "not the manager") {
		t.Fatalf("a removed manager's send answered %q (failed=%v)", text, failed)
	}
	if entries, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0); len(entries) != 0 {
		t.Fatalf("a refused send wrote %d entries", len(entries))
	}
}

// ── what the verbs write ────────────────────────────────────────────────────

func TestTheVerbsWriteTrafficToTheContract(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	ctx := context.Background()
	calls := []struct {
		agent *Agent
		run   func(context.Context, json.RawMessage) (string, bool, error)
		args  string
		want  teams.Entry
	}{
		{manager, manager.teamSendTool, `{"to":"@web","text":"ship the header"}`,
			teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "ship the header", Member: convKeyOf(t, fixture.web)}},
		{manager, manager.teamSendTool, `{"to":"everyone","text":"freeze main","kind":"directive"}`,
			teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: teams.ToEveryone, Text: "freeze main"}},
		{manager, manager.teamStopTool, `{"handle":"parser","reason":"wrong file"}`,
			teams.Entry{Kind: teams.KindStop, From: teams.FromManager, To: "parser", Text: "wrong file", Member: convKeyOf(t, fixture.parser)}},
		{manager, manager.teamStartTool, `{"handle":"docs","brief":"write the README"}`,
			teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "docs", Text: "write the README"}},
		{web, web.teamPostTool, `{"to":"room","text":"header shipped"}`,
			teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToRoom, Text: "header shipped"}},
		{web, web.teamPostTool, `{"to":"manager","text":"blocked on the API"}`,
			teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToManager, Text: "blocked on the API"}},
		{web, web.teamPostTool, `{"to":"parser","text":"what does the token look like?"}`,
			teams.Entry{Kind: teams.KindNote, From: "web", To: "parser", Text: "what does the token look like?", Member: convKeyOf(t, fixture.parser)}},
	}
	for index, call := range calls {
		text, failed, err := call.run(ctx, json.RawMessage(call.args))
		if err != nil || failed {
			t.Fatalf("call %d %s answered %q (failed=%v, err=%v)", index, call.args, text, failed, err)
		}
	}
	entries, err := teams.ReadTraffic(fixture.profile, fixture.teamID, teamLogStart, 0)
	if err != nil || len(entries) != len(calls) {
		t.Fatalf("traffic holds %d entries (err %v), want %d", len(entries), err, len(calls))
	}
	for index, entry := range entries {
		want := calls[index].want
		if entry.Kind != want.Kind || entry.From != want.From || entry.To != want.To || entry.Text != want.Text || entry.Member != want.Member {
			t.Errorf("entry %d is %+v, want kind %s from %s to %s text %q member %q",
				index, entry, want.Kind, want.From, want.To, want.Text, want.Member)
		}
	}
}

func TestTheVerbsRefuseWhatTheContractCannotCarry(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	ctx := context.Background()
	refused := []struct {
		run  func(context.Context, json.RawMessage) (string, bool, error)
		args string
	}{
		{manager.teamSendTool, `{"to":"nobody","text":"hi"}`},
		{manager.teamSendTool, `{"to":"boss","text":"to myself"}`},
		{manager.teamSendTool, `{"to":"web","text":"hi","kind":"order"}`},
		{manager.teamStartTool, `{"handle":"web","brief":"a second web"}`},
		{manager.teamStartTool, `{"handle":"Not A Handle!","brief":"x"}`},
		{manager.teamStopTool, `{"handle":"boss"}`},
		{manager.teamReadTool, `{"handle":"boss"}`},
		{manager.teamPostTool, `{"to":"room","text":"a manager is not a member here"}`},
	}
	for _, call := range refused {
		if text, failed, _ := call.run(ctx, json.RawMessage(call.args)); !failed {
			t.Errorf("%s was not refused: %q", call.args, text)
		}
	}
	if entries, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0); len(entries) != 0 {
		t.Fatalf("refused calls wrote %d entries", len(entries))
	}
}

// ── delivery ────────────────────────────────────────────────────────────────

func TestAMemberIsToldWhatIsAddressedToItOnceAndOnlyThat(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	if news := web.teamBoundary(); news != "" {
		t.Fatalf("a fresh member was told %q before anything was said", news)
	}
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "ship the header"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: teams.ToEveryone, Text: "freeze main"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "parser", To: teams.ToRoom, Text: "tokens are ready"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "parser", Text: "not for web"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToRoom, Text: "my own post"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStop, From: teams.FromManager, To: "web", Text: "stop"})

	news := web.teamBoundary()
	for _, want := range []string{"◆ from manager: ship the header", "◆ directive from manager to everyone: freeze main", "from @parser to the room: tokens are ready", "not the person's words"} {
		if !strings.Contains(news, want) {
			t.Errorf("the member's note lacks %q:\n%s", want, news)
		}
	}
	for _, never := range []string{"not for web", "my own post", "stop"} {
		if strings.Contains(news, never+"\n") || strings.Contains(news, ": "+never) {
			t.Errorf("the member was told %q, which was not addressed to it:\n%s", never, news)
		}
	}
	if again := web.teamBoundary(); again != "" {
		t.Fatalf("the same traffic was delivered twice:\n%s", again)
	}
}

func TestTheManagerIsToldEveryMembersPostAndNotItsOwnLines(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToManager, Text: "blocked on the API"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "parser", To: "web", Text: "token shape attached"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "my own send"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindYou, From: teams.FromYou, To: teams.ToManager, Text: "what the person typed"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: teams.FromSystem, To: teams.ToManager, Text: "web finished"})
	news := manager.teamBoundary()
	for _, want := range []string{"from @web: blocked on the API", "from @parser to @web: token shape attached", "which you manage"} {
		if !strings.Contains(news, want) {
			t.Errorf("the manager's note lacks %q:\n%s", want, news)
		}
	}
	for _, never := range []string{"my own send", "what the person typed", "web finished"} {
		if strings.Contains(news, never) {
			t.Errorf("the manager was told %q:\n%s", never, news)
		}
	}
}

// THE CURSOR OUTLIVES THE PROCESS. A conversation reopened is handed what was
// said while it was closed, and nothing it was handed before.
func TestAReopenedConversationResumesFromItsCursor(t *testing.T) {
	fixture := newTeamFixture(t, true)
	first := teamAgent(t, fixture, fixture.web, nil, nil)
	first.teamBoundary()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "one"})
	if news := first.teamBoundary(); !strings.Contains(news, "one") {
		t.Fatalf("the first life was not told: %q", news)
	}
	_ = first.Close()
	first.SettleWrites()

	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "two"})
	second := teamAgent(t, fixture, fixture.web, nil, nil)
	news := second.teamBoundary()
	if !strings.Contains(news, "◆ from manager: two") {
		t.Fatalf("the reopened conversation was not told what was said while it was closed: %q", news)
	}
	if strings.Contains(news, ": one") {
		t.Fatalf("the reopened conversation was told again what it was already told: %q", news)
	}
}

// A FRESH CONVERSATION DOES NOT REPLAY THE TEAM, except what came after its own
// start.
func TestAFreshConversationStartsAtItsBirthOrItsOwnStart(t *testing.T) {
	fixture := newTeamFixture(t, true)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "old history"})
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	if news := web.teamBoundary(); news != "" {
		t.Fatalf("a fresh member was handed the team's history: %q", news)
	}

	// parser was started by the manager, and the manager spoke to it before the
	// interface had opened it.
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "parser", Text: "before its start"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "parser", Text: "the brief"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "parser", Text: "after its start"})
	time.Sleep(2 * time.Millisecond)
	parser := teamAgent(t, fixture, fixture.parser, nil, nil)
	news := parser.teamBoundary()
	if !strings.Contains(news, "after its start") {
		t.Fatalf("a started member lost what the manager said after its start: %q", news)
	}
	if !strings.Contains(news, teamBriefWord+": the brief") {
		t.Fatalf("a started member was not handed its brief, marked as the manager's: %q", news)
	}
	if strings.Contains(news, "before its start") {
		t.Fatalf("a started member was handed history: %q", news)
	}
}

// AND DELIVERY IS A STEP BOUNDARY'S: a line the manager wrote reaches the
// member's next request, marked, and never as the person.
func TestDeliveredTrafficReachesTheNextRequestAsTheSessionsNote(t *testing.T) {
	fixture := newTeamFixture(t, true)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("on it"), nil },
	}}
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	web.teamBoundary()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "use the blue header"})
	events, err := web.Submit(context.Background(), "carry on")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	request := userTextIn(completer.request(0))
	if !strings.Contains(request, "◆ directive from manager: use the blue header") {
		t.Fatalf("the request did not carry the directive:\n%s", request)
	}
	web.mu.Lock()
	defer web.mu.Unlock()
	for _, message := range web.messages {
		if message.Role == "user" && strings.HasPrefix(messageText(message), "◆") {
			t.Fatal("the directive was recorded as a bare line, not under its team sentence")
		}
	}
}

// ── the digest ──────────────────────────────────────────────────────────────

func TestOnlyAManagerCarriesTheDigest(t *testing.T) {
	fixture := newTeamFixture(t, true)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToManager, Text: "header done"})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil },
	}}
	manager := teamAgent(t, fixture, fixture.manager, completer, nil)
	manager.refreshTeamDigest(context.Background())
	manager.mu.Lock()
	block := manager.teamDigestText
	manager.mu.Unlock()
	for _, want := range []string{`Team "harbor": 3 members, manager @boss.`, "@web", "@parser", "header done"} {
		if !strings.Contains(block, want) {
			t.Errorf("the manager's digest lacks %q:\n%s", want, block)
		}
	}
	events, err := manager.Submit(context.Background(), "how is the team?")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if request := userTextIn(completer.request(0)); !strings.Contains(request, teamNoteOpening) {
		t.Fatalf("the manager's request did not carry the team note:\n%s", request)
	}

	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.refreshTeamDigest(context.Background())
	web.mu.Lock()
	defer web.mu.Unlock()
	if web.teamDigestText != "" {
		t.Fatalf("a member was given a digest:\n%s", web.teamDigestText)
	}
}

func TestTheTeamNoteIsNeverThePersonsWords(t *testing.T) {
	if !isVolatileNote(teamNoteOpening + "\n\nanything") {
		t.Fatal("the team note would be drawn as something the person typed")
	}
}

// ── a member's state, off its journal ───────────────────────────────────────

func writeJournal(t *testing.T, path string, entries ...sessionEntry) {
	t.Helper()
	var lines []string
	for _, entry := range entries {
		if entry.Timestamp == "" {
			entry.Timestamp = time.Now().Format(time.RFC3339Nano)
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAMembersStateIsReadOffItsJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), placeTranscript)
	now := time.Now()
	call := func(id, name, args string) ai.ToolCall {
		return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: args}}
	}
	cases := []struct {
		name    string
		entries []sessionEntry
		state   string
		detail  string
	}{
		{"a turn that ended", []sessionEntry{
			{Type: "message", Role: "user", Content: "do it"},
			{Type: "message", Role: "assistant", Content: "done", ToolCalls: nil},
			{Type: "pace"},
		}, teams.StateIdle, ""},
		{"a turn under way", []sessionEntry{
			{Type: "pace"},
			{Type: "message", Role: "user", Content: "next"},
			{Type: "message", Role: "assistant", ToolCalls: []ai.ToolCall{call("c1", "edit", `{"path":"web/header.go"}`)}},
		}, teams.StateRunning, "web/header.go"},
		{"a question waiting", []sessionEntry{
			{Type: "message", Role: "user", Content: "next"},
			{Type: "message", Role: "assistant", ToolCalls: []ai.ToolCall{call("c2", "ask", `{"head":"Which colour for the header?"}`)}},
		}, teams.StateAsking, "Which colour for the header?"},
		{"a turn that failed", []sessionEntry{
			{Type: "message", Role: "user", Content: "next"},
			{Type: "error"},
		}, teams.StateFailed, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			writeJournal(t, path, c.entries...)
			state, ok := journalState(path, now)
			if !ok || state.State != c.state {
				t.Fatalf("state %+v (ok %v), want %s", state, ok, c.state)
			}
			if c.detail != "" && state.Question != c.detail && (len(state.Files) == 0 || state.Files[0] != c.detail) {
				t.Fatalf("state %+v does not carry %q", state, c.detail)
			}
		})
	}
}

func TestTeamReadIsBoundedAndSkipsTheSessionsOwnNotes(t *testing.T) {
	fixture := newTeamFixture(t, true)
	big := strings.Repeat("x", 5000)
	var entries []sessionEntry
	for index := 0; index < 60; index++ {
		entries = append(entries, sessionEntry{Type: "message", Role: "assistant", Content: big})
	}
	entries = append(entries,
		sessionEntry{Type: "message", Role: "user", Content: volatileNoteOpening + "\n\ncard"},
		sessionEntry{Type: "message", Role: "assistant", Content: "the newest line"})
	writeJournal(t, fixture.web, entries...)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	text, failed, _ := manager.teamReadTool(context.Background(), json.RawMessage(`{"handle":"web","messages":40}`))
	if failed {
		t.Fatalf("team_read failed: %s", text)
	}
	if len(text) > teamReadBytes+1024 {
		t.Fatalf("team_read handed back %d bytes, over its bound", len(text))
	}
	if !strings.Contains(text, "the newest line") {
		t.Fatal("team_read lost the newest message to the bound")
	}
	if strings.Contains(text, "card") {
		t.Fatal("team_read showed the session's own note as the person's words")
	}
}

// ── the gates the rest of the build keeps ───────────────────────────────────

func TestEveryTeamVerbHasAFamilyAndAManualPage(t *testing.T) {
	for _, name := range teamToolNames {
		if ActionCategoryForTool(name) == ActionWork {
			t.Errorf("%s has no family in actioncategory.go", name)
		}
		if !manual.Chat().Mentions(name) {
			t.Errorf("no chat manual page mentions %s", name)
		}
	}
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	if _, err := toolDefinitions(append(manager.managerTools(), manager.memberTools()...)); err != nil {
		t.Fatalf("a team verb's schema does not parse: %v", err)
	}
}
