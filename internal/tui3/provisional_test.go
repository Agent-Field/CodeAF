package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A provider can spend seconds writing a tool preamble before announcing the
// call. The words are already available, but their role is not yet known.
func TestStreamingPreambleStaysCompactBeforeTheToolArrives(t *testing.T) {
	a := liveStepsApp(t)
	a.linear = true
	a.entries[len(a.entries)-1].status = toolOK
	a.entries[len(a.entries)-1].ended = a.now()
	const prose = "Agent-Field has sixty repositories. Now pulling full star histories with timestamps and stargazer profiles so I can compute recent growth and spot notable people."
	a.event(session.Event{Kind: session.EventTextDelta, Text: prose})
	page := livePage(a)
	if !strings.Contains(page, "Agent-Field has sixty repositories") || strings.Contains(page, "Now pulling full star histories") {
		t.Fatalf("unclassified prose escaped its compact heading:\n%s", page)
	}
	a.clock = func() time.Time { return liveStepsBase.Add(35 * time.Second) }
	a.touch()
	if page = livePage(a); strings.Contains(page, "stargazer profiles") {
		t.Fatalf("waiting promoted the preamble:\n%s", page)
	}
	a.event(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "stars", Hint: "bash gh api", Args: `{"command":"gh api"}`})
	if page = livePage(a); strings.Contains(page, "stargazer profiles") {
		t.Fatalf("tool arrival exposed the preamble:\n%s", page)
	}
	showLiveWork(t, a)
	a.setCapOpen(a.conversation(), len(a.entries)-2, true)
	if page = livePage(a); !strings.Contains(page, "stargazer profiles") {
		t.Fatalf("disclosure lost the original prose:\n%s", page)
	}
}

// Confirmation arrives at the response boundary, before later completion
// checks. Questions use the same path and cannot wait for a whole-turn finish.
func TestConfirmedResponseShowsItsWholeAnswerBeforeTurnDone(t *testing.T) {
	for _, text := range []string{
		"The repositories are listed below. The full answer contains the recent growth and the people who starred them.",
		"Which organization should I inspect? Please give its exact GitHub name so I can continue.",
	} {
		a := liveStepsApp(t)
		a.linear = true
		a.entries[len(a.entries)-1].status = toolOK
		a.entries[len(a.entries)-1].ended = a.now()
		a.event(session.Event{Kind: session.EventTextDelta, Text: text})
		if strings.Contains(livePage(a), "so I can continue") || strings.Contains(livePage(a), "the people who starred") {
			t.Fatal("fixture already exposed full response")
		}
		a.event(session.Event{Kind: session.EventAssistantDone})
		if a.state != stateWorking {
			t.Fatal("response confirmation ended the whole turn")
		}
		rows, _ := a.deckRows(a.conversation(), 200)
		var page strings.Builder
		for _, r := range rows {
			page.WriteString(plain(r.text))
			page.WriteByte('\n')
		}
		if !strings.Contains(strings.Join(strings.Fields(page.String()), " "), text) {
			t.Fatalf("confirmed response remained hidden:\n%s", page.String())
		}
		if a.entries[len(a.entries)-1].demoted {
			t.Fatal("confirmed response still has progress styling")
		}
	}
}

func TestTaskRoomUsesTheSameResponseConfirmation(t *testing.T) {
	a, _, _ := roomApp(t)
	a.room = a.newRoom(7, "Review stars")
	a.room.done = false
	a.room.turn = 1
	a.room.entries = []entry{{kind: entryUser, text: "Review stars", turn: 1}}
	const text = "The star history is ready. Recent growth is concentrated in the main repository."
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{Kind: session.EventTextDelta, Text: text}})
	if strings.Contains(roomText(a), "Recent growth") {
		t.Fatal("room exposed provisional body")
	}
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{Kind: session.EventAssistantDone}})
	if !strings.Contains(roomText(a), "Recent growth") {
		t.Fatalf("room lost confirmed answer:\n%s", roomText(a))
	}
}

func TestResponseConfirmationSurvivesTrailingReasoning(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state, a.turn, a.linear = stateWorking, 1, true
	a.event(session.Event{Kind: session.EventTextDelta, Text: "The star history is ready. Recent growth is concentrated in the main repository."})
	a.event(session.Event{Kind: session.EventReasoning, Text: "checking the result one last time"})
	a.event(session.Event{Kind: session.EventAssistantDone})
	if page := livePage(a); !strings.Contains(page, "Recent growth") {
		t.Fatalf("trailing reasoning hid the confirmed reply:\n%s", page)
	}
	for _, e := range a.entries {
		if e.kind == entryAssistant && e.demoted {
			t.Fatal("confirmed reply retained progress styling")
		}
	}
	a.event(session.Event{Kind: session.EventToolBegin, Tool: "read", CallID: "later", Args: `{"path":"later.md"}`})
	if page := livePage(a); strings.Contains(page, "Recent growth") {
		t.Fatalf("new work failed to compact the earlier reply:\n%s", page)
	}
}

func TestConfirmationPromotesEveryInterleavedAnswerFragment(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state, a.turn, a.linear = stateWorking, 1, true
	a.event(session.Event{Kind: session.EventTextDelta, Text: "The first answer section. Its complete details must remain visible."})
	a.event(session.Event{Kind: session.EventReasoning, Text: "checking another detail"})
	a.event(session.Event{Kind: session.EventTextDelta, Text: "The second answer section. More complete details belong to the same reply."})
	a.event(session.Event{Kind: session.EventAssistantDone})
	page := livePage(a)
	for _, want := range []string{"Its complete details", "More complete details"} {
		if !strings.Contains(page, want) {
			t.Fatalf("confirmation hid %q:\n%s", want, page)
		}
	}
	for _, e := range a.entries {
		if e.kind == entryAssistant && e.demoted {
			t.Fatal("confirmed fragment retained work styling")
		}
	}
	a.event(session.Event{Kind: session.EventTextDelta, Text: "A later response is still unclassified. It must not keep the previous answer promoted."})
	a.touch()
	if page = livePage(a); strings.Contains(page, "Its complete details") {
		t.Fatalf("later response failed to demote prior answer:\n%s", page)
	}
}

func TestConfirmationDoesNotAdoptAnEarlierFailedAttempt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state, a.turn, a.linear = stateWorking, 1, true
	a.event(session.Event{Kind: session.EventTextDelta, Text: "Discarded attempt opening. These details must not become the successful answer."})
	a.event(session.Event{Kind: session.EventReasoning, Text: "private retry boundary"})
	a.event(session.Event{Kind: session.EventTextDelta, Text: "Discarded attempt tail."})
	a.event(session.Event{Kind: session.EventRetrying, Text: "trying again"})
	a.event(session.Event{Kind: session.EventTextDelta, Text: "The replacement answer. Its full result is ready."})
	a.event(session.Event{Kind: session.EventAssistantDone})
	page := livePage(a)
	if strings.Contains(page, "Discarded") {
		t.Fatalf("failed attempt remained visible:\n%s", page)
	}
	if !strings.Contains(page, "Its full result is ready") {
		t.Fatalf("replacement not promoted:\n%s", page)
	}
	for _, e := range a.entries {
		if strings.Contains(e.text, "Discarded") && e.confirmed != nil {
			t.Fatal("confirmation adopted a failed attempt")
		}
	}
}

// A provider can interleave private reasoning between sections of one answer.
// Settling the turn must preserve the whole answer on both reading surfaces.
func TestCompletedInterleavedAnswerKeepsEverySection(t *testing.T) {
	for _, room := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "room"}[room], func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.state, a.turn, a.linear = stateWorking, 1, true
			a.entries = []entry{{kind: entryUser, text: "Review stars", turn: 1}}
			if room {
				a.room = a.newRoom(7, "Review stars")
				a.room.done, a.room.turn = false, 1
				a.room.entries = append([]entry(nil), a.entries...)
			}
			for _, ev := range []session.Event{
				{Kind: session.EventToolBegin, Tool: "read", CallID: "notes", Args: `{"path":"hidden-notes.md"}`},
				{Kind: session.EventToolEnd, Tool: "read", CallID: "notes", Output: "notes read"},
				{Kind: session.EventTextDelta, Text: "First section. Its complete details remain visible."},
				{Kind: session.EventReasoning, Text: "PRIVATE INTERLEAVED REASONING"},
				{Kind: session.EventTextDelta, Text: "Second section. Its complete result also remains visible."},
				{Kind: session.EventAssistantDone},
				{Kind: session.EventTurnDone},
			} {
				if room {
					drive(t, a, roomEventMsg{gen: a.room.gen, ev: ev})
				} else {
					a.event(ev)
				}
			}
			if room {
				drive(t, a, roomClosedMsg{gen: a.room.gen})
			}
			a.touch()
			page := livePage(a)
			if room {
				page = roomText(a)
			}
			for _, want := range []string{"Its complete details", "Its complete result"} {
				if !strings.Contains(page, want) {
					t.Fatalf("completion hid %q:\n%s", want, page)
				}
			}
			for _, hidden := range []string{"PRIVATE INTERLEAVED", "hidden-notes.md", "thought for"} {
				if strings.Contains(page, hidden) {
					t.Fatalf("completion exposed %q:\n%s", hidden, page)
				}
			}
		})
	}
}
