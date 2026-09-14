package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
				if v == nil {
					delete(does, field) // a field the call leaves out
					continue
				}
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
	applies, err := d.store.ApplicableScope(item.Workspace, item.Origin.SessionID, placementDepths(nearestPlaces(nil, placed)))
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
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "\nreport · reports/inbox-report.md — "+standingReportWho+"\n") || !strings.Contains(out, "\nfolder · Launch, where this conversation is placed") {
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
// always writes `when inbox/* changes`. That one spelling is now the only one:
// the model's own words used to win, until a card drew words naming a folder the
// pattern never reached (W5-B, [TestAFileWatchIsSaidFromItsPatternNotTheModelsWords]).
func TestAWatchWithNoWordsOfItsOwnSaysWhenItWakes(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"when_words": ""})), finalText("set up")}}, nil)
	card, _ := d.proposeInbox(t, d.yes)
	if card.WhenWords != "when inbox/* changes" || d.only(t).When.Words != standing.WatchWords("inbox/*") {
		t.Fatalf("a watch with no words of its own says %q (item %q)", card.WhenWords, d.only(t).When.Words)
	}
	said := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"when_words": "whenever something lands in my inbox"})), finalText("set up")}}, nil)
	if card, _ := said.proposeInbox(t, said.yes); card.WhenWords != "when inbox/* changes" {
		t.Fatalf("a watch is said by the model's words instead of its pattern: %q", card.WhenWords)
	}
}

// ── what the measurement of ten live runs found (2026-09-11) ────────────────

// A FILE THE PERSON NAMED IS ASKED ABOUT AS THE REPORT. One live run in ten set
// "keep reports/inbox-report.md current" up as work that keeps nothing: the
// call left does.report out, and the card, truthfully, promised no file. Now the
// call is refused with the field named and both honest answers; the path makes
// the terminal's item, and an explicit "" makes work that keeps no file — which
// the card then says in so many words.
func TestAFileTheSentenceNamesIsAskedAboutAsTheReport(t *testing.T) {
	noReport := inboxWork(map[string]any{"does": map[string]any{"report": nil}})
	d := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", noReport),
		standCall("s2", inboxWork(nil)),
		finalText("set up"),
	}}, nil)
	card, events := d.proposeInbox(t, d.yes)
	refusal := `Invalid arguments: their sentence names reports/inbox-report.md — send does.report "reports/inbox-report.md" if each run keeps that file current, or does.report "" if the work only reads it`
	if out := toolOutputs(events, "stand"); len(out) != 2 || !strings.HasPrefix(out[0], refusal+"\n") {
		t.Fatalf("the stand results were %q, want the refusal first:\n%s", out, refusal)
	}
	if card.Terms[1] != "report · reports/inbox-report.md — aforge publishes this file; the run never writes it" || d.only(t).Does.Report != "reports/inbox-report.md" {
		t.Fatalf("the retried call's card reads %q", card.Terms[1])
	}

	reads := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"does": map[string]any{"report": ""}})),
		finalText("set up"),
	}}, nil)
	card, _ = reads.proposeInbox(t, reads.yes)
	if card.Terms[1] != "report · none — no file is kept current" || reads.only(t).Does.Report != "" {
		t.Fatalf("work that keeps no file reads %q", card.Terms[1])
	}
}

// A WORD IS A FILE ONLY WHEN IT READS AS ONE, and the file the watch itself
// reaches is what wakes the work, never its report.
func TestOnlyAFileOutsideTheWatchIsAskedAbout(t *testing.T) {
	for word, want := range map[string]bool{
		"reports/inbox-report.md": true, "notes.txt": true, "data/2026.csv": true, "a.md": true,
		"e.g": false, "i.e": false, "x.c": false, "v1.2": false, "3.5": false, "inbox/*.md": false, "https://example.com/a.md": false, "inbox": false,
	} {
		if got := standingLooksLikeFile(word); got != want {
			t.Errorf("standingLooksLikeFile(%q) = %v, want %v", word, got, want)
		}
	}
	d := newChatDoor(t, nil, nil)
	asked := func(words string) string {
		var parsed standArguments
		if err := json.Unmarshal([]byte(inboxWork(map[string]any{"words": words, "does": map[string]any{"report": nil}})), &parsed); err != nil {
			t.Fatal(err)
		}
		item, problem := d.agent.standingItem(parsed, time.Now())
		if problem != "" {
			t.Fatal(problem)
		}
		return standingNamedReport(parsed, item)
	}
	// NONE OF THESE COULD BE THE REPORT, by the report's own law: a file the
	// watch reaches, one in a folder it watches, a home path, an absolute path
	// and one out of the project.
	if problem := asked("keep an eye on inbox/today.md and inbox/sub/notes.md, e.g. ~/notes.md /etc/hosts.txt ../elsewhere.md"); problem != "" {
		t.Fatalf("a file that could never be the report was asked about: %q", problem)
	}
	// AND EVERY FILE THAT COULD BE IS LISTED, once each.
	want := `Invalid arguments: their sentence names reports/a.md, notes/b.md — send does.report with the one each run keeps current, or does.report "" if the work only reads them`
	if problem := asked("keep reports/a.md and notes/b.md current, reports/a.md first"); problem != want {
		t.Fatalf("two named files = %q, want %q", problem, want)
	}
}

// LIMITS THE PERSON DID NOT NAME ARE DROPPED, AND THE CARD SAYS WHAT BINDS.
// Eight of ten live calls sent spending limits nobody asked for ($0.50 a run,
// 24 runs a day) and no cost_words, and the card's costs line was empty: the
// person agreed to limits they never saw. Unnamed limits now fall back to the
// quiet defaults; a named one is kept and the card states it from the item.
func TestLimitsThePersonDidNotNameAreDroppedAndTheCardSaysWhatBinds(t *testing.T) {
	unasked := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"rails": map[string]any{"per_run_usd": 0.5, "max_per_day": 24}})),
		finalText("set up"),
	}}, nil)
	card, events := unasked.proposeInbox(t, unasked.yes)
	if rails := unasked.only(t).Rails; rails.PerRunUSD != standDefaultPerRunUSD || rails.MaxPerDay != standDefaultMaxPerDay {
		t.Fatalf("unnamed limits stood: %+v", rails)
	}
	defaults := fmt.Sprintf("up to $%.2f a run (the default) · at most %d runs a day (the default) · shares the day's allowance", standDefaultPerRunUSD, standDefaultMaxPerDay)
	if card.CostWords != defaults {
		t.Fatalf("the card's costs line = %q, want %q", card.CostWords, defaults)
	}
	out := toolOutput(t, events, "stand")
	if !strings.Contains(out, "\ncosts · "+defaults+"\n") || !strings.Contains(out, "\nlimits not kept, because cost_words quoted no limit the person named: rails.per_run_usd 0.5, rails.max_per_day 24 — say only what costs · says\n") {
		t.Fatalf("the model was not told what binds and what was dropped: %q", out)
	}

	named := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"rails": map[string]any{"per_run_usd": 1}, "cost_words": "at most a dollar a run"})),
		finalText("set up"),
	}}, nil)
	card, _ = named.proposeInbox(t, named.yes)
	if rails := named.only(t).Rails; rails.PerRunUSD != 1 || rails.MaxPerDay != standDefaultMaxPerDay {
		t.Fatalf("a named limit did not stand: %+v", rails)
	}
	if card.CostWords != "up to $1.00 a run · shares the day's allowance" {
		t.Fatalf("a named limit's costs line = %q", card.CostWords)
	}
}

// WORK IN TWO FOLDERS SAYS BOTH, IS PLACED IN BOTH, AND IS READ BY BOTH'S RULES.
func TestWorkInTwoFoldersSaysBothAndIsPlacedInBoth(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	ctx := context.Background()
	launch, marketing := d.folder(t, "Launch"), d.folder(t, "Marketing")
	for _, folder := range []workspace.Collection{launch, marketing} {
		if err := d.org.AddPlacement(ctx, folder.ID, d.chat); err != nil {
			t.Fatal(err)
		}
	}
	d.rule(t, "RULE-LAUNCH-7: no email addresses.", launch.ID)
	d.rule(t, "RULE-MKT-9: cite the spec.", marketing.ID)
	card, _ := d.proposeInbox(t, d.yes)
	folderTerm, rules := card.Terms[2], strings.Join(card.Terms[3:], "\n")
	if !strings.HasPrefix(folderTerm, "folder · ") || !strings.HasSuffix(folderTerm, ", where this conversation is placed — their rules reach every run") ||
		!strings.Contains(folderTerm, "Launch") || !strings.Contains(folderTerm, "Marketing") {
		t.Fatalf("two folders read %q", folderTerm)
	}
	if !strings.Contains(rules, "RULE-LAUNCH-7") || !strings.Contains(rules, "RULE-MKT-9") {
		t.Fatalf("the card quoted %q, want both folders' rules", rules)
	}
	item := d.only(t)
	placed, err := d.org.GoverningCollections(ctx, workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})
	if err != nil || len(placed) != 2 {
		t.Fatalf("the work is placed in %v (%v), want both", placed, err)
	}
	raw, _ := os.ReadFile(d.store.LogPath(item.ID))
	if !strings.Contains(string(raw), "placed in folders ") || !strings.Contains(string(raw), "; their rules reach this work") {
		t.Fatalf("the item's log reads:\n%s", raw)
	}
}

// AN INHERITED FOLDER IS WRITTEN UNDER COLLECTIONS' LAW. A delegated answer may
// not bind a folder by naming one, and it may not bind the conversation's own
// either: work set up without the binding would run outside the rules this
// conversation is under, so it is refused. A conversation in no folder binds
// nothing and is not refused.
func TestAStewardCannotBindTheConversationsFolderToWork(t *testing.T) {
	d := newChatDoor(t, nil, nil)
	ctx := context.Background()
	launch := d.folder(t, "Launch")
	var parsed standArguments
	if err := json.Unmarshal([]byte(inboxWork(nil)), &parsed); err != nil {
		t.Fatal(err)
	}
	item, problem := d.agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatal(problem)
	}
	d.agent.principal = &Steward{}
	if _, problem := d.agent.standingPlacementFor(ctx, parsed, item); problem != "" {
		t.Fatalf("a conversation in no folder was refused under a steward: %q", problem)
	}
	if err := d.org.AddPlacement(ctx, launch.ID, d.chat); err != nil {
		t.Fatal(err)
	}
	if _, problem := d.agent.standingPlacementFor(ctx, parsed, item); problem != standingPlacementLaw+" — this conversation is placed in Launch" {
		t.Fatalf("a steward bound the conversation's folder: %q", problem)
	}
	d.agent.principal = nil
	if place, problem := d.agent.standingPlacementFor(ctx, parsed, item); problem != "" || place.names() != "Launch" {
		t.Fatalf("the person's own conversation = %+v %q", place, problem)
	}
}

// THE HOLD-LIMIT GATE IS ON THE CARD. A run whose rules outnumber what a run may
// carry stops before it acts; the card says so before the yes instead of
// quoting five rules and counting the rest. The folder is named, because a
// conversation placed there itself would meet the same gate before it could
// call anything.
func TestACardSaysWhenItsRulesAreMoreThanARunCanCarry(t *testing.T) {
	d := newChatDoor(t, nil, nil)
	launch := d.folder(t, "Launch")
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": launch.ID})), finalText("set up")}}
	for n := 0; n <= governingHoldLimit; n++ {
		d.rule(t, fmt.Sprintf("RULE-%d: say so.", n), launch.ID)
	}
	card, _ := d.proposeInbox(t, d.yes)
	want := fmt.Sprintf("rules · %d reach this work, more than the %d a run can carry — every run would stop until they are narrowed", governingHoldLimit+1, governingHoldLimit)
	if got := card.Terms[len(card.Terms)-1]; got != want || len(card.Terms) != 4 {
		t.Fatalf("the card's terms end %q (%d terms), want %q", got, len(card.Terms), want)
	}
}

// AFTER THE ONE LINE, THE MODEL IS STILL TOLD WHAT CHECKS THE WORK. The person's
// row about background checks is said once ever; the model's result carries the
// fact every time, so its reply about the second item is never a guess.
func TestTheModelHearsWhatChecksTheWorkAfterTheOneLine(t *testing.T) {
	store := newFakeStanding(t)
	watch := &fakeWatch{}
	standRatify(t, store, watch, t.TempDir())
	events := standRatify(t, store, watch, t.TempDir())
	if line := backgroundLine(events); line != "" {
		t.Fatalf("the person was told again: %q", line)
	}
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "\n"+standingBackgroundLine+"\n") {
		t.Fatalf("the model was not told checks are on: %q", out)
	}
	watch.installed = false // the person turned the row off since
	if out := toolOutput(t, standRatify(t, store, watch, t.TempDir()), "stand"); !strings.Contains(out, "\n"+standingBackgroundOff+"\n") {
		t.Fatalf("the model was not told checks are off: %q", out)
	}
}

// toolOutputs is every result one named tool gave in a turn, in order.
func toolOutputs(events []Event, tool string) []string {
	var out []string
	for _, event := range events {
		if event.Tool == tool && (event.Kind == EventToolEnd || event.Kind == EventToolFailed) {
			out = append(out, event.Output)
		}
	}
	return out
}

// A NAMED COUNT OF RUNS STANDS ON ITS OWN WORDS, AND ONE SENT WITHOUT THEM IS
// REFUSED. The review of round 2 found "no more than 3 runs a day" silently
// becoming the default 10: cost_words spoke only of money, so a model that
// followed the schema sent max_per_day with no cost_words, the count was
// dropped as unnamed, and neither the card nor the model heard about it.
// cost_words now covers a count of runs as well as money, and a count quoted
// there stands and leads the costs line.
//
// ROUND 3 DROPPED THE COUNT SENT WITHOUT ITS WORDS AND NAMED THE DROP; RULING R5
// REFUSES IT WHEN THEIR SENTENCE NAMES ONE. This test pinned the drop for the
// count the person named, and the live rails case showed what that costs: the
// person said yes to `at most 10 runs a day (the default)` over their own "more
// than once a day", because a card was drawn before the call was right. A wrong
// card is worse than a second call, so a count their sentence names is refused
// with cost_words named, and the drop-and-name stays for a limit nobody named.
func TestANamedCountOfRunsStandsOnItsOwnWordsAndADroppedOneIsNamed(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(standSchemaJSON), &schema); err != nil {
		t.Fatal(err)
	}
	if words := schema.Properties["cost_words"].Description; !strings.Contains(words, "how many runs") {
		t.Fatalf("cost_words still speaks only of money, so a named count has no words to stand on: %q", words)
	}

	named := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"rails": map[string]any{"max_per_day": 3}, "cost_words": "no more than 3 runs a day"})),
		finalText("set up"),
	}}, nil)
	card, _ := named.proposeInbox(t, named.yes)
	if n := named.only(t).Rails.MaxPerDay; n != 3 {
		t.Fatalf("a named count became %d a day", n)
	}
	if card.CostWords != "at most 3 runs a day · shares the day's allowance" {
		t.Fatalf("a named count's costs line = %q", card.CostWords)
	}

	// The count their sentence names, sent without its words: refused, and no
	// card is drawn.
	counted := "keep an eye on my inbox folder and keep reports/inbox-report.md current, no more than 3 runs a day"
	refused := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"words": counted, "rails": map[string]any{"max_per_day": 3}})),
		finalText("no"),
	}}, nil)
	events := refused.submitAnswering(t, counted, func(Event) { t.Fatal("a card was drawn for a named count sent without its words") })
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "rails.max_per_day 3") || !strings.Contains(out, "cost_words") || !strings.Contains(out, "3 runs a day") {
		t.Fatalf("the named count was not refused by name: %q", out)
	}

	// A count nobody named: dropped, and the drop is named.
	unquoted := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"rails": map[string]any{"max_per_day": 3}})),
		finalText("set up"),
	}}, nil)
	card, events = unquoted.proposeInbox(t, unquoted.yes)
	if n := unquoted.only(t).Rails.MaxPerDay; n != standDefaultMaxPerDay {
		t.Fatalf("an unquoted count stood at %d a day", n)
	}
	if want := fmt.Sprintf("at most %d runs a day (the default) · shares the day's allowance", standDefaultMaxPerDay); card.CostWords != want {
		t.Fatalf("the card hides the default that replaced the count: %q, want %q", card.CostWords, want)
	}
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "\nlimits not kept, because cost_words quoted no limit the person named: rails.max_per_day 3 — say only what costs · says\n") {
		t.Fatalf("the model was not told the count was dropped: %q", out)
	}
}

// A NAMED LIMIT IS ON THE CARD EVEN AT THE DEFAULT, AND NO CAP IS NEVER $0.00.
// The person who said "at most 10 a day" sees 10 a day, not silence that reads
// as "nothing was said"; and a per-run limit of 0 — the store's "no limit" —
// reads as no per-run limit rather than a figure of nothing.
func TestANamedLimitShowsEvenAtTheDefaultAndNoCapIsNeverZeroDollars(t *testing.T) {
	for _, c := range []struct {
		rails map[string]any
		words string
		want  string
	}{
		{map[string]any{"max_per_day": standDefaultMaxPerDay}, "at most ten a day", fmt.Sprintf("at most %d runs a day · shares the day's allowance", standDefaultMaxPerDay)},
		{map[string]any{"per_run_usd": 0}, "no limit per run", "no per-run limit · shares the day's allowance"},
	} {
		d := newChatDoor(t, &scriptedCompleter{steps: []step{
			standCall("s1", inboxWork(map[string]any{"rails": c.rails, "cost_words": c.words})),
			finalText("set up"),
		}}, nil)
		if card, _ := d.proposeInbox(t, d.yes); card.CostWords != c.want {
			t.Fatalf("%v said %q reads %q, want %q", c.rails, c.words, card.CostWords, c.want)
		}
	}
}

// A NEGATIVE LIMIT IS REFUSED, NOT DROPPED, WHATEVER THE WORDS. A call that
// sends -1 has an error to fix, and dropping it as unnamed would answer the
// model's mistake with a silence.
func TestANegativeLimitWithNoWordsIsRefusedNotDropped(t *testing.T) {
	for rails, want := range map[string]string{
		`{"per_run_usd":-1}`: "a per-run budget cannot be negative",
		`{"max_per_day":-2}`: "an item needs a max per day",
	} {
		var extra map[string]any
		if err := json.Unmarshal([]byte(rails), &extra); err != nil {
			t.Fatal(err)
		}
		d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"rails": extra})), finalText("no")}}, nil)
		events, err := d.agent.Submit(context.Background(), "set it up")
		if err != nil {
			t.Fatal(err)
		}
		collected := drainAnsweringStanding(t, events, func(Event) { t.Fatal("a card was drawn for a negative limit") })
		if out := toolOutput(t, collected, "stand"); !strings.Contains(out, want) {
			t.Fatalf("%s answered %q, want %q", rails, out, want)
		}
	}
}

// A RULE OVER FOLDERS IS BOUND UNDER COLLECTIONS' LAW TOO. folder_scope asked
// only whether a steward was answering; a session with nobody to ask reached
// the folder database before being refused elsewhere. Both are now refused by
// the one predicate `collections place` asks, in the folder law's own words.
func TestAFolderRuleIsBoundOnlyWhereFoldersMayBeBound(t *testing.T) {
	call := func(folder string) json.RawMessage {
		return json.RawMessage(`{"op":"propose","words":"inbox reports never quote email addresses","when":{"kind":"hold"},"folder_scope":{"collection_ids":["` + folder + `"]}}`)
	}
	for name, mutate := range map[string]func(*chatDoor){
		"nobody to ask": func(d *chatDoor) { d.agent.config.AskConsent = false },
		"a steward":     func(d *chatDoor) { d.agent.principal = &Steward{} },
	} {
		d := newChatDoor(t, nil, nil)
		launch := d.folder(t, "Launch")
		mutate(d)
		text, isError, err := d.agent.standTool(context.Background(), call(launch.ID))
		if err != nil || !isError || !strings.HasPrefix(text, "folder rules need the person's answer in a conversation") {
			t.Fatalf("%s: a folder rule answered %q isError=%v err=%v", name, text, isError, err)
		}
	}
}

// THE 64 KiB GATE IS ON THE CARD BESIDE THE 64-RULE ONE. Twenty rules is well
// under the count a run may carry, but their words rendered as the run renders
// them are more than it may carry, and every run would stop; the card says so
// in the gate's own measure.
func TestACardSaysWhenItsRulesAreMoreWordsThanARunCanCarry(t *testing.T) {
	d := newChatDoor(t, nil, nil)
	launch := d.folder(t, "Launch")
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": launch.ID})), finalText("set up")}}
	long := strings.Repeat("keep every inbox report plain and cite the source file for each line ", 60)
	for n := 0; n < 20; n++ {
		d.rule(t, fmt.Sprintf("RULE-%d: %s", n, long), launch.ID)
	}
	card, _ := d.proposeInbox(t, d.yes)
	got := card.Terms[len(card.Terms)-1]
	if !strings.HasPrefix(got, "rules · their words come to ") || !strings.HasSuffix(got, fmt.Sprintf(" KiB, more than the %d KiB a run can carry — every run would stop until they are narrowed", governingPromptBytes/1024)) || len(card.Terms) != 4 {
		t.Fatalf("the card's terms end %q (%d terms)", got, len(card.Terms))
	}
}

// THE FILES A SURFACE WARNS ABOUT ARE FILES. An item's words and instructions
// naming another report-shaped path are listed beside its stored report, the
// stored report itself is not, and an address or a link — whose tail looks like
// an extension — is never taken for a file anybody could publish to.
func TestStandingNamedFilesListsOnlyOtherReportShapedPaths(t *testing.T) {
	store, err := standing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Create(standing.Item{
		Words:     "keep reports/digest.md current and mail dana.lee@example.com, see https://example.com/a.md",
		Workspace: t.TempDir(),
		When:      standing.When{Kind: standing.WhenFile, Glob: "product/*"},
		Does:      standing.Action{Kind: standing.ActionTask, Brief: "rewrite reports/old-digest.md from the spec", Report: "reports/digest.md"},
		Rails:     standing.Rails{MaxPerDay: 2, PerRunUSD: 0.05},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := StandingNamedFiles(item); len(got) != 1 || got[0] != "reports/old-digest.md" {
		t.Fatalf("the named files read %v", got)
	}
}
