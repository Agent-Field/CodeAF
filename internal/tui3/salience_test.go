package tui3

// ── THE SALIENCE CONTRACT ───────────────────────────────────────────────────
//
//	THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A FACT.
//
// That sentence is the whole of docs/design/lens/DESIGN.md's Decision 4, and
// this file is the whole of its enforcement. A page may fold a fact away, dim
// it, put it a keypress down or draw it somewhere else entirely — what it may
// not do is fail to have it.
//
// The defect it closes is not hypothetical: the task room WAS the copy that
// fell behind. It never learned that a call finished, so no node's row ever said
// "took 4s"; a retry inside a task drew nothing at all, so a person watched a
// dead attempt's half-answer sit above the live one; a notice about a reshaped
// request went on the floor. Every one of those was a fact the chat had and the
// room did not, and every one of them was added to the chat by somebody who did
// not know a second wiring existed.
//
// So the table below walks EVERY event kind the engine can send through both
// pumps and asserts the room's list contains every kind of block the chat's
// does. The enumeration is read back out of internal/session's own source, so a
// kind added there and forgotten here fails this test rather than shipping.
//
// Lane L3 extends the same shape with the journal-role half — a steer mark, a
// note, a divider replayed through both shapers — which is why the case carries
// a name and a reason rather than a bare pair of event kinds.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// salienceCase is one event kind, an event of that kind worth drawing, and —
// where the two pages genuinely differ — the reason.
type salienceCase struct {
	// name is the identifier in internal/session. It is spelled out rather than
	// derived because it is what the enumeration gate below matches on, and a
	// kind's number is not stable across an insertion into that const block.
	name string
	ev   session.Event
	// chatOnly is why this event reaches the participant's page and no node's.
	// An empty string is the contract: whatever the chat drew, the room draws.
	//
	// IT IS A CLAIM THAT IS TESTED, not a comment. A kind carrying a reason must
	// leave the room's transcript untouched — if a room ever starts drawing one
	// of these, the reason has stopped being true and this test says so.
	chatOnly string
}

// salienceTable is every kind, in the order internal/session declares them.
var salienceTable = []salienceCase{
	{name: "EventTextDelta", ev: session.Event{Kind: session.EventTextDelta, Text: "the loader is fine"}},
	{name: "EventThinking", ev: session.Event{Kind: session.EventThinking}},
	{name: "EventReasoning", ev: session.Event{Kind: session.EventReasoning, Text: "the map is never made"}},
	{name: "EventToolForming", ev: session.Event{Kind: session.EventToolForming, CallID: "c1", Tool: "write",
		Hint: "write internal/config/load.go", ArgsText: `{"content":"package config`, Bytes: 22}},
	{name: "EventToolAnnounced", ev: session.Event{Kind: session.EventToolAnnounced, CallID: "c1", Tool: "write",
		Hint: "write internal/config/load.go", Args: `{"path":"internal/config/load.go"}`}},
	{name: "EventToolBegin", ev: session.Event{Kind: session.EventToolBegin, CallID: "c1", Tool: "write",
		Hint: "write internal/config/load.go", Args: `{"path":"internal/config/load.go"}`}},
	{name: "EventToolFinished", ev: session.Event{Kind: session.EventToolFinished, CallID: "c1", Tool: "write",
		Args: `{"path":"internal/config/load.go"}`, Took: 4 * time.Second}},
	{name: "EventToolEnd", ev: session.Event{Kind: session.EventToolEnd, CallID: "c1", Tool: "write",
		Args: `{"path":"internal/config/load.go"}`, Output: "written"}},
	{name: "EventToolFailed", ev: session.Event{Kind: session.EventToolFailed, CallID: "c1", Tool: "write",
		Args: `{"path":"internal/config/load.go"}`, Hint: "no such directory"}},
	{name: "EventCompacting", ev: session.Event{Kind: session.EventCompacting, Hint: "compacting ~84k tokens"}},
	{name: "EventCompacted", ev: session.Event{Kind: session.EventCompacted, Hint: "compacted, kept last ~20k"}},
	{name: "EventNudge", ev: session.Event{Kind: session.EventNudge, Tool: "read", Count: 3}},
	{name: "EventNotice", ev: session.Event{Kind: session.EventNotice, Text: "Retry 1/3: removed max_tokens"}},
	{name: "EventRetrying", ev: session.Event{Kind: session.EventRetrying, Text: "the stream stopped — trying again"}},
	{name: "EventGuardianAllowed", ev: session.Event{Kind: session.EventGuardianAllowed, Tool: "bash",
		Args: `{"command":"go test ./..."}`}},
	{name: "EventTaskReplyTags", ev: session.Event{Kind: session.EventTaskReplyTags,
		TaskReplyTags: []session.TaskReplyTag{{ID: 4, Title: "port the parser"}}}},
	{name: "EventError", ev: session.Event{Kind: session.EventError, Err: errSalience}},
	{name: "EventTitleChanged", ev: session.Event{Kind: session.EventTitleChanged, Text: "porting the parser"}},
	{name: "EventCaption", ev: session.Event{Kind: session.EventCaption, Text: "working out where the fold is minted"}},
	{name: "EventAssistantDone", ev: session.Event{Kind: session.EventAssistantDone}},

	// ── THE PARTICIPANT'S OWN ACTS ──────────────────────────────────────────
	//
	// Everything below carries a reason, and every reason is one of three facts
	// about a node: it cannot ask this keyboard anything, it is not the thing
	// that spends this conversation's money, and it is a WORKER rather than a
	// place work is delegated FROM.

	{name: "EventConsentRequest", chatOnly: "a node's policy never asks this keyboard: it allows everything " +
		"above the floor and the floor refuses rather than prompts (internal/session's consent.go), so a " +
		"question is a thing only the conversation is ever handed",
		ev: session.Event{Kind: session.EventConsentRequest, CallID: "c9", Tool: "bash", Hint: "bash rm -rf"}},
	{name: "EventTaskProposal", chatOnly: "delegation is the participant's act. A node does not propose work to " +
		"the person standing in its room — it does the work — and the card is the decision moment drawn where " +
		"the decision was made (task.go)",
		ev: session.Event{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 4, Title: "port the parser"}}},
	{name: "EventTaskUpdate", chatOnly: "a node's progress is the ROSTER's fact and the header's, not a block in " +
		"its own transcript: a room that narrated its own state would be the page telling you what the line " +
		"above it already says (task.go, room.go's header)",
		ev: session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 4, State: session.TaskRunning}}},
	{name: "EventJobUpdate", chatOnly: "a background job is a roster row and its page is a reading of its " +
		"log, not a transcript (jobstate.go): the update files a fact for the roster and draws no block " +
		"on either page",
		ev: session.Event{Kind: session.EventJobUpdate, Job: &session.JobNotice{ID: 4}}},
	{name: "EventTaskPhase", chatOnly: "which of its three lives a node is in is drawn in the header instrument, " +
		"once, rather than as a line each time it moves (taskphase.go)",
		ev: session.Event{Kind: session.EventTaskPhase, Task: &session.TaskNotice{ID: 4}}},
	{name: "EventTurnDone", chatOnly: "the two lines a turn ends with — what it changed and what it cost — are " +
		"the conversation's receipts. A room's numbers aggregate in its header instead of dribbling down the " +
		"scroll (docs/design/lens/DESIGN.md, Decision 3)",
		ev: session.Event{Kind: session.EventTurnDone}},
	{name: "EventSteerAccepted", chatOnly: "the elbow's landing clause is the chat's today; a room's steer " +
		"becomes the same elbow in lane L3 (docs/design/lens/DESIGN.md, Decision 2 and ruling 4)",
		ev: session.Event{Kind: session.EventSteerAccepted, Steer: &session.SteerNote{ID: 1, Words: "use the other file"}}},
	{name: "EventSteerConsumed", chatOnly: "the other half of the elbow's lifecycle, and lane L3's for the same " +
		"reason", ev: session.Event{Kind: session.EventSteerConsumed, Steer: &session.SteerNote{ID: 1, Words: "use the other file"}}},
	{name: "EventSteerFellThrough", chatOnly: "the third half of the elbow's lifecycle, and lane L3's for the " +
		"same reason", ev: session.Event{Kind: session.EventSteerFellThrough, Steer: &session.SteerNote{ID: 1, Words: "use the other file"}}},
	{name: "EventConnectAsk", chatOnly: "connecting an account is a question, and a node cannot be asked one",
		ev: session.Event{Kind: session.EventConnectAsk, Service: "google", ServiceName: "Google"}},
	{name: "EventConnectAuth", chatOnly: "the browser opens for the person who agreed to open it, which is the " +
		"person in the conversation", ev: session.Event{Kind: session.EventConnectAuth, Service: "google"}},
	{name: "EventConnectDone", chatOnly: "the end of that same sign-in", ev: session.Event{Kind: session.EventConnectDone, Service: "google"}},
	{name: "EventHarnessOffer", chatOnly: "an offer is a question (harness.go)", ev: session.Event{Kind: session.EventHarnessOffer, ID: 1, Text: "research"}},
	{name: "EventHarnessRun", chatOnly: "the run the person said yes to reports into the conversation that " +
		"offered it", ev: session.Event{Kind: session.EventHarnessRun, Text: "research · running"}},
	{name: "EventHarnessStep", chatOnly: "one step of that same run", ev: session.Event{Kind: session.EventHarnessStep, Text: "reading"}},
	{name: "EventHarnessDesign", chatOnly: "a design's own progress is drawn on its card and, inside a node's " +
		"room, on that page's one live line (harnesscard.go's [app.progressHarnessRoom])",
		ev: session.Event{Kind: session.EventHarnessDesign, ID: 7}},
	{name: "EventHarnessProgress", chatOnly: "the same card's live edge", ev: session.Event{Kind: session.EventHarnessProgress, ID: 7}},
	{name: "EventHarnessDesignDone", chatOnly: "the same card, settled", ev: session.Event{Kind: session.EventHarnessDesignDone, ID: 7}},
	{name: "EventHarnessDesignRevising", chatOnly: "the same card, revising", ev: session.Event{Kind: session.EventHarnessDesignRevising, ID: 7}},
	{name: "EventStandingProposal", chatOnly: "a standing order is a decision, and a decision needs the keyboard",
		ev: session.Event{Kind: session.EventStandingProposal}},
	{name: "EventStandingUpdate", chatOnly: "standing orders belong to the session, not to one node's work",
		ev: session.Event{Kind: session.EventStandingUpdate}},
	{name: "EventOrchestrateNote", chatOnly: "an adaptive run is a GRAPH and not a transcript; its page draws " +
		"the graph (roomorch.go)", ev: session.Event{Kind: session.EventOrchestrateNote, Text: "planning"}},
	{name: "EventOrchestrateFuel", chatOnly: "the same graph's budget", ev: session.Event{Kind: session.EventOrchestrateFuel}},
	{name: "EventOrchestratePause", chatOnly: "the same graph, paused", ev: session.Event{Kind: session.EventOrchestratePause}},
	{name: "EventSubharnessAsk", chatOnly: "a choice between arms is a question (harnesspanel.go)",
		ev: session.Event{Kind: session.EventSubharnessAsk}},
	{name: "EventSubharnessStep", chatOnly: "one step of the run behind that question", ev: session.Event{Kind: session.EventSubharnessStep}},
	{name: "EventSubharnessProposal", chatOnly: "the proposal that question settles into", ev: session.Event{Kind: session.EventSubharnessProposal}},
	{name: "EventSubharnessProposalOff", chatOnly: "that proposal withdrawn", ev: session.Event{Kind: session.EventSubharnessProposalOff}},
	{name: "EventTakeover", chatOnly: "a checkpoint take-over is the session's own act on the turn the person " +
		"typed", ev: session.Event{Kind: session.EventTakeover}},
}

// errSalience is the engine failing, in the one shape both pumps read.
var errSalience = errSalienceText("the provider closed the stream")

type errSalienceText string

func (e errSalienceText) Error() string { return string(e) }

// TestEveryEventKindIsInTheSalienceTable is the enumeration gate.
//
// It reads the kinds back out of internal/session's own const block rather than
// trusting a list kept by hand, because a list kept by hand is exactly what the
// task room's ingestion was: correct on the day it was written and one kind
// behind by the next wave. A kind added to the engine and not considered here
// fails the build, and considering it costs one line.
func TestEveryEventKindIsInTheSalienceTable(t *testing.T) {
	declared := declaredEventKinds(t)
	tabled := map[string]bool{}
	for _, c := range salienceTable {
		if tabled[c.name] {
			t.Fatalf("%s is in the salience table twice", c.name)
		}
		tabled[c.name] = true
	}
	var missing, unknown []string
	for _, name := range declared {
		if !tabled[name] {
			missing = append(missing, name)
		}
	}
	for name := range tabled {
		if !contains(declared, name) {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(missing) > 0 {
		t.Fatalf("internal/session sends %v and the salience table has never heard of them.\n"+
			"Add a row: the event as a person would meet it, and — only if the fact it carries "+
			"cannot reach a node at all — the reason. THE LENS MAY LOWER SALIENCE; IT MAY NOT "+
			"DROP A FACT.", missing)
	}
	if len(unknown) > 0 {
		t.Fatalf("the salience table names %v, which internal/session no longer declares", unknown)
	}
}

// declaredEventKinds is every EventKind identifier in internal/session's const
// block, read off the source.
func declaredEventKinds(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../session/session.go")
	if err != nil {
		t.Fatalf("reading the engine's event list: %v", err)
	}
	body := string(raw)
	start := strings.Index(body, "EventTextDelta EventKind = iota")
	if start < 0 {
		t.Fatal("internal/session/session.go no longer opens its event const block with " +
			"EventTextDelta; this test reads that block and has to be pointed at the new one")
	}
	end := strings.Index(body[start:], "\n)\n")
	if end < 0 {
		t.Fatal("the engine's event const block has no end")
	}
	line := regexp.MustCompile(`(?m)^\tEvent[A-Za-z]+ ?(?:EventKind = iota)?$`)
	var out []string
	for _, m := range line.FindAllString(body[start-len("\tEventTextDelta EventKind = iota")+len("\t"):start+end], -1) {
		out = append(out, strings.Fields(m)[0])
	}
	if len(out) < 20 {
		t.Fatalf("only %d event kinds were read out of internal/session; the scan is broken, "+
			"not the engine", len(out))
	}
	return out
}

func contains(all []string, one string) bool {
	for _, s := range all {
		if s == one {
			return true
		}
	}
	return false
}

// TestALensLowersSalienceAndNeverDropsAFact is the contract itself.
//
// Each kind is driven through the conversation's pump and through a node's, and
// what the two produce is compared BY KIND OF BLOCK rather than by text: a
// room is allowed to word a fact differently, fold it, or draw it dimmer — the
// design is a lens over one transcript, not a second transcript — and what it
// may not do is have no block for it at all.
func TestALensLowersSalienceAndNeverDropsAFact(t *testing.T) {
	for _, c := range salienceTable {
		t.Run(c.name, func(t *testing.T) {
			chat, room := salienceRun(t, c.ev)
			if c.chatOnly != "" {
				if len(room) > 0 {
					t.Fatalf("%s is listed as the conversation's alone — %s — but a node's page "+
						"drew %v for it. Either the reason has stopped being true and the row "+
						"should lose it, or the room has grown something it was never meant to.",
						c.name, c.chatOnly, kindWords(room))
				}
				return
			}
			for _, kind := range kindsOf(chat) {
				if !hasKind(room, kind) {
					t.Fatalf("THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A FACT.\n"+
						"%s draws %s in the conversation and nothing of that kind in a node's "+
						"room (chat: %v, room: %v).\nWire it in [feed.ingest], where both pages "+
						"read it — or, if the fact genuinely cannot reach a node, give the "+
						"table's row the reason.",
						c.name, kindWord(kind), kindWords(chat), kindWords(room))
				}
			}
		})
	}
}

// A ROOM SHOWS WHAT A CALL TOOK, which is the plainest thing the copy in
// room.go could not do: EventToolFinished was not in its switch at all, so a
// node's row never carried the figure and a person checking on work could not
// tell a call that took a moment from one that took four seconds.
func TestANodesRowSaysWhatTheCallTook(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.room = a.newRoom(7, "port the parser")
	a.room.turn = 1
	for _, ev := range []session.Event{
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Hint: "bash go test ./...",
			Args: `{"command":"go test ./..."}`},
		{Kind: session.EventToolFinished, CallID: "c1", Tool: "bash",
			Args: `{"command":"go test ./..."}`, Took: 4 * time.Second},
		{Kind: session.EventToolEnd, CallID: "c1", Tool: "bash",
			Args: `{"command":"go test ./..."}`, Output: "ok"},
	} {
		a.roomEvent(ev)
	}
	if n := len(a.room.entries); n != 1 {
		t.Fatalf("one call drew %d rows: %v", n, kindWords(a.room.entries))
	}
	if got := a.room.entries[0].ran; got != 4*time.Second {
		t.Fatalf("the node's row says the call took %v, want 4s", got)
	}
	// Inspect the detailed call through its live outline before checking the
	// displayed duration; compact activity deliberately omits tool telemetry.
	openRoomCompactWork(t, a)
	// The page's own spelling of it (toolstat.go), asserted as a reader meets it
	// rather than as the field holds it.
	if page := plain(strings.Join(roomLines(a), "\n")); !strings.Contains(page, "4.0s") {
		t.Fatalf("the figure never reached the page:\n%s", page)
	}
}

// AND TWO CALLS OF ONE NAME RESOLVE BY THEIR ARGUMENTS, on BOTH pages — ruling
// 3 of docs/design/lens/DESIGN.md. The chat used to take the oldest row of that
// name, which is right by convention and wrong half the time two same-name
// calls are in flight at once.
func TestAnEndPairsByArgumentsInTheConversationToo(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.turn = 1
	for _, ev := range []session.Event{
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Args: `{"command":"go test ./internal/config/"}`},
		{Kind: session.EventToolBegin, CallID: "c2", Tool: "bash", Args: `{"command":"go test ./internal/parse/"}`},
		// The SECOND call comes back first, which is what a parallel batch does.
		{Kind: session.EventToolEnd, CallID: "c2", Tool: "bash",
			Args: `{"command":"go test ./internal/parse/"}`, Output: "ok  internal/parse"},
	} {
		a.ingest(ev)
	}
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool {
			continue
		}
		parse := strings.Contains(e.detail.Args, "internal/parse")
		if parse == e.status.live() {
			t.Fatalf("the end settled the wrong row: %q is %v", e.detail.Args, e.status)
		}
	}
}

// salienceRun drives one event through both pumps and hands back the two lists.
//
// The two pages are two apps and not one with a room open, so that neither can
// borrow anything from the other's state: what is being compared is what an
// event DOES to a transcript, and a shared clock or a shared turn counter would
// be one more thing they agreed about by accident.
func salienceRun(t *testing.T, ev session.Event) (chat, room []entry) {
	t.Helper()
	participant := newTestApp(&fakeAgent{model: "m"})
	participant.turn = 1
	participant.apply(ev)

	overseer := newTestApp(&fakeAgent{model: "m"})
	overseer.room = overseer.newRoom(7, "port the parser")
	overseer.room.turn = 1
	overseer.roomEvent(ev)

	return participant.entries, overseer.room.entries
}

func kindsOf(es []entry) []entryKind {
	seen := map[entryKind]bool{}
	var out []entryKind
	for _, e := range es {
		if !seen[e.kind] {
			seen[e.kind] = true
			out = append(out, e.kind)
		}
	}
	return out
}

func hasKind(es []entry, kind entryKind) bool {
	for _, e := range es {
		if e.kind == kind {
			return true
		}
	}
	return false
}

func kindWords(es []entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, kindWord(e.kind))
	}
	return out
}

// kindWord names a block for a failure message. It is a table rather than a
// String method on the type because it exists for this test alone: nothing a
// person reads ever names a block's kind.
func kindWord(k entryKind) string {
	switch k {
	case entryUser:
		return "a person's line"
	case entryAssistant:
		return "an answer"
	case entryTool:
		return "a call"
	case entryDivider:
		return "a divider"
	case entryCompact:
		return "a compaction"
	case entryNote:
		return "a note"
	case entryThinking:
		return "reasoning"
	case entrySteer:
		return "an elbow"
	case entryTask:
		return "a proposal"
	case entryDone:
		return "a finished task"
	case entryHarness:
		return "a design"
	case entryConnect:
		return "a sign-in"
	}
	return "block " + itoa(int(k))
}

// roomLines is the node's page as a reader sees it, without the fold and the
// header the room draws around it.
func roomLines(a *app) []string {
	var out []string
	for _, r := range a.roomRows(a.bodyWidth()) {
		out = append(out, r.text)
	}
	return out
}
