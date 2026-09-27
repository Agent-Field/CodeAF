package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSettledNotificationIsOneRowUntilOpened(t *testing.T) {
	a, _, _ := taskApp(t)
	card := &taskDone{title: "Fix parser", status: session.ProjectTask(session.TaskFacts{State: session.TaskDone}), outcome: "internal account", result: "**Visible answer**"}
	if rows := a.doneRows(card, 100, false); len(rows) != 1 || strings.Contains(plain(strings.Join(rows, "\n")), "internal account") {
		t.Fatalf("collapsed card: %v", rows)
	}
	card.open = true
	if rows := plain(strings.Join(a.doneRows(card, 100, false), "\n")); !strings.Contains(rows, "Visible answer") {
		t.Fatalf("answer lost: %s", rows)
	}
}

func TestNotificationBatchFoldsAndKeepsPendingDecisionVisible(t *testing.T) {
	a, _, _ := taskApp(t)
	for i := 1; i <= 3; i++ {
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(uint64(i), "Completed work", session.TaskDone, session.TaskNotice{Report: "account"})})
	}
	d := deck{entries: a.entries}
	rows := a.doneCluster(d, nil, 0, len(d.entries), 100)
	if len(rows) != 1 {
		t.Fatalf("batch has %d rows, want one", len(rows))
	}
	a.openDone(rows[0].entry)
	if rows = a.doneCluster(d, nil, 0, len(d.entries), 100); len(rows) <= 3 {
		t.Fatalf("batch did not expand: %v", rows)
	}
	a.openDone(rows[0].entry)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "Needs decision", session.TaskUnverified, unverifiedNotice("needs review"))})
	d = deck{entries: a.entries}
	rows = a.doneCluster(d, nil, 0, len(d.entries), 100)
	text := ""
	for _, r := range rows {
		text += plain(r.text) + "\n"
	}
	if !strings.Contains(text, "Needs decision") || !strings.Contains(text, askCheckReason) {
		t.Fatalf("pending decision hidden: %s", text)
	}
}

func TestDismissNotificationsPreservesRecordsAndUndo(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Finished", session.TaskDone, session.TaskNotice{Report: "saved output"})}, streamEventMsg{gen: a.gen, ev: update(2, "Needs decision", session.TaskUnverified, unverifiedNotice("needs review"))})
	count := len(a.entries)
	first := a.doneEntryFor(1)
	if !a.dismissDone(first) {
		t.Fatal("settled card not dismissed")
	}
	if len(a.entries) != count || a.doneCardAt(first).report != "saved output" {
		t.Fatal("dismissal changed record")
	}
	if a.dismissDone(a.doneEntryFor(2)) {
		t.Fatal("pending decision dismissed")
	}
	a.dismissNotifications("undo")
	if a.doneCardAt(first).dismissed {
		t.Fatal("undo did not restore card")
	}
	a.dismissNotifications("")
	if !a.doneCardAt(first).dismissed || a.doneCardAt(a.doneEntryFor(2)).dismissed {
		t.Fatal("dismiss all changed pending decision")
	}
}

func TestDeleteDismissesSelectedNotificationAndNavigationSkipsHiddenCards(t *testing.T) {
	a, _, _ := taskApp(t)
	for i := 1; i <= 3; i++ {
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(uint64(i), "Completed work", session.TaskDone, session.TaskNotice{})})
	}
	first := a.doneEntryFor(1)
	if head := plain(a.doneHead(a.doneCardAt(first), 120, true)); !strings.Contains(head, "delete dismiss") {
		t.Fatalf("selected notification has no dismissal hint: %s", head)
	}
	a.sel = -1
	if !a.selectTool(1) || a.sel != first {
		t.Fatalf("folded header not selectable: %d", a.sel)
	}
	if a.selectTool(1) {
		t.Fatalf("folded child selected: %d", a.sel)
	}
	a.sel = first
	drive(t, a, key("delete"))
	if !a.doneCardAt(first).dismissed {
		t.Fatal("delete did not dismiss selected card")
	}
	if !a.selectTool(1) || a.sel == first {
		t.Fatalf("navigation selected hidden card: %d", a.sel)
	}
	a.dismissNotifications("")
	a.sel = -1
	if a.selectTool(1) {
		t.Fatalf("dismissed batch selectable: %d", a.sel)
	}
	a.dismissNotifications("undo")
	if !a.selectTool(1) {
		t.Fatal("undo did not restore keyboard access")
	}
}

func TestPendingDecisionStaysOutsideConversationWorkfold(t *testing.T) {
	a, _, _ := taskApp(t)
	card := &taskDone{id: 7, title: "Review parser", status: session.ProjectTask(session.TaskFacts{State: session.TaskUnverified})}
	a.entries = []entry{
		{kind: entryUser, text: "Fix it", turn: 1},
		{kind: entryTool, tool: "read", status: toolOK, turn: 1},
		{kind: entryDone, done: card, turn: 1},
		{kind: entryAssistant, text: "Please review the result.", settled: true, turn: 1},
	}
	a.touch()
	text := taskText(a)
	if !strings.Contains(text, "Review parser") || !strings.Contains(text, askCheckReason) {
		t.Fatalf("pending decision folded away: %s", text)
	}
}

func TestDismissedNotificationStaysHiddenOnUpdateButNewDecisionAppears(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Finished", session.TaskDone, session.TaskNotice{Report: "output"})})
	at := a.doneEntryFor(1)
	a.dismissDone(at)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Finished", session.TaskDone, session.TaskNotice{Report: "output", CostUSD: 1})})
	if !a.doneCardAt(at).dismissed {
		t.Fatal("same landing reappeared after metadata update")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Finished", session.TaskUnverified, unverifiedNotice("new decision"))})
	latest := a.doneCardAt(a.doneEntryFor(1))
	if latest.dismissed || latest.status.Tier != session.TaskTierYourCall {
		t.Fatal("new decision hidden by earlier dismissal")
	}
}

func TestDismissedNotificationsLeaveNoBlankRowsOrEmptyWorkfold(t *testing.T) {
	a, _, _ := taskApp(t)
	base := []entry{{kind: entryUser, text: "Hello", turn: 1}, {kind: entryAssistant, text: "Answer.", settled: true, turn: 1}}
	a.entries = append([]entry(nil), base...)
	a.touch()
	want := taskText(a)
	card := &taskDone{title: "Hidden", dismissed: true, status: session.ProjectTask(session.TaskFacts{State: session.TaskDone})}
	a.entries = []entry{base[0], {kind: entryDone, done: card, turn: 1}, base[1]}
	a.touch()
	if got := taskText(a); got != want {
		t.Fatalf("dismissal left clutter:\nwant %q\ngot %q", want, got)
	}
}
