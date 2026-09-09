package tui3

import (
	"strings"
	"testing"
)

// Leaving a task to inspect its parent must not throw away the work of finding
// an earlier call. This drives the same scroll and room doors as the interface.
func TestTaskReadingSurvivesLeavingAndNewOutput(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.height = 24
	fake.journal = callsJournal(t, 65)
	a.openRoom(7, "Port the loader")
	// Open current work before scrolling its call history. The task now opens
	// on a compact step, whose disclosure is a separate reading gesture.
	a.toggleLatestWorkfold()
	a.roomScroll(-10000)
	a.roomScroll(-3)
	a.roomScroll(-8)
	before := readingWindow(a)
	if a.room.stick {
		t.Fatal("fixture did not leave live edge")
	}
	a.closeRoom()
	fake.journal = callsJournal(t, 75)
	a.openRoom(7, "Port the loader")
	after := readingWindow(a)
	if a.room.stick || before != after {
		t.Fatalf("reopening lost the reading position\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestTaskReadingKeepsExpandedWorkAcrossSiblingVisit(t *testing.T) {
	a := openWorked(t)
	if !a.toggleLatestWorkfold() {
		t.Fatal("no work to expand")
	}
	before := roomText(a)
	a.openRoom(8, "Sibling")
	a.openRoom(7, "Draw two posters")
	a.room.done = true
	after := roomText(a)
	if before != after {
		t.Fatalf("returning folded the work again\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func readingWindow(a *app) string {
	rows, _ := a.roomWindow(a.bodyWidth(), a.viewHeight())
	var lines []string
	for _, r := range rows {
		lines = append(lines, plain(r.text))
	}
	return strings.Join(lines, "\n")
}

func TestTaskReadingDoesNotCrossConversationOrHost(t *testing.T) {
	for _, which := range []string{"conversation", "host", "guest"} {
		t.Run(which, func(t *testing.T) {
			a := openWorked(t)
			a.toggleLatestWorkfold()
			a.closeRoom()
			switch which {
			case "conversation":
				a.file += ".other"
			case "host":
				a.host = "another-machine"
			}
			a.openRoom(7, "Same number, different owner")
			if which == "guest" {
				a.room.guest = &taskGuest{session: a.file + ".guest"}
			}
			a.room.done = true
			page := roomText(a)
			if len(a.room.workOpen) != 0 || strings.Contains(page, "I have the aesthetic") {
				t.Fatalf("another owner's expansion leaked into %s", which)
			}
		})
	}
}

func TestTaskReadingWaitsForSuccessfulHostedRead(t *testing.T) {
	a := openWorked(t)
	a.toggleLatestWorkfold()
	before := roomText(a)
	a.closeRoom()
	a.openRoom(7, "Draw two posters")
	a.room.done = true
	a.room.loading = true
	a.roomRows(a.bodyWidth())
	if a.room.readingRestored {
		t.Fatal("loading consumed the saved reading")
	}
	a.closeRoom()
	a.openRoom(7, "Draw two posters")
	a.room.done = true
	a.room.readFailed = true
	a.roomRows(a.bodyWidth())
	if a.room.readingRestored {
		t.Fatal("failed read consumed the saved reading")
	}
	a.room.readFailed = false
	a.room.dirty = true
	if after := roomText(a); after != before {
		t.Fatalf("successful read did not restore the expanded work:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestTaskReadingFollowingLiveStillFollowsOnReturn(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.height = 24
	fake.journal = callsJournal(t, 65)
	a.openRoom(7, "Port the loader")
	a.roomScroll(-10000)
	a.roomScroll(-3)
	a.roomScroll(10000)
	a.closeRoom()
	fake.journal = callsJournal(t, 75)
	a.openRoom(7, "Port the loader")
	if !strings.Contains(readingWindow(a), "file74.go") || !a.room.stick {
		t.Fatal("explicit follow choice did not follow new output")
	}
}

func TestTaskReadingCacheIsBoundedAndRevisitsStayRecent(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = workedJournal(t)
	for i := 1; i <= roomReadingLimit; i++ {
		a.openRoom(uint64(i), "Task")
	}
	a.closeRoom()
	a.openRoom(1, "Task")
	a.closeRoom()
	a.openRoom(roomReadingLimit+1, "Task")
	a.closeRoom()
	if len(a.roomReadings) != roomReadingLimit {
		t.Fatal("reading cache exceeded its bound")
	}
	a.openRoom(1, "Task")
	key, _ := a.roomReadingKey()
	if _, ok := a.roomReadings[key]; !ok {
		t.Fatal("recently revisited task was evicted")
	}
	key.task = 2
	if _, ok := a.roomReadings[key]; ok {
		t.Fatal("oldest reading was not evicted")
	}
}

func TestTaskReadingResizeRetainsTheCallAndMissingHistoryStartsOldest(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.height = 24
	fake.journal = callsJournal(t, 65)
	a.openRoom(7, "Port the loader")
	a.roomScroll(-10000)
	a.roomScroll(-3)
	a.roomScroll(-8)
	visible, _ := a.roomWindow(a.bodyWidth(), a.viewHeight())
	first := ""
	for _, row := range visible {
		if row.entry >= 0 && row.entry < len(a.room.entries) {
			first = a.room.entries[row.entry].callID
			if first != "" {
				break
			}
		}
	}
	if first == "" {
		t.Fatal("fixture has no visible call")
	}
	a.closeRoom()
	a.width = 60
	a.openRoom(7, "Port the loader")
	visible, _ = a.roomWindow(a.bodyWidth(), a.viewHeight())
	found := false
	for _, row := range visible {
		if row.entry >= 0 && row.entry < len(a.room.entries) {
			found = found || a.room.entries[row.entry].callID == first
		}
	}
	if !found || a.room.stick {
		t.Fatal("resize lost the call being read")
	}
	a.closeRoom()
	fake.journal = callsJournal(t, 200)
	a.openRoom(7, "Port the loader")
	a.roomRows(a.bodyWidth())
	if a.room.offset != 0 || a.room.stick {
		t.Fatal("missing old history jumped to live output")
	}
}
