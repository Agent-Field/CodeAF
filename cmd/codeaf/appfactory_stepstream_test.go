package main

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func stepJournal(t *testing.T, transcript string, words string) {
	t.Helper()
	journal, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.WriteString(`{"type":"message","role":"assistant","content":"` + words + `"}` + "\n"); err != nil {
		t.Fatal(err)
	}
}

func appendStepPerson(transcript, words string) error {
	journal, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer journal.Close()
	_, err = journal.WriteString(`{"type":"message","role":"user","content":"` + words + `"}` + "\n")
	return err
}

func heldStepChat(t *testing.T) (backend *appBackend, rec *appRecorder, transcript, id string) {
	t.Helper()
	backend, rec = heldOverFakes(t)
	transcript, id = jobConversation(t)
	if _, err := backend.holdJobChat(id); err != nil {
		t.Fatal(err)
	}
	return backend, rec, transcript, id
}

func said(rec *appRecorder, id string) (deltas []appwire.TurnDelta, entries []appwire.Entry) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data {
		switch d := data.(type) {
		case appwire.TurnDelta:
			if d.Agent == id {
				deltas = append(deltas, d)
			}
		case appwire.EntryAdded:
			if d.Entry.Agent == id {
				entries = append(entries, d.Entry)
			}
		}
	}
	return deltas, entries
}

func waitDeltas(t *testing.T, rec *appRecorder, id, want string) []appwire.TurnDelta {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		deltas, _ := said(rec, id)
		var text strings.Builder
		for _, d := range deltas {
			text.WriteString(d.Text)
		}
		if text.String() == want {
			return deltas
		}
		if time.Now().After(deadline) {
			t.Fatalf("the words said are %q, want %q", text.String(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A step's words reach the app as they come, and the entry that ends them has
// the id the listing answers, so the page replaces what it streamed.
func TestAStepsWordsAndItsEntryReachAHeldChat(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "I looked "})
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "at it."})
	deltas := waitDeltas(t, rec, id, "I looked at it.")
	if deltas[0].Msg != 0 || deltas[0].Turn < appStepTurnBase {
		t.Fatalf("the words are placed at %+v", deltas[0])
	}
	stepJournal(t, transcript, "I looked at it.")
	backend.stageEvent(transcript, session.Event{Kind: session.EventAssistantDone})
	live := addedSaying(t, rec, "I looked at it.")
	page, _, err := backend.Entries(id, "", 8)
	if err != nil {
		t.Fatal(err)
	}
	listed := page[len(page)-1]
	if live.ID != listed.ID || live.Text != listed.Text {
		t.Fatalf("said %+v, the listing answers %+v", live, listed)
	}
	if live.Turn != deltas[0].Turn || live.Msg == nil || *live.Msg != deltas[0].Msg {
		t.Fatalf("the entry %+v does not close the words %+v", live, deltas[0])
	}
}

// Caught up without the event: words said under a turn, then a reading of the
// conversation that never heard the boundary, closes those same words: the
// listing carries the turn and message they were said under.
func TestAMissedEntryIsListedUnderTheTurnOfItsWords(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	if err := appendStepPerson(transcript, "plan: read the code"); err != nil {
		t.Fatal(err)
	}
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "the words"})
	deltas := waitDeltas(t, rec, id, "the words")
	stepJournal(t, transcript, "the words")
	page, _, err := backend.Entries(id, "", 8)
	if err != nil {
		t.Fatal(err)
	}
	listed := page[len(page)-1]
	if listed.Text != "the words" || listed.Turn != deltas[0].Turn || listed.Msg == nil || *listed.Msg != deltas[0].Msg {
		t.Fatalf("listed %+v for the words %+v", listed, deltas[0])
	}
	backend.stageEvent(transcript, session.Event{Kind: session.EventTurnDone})
	again := addedSaying(t, rec, "the words")
	if again.ID != listed.ID {
		t.Fatalf("said %q, listed %q", again.ID, listed.ID)
	}
}

// A chat let go and held again says only what is new, on its new hold.
func TestAChatHeldAgainSaysOnlyWhatIsNew(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	stepJournal(t, transcript, "before")
	backend.stageEvent(transcript, session.Event{Kind: session.EventTurnDone})
	addedSaying(t, rec, "before")
	a, err := backend.find(id)
	if err != nil {
		t.Fatal(err)
	}
	backend.letGo(a)
	stepJournal(t, transcript, "while nobody held it")
	if _, err := backend.holdJobChat(id); err != nil {
		t.Fatal(err)
	}
	backend.stageEvent(transcript, session.Event{Kind: session.EventToolBegin})
	stepJournal(t, transcript, "after")
	backend.stageEvent(transcript, session.Event{Kind: session.EventTurnDone})
	addedSaying(t, rec, "after")
	for _, entry := range mustSaid(rec, id) {
		if entry.Text == "while nobody held it" {
			t.Fatalf("what the hold's first reading answers is said again: %+v", entry)
		}
	}
}

// Opened mid-turn, the next words are placed by the listing, after the answers
// already saved, and those are not said again.
func TestAChatOpenedMidTurnPlacesWordsByTheListing(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	stepJournal(t, transcript, "the first message")
	stepJournal(t, transcript, "the second message")
	page, _, err := backend.Entries(id, "", 8)
	if err != nil {
		t.Fatal(err)
	}
	last := page[len(page)-1]
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "the third"})
	deltas := waitDeltas(t, rec, id, "the third")
	if last.Msg == nil || deltas[0].Msg != *last.Msg+1 {
		t.Fatalf("placed at msg %d after %+v", deltas[0].Msg, last)
	}
	stepJournal(t, transcript, "the third")
	backend.stageEvent(transcript, session.Event{Kind: session.EventTurnDone})
	entry := addedSaying(t, rec, "the third")
	if entry.Msg == nil || *entry.Msg != deltas[0].Msg {
		t.Fatalf("the entry %+v does not close the words at %d", entry, deltas[0].Msg)
	}
	if _, entries := said(rec, id); len(entries) != 1 {
		t.Fatalf("said %+v; what was in the journal when the chat opened is not said again", entries)
	}
}

// A boundary told twice, or an entry already said, says nothing again.
func TestAnEntryIsSaidOnce(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	backend.stageEvent(transcript, session.Event{Kind: session.EventToolBegin})
	stepJournal(t, transcript, "once")
	backend.stageEvent(transcript, session.Event{Kind: session.EventToolEnd})
	addedSaying(t, rec, "once")
	backend.stageEvent(transcript, session.Event{Kind: session.EventToolEnd})
	backend.stageEvent(transcript, session.Event{Kind: session.EventTurnDone})
	stepJournal(t, transcript, "twice")
	backend.stageEvent(transcript, session.Event{Kind: session.EventTurnDone})
	addedSaying(t, rec, "twice")
	if _, entries := said(rec, id); len(entries) != 2 {
		t.Fatalf("said %+v", entries)
	}
}

// A fast step costs the app few events: its tokens are said together.
func TestAFastStepsTokensAreSaidTogether(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	var want strings.Builder
	for range 500 {
		backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "word "})
		want.WriteString("word ")
	}
	deltas := waitDeltas(t, rec, id, want.String())
	if len(deltas) > 3 {
		t.Fatalf("500 quick tokens took %d events", len(deltas))
	}
}

// A void attempt's words not yet said are dropped, and no retry notice is said,
// since a step's chat is never told the turn end that would clear it.
func TestARetriedAttemptIsDropped(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "lost"})
	backend.stageEvent(transcript, session.Event{Kind: session.EventRetrying})
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "kept"})
	waitDeltas(t, rec, id, "kept")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data {
		if _, ok := data.(appwire.TurnRetrying); ok {
			t.Fatal("a retry notice was said")
		}
	}
}

func mustSaid(rec *appRecorder, id string) []appwire.Entry {
	_, entries := said(rec, id)
	return entries
}

// Older entries of a conversation with a person's message are not said again
// when the first words give that message its turn.
func TestPlacingWordsSaysNoOlderEntryAgain(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	if err := appendStepPerson(transcript, "plan: read the code"); err != nil {
		t.Fatal(err)
	}
	stepJournal(t, transcript, "older")
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "newer"})
	waitDeltas(t, rec, id, "newer")
	stepJournal(t, transcript, "newer")
	backend.stageEvent(transcript, session.Event{Kind: session.EventAssistantDone})
	addedSaying(t, rec, "newer")
	if entries := mustSaid(rec, id); len(entries) != 1 {
		t.Fatalf("said %+v; only the new answer is news", entries)
	}
}

// The entry the very first event of a hold reports is said, not taken as read.
func TestTheFirstBoundaryOfAHoldSaysItsEntry(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	stepJournal(t, transcript, "already read")
	stepJournal(t, transcript, "just written")
	backend.stageEvent(transcript, session.Event{Kind: session.EventAssistantDone})
	addedSaying(t, rec, "just written")
	if entries := mustSaid(rec, id); len(entries) != 1 {
		t.Fatalf("said %+v", entries)
	}
}

// A message the app already numbered keeps its number: the words and the entry
// that closes them are under one turn, or the page draws both.
func TestWordsUnderAMessageTheAppNumberedUseItsTurn(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	if err := appendStepPerson(transcript, "from the person"); err != nil {
		t.Fatal(err)
	}
	a, err := backend.find(id)
	if err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { a.marks = map[int]appTurnMark{0: {turn: 7}} })
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "the words"})
	deltas := waitDeltas(t, rec, id, "the words")
	stepJournal(t, transcript, "the words")
	backend.stageEvent(transcript, session.Event{Kind: session.EventAssistantDone})
	entry := addedSaying(t, rec, "the words")
	if deltas[0].Turn != 7 || entry.Turn != 7 {
		t.Fatalf("words under turn %d, entry under turn %d, want 7", deltas[0].Turn, entry.Turn)
	}
}

// A withdrawal is never drawn, as the manager's is not, and the message after
// it keeps the number the listing gives it.
func TestAWithdrawalIsNotStreamed(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: session.NoChangeReply})
	time.Sleep(3 * appStepDeltaEvery)
	backend.stageEvent(transcript, session.Event{Kind: session.EventAssistantDone})
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "real words"})
	deltas := waitDeltas(t, rec, id, "real words")
	if deltas[0].Msg != 0 {
		t.Fatalf("placed at %d", deltas[0].Msg)
	}
}

// Words after more tool rows than one reading looks at are still placed after
// the person's message and the answers they follow.
func TestWordsAfterManyToolRowsArePlaced(t *testing.T) {
	backend, rec, transcript, id := heldStepChat(t)
	if err := appendStepPerson(transcript, "plan: read the code"); err != nil {
		t.Fatal(err)
	}
	stepJournal(t, transcript, "first answer")
	journal, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for i := range appStepLook + 6 {
		call := "c" + strconv.Itoa(i)
		_, err := journal.WriteString(`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"` + call + `","function":{"name":"read","arguments":"{}"}}]}` + "\n" +
			`{"type":"message","role":"tool","toolCallId":"` + call + `","content":"ok"}` + "\n")
		if err != nil {
			t.Fatal(err)
		}
	}
	backend.stageEvent(transcript, session.Event{Kind: session.EventTextDelta, Text: "the words"})
	deltas := waitDeltas(t, rec, id, "the words")
	if deltas[0].Turn != appStepTurn(0) {
		t.Fatalf("placed under turn %d, want %d", deltas[0].Turn, appStepTurn(0))
	}
}

// A token of a chat nobody holds allocates nothing: a performance gate.
func TestAStepTokenNobodyHoldsAllocatesNothing(t *testing.T) {
	backend := &appBackend{agents: map[string]*appAgent{}}
	event := session.Event{Kind: session.EventTextDelta, Text: "word "}
	allocs := testing.AllocsPerRun(100, func() {
		backend.stageEvent("/nowhere/01M6H9RGH7DN5DBV4KNYJ5NRW8/.cell/transcript.jsonl", event)
	})
	if allocs != 0 {
		t.Fatalf("%v allocations a token", allocs)
	}
}

// A chat the app does not hold costs nothing: nothing is read or kept for it,
// even of a journal that is not there.
func TestAStepNobodyHoldsReadsNothing(t *testing.T) {
	backend, rec := heldOverFakes(t)
	transcript, id := jobConversation(t)
	if err := os.Remove(transcript); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []session.EventKind{session.EventTextDelta, session.EventAssistantDone, session.EventTurnDone} {
		backend.stageEvent(transcript, session.Event{Kind: kind, Text: "x"})
	}
	backend.locked(func() {
		if backend.stepStreams[id] != nil {
			t.Error("a stream was kept")
		}
	})
	if deltas, entries := said(rec, id); len(deltas)+len(entries) != 0 {
		t.Fatalf("said %+v %+v", deltas, entries)
	}
}

// A token of a chat nobody holds is only the lookup of its id.
func BenchmarkAStepTokenNobodyHolds(b *testing.B) {
	backend := &appBackend{agents: map[string]*appAgent{}}
	event := session.Event{Kind: session.EventTextDelta, Text: "word "}
	b.ReportAllocs()
	for b.Loop() {
		backend.stageEvent("/nowhere/01M6H9RGH7DN5DBV4KNYJ5NRW8/.cell/transcript.jsonl", event)
	}
}

// A step's events are told where they are read, with its journal.
func TestAStageToldItsEventsWithItsJournal(t *testing.T) {
	type heard struct {
		file string
		kind session.EventKind
	}
	var got []heard
	hear := func(file string, event session.Event) { got = append(got, heard{file, event.Kind}) }
	previous := stageNews.Swap(&hear)
	t.Cleanup(func() { stageNews.Store(previous) })
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "word"}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	(&stageConversation{file: "the-journal"}).drain(events)
	want := []heard{{"the-journal", session.EventTextDelta}, {"the-journal", session.EventTurnDone}}
	if !slices.Equal(got, want) {
		t.Fatalf("heard %+v", got)
	}
}
