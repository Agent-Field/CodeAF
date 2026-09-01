package tui3

// WHAT A CONVERSATION IS ABOUT, ON HOME.
//
// These are written from the person's side: they had an afternoon about a
// repository from a window they opened somewhere else, and a week later they
// come to home with the repository's name in their head and nothing else.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// about writes folders onto a conversation's meta.json the way the session
// stamps them when somebody names one (internal/session/places.go). It goes
// through the meta and not through an agent because home reads the FOLDER: a
// row on this screen is built from meta.json by a surface that never opened the
// conversation.
func (l *homeLab) about(transcript string, folders ...string) {
	l.t.Helper()
	dir := filepath.Dir(transcript)
	meta, err := session.LoadMeta(dir)
	if err != nil {
		l.t.Fatal(err)
	}
	for _, folder := range folders {
		meta.Places = append(meta.Places, session.PlaceRef{
			Path: folder, Arrival: session.PlaceSaid, Referred: time.Now(),
		})
	}
	if err := session.SaveMeta(dir, meta); err != nil {
		l.t.Fatal(err)
	}
}

// A ROW SAYS WHAT ITS CONVERSATION IS ALSO ABOUT, and it says the age too. The
// clause is last on the tail on purpose — see [homeNote] — so this pins both:
// the new fact is there, and it did not cost the old one.
func TestARowSaysWhatItsConversationIsAlsoAbout(t *testing.T) {
	now := time.Now()
	row := session.SessionRow{
		Project: "beta", ProjectDir: "/tmp/beta", Workspace: "/tmp/beta",
		At:     now.Add(-2 * time.Hour),
		Places: []session.PlaceRef{{Path: "/home/p/code/wisp", Arrival: session.PlaceSaid}},
	}
	note := homeNote(row, false, "", "", markNone, false, 0, now)
	if !strings.Contains(note, "also about wisp") {
		t.Fatalf("the row's tail is %q, want the folder it is also about", note)
	}
	if !strings.Contains(note, "2h") {
		t.Fatalf("the row's tail is %q, want the age still on it", note)
	}

	// MORE THAN ONE NAMES ONE AND COUNTS THE REST, because a row is a fixed
	// shape whatever it is about.
	row.Places = append(row.Places,
		session.PlaceRef{Path: "/home/p/notes"},
		session.PlaceRef{Path: "/home/p/code/lantern"})
	if note := homeNote(row, false, "", "", markNone, false, 0, now); !strings.Contains(note, "also about wisp +2") {
		t.Fatalf("the row's tail is %q, want one name and a count", note)
	}
}

// AND A CONVERSATION ABOUT EXACTLY WHERE IT IS STANDING SAYS NOTHING — three
// ways, all of them the emptiness law: no folders at all, the standing folder
// itself, and a folder whose name is the project's own name, which would be the
// row saying one word twice.
func TestARowAboutWhereItStandsSaysNothingExtra(t *testing.T) {
	now := time.Now()
	plain := session.SessionRow{
		Project: "beta", ProjectDir: "/tmp/beta", Workspace: "/tmp/beta", At: now.Add(-time.Hour),
	}
	for name, row := range map[string]session.SessionRow{
		"a conversation with no folders": plain,
		"the standing folder itself": func() session.SessionRow {
			row := plain
			row.Places = []session.PlaceRef{{Path: "/tmp/beta", Arrival: session.PlaceSaid}}
			return row
		}(),
		"a folder with the project's own name": func() session.SessionRow {
			row := plain
			row.Places = []session.PlaceRef{{Path: "/home/p/code/beta", Arrival: session.PlaceSaid}}
			return row
		}(),
	} {
		if note := homeNote(row, false, "", "", markNone, false, 0, now); strings.Contains(note, homeAlsoWord) {
			t.Fatalf("%s drew %q", name, note)
		}
	}
}

// TYPING A FOLDER'S NAME FINDS THE CONVERSATION ABOUT IT, WHEREVER IT LIVES.
// This is the whole point of carrying the set onto the row: an afternoon spent
// on `wisp` out of a window opened in `beta` used to be findable by nothing but
// a title nobody may ever have given it.
func TestTypingAFolderNameFindsTheConversationHeldElsewhere(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	wisp := lab.workspace("wisp")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "notes on tuesday", lab.workspace("alpha"), now)
	away := lab.session("-tmp-beta", "bbbb000000000001", "an afternoon", lab.workspace("beta"), now.Add(-time.Hour))
	lab.about(away, wisp)

	a := lab.app(mine)
	a.openHome()
	for _, r := range "wisp" {
		a.homeKey(key(string(r)))
	}
	found, other := false, false
	for _, line := range a.home.lines {
		if line.kind != homeSession {
			continue
		}
		switch line.row.Transcript {
		case away:
			found = true
		case mine:
			other = true
		}
	}
	if !found {
		t.Fatalf("typing the folder's name did not reach the conversation about it:\n%s", homeText(a))
	}
	// AND IT IS THE FOLDER DOING IT. A conversation about nothing of the kind
	// staying off the list is what tells a match from a list that stopped
	// narrowing.
	if other {
		t.Fatalf("the folder's name matched a conversation that is not about it:\n%s", homeText(a))
	}
}

// THE CARD NAMES THEM ALL, AND FOLDS RATHER THAN GROWING. Nothing on that
// column may grow with the data (homebands.go), so a conversation about five
// folders draws three and a fold line that says how many are behind it.
func TestTheCardNamesTheFoldersAndFoldsTheRest(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "an afternoon", lab.workspace("alpha"), now)
	// THE CARD UNDER THE CURSOR IS ANOTHER CONVERSATION'S. The one this window
	// is holding wears the switcher's acting card instead (home.go's
	// [app.homeSwitchCard]), which is a different pane and not this band's.
	away := lab.session("-tmp-beta", "bbbb000000000001", "a week of it", lab.workspace("beta"), now.Add(-time.Hour))
	folders := []string{
		lab.workspace("wisp"), lab.workspace("lantern"), lab.workspace("ledger"),
		lab.workspace("notes"), lab.workspace("harbour"),
	}
	lab.about(away, folders...)

	a := lab.app(mine)
	a.openHome()
	card := strings.Join(homeCardFor(t, a, away), "\n")
	if !strings.Contains(card, homeAlsoWord) {
		t.Fatalf("the card does not say what the conversation is also about:\n%s", card)
	}
	for _, name := range []string{"wisp", "lantern", "ledger"} {
		if !strings.Contains(card, name) {
			t.Fatalf("the card does not name %s:\n%s", name, card)
		}
	}
	if !strings.Contains(card, "…2 more folders") {
		t.Fatalf("the card grew with the data instead of folding:\n%s", card)
	}
}
