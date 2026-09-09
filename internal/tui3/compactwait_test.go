package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestCompactWaitingKeepsFinishedWordsStill(t *testing.T) {
	a := liveStepsApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	a.entries[len(a.entries)-1].status = toolOK
	a.entries[len(a.entries)-1].ended = liveStepsBase.Add(8 * time.Second)
	captionTimeAt(a, 500*time.Millisecond)
	a.touch()
	before := liveStepRowsOf(t, a, 100)
	captionTimeAt(a, 800*time.Millisecond)
	a.touch()
	after := liveStepRowsOf(t, a, 100)
	if len(before) != len(after) {
		t.Fatal("waiting moved the caption")
	}
	for i, r := range before {
		if plain(r.text) != plain(after[i].text) {
			t.Fatal("waiting changed characters")
		}
		if i == len(before)-1 {
			if strings.Split(r.text, "  ")[0] != strings.Split(after[i].text, "  ")[0] {
				t.Fatal("finished caption or semantic icon shimmered")
			}
			if r.text == after[i].text {
				t.Fatal("inline activity mark did not animate")
			}
		} else if r.text != after[i].text {
			t.Fatal("older caption moved")
		}
	}
}

func TestCompactWaitUsesOnlyKnownResponseAge(t *testing.T) {
	forgetPhases()
	t.Cleanup(forgetPhases)
	a := liveStepsApp(t)
	a.entries[len(a.entries)-1].status = toolOK
	a.entries[len(a.entries)-1].ended = liveStepsBase.Add(8 * time.Second)
	a.awaited = liveStepsBase.Add(8 * time.Second)
	a.turnBegan = a.awaited.Add(-time.Minute)
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{9999 * time.Millisecond, ""}, {10 * time.Second, "awaiting response · 10s"}, {61 * time.Second, "awaiting response · 1m 1s"},
	} {
		a.clock = func() time.Time { return a.awaited.Add(tc.age) }
		if got := a.compactWaitWords(a.conversation()); got != tc.want {
			t.Fatalf("age %s: got %q, want %q", tc.age, got, tc.want)
		}
		if got := a.compactWaitWords(deck{lens: overseerLens}); got != "" {
			t.Fatalf("room borrowed parent wait: %q", got)
		}
	}
	a.awaited = time.Time{}
	a.turnBegan = time.Time{}
	if got := a.compactWaitWords(a.conversation()); got != "" {
		t.Fatalf("unknown wait invented a clock: %q", got)
	}
}

func TestCompactWaitTimesWorkAfterACompletedPostCorrectionStep(t *testing.T) {
	forgetPhases()
	t.Cleanup(forgetPhases)
	a := liveStepsApp(t)
	a.width = 160
	a.model = phaseModel
	a.entries = append(a.entries,
		entry{kind: entrySteer, turn: 1, steer: &steerElbow{words: "Also check the organization list", consumed: true}},
		entry{kind: entryAssistant, turn: 1, text: "Yes – Agent-Field was in your org list.", settled: true},
		entry{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			began: liveStepsBase.Add(2 * time.Second), ended: liveStepsBase.Add(3 * time.Second)},
		entry{kind: entryThinking, turn: 1, text: "checking what remains", began: liveStepsBase.Add(4 * time.Second)},
	)
	a.turnBegan = liveStepsBase
	a.awaited = time.Time{}
	for _, tc := range []struct {
		at   time.Duration
		want string
	}{
		{9999 * time.Millisecond, ""},
		{10 * time.Second, "still working · 10s"},
		{3*time.Minute + 55*time.Second, "still working · 3m 55s"},
	} {
		a.clock = func() time.Time { return liveStepsBase.Add(tc.at) }
		PostPhaseNews(PhaseNews{Model: phaseModel, Role: lane.RoleTalk,
			Phase: provider.PhaseThinking, Since: liveStepsBase.Add(4 * time.Second), At: a.now()})
		a.touch()
		page := livePage(a)
		if !strings.Contains(page, "Yes – Agent-Field was in your org list") {
			t.Fatalf("at %s the completed post-correction caption vanished:\n%s", tc.at, page)
		}
		if tc.want == "" {
			if strings.Contains(page, "still working ·") {
				t.Fatalf("at %s generic elapsed appeared early:\n%s", tc.at, page)
			}
		} else if !strings.Contains(page, tc.want) {
			t.Fatalf("at %s page missed %q:\n%s", tc.at, tc.want, page)
		}
		if strings.Contains(page, "awaiting response") {
			t.Fatalf("reasoning was called a response wait:\n%s", page)
		}
	}
	if got := a.compactWaitWords(deck{lens: overseerLens}); got != "" {
		t.Fatalf("task room borrowed the conversation clock: %q", got)
	}
}

func TestCompactWaitDistinguishesLostConnectionFromResponse(t *testing.T) {
	forgetPhases()
	t.Cleanup(forgetPhases)
	a := liveStepsApp(t)
	a.entries[len(a.entries)-1].status = toolOK
	a.entries[len(a.entries)-1].ended = liveStepsBase.Add(8 * time.Second)
	a.awaited = liveStepsBase.Add(8 * time.Second)
	a.model = phaseModel
	for _, age := range []time.Duration{time.Second, 15 * time.Second} {
		a.clock = func() time.Time { return a.awaited.Add(age) }
		PostPhaseNews(PhaseNews{Model: phaseModel, Role: lane.RoleTalk,
			Phase: provider.PhaseConnectionLost, Since: a.awaited, At: a.now()})
		if got := a.compactWaitWords(a.conversation()); got != "waiting for connection" {
			t.Fatalf("offline at %s drew %q", age, got)
		}
		if got := a.compactWaitWords(deck{lens: overseerLens}); got != "" {
			t.Fatalf("task borrowed its parent's connection state: %q", got)
		}
	}
	PostPhaseNews(PhaseNews{Model: phaseModel, Role: lane.RoleTalk,
		Phase: provider.PhaseFirstWord, Since: a.awaited, At: a.now()})
	if got := a.compactWaitWords(a.conversation()); got != "awaiting response · 15s" {
		t.Fatalf("recovered response wait drew %q", got)
	}
}

func TestCompactWaitFallbackPreservesTextAndAccessibleMotion(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256} {
		for _, linear := range []bool{false, true} {
			a := liveStepsApp(t)
			a.linear = linear
			a.pal = newPalette(profile, linear)
			captionTimeAt(a, 500*time.Millisecond)
			before := a.compactWaitMark()
			captionTimeAt(a, 800*time.Millisecond)
			if (linear || profile == tokens.ANSI256) && before != a.compactWaitMark() {
				t.Fatal("accessible activity animated")
			}
			for _, room := range []int{4, 5, 6, 7, 80} {
				tail, inline := a.compactWaitSuffix("text", room, a.conversation())
				if inline != (room >= 7) || (inline && len(plain(tail)) == 0) {
					t.Fatalf("room %d: wrong inline fallback", room)
				}
			}
		}
	}
}
