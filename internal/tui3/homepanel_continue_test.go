package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
)

// fakeTaker is a [Taker] that records what it was asked and answers as told.
type fakeTaker struct {
	cells []string
	taken Taken
	err   error
	// on runs inside Take, for a test that needs the chat to appear on this
	// machine's disk the way a real takeover would leave it.
	on func(cell string)
}

func (f *fakeTaker) Take(_ context.Context, cell string) (Taken, error) {
	f.cells = append(f.cells, cell)
	if f.on != nil {
		f.on(cell)
	}
	return f.taken, f.err
}

// verbs records the branch verbs a row ran.
type verbs struct{ merged, discarded []string }

func (v *verbs) actions(merge bool) BranchActions {
	out := BranchActions{Discard: func(_ context.Context, id string) error { v.discarded = append(v.discarded, id); return nil }}
	if merge {
		out.Merge = func(_ context.Context, id string) error { v.merged = append(v.merged, id); return nil }
	}
	return out
}

// continueRows is a chat that went off, one that went off with nothing left
// behind it, and a branch.
var continueRows = chatlist.Static{
	{Cell: "c-off", Title: "Port the picker", Device: "studio", Status: chatlist.Off, DurableAgo: 3 * time.Hour, Pending: 3},
	{Cell: "c-clean", Title: "Tidy the docs", Device: "studio", Status: chatlist.Off, DurableAgo: 90 * time.Second},
	{Cell: "c-branch", Title: "Fix the flaky test", Device: "laptop", Status: chatlist.Branch, OrphanTurns: 2, DurableAgo: 5 * time.Hour},
	{Cell: "c-short", Title: "Trim logs", Device: "mac", Status: chatlist.Branch, OrphanTurns: 4, DurableAgo: 6 * time.Hour},
	{Cell: "c-run", Title: "Nightly index rebuild", Device: "studio", Status: chatlist.Running, DurableAgo: 2 * time.Minute},
}

// standOn puts the cursor on the chat from another machine and presses enter.
func standOn(t *testing.T, a *app, cell string) {
	t.Helper()
	for at, line := range a.home.lines {
		if line.remote != nil && line.remote.Cell == cell {
			a.dropHomeAsk()
			a.home.cursor = at
			drive(t, a, key("enter"))
			return
		}
	}
	t.Fatalf("no row for %s:\n%s", cell, homeText(a))
}

// confirm answers the card with its first answer.
func confirm(t *testing.T, a *app) {
	t.Helper()
	drive(t, a, key("1"), key("enter"))
}

func continueHome(t *testing.T, taker Taker, src chatlist.Source, width int) *app {
	t.Helper()
	a := machinesHome(t, src, width)
	a.taker = taker
	return a
}

func TestTakeoverScreenCopy(t *testing.T) {
	a := continueHome(t, &fakeTaker{}, continueRows, 200)
	standOn(t, a, "c-off")
	text := homeText(a)
	for _, want := range []string{
		"last durable turn 10800s ago; up to 3 turns may still be on studio",
		chatlist.OfferContinue,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the takeover screen does not say %q:\n%s", want, text)
		}
	}
	// K = 0 leaves the `up to` clause out.
	standOn(t, a, "c-clean")
	text = homeText(a)
	if !strings.Contains(text, "last durable turn 90s ago") || strings.Contains(text, "up to") {
		t.Errorf("a takeover with nothing left behind says the wrong thing:\n%s", text)
	}
}

// A chat running on another machine is continued from the same screen: it says
// where the chat runs now, and confirming takes it.
func TestTakeoverScreenOffersARunningChat(t *testing.T) {
	taker := &fakeTaker{}
	a := continueHome(t, taker, continueRows, 200)
	standOn(t, a, "c-run")
	if text := homeText(a); !strings.Contains(text, "running on studio; last durable turn 120s ago") {
		t.Fatalf("the screen does not name where the chat runs:\n%s", text)
	}
	confirm(t, a)
	if fmt.Sprint(taker.cells) != "[c-run]" {
		t.Fatalf("Take was asked for %v", taker.cells)
	}
}

func TestTakeoverScreenConfirmCallsTake(t *testing.T) {
	taker := &fakeTaker{}
	a := continueHome(t, taker, continueRows, 200)
	standOn(t, a, "c-off")
	// enter on the card alone takes the answer that loses nothing.
	drive(t, a, key("enter"))
	if len(taker.cells) != 0 {
		t.Fatalf("leaning on enter took the chat: %v", taker.cells)
	}
	standOn(t, a, "c-off")
	confirm(t, a)
	if fmt.Sprint(taker.cells) != "[c-off]" {
		t.Fatalf("Take was asked for %v", taker.cells)
	}
}

func TestTakeoverScreenOpensTheChatAndSaysWhatWasKept(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	mine := lab.session("project", "0000000000000001", "Mine", workspace, time.Now())
	a := lab.app(mine)
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())
	a.machines = continueRows
	a.taker = &fakeTaker{
		taken: Taken{Kept: "c-kept", KeptTurns: 2, Device: "desk", Elapsed: 3200 * time.Millisecond},
		on: func(cell string) {
			lab.session("project", cell, "Port the picker", workspace, time.Now())
		},
	}
	drain(t, a, a.askMachines())
	standOn(t, a, "c-off")
	confirm(t, a)
	if a.at(pageHome) {
		t.Fatalf("a takeover that worked left the person on home:\n%s", homeText(a))
	}
	want := chatlist.Moved("", 3200*time.Millisecond, false) + " " + chatlist.KeptEdits(2, "desk")
	if got := plain(lastNote(t, a)); got != want {
		t.Errorf("the takeover sentence is %q, want %q", got, want)
	}
}

// winnerTakes is a Source that lists the chat as off until it is asked again,
// and then as running on the device that won it.
type winnerTakes struct{ asked int }

func (w *winnerTakes) Rows(context.Context) ([]chatlist.Row, error) {
	w.asked++
	rows := append([]chatlist.Row(nil), continueRows...)
	if w.asked > 1 {
		rows[0].Status, rows[0].Device = chatlist.Running, "desk"
	}
	return rows, nil
}

func TestTakeoverScreenLostRace(t *testing.T) {
	taker := &fakeTaker{err: fmt.Errorf("take: %w", directory.ErrLeaseHeld)}
	a := continueHome(t, taker, &winnerTakes{}, 200)
	standOn(t, a, "c-off")
	confirm(t, a)
	text := homeText(a)
	if !strings.Contains(text, "Port the picker  running on desk") || !strings.Contains(text, lostRaceWord) {
		t.Fatalf("the row did not come back as running on the winner, with one line saying so:\n%s", text)
	}
	if len(taker.cells) != 1 {
		t.Fatalf("the takeover ran %d times", len(taker.cells))
	}
}

func TestTakeoverScreenRefusedWhenUnreachable(t *testing.T) {
	taker := &fakeTaker{}
	src := &failing{}
	a := continueHome(t, taker, src, 200)
	drain(t, a, a.askMachines())
	standOn(t, a, "c-off")
	text := homeText(a)
	if len(taker.cells) != 0 || !strings.Contains(text, chatlist.Unreachable) {
		t.Fatalf("the offer was not disabled with its reason:\n%s", text)
	}
	if strings.Contains(text, continueAsk) {
		t.Fatalf("a takeover was offered with the directory out of reach:\n%s", text)
	}
}

func TestTakeoverScreenAbsentWithoutATaker(t *testing.T) {
	a := continueHome(t, nil, continueRows, 200)
	standOn(t, a, "c-off")
	if strings.Contains(homeText(a), continueAsk) {
		t.Fatalf("an offer with nothing behind it was drawn:\n%s", homeText(a))
	}
}

func TestBranchRowCopy(t *testing.T) {
	v := &verbs{}
	a := continueHome(t, nil, continueRows, 200)
	a.branches = v.actions(true)
	drain(t, a, a.askMachines())
	if !strings.Contains(homeText(a), "2 turns from laptop: merge / discard") {
		t.Fatalf("the branch sentence is missing:\n%s", homeText(a))
	}
	// A surface that cannot merge does not say it can.
	a.branches = v.actions(false)
	drain(t, a, a.askMachines())
	text := homeText(a)
	if strings.Contains(text, "merge") || !strings.Contains(text, "2 turns from laptop: discard") {
		t.Fatalf("a row without a merge promised one:\n%s", text)
	}
}

func TestBranchRowMergeCallsMerge(t *testing.T) {
	v := &verbs{}
	a := continueHome(t, nil, continueRows, 200)
	a.branches = v.actions(true)
	drain(t, a, a.askMachines())
	standOn(t, a, "c-branch")
	confirm(t, a)
	if fmt.Sprint(v.merged, v.discarded) != "[c-branch] []" {
		t.Fatalf("merge ran %v, discard ran %v", v.merged, v.discarded)
	}
}

func TestBranchRowDiscardCallsDiscard(t *testing.T) {
	v := &verbs{}
	a := continueHome(t, nil, continueRows, 200)
	a.branches = v.actions(true)
	drain(t, a, a.askMachines())
	standOn(t, a, "c-branch")
	drive(t, a, key("2"), key("enter"))
	if fmt.Sprint(v.merged, v.discarded) != "[] [c-branch]" {
		t.Fatalf("merge ran %v, discard ran %v", v.merged, v.discarded)
	}
	// Leaving them runs nothing.
	standOn(t, a, "c-branch")
	drive(t, a, key("enter"))
	if len(v.discarded) != 1 {
		t.Fatalf("leaning on enter discarded again: %v", v.discarded)
	}
}

func TestBranchRowDiscardOnlyWhenMergeIsAbsent(t *testing.T) {
	v := &verbs{}
	a := continueHome(t, nil, continueRows, 200)
	a.branches = v.actions(false)
	drain(t, a, a.askMachines())
	standOn(t, a, "c-branch")
	text := homeText(a)
	if strings.Contains(text, "merge") || !strings.Contains(text, "discard") {
		t.Fatalf("the card offered the wrong verbs:\n%s", text)
	}
	drive(t, a, key("1"), key("enter"))
	if len(v.discarded) != 1 {
		t.Fatalf("discard did not run: %v", v.discarded)
	}
}

func TestBranchRowGolden(t *testing.T) {
	golden := map[int][]string{
		80: {
			"   Fix the flaky test  2 turns from laptop: merge / discard                   5h",
			"   Trim logs  4 turns from mac: merge / discard                               6h",
		},
		60: {
			"   Fix the flaky test  2 turns · laptop                   5h",
			"   Trim logs  4 turns from mac: merge / discard           6h",
		},
		40: {
			"  Fix the flaky ...  2 turns · laptop 5h",
			"  Trim logs  4 turns · mac            6h",
		},
	}
	for width, want := range golden {
		a := continueHome(t, nil, continueRows, width)
		got := linesWith(a, "Fix the", "Trim logs")
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("width %d:\n%s\nwant:\n%s", width, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// cardRows is the takeover screen's rows: everything the frame draws inside the
// card's edges.
func cardRows(a *app) []string {
	var out []string
	for _, line := range strings.Split(homeText(a), "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "╭") || strings.HasPrefix(t, "│") || strings.HasPrefix(t, "╰") {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return out
}

func TestTakeoverScreenGolden(t *testing.T) {
	golden := map[int][]string{
		80: {
			"╭─ ? Continue this chat here? ────────────────────────────────────────────────╮",
			"│ last durable turn 10800s ago; up to 3 turns may still be on studio          │",
			"│                                                                             │",
			"│   1  continue here                                                          │",
			"│ ▸ 2  leave it there                                            safe answer  │",
			"│                                                                             │",
			"╰─ esc leave it there ────────────────────────────────────────────────────────╯",
		},
		40: {
			"╭─ ? Continue this chat here? ────────╮",
			"│ last durable turn 10800s ago; up to │",
			"│ 3 turns may still be on studio      │",
			"│                                     │",
			"│   1  continue here                  │",
			"│ ▸ 2  leave it there    safe answer  │",
			"│                                     │",
			"╰─ esc leave it there ────────────────╯",
		},
	}
	for width, want := range golden {
		a := continueHome(t, &fakeTaker{}, continueRows, width)
		standOn(t, a, "c-off")
		if got := cardRows(a); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("width %d:\n%s\nwant:\n%s", width, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// A takeover that kept edits and brought task copies says both in one line, and
// one that did neither says nothing.
func TestTakenSaidJoinsWhatTheTakeoverDid(t *testing.T) {
	both := takenSaid(Taken{Kept: "c-kept", KeptTurns: 2, Device: "desk", TaskCopies: []string{"n1"}})
	want := chatlist.KeptEdits(2, "desk") + "; " + chatlist.CopiesCame([]string{"n1"})
	if both != want {
		t.Errorf("takenSaid = %q, want %q", both, want)
	}
	if got := takenSaid(Taken{}); got != "" {
		t.Errorf("a takeover that did nothing said %q", got)
	}
}

// TestTheArrivalCardComesOnEveryTakeWhetherOrNotHomeListsTheChat is the seam the
// card depends on: it is raised when the takeover ends, not when home happens to
// list the chat by then.
func TestTheArrivalCardComesOnEveryTakeWhetherOrNotHomeListsTheChat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listed bool
	}{{"listed", true}, {"not listed", false}} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHomeLab(t)
			workspace := lab.workspace("project")
			mine := lab.session("project", "0000000000000001", "Mine", workspace, time.Now())
			a := lab.app(mine)
			a.width, a.height = 200, 70
			drain(t, a, a.openHome())
			a.machines = continueRows
			ft := &fakeTaker{taken: Taken{Resume: machine.Resume{From: "desk", Now: workspace, Uncommitted: []string{"README.md"}}}}
			ft.on = func(cell string) {
				id := cell
				if !tc.listed {
					id = "0000000000000009" // home lists this one; the take names another
				}
				ft.taken.Transcript = lab.session("project", id, "Port the picker", workspace, time.Now())
			}
			a.taker = ft
			drain(t, a, a.askMachines())
			standOn(t, a, "c-off")
			confirm(t, a)
			askedCard(t, a)
		})
	}
}
