package tui3

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// otherMachines is one chat in each state a person can find on another machine.
var otherMachines = chatlist.Static{
	{Cell: "c-run", Title: "Nightly index rebuild", Device: "studio", Status: chatlist.Running, DurableAgo: 2 * time.Minute},
	{Cell: "c-off", Title: "Port the picker", Device: "studio", Status: chatlist.Off, DurableAgo: 3 * time.Hour},
	{Cell: "c-branch", Title: "Fix the flaky test", Device: "laptop", Status: chatlist.Branch, OrphanTurns: 2, DurableAgo: 5 * time.Hour},
	{Cell: "c-idle", Title: "Notes on the migration", Device: "studio", Status: chatlist.Idle, DurableAgo: 26 * time.Hour},
	{Cell: "c-here", Title: "Held by this machine", Device: "here", Status: chatlist.Here, DurableAgo: time.Minute},
}

// machinesHome opens home with a source and lets its first listing land.
func machinesHome(t *testing.T, src chatlist.Source, width int) *app {
	t.Helper()
	a, _ := homeTabsFixture(t)
	a.machines = src
	// A merge is behind the row, so its sentence is the frozen one; the tests of a
	// surface with none say so themselves (homepanel_continue_test.go).
	a.branches = BranchActions{Merge: func(context.Context, string) error { return nil }}
	a.width, a.height = width, 70
	drain(t, a, a.askMachines())
	return a
}

// linesWith is the rows of the frame that carry any of the words, trimmed.
func linesWith(a *app, words ...string) []string {
	var out []string
	for _, line := range strings.Split(homeText(a), "\n") {
		for _, w := range words {
			if strings.Contains(line, w) {
				out = append(out, strings.TrimRight(line, " "))
				break
			}
		}
	}
	return out
}

func TestHomeRowsFromOtherMachines(t *testing.T) {
	a := machinesHome(t, otherMachines, 200)
	text := homeText(a)
	for _, want := range []string{
		"Nightly index rebuild", "running on studio",
		"Port the picker", "studio off",
		"Fix the flaky test", "2 turns from laptop: merge / discard",
		"Notes on the migration",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("home does not say %q:\n%s", want, text)
		}
	}
	// A chat this machine holds is listed once already, and a released one says
	// nothing about where it is.
	if strings.Contains(text, "Held by this machine") {
		t.Errorf("a chat held here is drawn again as another machine's")
	}
	for _, line := range linesWith(a, "Notes on the migration") {
		for _, banned := range []string{"idle", "here", "studio"} {
			if strings.Contains(line, banned) {
				t.Errorf("an idle chat says %q: %q", banned, line)
			}
		}
	}
}

func TestHomeRowsWithoutASourceAreUnchanged(t *testing.T) {
	a := machinesHome(t, nil, 120)
	if got := linesWith(a, "running on", " off", "merge / discard", chatlist.Unreachable); len(got) != 0 {
		t.Fatalf("a machine with no source drew %q", got)
	}
}

// failing is a Source that answers once and then cannot be reached.
type failing struct{ calls int }

func (f *failing) Rows(ctx context.Context) ([]chatlist.Row, error) {
	f.calls++
	if f.calls > 1 {
		return nil, errors.New("no route")
	}
	return otherMachines, nil
}

func TestUnreachableKeepsLastListing(t *testing.T) {
	src := &failing{}
	a := machinesHome(t, src, 120)
	if strings.Contains(homeText(a), chatlist.Unreachable) {
		t.Fatal("reachable machines were called unreachable")
	}
	drain(t, a, a.askMachines())
	text := homeText(a)
	if !strings.Contains(text, chatlist.Unreachable) {
		t.Fatalf("no unreachable line:\n%s", text)
	}
	if !strings.Contains(text, "Nightly index rebuild") || !strings.Contains(text, "running on studio") {
		t.Fatalf("the last listing was dropped:\n%s", text)
	}
}

// The rows at 80 and 40 columns are pinned as drawn: at 40 a status sentence
// that does not fit gives way whole and the name stays whole.
func TestHomeMachineRowsGolden(t *testing.T) {
	golden := map[int][]string{
		80: {
			"   Nightly index rebuild  running on studio                                   2m",
			"   Port the picker  studio off                                                3h",
			"   Fix the flaky test  2 turns from laptop: merge / discard                   5h",
			"   Notes on the migration                                                     1d",
		},
		40: {
			"  Nightly index rebuild               2m",
			"  Port the picker  studio off         3h",
			"  Fix the flaky test                  5h",
			"  Notes on the migration              1d",
		},
	}
	for width, want := range golden {
		a := machinesHome(t, otherMachines, width)
		got := linesWith(a, "Nightly", "Port the", "Fix the", "Notes on")
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("width %d:\n%s\nwant:\n%s", width, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// A machine with no chat of its own still lists the chats on the person's other
// machines at every width, and enter on one raises the takeover question. The
// emptiness law is about empty data: "nothing here but something elsewhere" is
// not empty, and a narrow frame is one more place the listing lands.
func TestFreshHomeListsRowsFromOtherMachines(t *testing.T) {
	for _, width := range []int{200, 120, 80, 40} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			lab := newHomeLab(t)
			a := lab.app("")
			a.width, a.height = width, 40
			a.taker = &fakeTaker{}
			a.machines = chatlist.Static{
				{Cell: "c-off", Title: "Port the picker", Device: "studio", Status: chatlist.Off, DurableAgo: 3 * time.Hour, Pending: 2},
			}
			a.branches = BranchActions{Merge: func(context.Context, string) error { return nil }}
			drain(t, a, a.openHome())

			if text := homeText(a); !strings.Contains(text, "Port the picker") {
				t.Fatalf("a fresh home does not list the chat on another machine:\n%s", text)
			}
			standOn(t, a, "c-off")
			if text := homeText(a); !strings.Contains(text, "studio") || strings.Contains(text, homeEmptyWord) {
				t.Fatalf("enter on the row raised no takeover question:\n%s", text)
			}
		})
	}
}

// TestAChatHeldElsewhereIsDrawnAsSuchWhicheverReadingComesFirst is the two
// orders of one fact: this machine lists the chat (it replicated) and the
// directory says another machine last held it, live or gone quiet. The row is
// the other machine's, with its note and its door, whether the machines reading
// lands before the local list does or after it.
func TestAChatHeldElsewhereIsDrawnAsSuchWhicheverReadingComesFirst(t *testing.T) {
	for _, status := range []chatlist.Status{chatlist.Running, chatlist.Off} {
		for _, readingFirst := range []bool{true, false} {
			name := fmt.Sprintf("%s/reading first=%v", status, readingFirst)
			t.Run(name, func(t *testing.T) {
				lab := newHomeLab(t)
				workspace := lab.workspace("project")
				id := "0000000000000001"
				src := chatlist.Static{{Cell: id, Title: "Port the picker", Device: "studio", DeviceID: "dev_studio",
					Status: status, DurableAgo: time.Minute}}
				var a *app
				if readingFirst {
					a = lab.app("")
					a.width, a.height = 200, 70
					a.machines = src
					drain(t, a, a.askMachines())
					lab.session("project", id, "Port the picker", workspace, time.Now())
					drain(t, a, a.openHome())
				} else {
					a = lab.app(lab.session("project", id, "Port the picker", workspace, time.Now()))
					a.width, a.height = 200, 70
					drain(t, a, a.openHome())
					a.machines = src
					drain(t, a, a.askMachines())
				}
				if got := linesWith(a, "studio"); len(got) == 0 {
					t.Fatalf("the chat is drawn as a plain local row:\n%s", homeText(a))
				}
			})
		}
	}
}

// A chat this machine itself let lapse is its own to pick up: no other
// machine's row stands over it.
func TestAChatThisMachineLetLapseStaysLocal(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	a := lab.app(lab.session("project", "0000000000000001", "Mine", workspace, time.Now()))
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())
	a.machines = chatlist.Static{{Cell: "0000000000000001", Title: "Mine", Device: "here", Status: chatlist.Off, Mine: true}}
	drain(t, a, a.askMachines())
	for _, line := range a.home.lines {
		if line.remote != nil {
			t.Fatalf("drew a remote row over this machine's own chat:\n%s", homeText(a))
		}
	}
}
