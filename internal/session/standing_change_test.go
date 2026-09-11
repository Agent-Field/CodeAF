package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── CHANGING WORK THROUGH THE CHAT, AND A REPORT PATH THAT TELLS THE TRUTH ──
//
// The person-like chat protocol of 2026-09-11 (validation-chat's scoreboard)
// failed five cases on one family of seams. An edit, a move and a change of mind
// each became a stop and a new card, because `stand` had no edit; the new item
// was then held `report-changed` at the file its predecessor had published,
// because the receipt belonged to the item and not to the file. A report path
// that already held the person's own file was drawn on the card as though
// aforge would simply publish there, and every run was then held. The chat
// wrote files the card had just said aforge owns. A named count sent without
// its words was dropped and the card showed the default. And a probe's card
// said nothing under `when ·`. Each test below failed on the head before these
// rulings (R1, R2, R3, R5, R7, R11, R12).

// fireReport runs one firing of item that replies with body as its report,
// in run folder n, through the runner the pass uses.
func fireReport(t *testing.T, store *standing.Store, item standing.Item, n int, body string) standing.Outcome {
	t.Helper()
	runDir := filepath.Join(store.RunsDir(item.ID), standing.RunName(n))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{saying("<report>\n" + body + "\n</report>")}}
	outcome, err := standingChildRunner(t, store.Root(), model).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

// submitAnswering runs one turn of the conversation, answering any card with
// answer, and hands back the turn's events.
func (d *chatDoor) submitAnswering(t *testing.T, said string, answer func(Event)) []Event {
	t.Helper()
	events, err := d.agent.Submit(context.Background(), said)
	if err != nil {
		t.Fatal(err)
	}
	return drainAnsweringStanding(t, events, answer)
}

// R1. AN EDIT THROUGH THE CHAT IS THE TERMINAL'S EDIT: THE SAME ITEM, ONE
// VERSION ON, WITH ITS READINGS AND ITS RECEIPT KEPT. The lifecycle case asked
// to "also list who owns each request"; with no edit to reach for, the chat
// stopped the digest and proposed a new one, whose first pass swallowed a
// change as its baseline and whose first run was held at the file its
// predecessor had published. Now the card shows what changes, the yes revises
// the item in place, and the next run publishes over the last report.
func TestAnEditThroughTheChatKeepsTheItemItsReadingsAndItsReceipt(t *testing.T) {
	const newer = "Read the changed files in inbox/ and write a short report of new decisions and requests, and who owns each request."
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	first := d.only(t)
	// What the item has read, and what it has published, before the edit.
	first.Fingerprint = "the-reading-before-the-edit"
	if err := d.store.Save(first); err != nil {
		t.Fatal(err)
	}
	if outcome := fireReport(t, d.store, first, 1, "# Inbox\n- one decision"); outcome.Published == nil {
		t.Fatalf("the first report was not published: %+v", outcome)
	}

	edit, _ := json.Marshal(map[string]any{
		"op": "edit", "id": first.ID, "words": "actually, in that digest also list who owns each request",
		"does": map[string]any{"instructions": newer},
	})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("changed")}}
	var card *StandingNotice
	events := d.submitAnswering(t, "actually, in that digest also list who owns each request", func(event Event) {
		card = event.Standing
		d.yes(event)
	})
	if card == nil {
		t.Fatalf("no card was drawn for the edit; the tool said %q", toolOutput(t, events, "stand"))
	}
	var does string
	for _, term := range card.Terms {
		if strings.HasPrefix(term, "does · ") {
			does = term
		}
	}
	if !strings.Contains(does, " → ") || !strings.HasSuffix(does, newer) {
		t.Fatalf("the edit card does not show what changes, old → new: %q", card.Terms)
	}

	after := d.only(t)
	if after.ID != first.ID || after.SpecRevision != 2 || after.Does.Brief != newer {
		t.Fatalf("the edit made %s version %d with %q, want %s version 2 with the new instructions", after.ID, after.SpecRevision, after.Does.Brief, first.ID)
	}
	if after.Fingerprint != "the-reading-before-the-edit" {
		t.Fatalf("the edit threw away what the item had read: %q", after.Fingerprint)
	}
	if outcome := fireReport(t, d.store, after, 2, "# Inbox\n- one decision (owner: Priya)"); outcome.Published == nil || outcome.Withheld != "" {
		t.Fatalf("the report the item published before its edit held the next one back: %+v", outcome)
	}
}

// R1. THERE IS ONE SPELLING OF THE VERB. `change` is what a card answers and
// never an op, and a call that reaches for it is told the ops there are.
func TestAnOpCalledChangeIsNoOpAndTheRefusalNamesEdit(t *testing.T) {
	agent := standingAgent(t, &scriptedCompleter{}, newFakeStanding(t), nil)
	for _, op := range []string{"change", "revise"} {
		text, failed, err := agent.standTool(context.Background(), json.RawMessage(`{"op":"`+op+`"}`))
		if err != nil || !failed {
			t.Fatalf("op %s answered %q (failed %v, %v)", op, text, failed, err)
		}
		if want := "no op called \"" + op + "\" — propose, list, edit, pause, resume or stop"; !strings.Contains(text, want) {
			t.Fatalf("op %s answered %q, want %q", op, text, want)
		}
	}
}

// R2. A SUCCESSOR PUBLISHING WHERE ITS STOPPED PREDECESSOR PUBLISHED IS NOT
// WRITING OVER A PERSON. "Is this file what aforge last published here" is a
// fact about the file, so the receipt is the path's and not the item's.
func TestASuccessorPublishesWhereItsStoppedPredecessorPublished(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, first := storedReporting(t, root, workspace)
	if outcome := fireReport(t, store, first, 1, "# Report\n- from the first item"); outcome.Published == nil {
		t.Fatalf("the first item did not publish: %+v", outcome)
	}
	if _, err := store.SetStatus(first.ID, standing.StatusRetired, standing.StoppedWhy); err != nil {
		t.Fatal(err)
	}
	successor := reporting(workspace)
	successor.ID = ""
	second, err := store.Create(successor)
	if err != nil {
		t.Fatal(err)
	}
	outcome := fireReport(t, store, second, 1, "# Report\n- from its successor")
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if outcome.Published == nil || outcome.Withheld != "" || string(raw) != "# Report\n- from its successor\n" {
		t.Fatalf("the successor was held at its predecessor's report: %q (outcome %+v)", raw, outcome)
	}
}

// R3. A FILE ALREADY AT THE REPORT PATH IS SAID ON THE CARD, AND THE YES ADOPTS
// IT. h2-spec and permission both drew `aforge publishes this file` over a file
// aforge had never written, and every run was then held. The card now says the
// file is there and will be replaced, the tool's answer says the same, and the
// informed yes is the receipt the first run publishes over.
func TestAReportFileAlreadyThereIsSaidOnTheCardAndTheYesAdoptsIt(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	theirs := filepath.Join(d.project, "reports", "inbox-report.md")
	if err := os.MkdirAll(filepath.Dir(theirs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(theirs, []byte("# my notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	card, events := d.proposeInbox(t, d.yes)
	want := "report · reports/inbox-report.md — this file already exists (11 bytes, written moments ago); aforge will replace it"
	if card.Terms[1] != want {
		t.Fatalf("the card's report line = %q, want %q", card.Terms[1], want)
	}
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "\n"+want+"\n") {
		t.Fatalf("the model was not told the file is there: %q", out)
	}
	outcome := fireReport(t, d.store, d.only(t), 1, "# Inbox\n- the first report")
	raw, _ := os.ReadFile(theirs)
	if outcome.Published == nil || string(raw) != "# Inbox\n- the first report\n" {
		t.Fatalf("the file the person agreed to replace held the first report back: %q (outcome %+v)", raw, outcome)
	}
}

// R11. THE CHAT DOES NOT WRITE A FILE aforge PUBLISHES. h2-spec seeded the
// report before proposing, and policy wrote over one after a change of mind; the
// card had just said the run never writes it, and the next run was held. The
// conversation's own write is refused, naming the work and the edit that changes
// what it says.
func TestTheChatCannotWriteAReportAforgePublishes(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	d.agent.client = &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("w1", "write", `{"path":"reports/inbox-report.md","content":"# seeded by the chat\n"}`), nil
		},
		finalText("done"),
	}}
	events := d.submitAnswering(t, "put a first version of the report there now", nil)
	out := toolOutput(t, events, "write")
	if _, err := os.Stat(filepath.Join(d.project, "reports", "inbox-report.md")); !os.IsNotExist(err) {
		t.Fatalf("the chat wrote the report aforge publishes (the write said %q)", out)
	}
	if !strings.Contains(out, item.ID) || !strings.Contains(out, "op edit") {
		t.Fatalf("the refusal = %q, want it to name the work (%s) and op edit", out, item.ID)
	}
}

// R5. A COUNT THEIR SENTENCE NAMES, SENT WITHOUT ITS WORDS, IS REFUSED — NEVER
// DROPPED. rails: "don't let it run more than once a day" went out as
// max_per_day 1 with no cost_words, the count was dropped, and the person said
// yes to `at most 10 runs a day (the default)`. A wrong card is worse than a
// second call.
func TestACountTheirSentenceNamesIsRefusedWithoutItsWords(t *testing.T) {
	words := "keep an eye on inbox/ and keep reports/inbox.md current, but don't let it run more than once a day"
	call := func(extra map[string]any) string {
		body := map[string]any{"words": words, "rails": map[string]any{"max_per_day": 1}, "does": map[string]any{"report": "reports/inbox.md"}}
		for key, value := range extra {
			body[key] = value
		}
		return inboxWork(body)
	}
	d := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", call(nil)),
		standCall("s2", call(map[string]any{"cost_words": "no more than once a day"})),
		finalText("set up"),
	}}, nil)
	var cards []StandingNotice
	events := d.submitAnswering(t, words, func(event Event) {
		cards = append(cards, *event.Standing)
		d.yes(event)
	})
	out := toolOutputs(events, "stand")
	if len(out) != 2 || !strings.Contains(out[0], "cost_words") || !strings.Contains(out[0], "once a day") {
		t.Fatalf("the stand results were %q, want the first refused naming cost_words and their words", out)
	}
	if len(cards) != 1 || !strings.HasPrefix(cards[0].CostWords, "at most 1 run a day") {
		t.Fatalf("the cards drawn were %+v, want one saying at most 1 run a day", cards)
	}
	if n := d.only(t).Rails.MaxPerDay; n != 1 {
		t.Fatalf("the named count stood at %d a day", n)
	}
}

// R7. THE STAND RESULT CARRIES THE CARD'S LINES, VERBATIM. move replied "Work
// folder has no conditions of its own on record" over a card that had just named
// the Work rule, and nested quoted $0.75 a run over a card that said $5.00: a
// model told only "set up" writes its reply from what it remembers sending.
func TestTheStandResultCarriesTheCardsLinesVerbatim(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	launch := d.folder(t, "Launch")
	if err := d.org.AddPlacement(context.Background(), launch.ID, d.chat); err != nil {
		t.Fatal(err)
	}
	d.rule(t, "RULE-LAUNCH-7: inbox reports never quote email addresses.", launch.ID)
	card, events := d.proposeInbox(t, d.yes)
	out := toolOutput(t, events, "stand")
	lines := []string{"when · " + card.WhenWords, "costs · " + card.CostWords}
	for _, term := range card.Terms {
		if !strings.HasPrefix(term, standingDoesTag) {
			lines = append(lines, term)
		}
	}
	for _, line := range append(lines, "rule · RULE-LAUNCH-7: inbox reports never quote email addresses.", "folder · Launch, where this conversation is placed — its rules reach every run") {
		if !strings.Contains(out, "\n"+line+"\n") {
			t.Errorf("the stand result does not carry %q:\n%s", line, out)
		}
	}
}

// R12. EVERY CARD SAYS WHEN IT WAKES. probe approved `when ·` blank over a check
// that ran every minute: the fallback covered only a file watch.
func TestEveryKindOfCardSaysWhenItWakes(t *testing.T) {
	at := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	for kind, when := range map[string]map[string]any{
		"probe": {"kind": "probe", "probe": map[string]any{"command": "cat ci/status.txt"}, "probe_every": "1m"},
		"every": {"kind": "every", "every": "20m"},
		"idle":  {"kind": "idle", "idle_for": "45m"},
		"at":    {"kind": "at", "at": at},
	} {
		body, _ := json.Marshal(map[string]any{"op": "propose", "words": "tell me here", "when": when, "does": map[string]any{"kind": "say", "say": "it happened"}})
		agent := standingAgent(t, &scriptedCompleter{steps: []step{standCall("s1", string(body)), finalText("set up")}}, newFakeStanding(t), nil)
		events, err := agent.Submit(context.Background(), "tell me here")
		if err != nil {
			t.Fatal(err)
		}
		var card *StandingNotice
		drainAnsweringStanding(t, events, func(event Event) {
			card = event.Standing
			agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
		})
		if card == nil || strings.TrimSpace(card.WhenWords) == "" {
			t.Errorf("a %s card says nothing under when · (%+v)", kind, card)
		}
	}
}

// ── what the rulings also hold, beyond the defects they close ───────────────

// R3 AT THE YES, NOT ONLY AT THE CARD (the scale audit's L2). The person agreed
// to replace the bytes the card described; a file saved again between the card
// and the yes is not what they agreed to, so it is not adopted, the tool's
// answer says so, and the first report waits for them.
func TestAFileThatChangedAfterTheCardIsNotAdopted(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	theirs := filepath.Join(d.project, "reports", "inbox-report.md")
	if err := os.MkdirAll(filepath.Dir(theirs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(theirs, []byte("# my notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, events := d.proposeInbox(t, func(event Event) {
		if err := os.WriteFile(theirs, []byte("# my notes, and a line typed while the card was up\n"), 0o644); err != nil {
			t.Error(err)
		}
		d.yes(event)
	})
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "\nreport · reports/inbox-report.md was not adopted: it changed after the card was drawn") {
		t.Fatalf("the model was not told the file was not adopted: %q", out)
	}
	if outcome := fireReport(t, d.store, d.only(t), 1, "# Inbox\n- the first report"); outcome.Withheld != "report-changed" {
		t.Fatalf("a file saved after the card was replaced: %+v", outcome)
	}
}

// R1, WHAT AN EDIT CANNOT DO IS REFUSED WITH THE ROAD THAT DOES IT. A move to a
// folder that is not there is refused as a proposal's placement is; a second
// report on one file is an edit of the first.
func TestAnEditAndASecondReportAreToldTheirRoads(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	move, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "placement": "0123456789abcdef"})
	text, failed, err := d.agent.standTool(context.Background(), move)
	if err != nil || !failed || !strings.Contains(text, "folder not found: 0123456789abcdef") {
		t.Fatalf("a move by edit to no folder answered %q (failed %v, %v)", text, failed, err)
	}
	text, failed, err = d.agent.standTool(context.Background(), json.RawMessage(inboxWork(map[string]any{"words": "keep my inbox digest in reports/inbox-report.md"})))
	if err != nil || !failed || !strings.Contains(text, "is already the report of") || !strings.Contains(text, "op edit") {
		t.Fatalf("a second item on one report answered %q (failed %v, %v)", text, failed, err)
	}
	if d.only(t).ID != item.ID {
		t.Fatal("a refused call changed what stands")
	}
}

// R1, THE YES IS FENCED ON THE VERSION THE CARD WAS DRAWN FROM. An edit made
// elsewhere while the card was up is not written over: nothing changes, and the
// model is told to read it again.
func TestAnEditAnsweredAfterAnotherEditChangesNothing(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	edit, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "does": map[string]any{"instructions": "the chat's instructions"}})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("changed")}}
	events := d.submitAnswering(t, "change the digest", func(event Event) {
		if _, _, err := d.store.Revise(item.ID, item.SpecRevision, func(changed *standing.Item) error {
			changed.Does.Brief = "the terminal's instructions"
			return nil
		}); err != nil {
			t.Error(err)
		}
		d.yes(event)
	})
	if out := toolOutput(t, events, "stand"); !strings.Contains(out, "nothing was changed: it was changed elsewhere") {
		t.Fatalf("the late yes answered %q", out)
	}
	if after := d.only(t); after.Does.Brief != "the terminal's instructions" || after.SpecRevision != 2 {
		t.Fatalf("the late yes wrote over the other edit: version %d %q", after.SpecRevision, after.Does.Brief)
	}
}

// R1 AND R12, AN EDIT CARD'S BANDS. A cadence that changes reads `old → new`;
// a cost that does not is the item's own, a limit set when the work was made
// included.
func TestAnEditCardSaysTheOldAndTheNewAndKeepsTheRest(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"rails": map[string]any{"max_per_day": 2}, "cost_words": "twice a day at most"})),
		finalText("set up"),
	}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	edit, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "when": map[string]any{"kind": "file", "glob": "inbox/**/*.md"}, "when_words": "whenever a note lands anywhere in inbox"})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("changed")}}
	var card *StandingNotice
	d.submitAnswering(t, "watch the subfolders too", func(event Event) {
		card = event.Standing
		d.agent.ResolveStanding(event.Standing.ID, StandingAnswer{})
	})
	if card == nil {
		t.Fatal("no card was drawn")
	}
	if card.WhenWords != "when inbox/* changes → whenever a note lands anywhere in inbox" {
		t.Fatalf("the when band reads %q", card.WhenWords)
	}
	if !strings.HasPrefix(card.CostWords, "at most 2 runs a day") {
		t.Fatalf("the costs band hid the limit the work was made with: %q", card.CostWords)
	}
	if card.Terms[0] != "changes · what wakes it"+standingChangesKept {
		t.Fatalf("the first line reads %q", card.Terms[0])
	}
	for _, option := range card.Options {
		if option.Key == StandingOnceKey {
			t.Fatal("an edit card offered just once")
		}
	}
	if after := d.only(t); after.SpecRevision != 1 || after.When.Glob != "inbox/*" {
		t.Fatalf("a declined edit changed the work: %+v", after.When)
	}
}

// TestAnEditCardShowsAChangePastTheClip is the live lifecycle run of
// 2026-09-11 (W5-A, run 1): "also list who owns each request" appended a
// sentence to instructions longer than a card line, both versions clipped to
// the same first 160 characters, and the card said `changes · instructions`
// over one unchanged `does ·` line — a yes asked for on a change nobody could
// see. The line now starts where the two texts begin to differ.
func TestAnEditCardShowsAChangePastTheClip(t *testing.T) {
	long := "Read anything new in inbox/ (files whose modification time is after the last digest's mtime or after this task's first run if there is no digest yet). For each new file, add one line to the digest."
	d := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"does": map[string]any{"instructions": long}})),
		finalText("set up"),
	}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	edit, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "does": map[string]any{"instructions": strings.Replace(long, "add one line", "add one line naming who owns each request", 1)}})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("changed")}}
	var card *StandingNotice
	d.submitAnswering(t, "also list who owns each request", func(event Event) {
		card = event.Standing
		d.agent.ResolveStanding(event.Standing.ID, StandingAnswer{})
	})
	if card == nil {
		t.Fatal("no card was drawn")
	}
	var does string
	for _, term := range card.Terms {
		if strings.HasPrefix(term, standingDoesTag) {
			does = term
		}
	}
	if !strings.Contains(does, " → ") || !strings.Contains(does, "who owns each request") {
		t.Fatalf("the does line hides the change: %q", does)
	}
	if want := "does · …add one line to the digest. → …add one line naming who owns each request to the digest."; does != want {
		t.Fatalf("the does line reads\n%q, want\n%q", does, want)
	}
}

// TestAMoveIsAnEditOfWhereItIsPlaced is the live move runs of 2026-09-11
// (W5-A, runs 2 and 4): "move it to my Work folder instead of Personal" was
// sent as an edit with placement, refused towards `collections place and
// unplace`, and the model placed the notes watch in Work and left it in
// Personal — twice, the second time told outright to unplace — so its runs
// kept both folders' rules. The edit card now draws the move and the yes makes
// it whole: in Work, out of Personal, the same item.
func TestAMoveIsAnEditOfWhereItIsPlaced(t *testing.T) {
	d := newChatDoor(t, nil, nil)
	personal, work := d.folder(t, "Personal"), d.folder(t, "Work")
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s1", inboxWork(map[string]any{"placement": personal.ID})), finalText("set up")}}
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	move, _ := json.Marshal(map[string]any{"op": "edit", "id": item.ID, "placement": "Work"})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(move)), finalText("moved")}}
	var card *StandingNotice
	events := d.submitAnswering(t, "move it to my Work folder instead of Personal", func(event Event) {
		card = event.Standing
		d.yes(event)
	})
	if card == nil {
		t.Fatalf("no card was drawn for the move; the tool said %q", toolOutput(t, events, "stand"))
	}
	if !slices.Contains(card.Terms, "changes · folder"+standingChangesKept) || !slices.ContainsFunc(card.Terms, func(term string) bool {
		return strings.HasPrefix(term, "folder · Personal") && strings.Contains(term, " → Work")
	}) {
		t.Fatalf("the card does not draw the move: %q", card.Terms)
	}
	if out := toolOutput(t, events, "stand"); !strings.HasPrefix(out, "moved "+item.ID+" to Work") {
		t.Fatalf("the move answered %q", out)
	}
	governing, err := d.org.GoverningCollections(context.Background(), workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	var direct []string
	for _, folder := range governing {
		if folder.Depth == 0 {
			direct = append(direct, folder.Name)
		}
	}
	if !slices.Equal(direct, []string{work.Name}) {
		t.Fatalf("after the move the work is placed in %q, want only Work", direct)
	}
	if after := d.only(t); after.ID != item.ID || after.SpecRevision != item.SpecRevision {
		t.Fatalf("a move revised or replaced the work: %+v", after)
	}
}

// TestAnEditNamedByTheirWordsFindsTheItem is three of five live lifecycle runs
// of 2026-09-11 (W5-A): the model sent op edit with the item's own sentence in
// words and no id — as it does for pause and stop, and as stand's description
// says all four take "an id or the person's own words" — and was refused for
// the missing id. An edit is named the way the other three are.
func TestAnEditNamedByTheirWordsFindsTheItem(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	d.proposeInbox(t, d.yes)
	item := d.only(t)
	edit, _ := json.Marshal(map[string]any{"op": "edit", "words": item.Words, "does": map[string]any{"instructions": "Read the changed files in inbox/ and list each request with who owns it."}})
	d.agent.client = &scriptedCompleter{steps: []step{standCall("s2", string(edit)), finalText("changed")}}
	events := d.submitAnswering(t, "also list who owns each request", d.yes)
	if out := toolOutput(t, events, "stand"); !strings.HasPrefix(out, "revised "+item.ID+" to version 2: instructions") {
		t.Fatalf("an edit named by its words answered %q", out)
	}
	if after := d.only(t); after.ID != item.ID || after.SpecRevision != 2 {
		t.Fatalf("the edit did not revise the item: %+v", after)
	}
	log, _ := os.ReadFile(d.store.LogPath(item.ID))
	if strings.Contains(string(log), "— "+strconv.Quote(item.Words)) {
		t.Fatalf("the item's own sentence was logged as the words of the change: %q", log)
	}
}

// TestTheirLimitsWordsWithNoLimitAreRefused is the live rails run of
// 2026-09-11 (W5-A, run 1): refused for sending max_per_day 1 without
// cost_words, the model sent cost_words "once a day" and dropped the rail, and
// the person said yes to `shares the day's allowance` over their own limit. A
// proposal whose sentence names a limit and whose cost_words say it, with no
// rail to bind it, is refused naming the rails.
func TestTheirLimitsWordsWithNoLimitAreRefused(t *testing.T) {
	words := "keep an eye on inbox/ and keep reports/inbox.md current, but don't let it run more than once a day"
	d := newChatDoor(t, &scriptedCompleter{steps: []step{
		standCall("s1", inboxWork(map[string]any{"words": words, "cost_words": "once a day", "does": map[string]any{"report": "reports/inbox.md"}})),
		standCall("s2", inboxWork(map[string]any{"words": words, "cost_words": "once a day", "rails": map[string]any{"max_per_day": 1}, "does": map[string]any{"report": "reports/inbox.md"}})),
		finalText("set up"),
	}}, nil)
	var cards []StandingNotice
	events := d.submitAnswering(t, words, func(event Event) {
		cards = append(cards, *event.Standing)
		d.yes(event)
	})
	out := toolOutputs(events, "stand")
	if len(out) != 2 || !strings.Contains(out[0], "rails.max_per_day") || !strings.Contains(out[0], "once a day") {
		t.Fatalf("the stand results were %q, want the first refused naming the rail", out)
	}
	if len(cards) != 1 || !strings.HasPrefix(cards[0].CostWords, "at most 1 run a day") {
		t.Fatalf("the cards drawn were %+v, want one saying at most 1 run a day", cards)
	}
}
