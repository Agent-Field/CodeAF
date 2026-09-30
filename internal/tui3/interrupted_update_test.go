package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Exercise response ownership through successive transitions, rather than
// assembling the desired final entry flags by hand.
func TestInterruptedUpdatesAcrossStopSteerRetryAndSharedDecks(t *testing.T) {
	for _, ending := range []string{"stop", "steer", "retry"} {
		for _, width := range []int{32, 100} {
			for _, surface := range []struct {
				name string
				lens lens
			}{{"chat", participantLens}, {"task", overseerLens}, {"nested", transcriptLens}} {
				t.Run(fmt.Sprintf("%s/%s/%d", ending, surface.name, width), func(t *testing.T) {
					a := newTestApp(&fakeAgent{model: "m"})
					a.workMode = config.WorkFold
					a.turn = 1
					a.state = stateWorking
					a.said(entry{kind: entryUser, text: "Check files", turn: 1})
					a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] First result ready."})
					runBatch(&a.feed, "read", "done")
					a.steerAccepted(&session.SteerNote{ID: 1, Words: "Check exports too"})
					a.steerConsumed(&session.SteerNote{ID: 1})
					a.ingest(session.Event{Kind: session.EventCompacting, Hint: "PRIVATE COMPACTION"})
					a.ingest(session.Event{Kind: session.EventCompacted, Hint: "PRIVATE COMPACTION FINISHED"})
					a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] Second result"})
					a.ingest(session.Event{Kind: session.EventReasoning, Text: "PRIVATE REASONING"})
					a.ingest(session.Event{Kind: session.EventTextDelta, Text: " arriving."})
					a.ingest(session.Event{Kind: session.EventToolForming, Tool: "bash", CallID: "abandoned", ArgsText: "PRIVATE ABANDONED PAYLOAD"})
					wanted := []string{"First result ready.", "Check exports too"}
					hidden := []string{"PRIVATE ABANDONED PAYLOAD", "PRIVATE COMPACTION", "PRIVATE REASONING", "[update]"}
					switch ending {
					case "steer":
						a.steerAccepted(&session.SteerNote{ID: 2, Words: "Use Decimal"})
						a.steerConsumed(&session.SteerNote{ID: 2})
						wanted = append(wanted, "Use Decimal")
						a.ingest(session.Event{Kind: session.EventSteerConsumed, Steer: &session.SteerNote{ID: 2}})
						wanted = append(wanted, "Second result arriving.")
						a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] Export result arriving."})
						wanted = append(wanted, "Export result arriving.")
					case "retry":
						a.ingest(session.Event{Kind: session.EventRetrying, Hint: "retrying"})
						hidden = append(hidden, "Second result arriving.")
						a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] Replacement arriving."})
						wanted = append(wanted, "Replacement arriving.")
					default:
						wanted = append(wanted, "Second result arriving.")
					}
					a.interrupt()
					a.settle()
					for _, e := range a.entries {
						if e.kind == entryAssistant && e.cut && confirmedAnswer(&e) {
							t.Fatal("interrupted update certified complete")
						}
					}
					d := deck{entries: a.entries, lens: surface.lens}
					assertInterruptedDeck(t, a, d, width, wanted, hidden)
					// Opening and closing disclosure must not change the conversation.
					d.workOpen = map[int]bool{}
					d.capOpen = map[int]bool{}
					for _, f := range a.deckFolds(d) {
						d.workOpen[f.key] = true
					}
					for i := range d.entries {
						d.capOpen[i] = true
					}
					a.deckRows(d, width)
					d.workOpen = nil
					d.capOpen = nil
					assertInterruptedDeck(t, a, d, width, wanted, hidden)
				})
			}
		}
	}
}

func assertInterruptedDeck(t *testing.T, a *app, d deck, width int, wanted, hidden []string) {
	t.Helper()
	rows, _ := a.deckRows(d, width)
	var lines []string
	activity := 0
	for _, r := range rows {
		lines = append(lines, plain(r.text))
		if r.hit == hitWorkFold {
			activity++
		}
	}
	page := strings.Join(lines, "\n")
	for _, s := range wanted {
		if strings.Count(page, s) != 1 {
			t.Fatalf("want once %q:\n%s", s, page)
		}
	}
	for _, s := range hidden {
		if strings.Contains(page, s) {
			t.Fatalf("leaked %q:\n%s", s, page)
		}
	}
	if !strings.Contains(page, "interrupted") {
		t.Fatalf("partial response lacks interrupted status:\n%s", page)
	}
	if activity > liveStepRows {
		t.Fatalf("%d activity rows:\n%s", activity, page)
	}
}

func TestInterruptedUpdateJournalReopensWithoutPromotingNarration(t *testing.T) {
	journal := []byte(`{"type":"message","role":"user","content":"Check files"}
{"type":"message","role":"assistant","content":"[update] First result ready.","presentation":{"audience":"human"}}
{"type":"message","role":"assistant","content":"[update] Second result arriving.\n\n[incomplete tool call dropped when you steered]","presentation":{"audience":"human","text":"[update] Second result arriving.","interrupted":true}}
{"type":"message","role":"assistant","content":"PRIVATE UNFINISHED NARRATION","presentation":{"audience":"operational","interrupted":true}}
{"type":"message","role":"assistant","content":"HANDOVER BOOKKEEPING\nRETAINED SECOND LINE","presentation":{"audience":"operational"}}
`)
	record := session.ReadTranscriptBytes(journal)
	for _, surface := range []lens{participantLens, overseerLens, transcriptLens} {
		for _, width := range []int{32, 100} {
			a := newTestApp(&fakeAgent{model: "m"})
			a.workMode = config.WorkFold
			es, _ := a.replayBlocks(record.Entries, chatReplay(0))
			for _, e := range es {
				if e.text == "Second result arriving." && (confirmedAnswer(&e) || !interruptedUpdate(&e)) {
					t.Fatal("reopen lost incomplete human audience")
				}
			}
			d := deck{entries: es, lens: surface}
			assertInterruptedDeck(t, a, d, width, []string{"Check files", "First result ready.", "Second result arriving."}, []string{"PRIVATE UNFINISHED", "HANDOVER BOOKKEEPING", "RETAINED SECOND LINE", "incomplete tool call", "[update]"})
			d.workOpen = map[int]bool{}
			for _, f := range a.deckFolds(d) {
				d.workOpen[f.key] = true
			}
			d.capOpen = map[int]bool{}
			for i := range es {
				d.capOpen[i] = true
			}
			rows, _ := a.deckRows(d, width)
			var page strings.Builder
			for _, r := range rows {
				page.WriteString(plain(r.text))
				page.WriteByte('\n')
			}
			if !strings.Contains(page.String(), "PRIVATE UNFINISHED NARRATION") || !strings.Contains(page.String(), "RETAINED SECOND LINE") {
				t.Fatalf("disclosure lost interrupted narration:\n%s", page.String())
			}
		}
	}
}

func TestSourceAddressedTextKeepsAudienceAcrossBatchesAndStop(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn = 1
	a.state = stateWorking
	a.workMode = config.WorkFold
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[vision: reader]\n\n", Addressed: true})
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "The image has "})
	a.ingest(session.Event{Kind: session.EventReasoning, Text: "PRIVATE REASONING"})
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "two panels."})
	a.interrupt()
	a.settle()
	for _, l := range []lens{participantLens, overseerLens, transcriptLens} {
		assertInterruptedDeck(t, a, deck{entries: a.entries, lens: l}, 40, []string{"[vision: reader]", "The image has two panels."}, []string{"PRIVATE REASONING", "[update]"})
	}
	a.turn++
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "ordinary next turn"})
	runBatch(&a.feed, "read", "next")
	for _, e := range a.entries {
		if e.turn == a.turn && e.kind == entryAssistant && e.addressed {
			t.Fatal("source audience leaked into next turn")
		}
	}
}

func TestTextCoalescingKeepsSourceAudienceBoundary(t *testing.T) {
	internal := session.Event{Kind: session.EventTextDelta, Text: "internal"}
	human := session.Event{Kind: session.EventTextDelta, Text: "human", Addressed: true}
	if foldsInto(internal, human) || foldsInto(human, internal) {
		t.Fatal("coalescing crossed source audience boundary")
	}
	if !foldsInto(human, human) {
		t.Fatal("same-audience text no longer coalesces")
	}
}

func TestInterruptedUpdateRejectsInFlightCompletionAfterEscape(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn = 1
	a.state = stateWorking
	a.workMode = config.WorkFold
	stream := make(chan session.Event, 3)
	a.stream = stream
	a.apply(session.Event{Kind: session.EventTextDelta, Text: "[update] A finding is arriving."})
	a.interrupt()
	// Use the actual application event gate: these events were already queued
	// when Escape arrived, but cannot create another response after the stop.
	a.Update(streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTextDelta, Text: "LATE WORDS"}})
	a.Update(streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventAssistantDone}})
	a.Update(streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolForming, Tool: "bash", CallID: "late", ArgsText: "LATE PAYLOAD"}})
	close(stream)
	a.Update(streamClosedMsg{gen: a.gen})
	for _, e := range a.entries {
		if e.kind == entryAssistant && e.text != "" && (!interruptedUpdate(&e) || confirmedAnswer(&e)) {
			t.Fatal("late completion promoted interrupted update")
		}
	}
	assertInterruptedDeck(t, a, deck{entries: a.entries, lens: participantLens}, 60, []string{"A finding is arriving."}, []string{"LATE WORDS", "LATE PAYLOAD", "[update]"})
}
