package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A held conversation with a queued or running node used to wear the working
// mark on its tab and the idle mark on its switcher row. A person saw two
// answers for the same conversation on one frame.
func TestC1AHeldTaskSaysWorkingOnItsTabAndSwitcherRow(t *testing.T) {
	for _, state := range []session.TaskState{session.TaskQueued, session.TaskRunning} {
		t.Run(string(state), func(t *testing.T) {
			a := newTestApp(&fakeAgent{})
			agent := newAsyncAgent()
			watch := &behindWatch{}
			held := &kept{
				conv:  Conversation{Agent: agent, SessionFile: "/tmp/held.jsonl"},
				watch: watch,
			}
			key := a.convKey(held.conv.SessionFile)
			a.behind = map[string]*kept{key: held}
			watch.noteTask(&session.TaskNotice{ID: 7, State: state})
			if state == session.TaskRunning {
				agent.nodes = []session.TaskIndexEntry{{ID: "7", Status: string(session.TaskRunning)}}
			}

			row := a.hopKept(held, a.now())
			assertHopSignalAgrees(t, a.tabSignalFor(key, false), row)
			if !row.moving {
				t.Fatalf("a held node in %s drew an idle switcher row: %+v", state, row)
			}
			if state == session.TaskQueued && row.note != tabWorkingWord {
				t.Fatalf("a queued node says %q, want %q", row.note, tabWorkingWord)
			}
			if state == session.TaskRunning && row.note != "1 task running" {
				t.Fatalf("a running node lost its counted note: %q", row.note)
			}
		})
	}
}

// A held turn or background command used to be recomputed separately for the
// switcher. If either reading drifts, the tab and row again claim different
// states for work that is plainly still running.
func TestC2AHeldTurnOrJobSaysWorkingOnItsTabAndSwitcherRow(t *testing.T) {
	for _, work := range []string{"turn", "job"} {
		t.Run(work, func(t *testing.T) {
			a := newTestApp(&fakeAgent{})
			watch := &behindWatch{}
			held := &kept{
				conv:  Conversation{Agent: &fakeAgent{}, SessionFile: "/tmp/held.jsonl"},
				watch: watch,
			}
			key := a.convKey(held.conv.SessionFile)
			a.behind = map[string]*kept{key: held}
			if work == "turn" {
				watch.turning.Store(true)
			} else {
				watch.noteJob(&session.JobNotice{ID: 8, State: session.JobRunning})
			}

			row := a.hopKept(held, a.now())
			assertHopSignalAgrees(t, a.tabSignalFor(key, false), row)
			if !row.moving || row.note != tabWorkingWord {
				t.Fatalf("a held %s drew %+v", work, row)
			}
		})
	}
}

// The row a person is standing on used to have no state mark at all. A live
// turn, task, or command must wear the same working mark there as on its tab
// without replacing the row's `you are here` note.
func TestC3TheFrontConversationWorkingMarkMatchesItsTab(t *testing.T) {
	for _, work := range []string{"turn", "task", "job"} {
		t.Run(work, func(t *testing.T) {
			a := newTestApp(&fakeAgent{})
			switch work {
			case "turn":
				a.state = stateWorking
			case "task":
				a.taskSeen = map[uint64]session.TaskState{7: session.TaskRunning}
			case "job":
				a.jobs = []session.JobNotice{{ID: 8, State: session.JobRunning}}
			}

			row := a.hopFront(a.now())
			assertHopSignalAgrees(t, a.frontSignal(), row)
			if !row.moving || row.note != hopHereWord {
				t.Fatalf("the front conversation's %s drew %+v", work, row)
			}
		})
	}
}

// A question is the part a person can act on, so it must outrank simultaneous
// work on both a held row and the row in front. Two marks with different
// precedence would send the person's attention to different places.
func TestC4NeedsAPersonOutranksWorkingOnTabsAndSwitcherRows(t *testing.T) {
	t.Run("held", func(t *testing.T) {
		a := newTestApp(&fakeAgent{})
		watch := &behindWatch{}
		watch.waits.Store(true)
		watch.turning.Store(true)
		held := &kept{
			conv:  Conversation{Agent: &fakeAgent{}, SessionFile: "/tmp/held.jsonl"},
			watch: watch,
		}
		key := a.convKey(held.conv.SessionFile)
		a.behind = map[string]*kept{key: held}

		row := a.hopKept(held, a.now())
		assertHopSignalAgrees(t, a.tabSignalFor(key, false), row)
		if !row.needs || row.moving {
			t.Fatalf("a held question did not outrank its work: %+v", row)
		}
	})

	t.Run("front", func(t *testing.T) {
		a := newTestApp(&fakeAgent{})
		a.state = stateWorking
		a.asks = []ask{{id: 1}}

		row := a.hopFront(a.now())
		assertHopSignalAgrees(t, a.frontSignal(), row)
		if !row.needs || row.moving {
			t.Fatalf("the question in front did not outrank its work: %+v", row)
		}
	})
}

// A quiet conversation must keep the mark cell empty and retain its ordinary
// note. A remembered conversation with no watcher knows even less and must not
// panic or invent activity.
func TestC5AnIdleOrUnwatchedConversationClaimsNothing(t *testing.T) {
	for _, watch := range []*behindWatch{{}, nil} {
		a := newTestApp(&fakeAgent{})
		held := &kept{
			conv:  Conversation{Agent: &fakeAgent{}, SessionFile: "/tmp/held.jsonl"},
			watch: watch,
		}
		key := a.convKey(held.conv.SessionFile)
		a.behind = map[string]*kept{key: held}

		row := a.hopKept(held, a.now())
		assertHopSignalAgrees(t, a.tabSignalFor(key, false), row)
		if row.needs || row.moving || row.note != hopNothingWord {
			t.Fatalf("an idle conversation claimed activity: %+v", row)
		}
	}
}

// Before naming finishes, the tab, breadcrumb, and switcher used to disagree
// between `Untitled` and `new conversation`. One thing has one name everywhere
// it is readable on the frame.
func TestC6AnUnnamedConversationHasOneNameEverywhere(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 120, 30
	for place, got := range map[string]string{
		"tab name":        chatTabName(""),
		"breadcrumb root": a.chatCrumbWord(),
		"switcher row":    hopNewWord,
		"unnamed word":    unnamedConversationWord,
	} {
		if got != unnamedConversationWord {
			t.Fatalf("the %s calls an unnamed conversation %q, want %q", place, got, unnamedConversationWord)
		}
	}
	row := plain(tabsRowOf(a))
	if !strings.Contains(row, unnamedConversationWord) || strings.Contains(row, "Untitled") {
		t.Fatalf("the unnamed tab has another name: %q", row)
	}
}

// The defect was visible on one frame: a working glyph on a held tab and an
// idle glyph for that same conversation on the open switcher card. The rendered
// rows must carry the same character, independent of colour.
func TestC6AWorkingTabAndItsSwitcherRowRenderTheSameMark(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 120, 30
	a.file = "/tmp/front.jsonl"
	a.title = "front conversation"
	watch := &behindWatch{}
	watch.noteTask(&session.TaskNotice{ID: 7, State: session.TaskQueued})
	held := &kept{
		conv:  Conversation{Agent: &fakeAgent{}, SessionFile: "/tmp/held.jsonl"},
		side:  &aside{title: "held conversation"},
		watch: watch,
	}
	key := a.convKey(held.conv.SessionFile)
	a.behind = map[string]*kept{key: held}
	a.prev = []string{key}

	glyph := tabSignalGlyph(tabWorking, false)
	tabRow := plain(tabsRowOf(a))
	cardRow := plain(hopLine(a.hopKept(held, a.now()), 0, false, false, 100, a.pal))
	if !strings.Contains(tabRow, glyph) || !strings.Contains(cardRow, glyph) {
		t.Fatalf("the same working conversation has different marks:\ntab:  %q\ncard: %q", tabRow, cardRow)
	}
}

func assertHopSignalAgrees(t *testing.T, sig tabSignal, row hopRow) {
	t.Helper()
	if (sig == tabWorking) != row.moving || (sig == tabNeedsPerson) != row.needs {
		t.Fatalf("tab signal %v and switcher row disagree: %+v", sig, row)
	}
}
