package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ── THE CHAT DOOR ONTO WORK THAT RUNS ───────────────────────────────────────
//
// A person who says "keep an eye on my inbox folder and keep
// reports/inbox-report.md current" gets, after one card, the item the terminal
// makes with `aforge standing add --instructions … --report … --place …`. These
// tests hold the card's half of that: what it says before the yes, and what the
// yes writes. The run is the terminal's and the timer's, and the journey in
// internal/e2e (TestChatDoorJourney) drives it end to end.

// chatDoor is one conversation with a real standing store and a real folder
// database behind it, placed in no folder until a test places it.
type chatDoor struct {
	agent   *Agent
	store   *standing.Store
	org     *workspace.Store
	orgPath string
	project string
	chat    workspace.Ref
}

func newChatDoor(t *testing.T, completer Completer, mutate func(*Config)) *chatDoor {
	t.Helper()
	if completer == nil {
		completer = &scriptedCompleter{}
	}
	root := t.TempDir()
	store, err := standing.Open(filepath.Join(root, "standing"))
	if err != nil {
		t.Fatal(err)
	}
	orgPath := filepath.Join(root, "collections.db")
	org, err := workspace.Open(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = org.Close() })
	project := t.TempDir()
	place := Place{Dir: filepath.Join(root, "projects", "chat-inbox"), Workspace: project}
	if err := os.MkdirAll(place.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = project
		config.Place = place
		config.SessionFile = place.Transcript()
		config.Standing = &Standing{Store: store}
		config.Organization = &Organization{Path: orgPath}
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
		if mutate != nil {
			mutate(config)
		}
	})
	return &chatDoor{agent: agent, store: store, org: org, orgPath: orgPath, project: project, chat: workspace.Ref{Kind: workspace.ConversationKind, ID: place.ID()}}
}

func (d *chatDoor) folder(t *testing.T, name string) workspace.Collection {
	t.Helper()
	folder, err := d.org.Create(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return folder
}

// rule stands a folder rule the way `aforge standing add --hold --scope` does.
func (d *chatDoor) rule(t *testing.T, words, folder string) standing.Item {
	t.Helper()
	made, err := d.store.Create(standing.Item{
		Words: words, Workspace: d.project, When: standing.When{Kind: standing.WhenHold},
		Scope:    &standing.Scope{CollectionIDs: []string{folder}},
		Adoption: &standing.Adoption{Actor: "person", Via: standing.DoorTerminal},
	})
	if err != nil {
		t.Fatal(err)
	}
	return made
}

// inboxWork is the stand call a model makes for the journey's sentence.
func inboxWork(extra map[string]any) string {
	does := map[string]any{"kind": "task", "instructions": "Read the changed files in inbox/ and write a short report of new decisions and requests.", "report": "reports/inbox-report.md"}
	body := map[string]any{
		"op": "propose", "words": "keep an eye on my inbox folder and keep reports/inbox-report.md current",
		"when": map[string]any{"kind": "file", "glob": "inbox/*"}, "does": does, "when_words": "when inbox/* changes",
	}
	for key, value := range extra {
		if key == "does" {
			for field, v := range value.(map[string]any) {
				does[field] = v
			}
			continue
		}
		body[key] = value
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// proposeInbox runs one turn of the journey's sentence, answering the card
// with answer, and hands back the card and the turn's events.
func (d *chatDoor) proposeInbox(t *testing.T, answer func(Event)) (StandingNotice, []Event) {
	t.Helper()
	events, err := d.agent.Submit(context.Background(), "keep an eye on my inbox folder and keep reports/inbox-report.md current")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainAnsweringStanding(t, events, answer)
	card, found := firstOfKind(collected, EventStandingProposal)
	if !found {
		t.Fatalf("no card was drawn; the tool said %q", toolOutput(t, collected, "stand"))
	}
	return *card.Standing, collected
}

func (d *chatDoor) yes(event Event) {
	d.agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
}

func (d *chatDoor) only(t *testing.T) standing.Item {
	t.Helper()
	items, err := d.store.ForWorkspace(d.project)
	if err != nil {
		t.Fatal(err)
	}
	var work []standing.Item
	for _, item := range items {
		if item.When.Kind != standing.WhenHold {
			work = append(work, item)
		}
	}
	if len(work) != 1 {
		t.Fatalf("%d pieces of work stand, want 1", len(work))
	}
	return work[0]
}

// THE WORK IS SPELLED `instructions`, ONCE. The schema the model reads names the
// work, the report and the placement, and never the retired `brief`: a second
// spelling of one concept is two things to learn for one idea.
func TestTheStandSchemaSpellsTheWorkInstructionsOnce(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(standSchemaJSON), &schema); err != nil {
		t.Fatal(err)
	}
	var does struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema.Properties["does"], &does); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"instructions", "report"} {
		if _, ok := does.Properties[field]; !ok {
			t.Errorf("does.%s is missing from the schema", field)
		}
	}
	if _, ok := does.Properties["brief"]; ok {
		t.Error("does.brief is still in the schema; the one spelling is does.instructions")
	}
	if _, ok := schema.Properties["placement"]; !ok {
		t.Error("placement is missing from the schema")
	}
	if strings.Contains(standSchemaJSON, "brief") {
		t.Errorf("the schema still says brief somewhere: %s", standSchemaJSON)
	}
}

// A CALL STILL SAYING `brief` IS TOLD `instructions`, AND NOTHING IS PROPOSED. A
// conversation resumed from before the rename must not have its work dropped
// and then be refused for a field it believes it sent.
func TestAStandCallThatStillSaysBriefIsToldInstructions(t *testing.T) {
	call := `{"op":"propose","words":"every night tidy the tests","when":{"kind":"every","every":"0 2 * * *"},"does":{"kind":"task","brief":"tidy the flaky tests"}}`
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", call), finalText("no")}}, nil)
	events, err := d.agent.Submit(context.Background(), "every night tidy the tests")
	if err != nil {
		t.Fatal(err)
	}
	collected := drainAnsweringStanding(t, events, func(Event) { t.Fatal("a card was drawn for a refused call") })
	if out := toolOutput(t, collected, "stand"); !strings.Contains(out, standingRetiredBrief) {
		t.Fatalf("the refusal = %q, want it to name does.instructions", out)
	}
}

// THE CARD SAYS WHAT A YES AGREES TO, AND THE YES WRITES EXACTLY THAT. The
// conversation is placed in Launch, a rule is placed on Launch, and nobody
// names a folder: the card says the work goes where the conversation is, that
// aforge writes the report, and quotes the Launch rule — read the way the run
// will read it. The yes makes the terminal's item: instructions version 1, the
// report, the placement, a receipt that says it came through the chat.
func TestAChatCardForWorkThatRunsSaysWhatAYesAgreesTo(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	ctx := context.Background()
	launch := d.folder(t, "Launch")
	marketing := d.folder(t, "Marketing")
	if err := d.org.AddPlacement(ctx, launch.ID, d.chat); err != nil {
		t.Fatal(err)
	}
	launchRule := d.rule(t, "RULE-LAUNCH-7: inbox reports never quote email addresses.", launch.ID)
	d.rule(t, "RULE-MKT-9: marketing notes cite the spec.", marketing.ID)

	card, events := d.proposeInbox(t, d.yes)
	want := []string{
		"does · Read the changed files in inbox/ and write a short report of new decisions and requests.",
		"report · reports/inbox-report.md — aforge publishes this file; the run never writes it",
		"folder · Launch, where this conversation is placed — its rules reach every run",
		"rule · RULE-LAUNCH-7: inbox reports never quote email addresses.",
	}
	if !reflect.DeepEqual(card.Terms, want) {
		t.Fatalf("the card's terms:\n%s\nwant:\n%s", strings.Join(card.Terms, "\n"), strings.Join(want, "\n"))
	}

	item := d.only(t)
	if item.Does.Kind != standing.ActionTask || item.Does.Brief != "Read the changed files in inbox/ and write a short report of new decisions and requests." || item.Does.Report != "reports/inbox-report.md" {
		t.Fatalf("the item's work = %+v", item.Does)
	}
	if item.SpecRevision != 1 || item.When.Kind != standing.WhenFile || item.When.Glob != "inbox/*" {
		t.Fatalf("the item = spec %d when %+v", item.SpecRevision, item.When)
	}
	if a := item.Adoption; a == nil || a.Actor != "person" || a.Via != standing.DoorChat || a.ProposalID == 0 {
		t.Fatalf("the receipt = %+v, want the person through the chat", item.Adoption)
	}
	placed, err := d.org.GoverningCollections(ctx, workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})
	if err != nil || len(placed) != 1 || placed[0].ID != launch.ID || placed[0].Depth != 0 {
		t.Fatalf("the work is placed in %v (%v), want Launch", placed, err)
	}
	// AND THE RULES THE CARD QUOTED ARE THE RULES THE RUN WILL READ, from the
	// placement the yes wrote rather than the one the card imagined.
	collections := map[string]int{}
	nearestDepths(collections, placed)
	applies, err := d.store.ApplicableScope(item.Workspace, item.Origin.SessionID, collections)
	if err != nil {
		t.Fatal(err)
	}
	if rules := GoverningRules(applies); len(rules) != 1 || rules[0].ID != launchRule.ID {
		t.Fatalf("the run would read %v, the card quoted the Launch rule", rules)
	}
	raw, _ := os.ReadFile(d.store.LogPath(item.ID))
	if log := string(raw); !strings.Contains(log, "set up in the chat") || !strings.Contains(log, "placed in folder "+launch.ID) {
		t.Fatalf("the item's log does not say which door or folder:\n%s", log)
	}
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "report: reports/inbox-report.md — "+standingReportWho) || !strings.Contains(out, "placed in: Launch") {
		t.Fatalf("the model was not told where the report goes or where the work is placed: %q", out)
	}
}

// A FOLDER NAMED WINS, AND NO FOLDER IS ALLOWED. The interaction may precede its
// organization: work said in a conversation that is in no folder is in none,
// and the card says so plainly rather than demanding one first.
func TestANamedFolderWinsAndNoFolderIsAllowed(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	card, _ := d.proposeInbox(t, d.yes)
	if got := card.Terms[2:]; !reflect.DeepEqual(got, []string{"folder · none — it can be placed in one later", "rules · none reach this work yet"}) {
		t.Fatalf("an unfiled card says %q", got)
	}
	item := d.only(t)
	if placed, _ := d.org.GoverningCollections(context.Background(), workspace.Ref{Kind: workspace.StandingKind, ID: item.ID}); len(placed) != 0 {
		t.Fatalf("unfiled work was placed in %v", placed)
	}

	named := newChatDoor(t, nil, nil)
	launch, marketing := named.folder(t, "Launch"), named.folder(t, "Marketing")
	if err := named.org.AddPlacement(context.Background(), launch.ID, named.chat); err != nil {
		t.Fatal(err)
	}
	named.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": marketing.ID})), finalText("set up")}}
	card, _ = named.proposeInbox(t, named.yes)
	if card.Terms[2] != "folder · Marketing — its rules reach every run" {
		t.Fatalf("a named folder reads %q", card.Terms[2])
	}
	work := named.only(t)
	if placed, _ := named.org.GoverningCollections(context.Background(), workspace.Ref{Kind: workspace.StandingKind, ID: work.ID}); len(placed) != 1 || placed[0].ID != marketing.ID {
		t.Fatalf("named work was placed in %v, want Marketing only", placed)
	}
}

// WHAT A CARD COULD NEVER KEEP IS REFUSED BEFORE THE CARD. A report inside its
// own watch, a report folder that leads out of the project through a link, a
// folder that does not exist, and a folder for a line to say are each refused
// in the model's grammar, and no card is drawn for any of them.
func TestWhatACardCouldNeverKeepIsRefusedBeforeTheCard(t *testing.T) {
	outside := t.TempDir()
	for _, c := range []struct {
		name  string
		call  func(project string) string
		setup func(t *testing.T, project string)
		want  string
	}{
		{"a report inside its own watch", func(string) string {
			return inboxWork(map[string]any{"does": map[string]any{"report": "inbox/report.md"}})
		}, nil, "would be one of the files it watches"},
		{"a report folder linked out of the project", func(string) string {
			return inboxWork(map[string]any{"does": map[string]any{"report": "escape/report.md"}})
		}, func(t *testing.T, project string) {
			if err := os.Symlink(outside, filepath.Join(project, "escape")); err != nil {
				t.Fatal(err)
			}
		}, "does.report the report folder resolves outside the project"},
		{"a folder that does not exist", func(string) string {
			return inboxWork(map[string]any{"placement": "no-such-folder"})
		}, nil, "folder not found: no-such-folder"},
		{"a folder for a line to say", func(string) string {
			return `{"op":"propose","words":"tell me when inbox changes","when":{"kind":"file","glob":"inbox/*"},"does":{"kind":"say","say":"inbox changed"},"placement":"x"}`
		}, nil, "placement is for work that runs"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := newChatDoor(t, nil, nil)
			if c.setup != nil {
				c.setup(t, d.project)
			}
			d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", c.call(d.project)), finalText("no")}}
			events, err := d.agent.Submit(context.Background(), "set it up")
			if err != nil {
				t.Fatal(err)
			}
			collected := drainAnsweringStanding(t, events, func(Event) { t.Fatal("a card was drawn for work that could never keep it") })
			if out := toolOutput(t, collected, "stand"); !strings.Contains(out, c.want) {
				t.Fatalf("the refusal = %q, want %q", out, c.want)
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Fatalf("something landed outside the project: %v", entries)
			}
		})
	}
}

// A PLACEMENT THAT DID NOT TAKE IS A YES THAT DID NOT TAKE. The person agreed to
// work under the Launch rule, and the folder is bound BEFORE the work exists, so
// a placement that cannot be written leaves no work at all — not work running
// outside the rules the card quoted — and the conversation says nothing was set
// up.
func TestAPlacementThatDidNotTakeLeavesNothingRunning(t *testing.T) {
	d := newChatDoor(t, nil, nil)
	launch := d.folder(t, "Launch")
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": launch.ID})), finalText("no")}}
	_, events := d.proposeInbox(t, func(event Event) {
		// The folder database goes away between the card and the yes.
		d.agent.config.Organization.Path = filepath.Join(t.TempDir(), "gone.db")
		d.yes(event)
	})
	if out := toolOutput(t, events, "stand"); !strings.HasPrefix(out, "nothing was set up: could not be placed in Launch") {
		t.Fatalf("the tool said %q", out)
	}
	if items, _ := d.store.ForWorkspace(d.project); len(items) != 0 {
		t.Fatalf("work stands although its folder could not be bound: %+v", items)
	}
}

// watchedCreate is the real store with its Create observed: it runs before
// the item is written, so a test can see what was already true at that moment.
type watchedCreate struct {
	*standing.Store
	before func(standing.Item)
}

func (w watchedCreate) Create(item standing.Item) (standing.Item, error) {
	w.before(item)
	return w.Store.Create(item)
}

// THE FOLDER IS BOUND BEFORE THE WORK EXISTS. At the moment the item is
// written, its id is already placed in the folder the card named, so there is
// no moment — and no crash between two writes — in which the work stands
// outside the rules the person was shown.
func TestTheFolderIsBoundBeforeTheWorkExists(t *testing.T) {
	d := newChatDoor(t, nil, nil)
	launch := d.folder(t, "Launch")
	var atCreate []workspace.GoverningCollection
	d.agent.config.standingItems = watchedCreate{Store: d.store, before: func(item standing.Item) {
		atCreate, _ = d.org.GoverningCollections(context.Background(), workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})
	}}
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": launch.ID})), finalText("set up")}}
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	if len(atCreate) != 1 || atCreate[0].ID != launch.ID {
		t.Fatalf("when item %s was written its folders were %v, want Launch already bound", item.ID, atCreate)
	}
}

// NO TIMER HERE IS SAID, WITH WHAT DOES CHECK IT. On a host with no timer to
// install, work that wakes is checked only while a window is open or when the
// person runs a check, and the conversation says so under the card. A rule
// never wakes and says nothing.
func TestAHostWithNoTimerSaysHowChecksHappen(t *testing.T) {
	store := newFakeStanding(t)
	events := standRatify(t, store, nil, t.TempDir())
	if line := backgroundLine(events); line != standingNoTimerLine {
		t.Fatalf("a host with no timer said %q, want %q", line, standingNoTimerLine)
	}
	// AND THE MODEL IS TOLD THE SAME, so its one line cannot promise checks
	// the row under the card just said there are none of.
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "\n"+standingNoTimerLine+"\n") {
		t.Fatalf("the model was not told there is no timer: %q", out)
	}
	if !strings.Contains(standingNoTimerLine, "aforge standing check") {
		t.Fatalf("the line does not say how the person runs a check: %q", standingNoTimerLine)
	}
	hold := `{"op":"propose","words":"never touch the public API","when":{"kind":"hold"}}`
	agent := standingAgent(t, &scriptedCompleter{steps: []step{standCall("s1", hold), finalText("set up")}}, store, nil)
	held, err := agent.Submit(context.Background(), "never touch the public API")
	if err != nil {
		t.Fatal(err)
	}
	if line := backgroundLine(drainAnsweringStanding(t, held, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})); line != "" {
		t.Fatalf("a rule said something about checks: %q", line)
	}
}

// refusingSystemd is a Linux login with no user service manager to talk to —
// a headless box, a container, an ssh session without lingering — which is the
// ordinary way the real timer fails to start on Linux.
type refusingSystemd struct{ calls []string }

func (r *refusingSystemd) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return errors.New(`exec: "systemctl": executable file not found in $PATH`)
}

// THE REAL LINUX TIMER THAT WOULD NOT START SAYS SO, AND NOTHING IS LEFT BEHIND.
// This is the production timer ([standing.NewWatch]) on the Linux arm with only
// the process boundary replaced: the conversation is told the install did not
// take, why, and what checks the work meanwhile — never the success line — and
// the unit files it wrote are removed again.
func TestALinuxTimerThatWouldNotStartSaysSoAndLeavesNothing(t *testing.T) {
	home, systemd := t.TempDir(), &refusingSystemd{}
	watch, err := standing.NewWatch(standing.WatchOptions{
		Platform: "linux", HomeDir: home, Executable: filepath.Join(home, "aforge"), StateRoot: t.TempDir(), UID: 1000, Runner: systemd,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := newFakeStanding(t)
	events := standRatify(t, store, watch, t.TempDir())
	line := backgroundLine(events)
	if line == standingBackgroundLine || !strings.HasPrefix(line, standingBackgroundFailed) ||
		!strings.Contains(line, "systemctl") || !strings.Contains(line, standingChecksHow) {
		t.Fatalf("a timer that never started said %q", line)
	}
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, line) || strings.Contains(out, standingBackgroundLine) {
		t.Fatalf("the model was told something other than the failed install: %q", out)
	}
	if len(systemd.calls) == 0 || !strings.HasPrefix(systemd.calls[0], "systemctl --user") {
		t.Fatalf("the Linux arm never asked systemd: %v", systemd.calls)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".config", "systemd", "user")); len(entries) != 0 {
		t.Fatalf("a timer that did not start left its units behind: %v", entries)
	}
	if status, err := watch.Status(); err != nil || status.Installed {
		t.Fatalf("the timer reads installed=%v (%v) after a refused install", status.Installed, err)
	}
}

// A WATCH SAYS WHEN IT WAKES EVEN WHEN THE MODEL SAID NOTHING. The first live
// run of the chat door (2026-09-11) proposed a file watch with no when_words,
// and its card and record said nothing about when it wakes; the terminal
// always writes `when inbox/* changes`. The fallback is that one spelling, and
// the model's own words still win.
func TestAWatchWithNoWordsOfItsOwnSaysWhenItWakes(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"when_words": ""})), finalText("set up")}}, nil)
	card, _ := d.proposeInbox(t, d.yes)
	if card.WhenWords != "when inbox/* changes" || d.only(t).When.Words != standing.WatchWords("inbox/*") {
		t.Fatalf("a watch with no words of its own says %q (item %q)", card.WhenWords, d.only(t).When.Words)
	}
	said := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"when_words": "whenever something lands in my inbox"})), finalText("set up")}}, nil)
	if card, _ := said.proposeInbox(t, said.yes); card.WhenWords != "whenever something lands in my inbox" {
		t.Fatalf("the model's own words were replaced: %q", card.WhenWords)
	}
}
