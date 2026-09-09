package tui3

import (
	"strings"
	"testing"
)

// Dismissing one destination must not move the targets the person kept open.
func TestClosingATabKeepsTheOtherTabsInPlace(t *testing.T) {
	a, _, _ := tabApp(t)
	initial := append([]chatTab(nil), a.tabList()...)
	if len(initial) < 3 {
		t.Fatal("fixture needs three tabs")
	}
	cmd, ok := a.bringForward(initial[0].file)
	if !ok {
		t.Fatal("fixture's first tab was not held")
	}
	drain(t, a, cmd)
	tabs := append([]chatTab(nil), a.tabList()...)
	if len(tabs) < 3 {
		t.Fatal("fixture needs three tabs")
	}
	var target chatTab
	for _, tab := range tabs {
		if !tab.here {
			target = tab
			break
		}
	}
	// Change recency without changing presentation order. Closing must not
	// reconstruct the surviving row from this different ordering.
	for _, tab := range tabs {
		if tab.key != target.key {
			a.rememberOpen(tab.key)
		}
	}
	want := []string{}
	for _, tab := range tabs {
		if tab.key != target.key {
			want = append(want, tab.key)
		}
	}
	a.tabDismiss(target)
	got := []string{}
	for _, tab := range a.tabList() {
		got = append(got, tab.key)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("closing one tab moved the others: got %v want %v", got, want)
	}
}

// Putting away the last visible tab must not reopen something already dismissed.
func TestClosingTheLastVisibleTabKeepsHiddenTabsHidden(t *testing.T) {
	a, old, newer := tabApp(t)
	front := a.agent.(*fakeAgent)
	tabs := append([]chatTab(nil), a.tabList()...)
	var current chatTab
	for _, tab := range tabs {
		if tab.here {
			current = tab
		} else {
			a.tabDismiss(tab)
		}
	}
	a.tabDismiss(current)
	if a.page != pageHome {
		t.Fatalf("last visible tab reopened a hidden chat: page=%v, file=%q", a.page, a.file)
	}
	if front.closes+old.closes+newer.closes != 0 || front.stops+old.stops+newer.stops != 0 {
		t.Fatal("dismissing a tab stopped an agent")
	}
}

// An unnamed chat is still a view that can be put away without creating work.
func TestClosingAnUnnamedTabGoesHomeWithoutEndingTheAgent(t *testing.T) {
	f := &fakeAgent{model: "m"}
	a := newTestApp(f)
	a.width, a.height = 80, 32
	a.tabDismiss(chatTab{here: true, word: "main"})
	if a.page != pageHome {
		t.Fatalf("the unnamed tab did not go home: %v", a.page)
	}
	if f.closes != 0 || f.stops != 0 {
		t.Fatal("dismissing an unnamed tab stopped work")
	}
}
