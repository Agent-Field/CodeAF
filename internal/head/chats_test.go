package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── chats: the product layer over sessions ─────────────────────────────────
//
// Three behaviours are pinned here, and each of them is a claim about what the
// person experiences rather than about a function's return value: a thread
// resumed after a gap opens with its own arc; a pivot taken as its own thread
// moves the window and carries their words over untouched; and a conversation
// with something hanging in it stays visible while a settled one sinks.

func postAgentLine(t *testing.T, graph *store.Store, session, body string) store.Message {
	t.Helper()
	message, err := graph.PostMessage(store.Message{
		SessionID: session, Role: store.RoleAgent, Body: body,
	})
	if err != nil {
		t.Fatalf("post agent %q: %v", body, err)
	}
	return message
}

// A conversation picked up after a gap is met with its arc: what it is about,
// what it opened on, where it was left, and the one thing still hanging. All of
// it read off the journal, none of it a provider call.
func TestReentryBriefRendersTheArcAndTheOpenThing(t *testing.T) {
	graph := openHeadStore(t)
	session := "pricing"
	if _, err := graph.OpenSession(session, "Pricing rework", "tui"); err != nil {
		t.Fatal(err)
	}
	spliceHeadJob(t, graph, session, "audit", "Pricing audit")
	postUser(t, graph, session, "can we redo how the pricing page reads")
	postAgentLine(t, graph, session, "Put the audit in hand.")
	postUser(t, graph, session, "what about the discount tier?")

	head := New(nil, graph)
	arriving := postUser(t, graph, session, "right, where were we")
	brief := head.threadBrief(session, arriving.Seq, time.Now())

	for _, want := range []string{
		`"Pricing rework"`,
		"It opened on: \"can we redo how the pricing page reads\"",
		"Pricing audit",
		"Where it was left:",
		"Still open: they said \"what about the discount tier?\" and you never answered",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the arc does not carry %q:\n%s", want, brief)
		}
	}
	if len(brief) > threadBriefBytes {
		t.Fatalf("the arc is %d bytes, over its %d ceiling", len(brief), threadBriefBytes)
	}
}

// The brief is for RE-ENTRY, so it costs an ordinary next message nothing. The
// gap is measured from the last thing the room said back, which is what makes a
// run of messages typed in one breath a single continuous conversation rather
// than a re-entry into itself.
func TestReentryBlockFiresOnlyAfterTheGap(t *testing.T) {
	graph := openHeadStore(t)
	session := "gap"
	if _, err := graph.OpenSession(session, "Deploy triage", "tui"); err != nil {
		t.Fatal(err)
	}
	postUser(t, graph, session, "why do the deploys keep failing")
	reply := postAgentLine(t, graph, session, "Two of them timed out on the migration step.")
	next := postUser(t, graph, session, "and the third?")

	head := New(nil, graph)
	if block := head.reentryBlock(next, reply.Time.Add(threadGap-time.Minute)); block != "" {
		t.Fatalf("a conversation still in progress was treated as a re-entry:\n%s", block)
	}

	block := head.reentryBlock(next, reply.Time.Add(threadGap+time.Minute))
	if block == "" {
		t.Fatal("a conversation resumed after the gap got no arc at all")
	}
	if !strings.HasPrefix(block, "Picking this thread back up") {
		t.Fatalf("the block does not announce itself as a re-entry:\n%s", block)
	}
	if !strings.Contains(block, "Open your reply by re-entering that arc") {
		t.Fatalf("the model was never told to speak the arc before answering:\n%s", block)
	}
}

// A brand-new room has no arc to re-enter, and inventing one would have the head
// reminiscing about a conversation that has not happened.
func TestReentryBlockSaysNothingInAFreshRoom(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	first := postUser(t, graph, "brand-new", "hello")
	if block := head.reentryBlock(first, time.Now().Add(72*time.Hour)); block != "" {
		t.Fatalf("a room with nothing behind it was given a re-entry brief:\n%s", block)
	}
}

// The split's whole contract with the surface is two journaled rows. This is the
// shape the other half of the product is built against, so it is asserted
// exactly: a system row in the OLD room carrying the room-switch part, and the
// person's own words reposted verbatim into the NEW one.
func TestSplitSettlementJournalsTheSwitchAndCarriesTheirWordsOver(t *testing.T) {
	graph := openHeadStore(t)
	session := "old-room"
	head := New(nil, graph)

	postUser(t, graph, session, "the pricing page copy is too long")
	postAgentLine(t, graph, session, "Trimmed it to three lines.")
	pivot := postUser(t, graph, session, "unrelated — why do the deploys keep failing?")

	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: "Take the deploys as their own thread?",
		Urgency: store.QuestionBlocking, Category: store.QuestionCategoryThreadSplit,
		DefaultAnswer: "1", Options: []store.QuestionOption{
			{Label: "Its own thread", Value: askOptionPrefix + "1"},
			{Label: "Keep it here", Value: askOptionPrefix + "2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}

	yes := postUser(t, graph, session, "1")
	handled, err := head.answerAgentQuestion(context.Background(), yes)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("a settled split let the turn carry on, so the head would answer in the room they just left")
	}

	old, err := graph.Messages(session, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	switchRow := old[len(old)-1]
	if switchRow.Role != store.RoleSystem {
		t.Fatalf("the switch was spoken rather than marked: role=%q", switchRow.Role)
	}
	if switchRow.Body != roomSwitchBody {
		t.Fatalf("the old room's last line is %q, want %q", switchRow.Body, roomSwitchBody)
	}
	if len(switchRow.Parts) != 1 {
		t.Fatalf("the switch carries %d parts, want exactly one: %+v", len(switchRow.Parts), switchRow.Parts)
	}
	newRoom, ok := store.RoomSwitchTarget(switchRow.Parts[0])
	if !ok {
		t.Fatalf("the row carries no room-switch part: %+v", switchRow.Parts[0])
	}

	// The new room exists as a room, unnamed — the scribe names it from its first
	// exchange on its ordinary post-turn lane, and a title minted here would be a
	// second namer to keep in step with the first.
	session2, found, err := graph.Session(newRoom)
	if err != nil || !found {
		t.Fatalf("the room the switch points at does not exist: %v found=%t", err, found)
	}
	if strings.TrimSpace(session2.Title) != "" {
		t.Fatalf("the new room was named at mint time: %q", session2.Title)
	}

	carried, err := graph.Messages(newRoom, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(carried) != 1 {
		t.Fatalf("the new room opened with %d rows, want exactly their pivot: %+v", len(carried), carried)
	}
	if carried[0].Role != store.RoleUser {
		t.Fatalf("their words were carried over under the wrong role: %q", carried[0].Role)
	}
	if carried[0].Body != pivot.Body {
		t.Fatalf("their words were not carried over verbatim:\n got %q\nwant %q", carried[0].Body, pivot.Body)
	}

	// And the answer was still counted, because a settled split is a settled ask.
	stat, err := graph.QuestionCategoryStats(store.QuestionCategoryThreadSplit)
	if err != nil {
		t.Fatal(err)
	}
	if stat.N != 1 || stat.Accepted != 1 {
		t.Fatalf("the split answer never reached the projection: %+v", stat)
	}
}

// The new room is nameable the moment it has an exchange in it, through exactly
// the lane every other room is named on.
func TestSplitRoomIsNamedByTheOrdinaryScribeLane(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	pivot := postUser(t, graph, "old", "unrelated — why do the deploys keep failing?")
	newRoom, err := head.splitThread("old", pivot)
	if err != nil {
		t.Fatal(err)
	}
	postAgentLine(t, graph, newRoom, "Two of them timed out on the migration step.")

	scribe := &scribeClientRecorder{answers: []string{"Deploy failures"}}
	New(scribe, graph).nameRoom(context.Background(), newRoom)

	named, found, err := graph.Session(newRoom)
	if err != nil || !found {
		t.Fatal(err)
	}
	if named.Title != "Deploy failures" {
		t.Fatalf("the scribe's ordinary lane did not name the new room: %q", named.Title)
	}
}

// Keeping it here is not a split. The answer is settled for the measurement and
// then travels on to the loop that asked, exactly as every other categorized ask
// does — nothing moves, and nobody's window jumps.
func TestSplitDeclinedLeavesTheRoomAlone(t *testing.T) {
	graph := openHeadStore(t)
	session := "stay"
	head := New(nil, graph)
	postUser(t, graph, session, "the pricing page copy is too long")
	postAgentLine(t, graph, session, "Trimmed it to three lines.")
	postUser(t, graph, session, "unrelated — why do the deploys keep failing?")

	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: "Take the deploys as their own thread?",
		Urgency: store.QuestionBlocking, Category: store.QuestionCategoryThreadSplit,
		DefaultAnswer: "1", Options: []store.QuestionOption{
			{Label: "Its own thread", Value: askOptionPrefix + "1"},
			{Label: "Keep it here", Value: askOptionPrefix + "2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	before, err := graph.Sessions()
	if err != nil {
		t.Fatal(err)
	}

	no := postUser(t, graph, session, "2")
	handled, err := head.answerAgentQuestion(context.Background(), no)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("the gates spent a declined split, so the loop that asked would never hear it")
	}
	after, err := graph.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("declining the offer still opened a room: %d rooms before, %d after", len(before), len(after))
	}
}

// Once the answer has been the same often enough, the offer stops being put and
// the move simply happens — with the switch row standing as the one clause said.
func TestSplitFlipsToActingWhenItHasBeenLearned(t *testing.T) {
	graph := openHeadStore(t)
	session := "learned-split"
	for index := 0; index < store.VOIMinSamples; index++ {
		answerSplitQuestion(t, graph, session, askOptionPrefix+"1")
	}
	if ask, _, err := graph.ShouldAsk(store.QuestionCategoryThreadSplit); err != nil || ask {
		t.Fatalf("the gate never closed on a consistently answered split: ask=%t err=%v", ask, err)
	}

	head := New(nil, graph)
	pivot := postUser(t, graph, session, "unrelated — why do the deploys keep failing?")
	run := &beltRun{head: head, user: pivot}
	result, failed := run.execute(beltToolAsk, beltArguments(t, map[string]any{
		"question": "Take that as its own thread?",
		"options":  []string{"Its own thread", "Keep it here"},
		"category": "split",
	}))
	if failed {
		t.Fatalf("the learned split reported failure: %s", result)
	}
	if !run.spoke {
		t.Fatal("the turn carried on after the window had already moved")
	}
	if !strings.Contains(result, "taken as its own thread") {
		t.Fatalf("the tool did not say what it did: %q", result)
	}
	if open, err := graph.OpenQuestions(session, 10); err != nil || len(open) != 0 {
		t.Fatalf("a question was put after the gate closed: %+v err=%v", open, err)
	}

	messages, err := graph.Messages(session, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	newRoom, ok := store.RoomSwitchTarget(last.Parts[0])
	if last.Role != store.RoleSystem || last.Body != roomSwitchBody || !ok {
		t.Fatalf("the assumed split did not journal the switch: %+v", last)
	}
	carried, err := graph.Messages(newRoom, 0, 10)
	if err != nil || len(carried) != 1 || carried[0].Body != pivot.Body {
		t.Fatalf("their words were not carried into the new room: %+v err=%v", carried, err)
	}
}

// answerSplitQuestion seeds one settled split ask the way the tool mints it, so
// the acceptance projection reads exactly the rows it will read in production.
func answerSplitQuestion(t *testing.T, graph *store.Store, session, resolution string) {
	t.Helper()
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: session, Text: "Its own thread?",
		Urgency: store.QuestionBlocking, Category: store.QuestionCategoryThreadSplit,
		DefaultAnswer: "1", Options: []store.QuestionOption{
			{Label: "Its own thread", Value: askOptionPrefix + "1"},
			{Label: "Keep it here", Value: askOptionPrefix + "2"},
		},
	})
	if err != nil {
		t.Fatalf("seed split question: %v", err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatalf("surface split question: %v", err)
	}
	answer := postUser(t, graph, session, resolution)
	if err := graph.ResolveQuestion(question.Seq, store.QuestionAnswered, resolution, answer.Seq); err != nil {
		t.Fatalf("settle split question: %v", err)
	}
}

// "Call this thread X" is a change like any other, and it reaches the room the
// two of them are standing in.
func TestChangeRenamesTheThreadTheyArePointingAt(t *testing.T) {
	graph := openHeadStore(t)
	session := "namer"
	if _, err := graph.OpenSession(session, "", "tui"); err != nil {
		t.Fatal(err)
	}
	head := New(nil, graph)
	run := &beltRun{head: head, user: postUser(t, graph, session, "call this thread pricing rework")}

	result, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": "this thread",
		"words":  "call this thread pricing rework",
	}))
	if failed {
		t.Fatalf("renaming the thread failed: %s", result)
	}
	named, found, err := graph.Session(session)
	if err != nil || !found {
		t.Fatal(err)
	}
	if named.Title != "pricing rework" {
		t.Fatalf("the room is called %q, want %q", named.Title, "pricing rework")
	}
}

// A room named by its id is reachable too, which is what makes the switcher's
// rows actionable from the conversation.
func TestChangeRenamesAThreadNamedByID(t *testing.T) {
	graph := openHeadStore(t)
	if _, err := graph.OpenSession("other-room", "", "tui"); err != nil {
		t.Fatal(err)
	}
	head := New(nil, graph)
	run := &beltRun{head: head, user: postUser(t, graph, "here", "rename that one")}

	result, failed := run.execute(beltToolChange, beltArguments(t, map[string]any{
		"target": "other-room",
		"words":  "call it deploy triage",
	}))
	if failed {
		t.Fatalf("renaming another room failed: %s", result)
	}
	named, _, err := graph.Session("other-room")
	if err != nil {
		t.Fatal(err)
	}
	if named.Title != "deploy triage" {
		t.Fatalf("the room is called %q, want %q", named.Title, "deploy triage")
	}
}

// A sentence that is a request rather than a name is refused rather than
// becoming a room title.
func TestThreadNameFromRefusesASentence(t *testing.T) {
	if name := threadNameFrom("we should probably rename this at some point when pricing settles"); name != "" {
		t.Fatalf("a whole sentence became a room name: %q", name)
	}
	if name := threadNameFrom("call this thread pricing rework"); name != "pricing rework" {
		t.Fatalf("the name was not read out of the sentence: %q", name)
	}
}

// "What's going on" is one question with two halves: the work that is running,
// and the conversations that are still hanging. The status read answers both.
func TestStatusReadNamesTheOpenThreads(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "pricing", "what about the discount tier?")
	postUser(t, graph, "settled", "what is the capital of France")
	postAgentLine(t, graph, "settled", "Paris.")
	if _, err := graph.RenameSession("pricing", "Pricing rework"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RenameSession("settled", "Geography"); err != nil {
		t.Fatal(err)
	}

	head := New(nil, graph)
	status := head.lensStatus("pricing")
	if !strings.Contains(status, "OPEN THREADS") {
		t.Fatalf("the whole-system read has no open-threads section:\n%s", status)
	}
	if !strings.Contains(status, "Pricing rework") {
		t.Fatalf("a hanging conversation is missing from the alive glance:\n%s", status)
	}
	if strings.Contains(status, "Geography") {
		t.Fatalf("a settled conversation is being reported as alive:\n%s", status)
	}
	if !strings.Contains(status, "left at: \"what about the discount tier?\"") {
		t.Fatalf("the open thread does not say where it was left:\n%s", status)
	}
}

// spliceHeadJob admits one job root stamped with the room that asked for it.
func spliceHeadJob(t *testing.T, graph *store.Store, session, id, title string) {
	t.Helper()
	err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Title: title, Brief: title, Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: title})
	if err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
}
