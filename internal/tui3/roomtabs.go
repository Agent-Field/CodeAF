package tui3

// roomtabs.go is THE TASK ROOM'S TWO TABS: the transcript, which is what the
// work said and did, and the work, which is what it changed.
//
// EVERY TASK'S PAGE HAS BOTH, on either engine, because there is one page type
// (planroom.go). They are named at the right end of the trail row, beside the
// way out, where they cost no row of the page: `tab` over an empty box moves
// between them, and a press on either name opens it.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// roomTab is which of the room's two tabs is on screen. The zero value is the
// transcript, which is what every page opens on.
type roomTab int

const (
	roomTabTranscript roomTab = iota
	roomTabWork
)

// The tabs' names, as the trail row draws them and the manual quotes them.
const (
	roomTabTranscriptWord = "transcript"
	roomTabWorkWord       = "work"
	// roomTabKey is the key that moves between the tabs, over an empty box. A
	// box with words in it keeps the key for what it already does there.
	roomTabKey = "tab"
)

// roomTabWords is the tabs in the order they are drawn.
var roomTabWords = []string{roomTabTranscriptWord, roomTabWorkWord}

// roomHasTabs reports whether the page on screen is a task's page. A run's
// graph is a page of its own kind and draws no tabs.
func (a *app) roomHasTabs() bool {
	return a.room != nil && a.room.orch == nil
}

// roomTabTo opens one tab and asks for what it draws.
func (a *app) roomTabTo(tab roomTab) tea.Cmd {
	if !a.roomHasTabs() || a.room.tab == tab {
		return nil
	}
	a.room.tab = tab
	a.room.offset, a.room.stick = 0, true
	a.roomTouched()
	a.touch()
	return a.planRoomWorkRead(true)
}

// roomTabNext is `tab` over an empty box: the other tab.
func (a *app) roomTabNext() tea.Cmd {
	if !a.roomHasTabs() {
		return nil
	}
	if a.room.tab == roomTabWork {
		return a.roomTabTo(roomTabTranscript)
	}
	return a.roomTabTo(roomTabWork)
}

// roomTabsLabel is the tabs as the trail row draws them, painted, and the
// columns each name takes in the label, from the label's own start.
func (a *app) roomTabsLabel() (string, int, []hudSpan) {
	if !a.roomHasTabs() {
		return "", 0, nil
	}
	var painted strings.Builder
	spans := make([]hudSpan, 0, len(roomTabWords))
	at := 0
	for i, word := range roomTabWords {
		if i > 0 {
			painted.WriteString(a.pal.dim(railSep))
			at += ansi.StringWidth(railSep)
		}
		if roomTab(i) == a.room.tab {
			painted.WriteString(a.pal.accent(word))
		} else {
			painted.WriteString(a.pal.dim(word))
		}
		spans = append(spans, hudSpan{from: at, to: at + ansi.StringWidth(word)})
		at += ansi.StringWidth(word)
	}
	return painted.String(), at, spans
}

// roomTabPress opens the tab a press on the trail row landed on.
func (a *app) roomTabPress(x, y int) (tea.Cmd, bool) {
	if !a.roomOpen() || a.headHeight() == 0 || y != a.roomHeadRow() {
		return nil, false
	}
	for i, span := range a.roomTabSpans {
		if span.holds(x) {
			return a.roomTabTo(roomTab(i)), true
		}
	}
	return nil, false
}
