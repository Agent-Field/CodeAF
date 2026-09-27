package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestExplicitUpdateSurvivesToolsAtEveryMarkerSplit(t *testing.T) {
	for split := 0; split <= len(session.UserUpdatePrefix); split++ {
		a := newTestApp(&fakeAgent{model: "m"})
		a.workMode = config.WorkFold
		a.turn = 1
		a.entries = append(a.entries, entry{kind: entryUser, text: "Check both", turn: 1})
		first := session.UserUpdatePrefix[:split]
		a.ingest(session.Event{Kind: session.EventTextDelta, Text: first})
		for _, e := range a.entries {
			if e.kind == entryAssistant && strings.Contains(e.text, "[") {
				t.Fatalf("split %d leaked marker: %q", split, e.text)
			}
		}
		a.ingest(session.Event{Kind: session.EventTextDelta, Text: session.UserUpdatePrefix[split:] + "\n **First result"})
		a.ingest(session.Event{Kind: session.EventReasoning, Text: "private analysis"})
		a.ingest(session.Event{Kind: session.EventTextDelta, Text: " is ready.**"})
		runBatch(&a.feed, "read", "c1")
		a.ingest(session.Event{Kind: session.EventTextDelta, Text: "Routine tool narration."})
		runBatch(&a.feed, "read", "c2")
		a.ingest(session.Event{Kind: session.EventTextDelta, Text: "Both results are ready."})
		a.ingest(session.Event{Kind: session.EventAssistantDone})
		a.touch()
		page := strings.Join(plainRows(a), "\n")
		for _, want := range []string{"First result is ready.", "Both results are ready."} {
			if strings.Count(page, want) != 1 {
				t.Fatalf("split %d lost/duplicated %q:\n%s", split, want, page)
			}
		}
		for _, hidden := range []string{session.UserUpdatePrefix, "Routine tool narration", "private analysis", "**"} {
			if strings.Contains(page, hidden) {
				t.Fatalf("split %d leaked %q:\n%s", split, hidden, page)
			}
		}
	}
}

func TestUpdatePrefixPreservesOrdinaryTextAndIncompleteLiteral(t *testing.T) {
	for _, text := range []string{"[updated] report", "[up", "Mention [update] here", "  ordinary  trailing  ", "[update](url)"} {
		f := newFeed(feedHooks{})
		f.turn = 1
		for _, r := range text {
			f.ingest(session.Event{Kind: session.EventTextDelta, Text: string(r)})
		}
		f.ingest(session.Event{Kind: session.EventAssistantDone})
		want, _ := session.UserFacingUpdate(text)
		got := ""
		for _, e := range f.entries {
			if e.kind == entryAssistant {
				got += e.text
			}
		}
		if got != want {
			t.Fatalf("%q became %q, want %q", text, got, want)
		}
	}
}

func TestExplicitUpdateKeepsQueuedUserOrderAndSurvivesLaterStop(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn = 1
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] First"})
	a.said(entry{kind: entryUser, text: "Also check exports", turn: 1})
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: " result ready."})
	runBatch(&a.feed, "read", "c1")
	updateAt, userAt := -1, -1
	for i, e := range a.entries {
		if e.kind == entryAssistant && e.text == "First result ready." {
			updateAt = i
			if !confirmedAnswer(&e) {
				t.Fatal("marked update not confirmed before tools")
			}
		}
		if e.kind == entryUser && e.text == "Also check exports" {
			userAt = i
		}
	}
	if updateAt < 0 || userAt < updateAt {
		t.Fatalf("queued user reordered: update=%d user=%d", updateAt, userAt)
	}
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "unfinished later narration"})
	a.cutTurn(1)
	if a.entries[updateAt].cut || !confirmedAnswer(&a.entries[updateAt]) {
		t.Fatal("later stop withdrew delivered update")
	}
}

func TestUpdateMarkerDoesNotLeakAcrossRetryOrTurn(t *testing.T) {
	f := newFeed(feedHooks{})
	f.turn = 1
	f.ingest(session.Event{Kind: session.EventTextDelta, Text: "[up"})
	f.ingest(session.Event{Kind: session.EventRetrying, Hint: "retrying"})
	f.ingest(session.Event{Kind: session.EventTextDelta, Text: "ordinary preamble"})
	runBatch(&f, "read", "c1")
	for _, e := range f.entries {
		if e.kind == entryAssistant && confirmedAnswer(&e) {
			t.Fatal("retry inherited explicit update marker")
		}
	}
	f.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] "})
	f.turn++
	f.ingest(session.Event{Kind: session.EventTextDelta, Text: "ordinary next turn"})
	runBatch(&f, "read", "c2")
	for _, e := range f.entries {
		if e.turn == f.turn && e.kind == entryAssistant && confirmedAnswer(&e) {
			t.Fatal("next turn inherited update marker")
		}
	}
}

func TestExplicitUpdateStreamsAsAddressedButNotCompletedAcrossLenses(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn = 1
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] A result is arriving"})
	at := a.live
	if at < 0 || !a.entries[at].addressed || confirmedAnswer(&a.entries[at]) {
		t.Fatal("audience was confused with completion")
	}
	for _, l := range []lens{participantLens, overseerLens, transcriptLens} {
		d := deck{entries: a.entries, lens: l, runningTurn: 1}
		folds := a.deckFolds(d)
		if workEntry(d.entries, folds, at) {
			t.Fatal("marked live update treated as operational narration")
		}
		rows, _ := a.deckRows(d, 100)
		text := ""
		for _, r := range rows {
			text += plain(r.text) + "\n"
		}
		if !strings.Contains(text, "A result is arriving") || strings.Contains(text, "[update]") {
			t.Fatalf("live update hidden/leaking protocol: %s", text)
		}
	}
	a.cutTurn(1)
	if confirmedAnswer(&a.entries[at]) || !a.entries[at].cut {
		t.Fatal("interrupted partial update claimed completion")
	}
	if !workEntry(a.entries, nil, at) {
		t.Fatal("audience bypass promoted an interrupted partial update")
	}
}

func TestUpdateProbeDoesNotCrossIntoTextlessNewTurn(t *testing.T) {
	for _, boundary := range []struct {
		name  string
		event session.Event
	}{
		{"tool forming", session.Event{Kind: session.EventToolForming, Tool: "read"}},
		{"tool announced", session.Event{Kind: session.EventToolAnnounced, Tool: "read"}},
		{"tool begin", session.Event{Kind: session.EventToolBegin, Tool: "read"}},
		{"empty response", session.Event{Kind: session.EventAssistantDone}},
	} {
		for _, prefix := range []string{"[up", "[update] "} {
			t.Run(boundary.name+"/"+prefix, func(t *testing.T) {
				f := newFeed(feedHooks{})
				f.turn = 1
				f.ingest(session.Event{Kind: session.EventTextDelta, Text: prefix})
				f.closeLive()
				f.turn = 2
				f.ingest(boundary.event)
				for _, e := range f.entries {
					if e.turn == 2 && e.kind == entryAssistant {
						t.Fatalf("new turn inherited old response probe: %#v", e)
					}
				}
				f.ingest(session.Event{Kind: session.EventTextDelta, Text: "ordinary next turn"})
				f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "read"})
				found := false
				for _, e := range f.entries {
					if e.turn == 2 && e.kind == entryAssistant {
						found = true
						if e.text != "ordinary next turn" || e.addressed || confirmedAnswer(&e) {
							t.Fatalf("ordinary response inherited explicit audience: %#v", e)
						}
					}
				}
				if !found {
					t.Fatal("next turn lost legitimate text")
				}
			})
		}
	}
}
