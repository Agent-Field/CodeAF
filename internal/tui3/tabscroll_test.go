package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func manyTabApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := tabApp(t)
	a.start = func(string) (Conversation, error) {
		t.Fatal("browsing must not create a conversation")
		return Conversation{}, nil
	}
	a.resume = func(string) (Agent, error) {
		t.Fatal("held tab navigation must not read history")
		return nil, nil
	}
	for i := 0; i < 9; i++ {
		file := fmt.Sprintf("/tmp/lab/parser-%02d.jsonl", i)
		a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: "/tmp/lab"}, &aside{since: a.now().Add(-time.Duration(i+1) * time.Minute), title: fmt.Sprintf("Parser %02d: 日本語 compatibility and recovery", i)})
	}
	a.chatTabs = nil
	a.chatTabBar = tabBar{}
	a.width, a.height = 120, 40
	a.input.setText("keep this half-written request")
	a.input.cursor = 7
	_ = a.tabsRow(a.width)
	if len(a.chatTabs) != 12 {
		t.Fatalf("many-tab fixture kept %d conversations", len(a.chatTabs))
	}
	drain(t, a, a.tabGo(a.chatTabs[0]))
	a.input.setText("keep this half-written request")
	a.input.cursor = 7
	_ = a.tabsRow(a.width)
	return a
}

func tabScrollTarget(t *testing.T, a *app, kind tabKind) tabHit {
	t.Helper()
	for _, hit := range a.chatTabHits {
		if hit.kind == kind {
			return hit
		}
	}
	t.Fatalf("missing tab control %v: %q", kind, plain(a.tabsRow(a.width)))
	return tabHit{}
}

func TestManyTabsRemainReadableWhileArrowsBrowseWithoutSwitching(t *testing.T) {
	a := manyTabApp(t)
	before, draft, caret, offset := a.file, a.input.String(), a.input.cursor, a.offset
	order := append([]chatTab(nil), a.chatTabs...)
	seen := map[string]bool{}
	for step := 0; step < 32; step++ {
		line := a.tabsRow(a.width)
		for _, hit := range a.chatTabHits {
			if hit.kind != tabHere && hit.kind != tabOther {
				continue
			}
			seen[hit.tab.key] = true
			if hit.span.to-hit.span.from < tabReadableCells {
				t.Fatalf("twelve tabs squeezed a label to %d cells: %q", hit.span.to-hit.span.from, plain(line))
			}
		}
		if a.tabView.to == a.tabView.total {
			break
		}
		right := tabScrollTarget(t, a, tabScrollRight)
		cmd, took := a.tabPress(right.span.to-1, tabStripRow)
		if !took || cmd != nil {
			t.Fatal("scroll arrow opened or started something")
		}
	}
	if len(seen) != 12 {
		t.Fatalf("arrows exposed only %d of twelve readable names", len(seen))
	}
	if a.file != before || a.input.String() != draft || a.input.cursor != caret || a.offset != offset {
		t.Fatal("browsing tabs moved conversation, draft, caret, or transcript")
	}
	for i, tab := range order {
		if a.chatTabs[i].key != tab.key {
			t.Fatal("browsing changed presentation order")
		}
	}
	hidden := a.chatTabs[1]
	drain(t, a, a.tabGo(hidden))
	_ = a.tabsRow(a.width)
	if a.tabView.browsing || a.file != hidden.file {
		t.Fatal("selecting hidden conversation did not reveal it and leave browse mode")
	}
	_ = tabScrollTarget(t, a, tabHere)
}

func TestTabStripWheelBrowsesBothAxesWithoutTouchingTheTranscript(t *testing.T) {
	a := manyTabApp(t)
	before, draft, offset := a.file, a.input.String(), a.offset
	for _, button := range []tea.MouseButton{tea.MouseWheelRight, tea.MouseWheelDown, tea.MouseWheelLeft, tea.MouseWheelUp} {
		_ = a.tabsRow(a.width)
		from := a.tabView.from
		drive(t, a, tea.MouseWheelMsg{X: 20, Y: tabStripRow, Button: button})
		if a.tabView.from == from {
			t.Fatalf("wheel %v did not browse tabs", button)
		}
		if a.file != before || a.input.String() != draft || a.offset != offset {
			t.Fatal("wheel over tabs scrolled or navigated the conversation")
		}
	}
	if a.tabWheel(tea.MouseWheelMsg{X: 20, Y: a.tabsHeight(a.width), Button: tea.MouseWheelDown}) {
		t.Fatal("tab strip swallowed a wheel below its region")
	}
}

func TestTabViewportFitsUnicodePlainAndCompactFramesAfterBrowsing(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256} {
		for _, ascii := range []bool{false, true} {
			a := manyTabApp(t)
			a.pal = newPalette(profile, ascii)
			// 36 is a compact strip: the strip had eight fewer cells while a
			// `home` piece led it, and forty columns was compact then; the
			// scroll floor itself ([tabReadableCells]) did not move.
			for _, width := range []int{160, 80, 36, 24, 12} {
				for _, height := range []int{40, 16} {
					a.width, a.height = width, height
					a.touch()
					line := a.tabsRow(width)
					if ansi.StringWidth(line) > width {
						t.Fatalf("many-tab strip overflow at %dx%d: %q", width, height, plain(line))
					}
					prev := 0
					for _, hit := range a.chatTabHits {
						if hit.span.from < prev || hit.span.to > width {
							t.Fatalf("overlapping or offscreen target at %d: %+v", width, hit)
						}
						for x := hit.span.from; x < hit.span.to; x++ {
							got, ok := a.tabAt(x, tabStripRow)
							if !ok || got.kind != hit.kind || got.tab.key != hit.tab.key {
								t.Fatalf("drawn target disagrees with hit at %d", x)
							}
						}
						prev = hit.span.to
					}
					if width <= 40 {
						_ = tabScrollTarget(t, a, tabHere)
						if a.tabView.scrollable {
							t.Fatal("compact strip spent the active name on scroll arrows")
						}
					} else {
						a.tabScroll(1)
					}
				}
			}
		}
	}
}

func TestClosingFromTabBrowseAndOpeningNewChatRevealTheirSelection(t *testing.T) {
	a := manyTabApp(t)
	a.tabScroll(1)
	_ = a.tabsRow(a.width)
	var close tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabClose && !hit.tab.here {
			close = hit
			break
		}
	}
	drain(t, a, a.tabDismiss(close.tab))
	_ = a.tabsRow(a.width)
	_ = tabScrollTarget(t, a, tabHere)
	if len(a.chatTabs) != 11 || a.tabView.browsing {
		t.Fatal("closing a browsed tab did not retain selection and the other eleven tabs")
	}
	lab := newStartLab(t)
	b := lab.app()
	b.width, b.height = 80, 40
	keepThree(t, b)
	_ = b.tabsRow(b.width)
	b.tabScroll(-1)
	openStart(t, b)
	b.input.setText("investigate unicode parser failures")
	_ = b.tabsRow(b.width)
	here := tabScrollTarget(t, b, tabHere)
	if !here.tab.start || !strings.Contains(plain(b.tabsRow(b.width)), "investigate") {
		t.Fatal("new chat failed to reveal its synthetic selected tab")
	}
}

// The new-chat door travels with a short run of tabs and settles at the edge
// only once the scrolling viewport fills the row. A sparse strip must not leave
// a long gap.
//
// IT USED TO CHECK THE `Chats ▾` CONTROL BESIDE IT, which is deleted
// (chattabs.go). With every tab spelled there is now nothing at all after the
// `+`, which is the other half of what this test is for: a row with no gap in
// the middle and no furniture on the end.
func TestNewChatFollowsTheLastVisibleTab(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.width, a.height = 160, 40
	keepThree(t, a)
	_ = a.tabsRow(a.width)
	last := 0
	for _, hit := range a.chatTabHits {
		if hit.kind == tabClose {
			last = hit.span.to
		}
	}
	plus := tabScrollTarget(t, a, tabNew)
	if plus.span.from != last+1 {
		t.Fatalf("the new-chat door left the tabs: last=%d plus=%+v", last, plus.span)
	}
	if plus.span.to >= a.width-10 {
		t.Fatal("sparse tabs pushed navigation to the far edge")
	}
	// EVERY TAB IS SPELLED AT THIS WIDTH, so there is no count and nothing else
	// after the door — the row simply stops.
	for _, hit := range a.chatTabHits {
		if hit.kind == tabFold {
			t.Fatalf("a row that spelled every tab drew a count anyway: %+v", hit.span)
		}
		if hit.span.from >= plus.span.to {
			t.Fatalf("something is drawn after the new-chat door: %+v", hit)
		}
	}
	for x := plus.span.from; x < plus.span.to; x++ {
		got, ok := a.tabAt(x, tabStripRow)
		if !ok || got.kind != plus.kind {
			t.Fatalf("adjacent control has wrong hit ownership: %+v", got)
		}
	}
}
