package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type olderAsk struct {
	before int
	limit  int
}

type fakeOlderAgent struct {
	*fakeAgent
	all      []session.DisplayEntry
	base     int
	calls    []olderAsk
	err      error
	forceEnd bool
}

func newFakeOlderAgent(total, base int) *fakeOlderAgent {
	all := make([]session.DisplayEntry, 0, total)
	for i := 0; i < total; i++ {
		role := "user"
		prefix := "question "
		if i%2 == 1 {
			role = "assistant"
			prefix = "answer "
		}
		all = append(all, session.DisplayEntry{
			Role: role,
			Text: prefix + itoa(i),
		})
	}
	tail := append([]session.DisplayEntry(nil), all[base:]...)
	return &fakeOlderAgent{
		fakeAgent: &fakeAgent{model: "m", past: tail},
		all:       all,
		base:      base,
	}
}

func (f *fakeOlderAgent) TranscriptBase() int {
	return f.base
}

func (f *fakeOlderAgent) Older(_ context.Context, before, limit int) ([]session.DisplayEntry, bool, error) {
	f.calls = append(f.calls, olderAsk{before: before, limit: limit})
	if f.err != nil {
		return nil, false, f.err
	}
	if before <= 0 || before > len(f.all) {
		return nil, false, nil
	}
	from := max(0, before-limit)
	page := append([]session.DisplayEntry(nil), f.all[from:before]...)
	more := from > 0 && !f.forceEnd
	return page, more, nil
}

func liveOlderApp(t *testing.T, agent Agent) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	a.dismissWelcome()
	a.takeUp(Conversation{
		Agent: agent,
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
	}, true)
	a.entries = nil
	a.recordRows = 0
	a.turn = 0
	a.replay()
	return a
}

// V4/T11 (contract 1, 2, 3): scrolling up to the oldest entry held locally in a
// live view asks the agent's OlderReader for the page before it with limit<=200,
// draws the arrived page above what was there without moving the line under the
// reader's eye, keeps absolute index counting for the next ask, and removes the
// top marker once more is false.
func TestScrollingUpAtTheTopOfALiveChatAsksTheHostForAnOlderPage(t *testing.T) {
	agent := newFakeOlderAgent(1000, 960)
	a := liveOlderApp(t, agent)

	if !a.moreHistory() {
		t.Fatal("live view at base 960 reported moreHistory() == false")
	}
	if got := plainRows(a)[0]; !strings.Contains(got, "earlier") {
		t.Fatalf("top row did not draw the earlier marker: %q", got)
	}
	if len(agent.calls) != 0 {
		t.Fatalf("Older called on open before scrolling: %+v", agent.calls)
	}

	// Scroll to the top of the initial 40 entries (960..999).
	for a.offsetFor(len(a.visible(a.bodyWidth())), a.viewHeight()) > a.viewHeight()+1 {
		scrollBy(t, a, -1)
	}
	if len(agent.calls) != 0 {
		t.Fatalf("Older called before entering the top prefetch margin: %+v", agent.calls)
	}

	beforeWin, _ := a.window(a.bodyWidth(), a.viewHeight())
	if len(beforeWin) == 0 {
		t.Fatal("viewport window is empty before older page prefetch")
	}
	readingLine := plain(beforeWin[0].text)

	// One step into the prefetch margin triggers the first Older request (before=960).
	scrollBy(t, a, -1)
	if len(agent.calls) != 1 {
		t.Fatalf("Older call count = %d, want 1", len(agent.calls))
	}
	if agent.calls[0].before != 960 || agent.calls[0].limit <= 0 || agent.calls[0].limit > 200 {
		t.Fatalf("first Older call = %+v, want before=960 and 0 < limit <= 200", agent.calls[0])
	}

	// The 200-entry page (760..959) is drawn above what was there and the reading position holds.
	drawn := drawnText(a)
	for _, want := range []string{"question 760", "answer 959", "question 960", "answer 999"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("transcript after first older page is missing %q", want)
		}
	}
	if strings.Contains(drawn, "answer 759") {
		t.Fatal("transcript already contains entry 759 before the second Older ask")
	}
	for _, check := range []string{"question 760", "question 960", "answer 999"} {
		if n := strings.Count(drawn, check); n != 1 {
			t.Fatalf("entry %q appeared %d times, want 1", check, n)
		}
	}

	afterWin, _ := a.window(a.bodyWidth(), a.viewHeight())
	foundAt := -1
	for i, r := range afterWin {
		if plain(r.text) == readingLine {
			foundAt = i
			break
		}
	}
	if foundAt < 0 || foundAt > 2 {
		t.Fatalf("the line under the reader moved to row %d across the older page arrival (was 0): %q", foundAt, readingLine)
	}
	if !a.moreHistory() {
		t.Fatal("moreHistory() became false while Older returned more=true")
	}

	// A second scroll to the top asks with before=760; more=false ends backfilling.
	agent.forceEnd = true
	for a.offsetFor(len(a.visible(a.bodyWidth())), a.viewHeight()) > 0 {
		scrollBy(t, a, -a.scrollPage())
	}
	if len(agent.calls) != 2 {
		t.Fatalf("Older call count after second scroll to top = %d, want 2: %+v", len(agent.calls), agent.calls)
	}
	if agent.calls[1].before != 760 || agent.calls[1].limit <= 0 || agent.calls[1].limit > 200 {
		t.Fatalf("second Older call = %+v, want before=760 and 0 < limit <= 200", agent.calls[1])
	}
	if a.moreHistory() {
		t.Fatal("moreHistory() stayed true after Older returned more=false")
	}
	if got := plainRows(a)[0]; strings.Contains(got, "earlier") {
		t.Fatalf("earlier marker remained at the top after more=false: %q", got)
	}

	// Scrolling up again at the top makes no further request.
	scrollBy(t, a, -a.scrollPage())
	if len(agent.calls) != 2 {
		t.Fatalf("Older called again after more=false: %+v", agent.calls)
	}
}

// V4/T11 (contract 4): when Older returns an error, the surface says it once as
// a note and does not retry in a loop; a later scroll to the top asks again.
func TestAnOlderPageThatFailsIsSaidOnceAndNotRetriedInALoop(t *testing.T) {
	agent := newFakeOlderAgent(1000, 960)
	wantErr := "Desktop did not answer in time"
	agent.err = errors.New(wantErr)
	a := liveOlderApp(t, agent)

	scrollBy(t, a, -1000)

	if len(agent.calls) != 1 {
		t.Fatalf("Older called %d times on first scroll to top, want 1", len(agent.calls))
	}
	if got := strings.Count(drawnText(a), wantErr); got != 1 {
		t.Fatalf("error note appeared %d times, want 1:\n%s", got, drawnText(a))
	}

	// A later scroll to the top asks again, and succeeds once the error clears.
	agent.err = nil
	scrollBy(t, a, -a.scrollPage())
	if len(agent.calls) != 2 {
		t.Fatalf("Older called %d times on later scroll to top, want 2", len(agent.calls))
	}
	if !strings.Contains(drawnText(a), "question 760") {
		t.Fatal("retry on later scroll to top did not prepend the older page")
	}
}

// V4/V14/T11 (contract 5): a conversation that is not a live view, or whose
// agent does not implement OlderReader, never calls Older and pages only its
// local transcript slice as before.
func TestAChatThatIsNotLiveNeverAsksForOlderPages(t *testing.T) {
	// 1. Not a live view (Live == nil), even when the agent implements OlderReader.
	agent := newFakeOlderAgent(1000, 960)
	local := newTestApp(agent)
	local.width, local.height = 80, 24
	local.dismissWelcome()
	local.entries = nil
	local.recordRows = 0
	local.turn = 0
	local.replay()

	if local.moreHistory() {
		t.Fatal("non-live chat with 40 local entries reported moreHistory() == true")
	}
	scrollToTop(t, local)
	if len(agent.calls) != 0 {
		t.Fatalf("non-live chat called Older: %+v", agent.calls)
	}

	// 2. A live view whose agent does not implement OlderReader pages locally only.
	plainAgent := &fakeAgent{model: "m", past: longPast(80)}
	livePlain := liveOlderApp(t, plainAgent)
	reads := plainAgent.transcriptReads
	scrollToTop(t, livePlain)
	if !strings.Contains(drawnText(livePlain), "question 0") {
		t.Fatal("live view without OlderReader did not backfill its local transcript")
	}
	if livePlain.moreHistory() {
		t.Fatal("live view without OlderReader still reported moreHistory() at the top")
	}
	if plainAgent.transcriptReads != reads {
		t.Fatalf("scrolling live view without OlderReader re-read Transcript() %d times", plainAgent.transcriptReads-reads)
	}
}
