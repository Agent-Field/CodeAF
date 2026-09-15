package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// toSafety puts the settings page on the tab these rows live on.
func toSafety(t *testing.T, a *app) {
	t.Helper()
	for at, title := range settingTabs {
		if title == tabSafety {
			a.sheet.tab = at
			a.sheet.build()
			return
		}
	}
	t.Fatal("there is no Safety tab")
}

// autonomySettingsApp is the settings page over a project that keeps question
// rules.
func autonomySettingsApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&autonomyTestAgent{fakeAgent: &fakeAgent{}, rules: map[session.AskKind]session.Policy{}})
	a.width, a.height = 120, 40
	// THE RULES ARE READ THE WAY THE PROGRAM READS THEM: off the update loop, on
	// the way up ([app.readAutonomy] is in [app.Init]'s standing list). A fixture
	// that reached through the door here would be testing a road nothing takes.
	spend(t, a, a.readAutonomy())
	a.openSettings()
	toSafety(t, a)
	return a
}

// A SETTING THAT CHANGES WHAT THE PROGRAM DOES ON ITS OWN IS NEVER REACHABLE
// ONLY BY A SLASH COMMAND (owner ruling 2026-09-11).
//
// `/autonomy` was the only door to these rules, and a slash command is invisible:
// somebody who has never been told the word cannot find the one setting that
// decides whether their work gets answered without them. So the rules have a row
// where settings are browsed — and it is the same store, read and written
// through the command's own door.
func TestTheAutonomyRulesHaveARowWhereSettingsAreBrowsed(t *testing.T) {
	a := autonomySettingsApp(t)
	found := map[string]bool{}
	for _, item := range a.sheet.items {
		if item.head == autonomyRowsHead {
			found["head"] = true
		}
		if item.autonomy != nil {
			found[string(item.autonomy.kind)] = true
		}
	}
	if !found["head"] {
		t.Fatalf("the Safety tab has no `%s` section:\n%s", autonomyRowsHead, settingsText(a))
	}
	for _, kind := range autonomyKinds {
		if !found[string(kind)] {
			t.Fatalf("there is no row for %q:\n%s", kind, settingsText(a))
		}
	}
	// AND THE COMMAND IS NAMED ON THE ROW, second rather than first: a person who
	// wants to write a rule for one kind, with a wait of their own, needs the
	// word — and this is where they can read it.
	if !strings.Contains(autonomyRowAbout, "/autonomy") {
		t.Fatalf("the row never names the command: %q", autonomyRowAbout)
	}
}

// AND THE ROW WRITES WHAT THE COMMAND WRITES, through the same door.
func TestTheAutonomyRowCyclesThroughTheSameDoorAsTheCommand(t *testing.T) {
	a := autonomySettingsApp(t)
	row := autonomyRowFor(t, a, session.AskChoice)
	if row.word != autonomyAskWord {
		t.Fatalf("a project with no rules written does not open on %q: %q", autonomyAskWord, row.word)
	}
	spend(t, a, a.autonomyRowNext(row))
	if !strings.Contains(row.word, autonomyRecommendWord) {
		t.Fatalf("the first press did not reach %q: %q", autonomyRecommendWord, row.word)
	}
	spend(t, a, a.autonomyRowNext(row))
	if row.word != autonomyDecideWord {
		t.Fatalf("the second press did not reach %q: %q", autonomyDecideWord, row.word)
	}
	// The command reads back what the row wrote, because there is one store.
	spend(t, a, a.slash("/autonomy"))
	if got := plain(lastNote(t, a)); !strings.Contains(got, "choice") || !strings.Contains(got, autonomyDecideWord) {
		t.Fatalf("the sheet did not read back what the row wrote:\n%s", got)
	}
	spend(t, a, a.autonomyRowNext(row))
	if row.word != autonomyAskWord {
		t.Fatalf("the third press did not come back round to %q: %q", autonomyAskWord, row.word)
	}
}

// THE TWO ROWS NOBODY MAY CHANGE SAY SO ON THEMSELVES AND DO NOT MOVE. The
// engine refuses both at its own door; a row that walked its value and then
// snapped back would be this page arguing with the engine in front of somebody.
func TestTheTwoFixedAutonomyRowsSayWhyAndDoNotMove(t *testing.T) {
	a := autonomySettingsApp(t)
	for kind, law := range map[session.AskKind]string{
		session.AskConfirmation:  autonomyAlwaysWord,
		session.AskClarification: autonomyNoClockWord,
	} {
		row := autonomyRowFor(t, a, kind)
		if !strings.Contains(row.word, law) {
			t.Fatalf("%q does not state its law: %q", kind, row.word)
		}
		before := row.word
		spend(t, a, a.autonomyRowNext(row))
		if row.word != before {
			t.Fatalf("%q moved: %q → %q", kind, before, row.word)
		}
	}
}

// AND A CONVERSATION WITH NOWHERE TO KEEP RULES DRAWS NO ROWS AT ALL, which is
// the emptiness law over a per-project setting: rules that cannot be kept are
// not drawn as rules that are.
func TestAConversationWithNoProjectDrawsNoAutonomyRows(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 120, 40
	a.openSettings()
	toSafety(t, a)
	for _, item := range a.sheet.items {
		if item.autonomy != nil || item.head == autonomyRowsHead {
			t.Fatalf("a conversation with no project drew question rules:\n%s", settingsText(a))
		}
	}
}

// autonomyRowFor is the row for one kind, or a failure naming what is there.
func autonomyRowFor(t *testing.T, a *app, kind session.AskKind) *autonomyRow {
	t.Helper()
	for _, item := range a.sheet.items {
		if item.autonomy != nil && item.autonomy.kind == kind {
			return item.autonomy
		}
	}
	t.Fatalf("there is no row for %q:\n%s", kind, settingsText(a))
	return nil
}

// settingsText is the page as a person reads it, for a failure message.
func settingsText(a *app) string {
	rows := make([]string, 0, len(a.sheet.items))
	for _, item := range a.sheet.items {
		switch {
		case item.head != "":
			rows = append(rows, "── "+item.head)
		case item.autonomy != nil:
			rows = append(rows, "   "+string(item.autonomy.kind)+"  "+item.autonomy.word)
		default:
			rows = append(rows, "   "+item.meta.label)
		}
	}
	return strings.Join(rows, "\n")
}
