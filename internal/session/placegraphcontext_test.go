package session

// THE PLACES A CONVERSATION IS FILED UNDER HAVE TO REACH THE MODEL, AND ONLY AT
// A TURN'S OPENING.
//
// Every assertion here reads a REQUEST the model was sent (or message[0] as the
// next one will carry it), never placeGraphText: a block composed and never
// rendered would pass every check and reach nobody.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

type placeFixture struct {
	store   *placegraph.Store
	path    string
	choices string
}

func newPlaceFixture(t *testing.T) placeFixture {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	store, err := placegraph.Open(placegraph.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	return placeFixture{store: store, path: path, choices: filepath.Join(dir, "place-choices.json")}
}

func (f placeFixture) place(t *testing.T, name string, ctx placegraph.Context, parents ...string) placegraph.Place {
	t.Helper()
	p, _, err := f.store.CreatePlace(placegraph.NewPlace{Name: name, Parents: parents, Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f placeFixture) file(t *testing.T, chat string, p placegraph.Place) {
	t.Helper()
	if _, _, err := f.store.AddChat(chat, p.ID, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
}

// placedAgent is a conversation on disk whose desktop place graph is f.
func placedAgent(t *testing.T, f placeFixture, completer Completer) (*Agent, string) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.SessionFile = journal
		c.PlaceGraph = &PlaceGraphDoor{Path: f.path, ChoicesPath: f.choices, Sources: placegraph.SourcePolicy{Deny: []string{}}}
	})
	return agent, journal
}

// turn runs one person's turn to its end and answers the index of the first
// request it sent.
func turn(t *testing.T, agent *Agent, completer *scriptedCompleter, text string) int {
	t.Helper()
	first := completer.requests()
	collect(t, mustSubmit(t, agent, text))
	if completer.requests() == first {
		t.Fatal("the turn sent no request")
	}
	return first
}

func requestSystem(messages []ai.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messageText(messages[0])
}

func TestPlaceGraphBlockRendersNothingForAnUnplacedChat(t *testing.T) {
	f := newPlaceFixture(t)
	f.place(t, "Software", placegraph.Context{Instructions: "never shown"})
	completer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil }}}
	agent, _ := placedAgent(t, f, completer)
	at := turn(t, agent, completer, "hello")
	if sys := requestSystem(completer.request(at)); strings.Contains(sys, placeGraphHeading) || strings.Contains(sys, "never shown") {
		t.Fatalf("an unplaced conversation was told about places:\n%s", sys)
	}
	// And a conversation with no door at all reads nothing.
	plain, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if strings.Contains(modelSees(t, plain), placeGraphHeading) {
		t.Fatal("no door, yet a places block")
	}
}

func TestPlaceInstructionsAreQuotedPerPlaceNeverBlended(t *testing.T) {
	f := newPlaceFixture(t)
	codeaf := f.place(t, "codeaf", placegraph.Context{})
	parser := f.place(t, "Config parser", placegraph.Context{Instructions: "Keep strict mode the default for public APIs"}, codeaf.ID)
	release := f.place(t, "Release", placegraph.Context{Instructions: "Write for customers, not engineers"}, codeaf.ID)
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	f.file(t, agent.id, parser)
	f.file(t, agent.id, release)

	sys := requestSystem(completer.request(turn(t, agent, completer, "write the notes")))
	for _, want := range []string{
		placeGraphHeading,
		"- Config parser — filed here",
		"- codeaf — inherited through Config parser, Release",
		"## Instructions from Config parser",
		"## Instructions from Release",
		"nearest shared place above",
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("missing %q in:\n%s", want, sys)
		}
	}
	strict := strings.Index(sys, "Keep strict mode")
	customers := strings.Index(sys, "Write for customers")
	between := strings.Index(sys, "## Instructions from Release")
	if !(strict < between && between < customers) {
		t.Fatal("each place's words must sit under that place's own heading")
	}
	// The places block sits between the base prompt and anything after it.
	if !strings.HasPrefix(sys, "SYSTEM") {
		t.Fatalf("the base prompt must lead message[0]:\n%s", sys[:min(len(sys), 200)])
	}
}

func TestPlaceGraphBlockRecomposesOnlyWhenGenerationMoves(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Software", placegraph.Context{Instructions: "rule one"})
	other := f.place(t, "Elsewhere", placegraph.Context{})
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	f.file(t, agent.id, p)

	first := requestSystem(completer.request(turn(t, agent, completer, "one")))
	second := requestSystem(completer.request(turn(t, agent, completer, "two")))
	if first != second || !strings.Contains(first, "rule one") {
		t.Fatalf("an unchanged graph must leave message[0] byte for byte:\n%s\n---\n%s", first, second)
	}
	// A commit that does not touch what this conversation uses moves the file
	// and is read, and still changes not one byte.
	if _, err := f.store.Rename(other.ID, "Elsewhere still"); err != nil {
		t.Fatal(err)
	}
	if third := requestSystem(completer.request(turn(t, agent, completer, "three"))); third != first {
		t.Fatal("an unrelated rename re-priced the conversation")
	}
	if _, err := f.store.SetContext(p.ID, placegraph.Context{Instructions: "rule two"}); err != nil {
		t.Fatal(err)
	}
	fourth := requestSystem(completer.request(turn(t, agent, completer, "four")))
	if !strings.Contains(fourth, "rule two") || strings.Contains(fourth, "rule one") {
		t.Fatalf("a changed instruction must reach the next turn:\n%s", fourth)
	}
}

func TestTheChildReReadsOnlyWhenTheGenerationMoves(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Software", placegraph.Context{Instructions: "rule one"})
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	f.file(t, agent.id, p)

	first := requestSystem(completer.request(turn(t, agent, completer, "one")))
	if !strings.Contains(first, "rule one") {
		t.Fatalf("missing the instruction:\n%s", first)
	}
	gen := agent.placeGraphStamp.generation
	if gen == 0 {
		t.Fatal("the turn did not record a generation")
	}
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "rule one") {
		t.Fatal("the instruction is not in the file")
	}
	// A rewrite that leaves the revision alone is not a commit. The child
	// compares the generation, so the model keeps the block it already has.
	swapped := strings.Replace(string(raw), "rule one", "rule two", 1)
	if err := os.WriteFile(f.path, []byte(swapped), 0o600); err != nil {
		t.Fatal(err)
	}
	second := requestSystem(completer.request(turn(t, agent, completer, "two")))
	if strings.Contains(second, "rule two") || !strings.Contains(second, "rule one") {
		t.Fatalf("a rewrite that left the generation alone reached the model:\n%s", second)
	}
	if agent.placeGraphStamp.generation != gen {
		t.Fatalf("generation moved from %d to %d without a commit", gen, agent.placeGraphStamp.generation)
	}
	if _, err := f.store.SetContext(p.ID, placegraph.Context{Instructions: "rule three"}); err != nil {
		t.Fatal(err)
	}
	third := requestSystem(completer.request(turn(t, agent, completer, "three")))
	if !strings.Contains(third, "rule three") || strings.Contains(third, "rule one") {
		t.Fatalf("a commit must reach the next turn:\n%s", third)
	}
	if agent.placeGraphStamp.generation <= gen {
		t.Fatal("a commit did not move the generation the child compares")
	}
}

func TestAPlaceAddedMidTurnAppliesFromTheNextTurn(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Marketing", placegraph.Context{Instructions: "Write in the brand voice"})
	var agent *Agent
	completer := &scriptedCompleter{}
	completer.steps = []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			// The person files the conversation while the reply is running.
			f.file(t, agent.id, p)
			return toolResponse("c1", "read", `{"path":"nothing-here.txt"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}
	agent, _ = placedAgent(t, f, completer)
	at := turn(t, agent, completer, "go")
	for i := at; i < completer.requests(); i++ {
		if strings.Contains(requestSystem(completer.request(i)), "brand voice") {
			t.Fatalf("request %d of the running turn changed its instructions mid-turn", i)
		}
	}
	next := completer.request(turn(t, agent, completer, "again"))
	if !strings.Contains(requestSystem(next), "Write in the brand voice") {
		t.Fatalf("the next turn does not carry the new place:\n%s", requestSystem(next))
	}
}

func TestTheChangeLineIsJournaledAndReplays(t *testing.T) {
	f := newPlaceFixture(t)
	dir := t.TempDir()
	brand := filepath.Join(dir, "brand-voice.md")
	if err := os.WriteFile(brand, []byte("short sentences"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := f.place(t, "Config parser", placegraph.Context{})
	release := f.place(t, "Release", placegraph.Context{Sources: []placegraph.Source{{ID: "s1", Kind: placegraph.SourceFile, Ref: brand, Label: "brand-voice.md", AddedBy: placegraph.AddedByYou}}})
	completer := &scriptedCompleter{}
	agent, journal := placedAgent(t, f, completer)
	f.file(t, agent.id, parser)
	turn(t, agent, completer, "first")

	_, receipt, err := f.store.AddChat(agent.id, release.ID, placegraph.AddedByYou)
	if err != nil {
		t.Fatal(err)
	}
	at := turn(t, agent, completer, "second")
	const line = "Now also using Release: brand-voice.md"
	seen := false
	for _, m := range completer.request(at) {
		if m.Role == "user" && strings.Contains(messageText(m), line) {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("the model was not told what changed: %v", messageTexts(completer.request(at)))
	}
	sys := requestSystem(completer.request(at))
	// A temporary directory may live under a repository on the runner, so
	// repository facts can sit between the source kind and its attribution.
	sourceNamed := false
	for _, row := range strings.Split(sys, "\n") {
		if strings.HasPrefix(row, "- "+brand+" — a file") && strings.HasSuffix(row, "; from Release") {
			sourceNamed = true
		}
	}
	if !sourceNamed {
		t.Fatalf("the source is not named with its place:\n%s", sys)
	}
	find := func(where string, entries []DisplayEntry) {
		t.Helper()
		for _, e := range entries {
			if e.Role == "aside" && e.AsideKind == NoteKindPlaces && strings.Contains(e.Text, line) {
				if len(e.UndoReceipts) != 1 || e.UndoReceipts[0] != receipt.ID {
					t.Fatalf("%s wrong undo provenance: %v", where, e.UndoReceipts)
				}
				return
			}
		}
		t.Fatalf("%s: no places line in %#v", where, entries)
	}
	find("live", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	find("reopened", reopen(t, journal).Transcript())
	find("detached", ReadTranscript(journal).Entries)
}

func TestARemovedPlaceSaysSoAndLeavesThePrompt(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Software", placegraph.Context{Instructions: "rule"})
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	f.file(t, agent.id, p)
	turn(t, agent, completer, "one")
	if _, err := f.store.RemoveChat(agent.id, p.ID); err != nil {
		t.Fatal(err)
	}
	at := turn(t, agent, completer, "two")
	if sys := requestSystem(completer.request(at)); strings.Contains(sys, placeGraphHeading) {
		t.Fatalf("an unfiled conversation still carries the place:\n%s", sys)
	}
	if !strings.Contains(strings.Join(messageTexts(completer.request(at)), "\n"), "No longer using Software") {
		t.Fatal("the removal was not said")
	}
}

func TestADamagedGraphKeepsWhatTheConversationHad(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Software", placegraph.Context{Instructions: "kept rule"})
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	f.file(t, agent.id, p)
	turn(t, agent, completer, "one")
	if err := os.WriteFile(f.path, []byte("{half a save"), 0o600); err != nil {
		t.Fatal(err)
	}
	if sys := requestSystem(completer.request(turn(t, agent, completer, "two"))); !strings.Contains(sys, "kept rule") {
		t.Fatal("a damaged file must not take the conversation's places away")
	}
	if data, _ := os.ReadFile(f.path); string(data) != "{half a save" {
		t.Fatal("the engine must never repair or move the graph file")
	}
}

func TestPlaceSourcesAreReferencesAndTheBudgetIsSaid(t *testing.T) {
	f := newPlaceFixture(t)
	var sources []placegraph.Source
	for i := range placegraph.ContextSourceBudget + 2 {
		sources = append(sources, placegraph.Source{ID: "u" + string(rune('a'+i)), Kind: placegraph.SourceURL, Ref: "https://docs.example/" + string(rune('a'+i)), AddedBy: placegraph.AddedByYou})
	}
	folder := t.TempDir()
	sources = append([]placegraph.Source{{ID: "f", Kind: placegraph.SourceFolder, Ref: folder, AddedBy: placegraph.AddedByYou}}, sources...)
	p := f.place(t, "Docs", placegraph.Context{Sources: sources})
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	workspace := agent.config.Workspace
	f.file(t, agent.id, p)
	sys := requestSystem(completer.request(turn(t, agent, completer, "go")))
	for _, want := range []string{
		"## Sources these places give",
		folder + " — a folder; from Docs",
		"https://docs.example/a — a web page, named here and not fetched; from Docs",
		"REFERENCES AND NOT THE WORKING DIRECTORY: " + workspace,
		"3 more sources from these places are left out: one conversation is given at most 12.",
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("missing %q in:\n%s", want, sys)
		}
	}
	if agent.config.Workspace != workspace {
		t.Fatal("a place folder moved the working directory")
	}
	if places := agent.Places(); len(places) != 0 {
		t.Fatalf("a place's folder was written into the attached set: %+v", places)
	}
}

func TestPlaceGraphUsingIsWhatTheModelWasGiven(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Software", placegraph.Context{Instructions: "rule"})
	completer := &scriptedCompleter{}
	agent, _ := placedAgent(t, f, completer)
	f.file(t, agent.id, p)
	turn(t, agent, completer, "go")
	snap, err := f.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	using := PlaceGraphUsing(snap, agent.id, nil, placegraph.SourcePolicy{Deny: []string{}})
	agent.mu.Lock()
	rendered := agent.placeGraphText
	agent.mu.Unlock()
	if placeGraphBlock(using, agent.config.Workspace) != rendered || using.Counts.Places != 1 {
		t.Fatalf("the popover and the prompt disagree:\n%+v\n%s", using, rendered)
	}
}
