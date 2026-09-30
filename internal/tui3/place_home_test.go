package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// switchLab is a machine with something of every kind on it: a conversation
// stopped on a question, one with work running, one that landed files, a watch
// that fired while nobody was looking, and a tail of quiet chats.
type switchLab struct {
	*homeLab
	now  time.Time
	mine string
}

func newSwitchLab(t *testing.T) *switchLab {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	l := &switchLab{homeLab: lab, now: now}
	alpha := lab.workspace("alpha")
	beta := lab.workspace("beta")
	l.mine = lab.session("-alpha", "aaaa000000000001", "porting the resume picker", alpha, now.Add(-2*time.Minute))
	lab.session("-alpha", "aaaa000000000002", "swarm task splitting", alpha, now.Add(-2*time.Hour))
	lab.presence("-alpha", "aaaa000000000002", session.PresenceWaiting, "add a --report-only mode?", now)
	lab.session("-beta", "bbbb000000000001", "bounty reward companies", beta, now.Add(-3*time.Hour))
	lab.presence("-beta", "bbbb000000000001", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "t1", StartedAt: now.Add(-3 * time.Hour)})
	lab.task("-beta", session.TaskIndexEntry{ID: "t1", SessionID: "bbbb000000000001",
		Title: "read 40 filings", Label: "read 40 filings", Status: string(session.TaskRunning)})
	for i := 0; i < 9; i++ {
		lab.session("-beta", "cccc00000000000"+string(rune('1'+i)), "quiet chat "+string(rune('a'+i)), beta,
			now.Add(-time.Duration(48+i*24)*time.Hour))
	}
	return l
}

func (l *switchLab) open(width, height int) *app {
	l.t.Helper()
	a := l.app(l.mine)
	a.width, a.height = width, height
	openHomeFixtureTabs(a)
	a.openHome()
	a.home.point(a.file)
	// AND ONE FRAME IS DRAWN, because the column's height reaches the list
	// through the draw (place_home.go's [placeHome.body] hands the room to
	// switcher.go's reading) and the list draws as many rows as the frame can
	// hold. A terminal does this before a person can look at it; a test that
	// read `a.home.lines` without it would be reading the list of a window with
	// no height.
	homeText(a)
	return a
}

func switchFrame(a *app) string { return homeText(a) }

// asks writes a presence file for a conversation STOPPED ON A QUESTION WITH
// OPTIONS — which is the only shape the strip and the digits can answer, and a
// bare `waiting` reason is not one ([answerable] states the three conditions).
func (l *homeLab) asks(bucket, id, text string, at time.Time) {
	l.t.Helper()
	dir := filepath.Join(l.project(bucket), id)
	raw, err := json.Marshal(map[string]any{
		"schema": 1, "sessionId": id, "workspace": "/tmp/alpha", "pid": 4242,
		"updatedAt": at.Format(time.RFC3339Nano), "state": string(session.PresenceWaiting),
		"reason": text,
		"question": session.PresenceQuestion{
			Kind: session.QuestionTask, ID: 7, Text: text, Asked: at,
			Options: []session.AnswerOption{{Key: "1", Label: "do it"}, {Key: "2", Label: "leave it"}},
		},
	})
	if err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), append(raw, '\n'), 0o600); err != nil {
		l.t.Fatal(err)
	}
}

// THE LEDGER IS A DOOR. Each `since you left` line opens the place that owns
// what it is about.
func TestSinceYouLeftLinesAreDoorsIntoTheirPlaces(t *testing.T) {
	lab := newSwitchLab(t)
	now := lab.now
	lab.task("-alpha", session.TaskIndexEntry{ID: "t9", SessionID: "aaaa000000000001",
		Title: "toy-scale validation", Label: "toy-scale validation",
		Status: string(session.TaskDone), EndedAt: now.Add(-time.Minute), FilesChanged: 1})
	a := lab.app(lab.mine)
	a.width, a.height = 120, 40
	// A watch that fired while nobody was looking is the best `since you left`
	// line this product will ever have, and it is a door into the standing place.
	a.stands.Items = func(string) []standing.Item {
		return []standing.Item{{ID: "w1", Words: "the 6am repo watch", Status: standing.StatusActive,
			Workspace: lab.workspace("alpha"), LastFired: now.Add(-time.Minute),
			LastChecked: now.Add(-time.Minute), LastCheckLine: "nothing had changed",
			LastOutcome: standing.OutcomeNothing}}
	}
	a.openHome()
	// A look stamp is what makes anything "since you left" at all.
	a.home.seen = now.Add(-30 * time.Minute)
	a.home.build()
	text := switchFrame(a)
	if !strings.Contains(text, "since you left") || !strings.Contains(text, "toy-scale validation") {
		t.Fatalf("no ledger:\n%s", text)
	}
	doors := map[string]bool{}
	for i, line := range a.home.lines {
		// THE LEDGER IS ITS OWN PANEL ON THE GRID, and the other panels' doors
		// into the same places (next up's rows open standing too) are not it.
		if line.kind != homeLedger || line.cell == nil || line.cell.panel != panelLeft {
			continue
		}
		if _, ok := parsePageWord(line.project); !ok {
			t.Fatalf("the ledger line %q names no place", line.project)
		}
		doors[line.project] = true
		if line.project != "standing" {
			continue
		}
		a.home.cursor = i
		a.homeEnter()
		// THE DOOR WAS WALKED THROUGH AND THE PLACE ANSWERED — with the watch the
		// line was about, on the shelf that holds it.
		//
		// This assertion used to be the OPPOSITE fact: this window holds no agent,
		// so the standing place refused in its own words and the refusal was the
		// proof the key had reached it. THE FOURTH SHELF ENDED THAT. A machine
		// holding an order in another project now has a page to open, and the
		// conversation seam coming back empty is no longer an answer about the
		// machine (place_standing_test.go's [TestAnOrderInAnotherProjectReachesTheStandingPage]
		// states the fault it repairs). The law under the old assertion is
		// untouched — the line is a door — so it is asked for directly: the place
		// is what the frame is now on, and the watch is on it. The refusal itself
		// is still pinned where it is still true, over a machine holding nothing
		// at all (the standing whisper's own tests, [placeWhisper]).
		if a.page != pageStanding || !a.at(pageStanding) {
			t.Fatalf("the standing ledger line opened nothing: page %v, notes %q",
				a.page, homeNotes(a))
		}
		if screen := standingPlaceScreen(a); !strings.Contains(screen, "the 6am repo watch") {
			t.Fatalf("the door opened a place without the watch it was about:\n%s", screen)
		}
		// HOME IS PUT BACK before the walk goes on, because the rest of this test
		// is about the ledger's other lines and a place left standing would have
		// them pressed into the wrong keyboard.
		a.showPage(pageHome)
	}
	if !doors["standing"] || !doors["sessions"] {
		t.Fatalf("the ledger drew %v, and both the watch and the landed work happened", doors)
	}
}

// A QUESTION IS ANSWERABLE FROM THE STRIP IN ITS OWN WORDS (SCREEN 1b), and the
// digits keep working.
func TestTheStripAnswersAQuestionInItsOwnWords(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	work := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "here", work, now)
	asking := lab.session("-alpha", "aaaa000000000002", "asking", work, now.Add(-time.Hour))
	lab.asks("-alpha", "aaaa000000000002", "run the sweep?", now)
	a := lab.app(mine)
	a.width, a.height = 120, 30
	var left []string
	a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
		left = append(left, key)
		return nil
	}
	a.openHome()
	a.home.point(asking)
	verbs := a.homeRowVerbs()
	if len(verbs) == 0 {
		t.Fatal("a waiting row offered no verbs")
	}
	// THE KEY IS THE OPTION'S OWN AND IS NEVER POSITIONAL (switcher.go's
	// [switcherQuestionVerbs]). The fixture's first answer is `1 do it`, so
	// `1` is the key the strip draws for it — this used to assert `y`, which
	// was the strip inventing a key that meant whatever happened to be first.
	found := false
	for _, v := range verbs {
		if v.key == '1' {
			v.do()
			found = true
		}
	}
	if !found {
		t.Fatalf("the strip did not carry the question's own first option: %+v", verbs)
	}
	if left[0] != "1" {
		t.Fatalf("the strip sent %q, not the option's own key", left[0])
	}
	if len(left) == 0 {
		t.Fatal("the strip's answer never reached the answer seam")
	}
}

// EVERY FACT THIS SCREEN DRAWS WAS READ ON ITS OWN CLOCK, and a window with no
// memory store draws no memory line at all.
//
// THAT IS THE EMPTINESS LAW AND NOT A GAP. The ledger's memory figures come
// through one seam, taken where every other disk-backed fact on this screen is
// taken ([app.readSwitchLedger]) — never on a draw, because there is SQLite
// behind it — and a window that cannot ask says nothing rather than saying zero.
func TestTheLedgersMemoryFiguresComeThroughOneSeam(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 30)
	if a.memory != nil {
		t.Fatal("this test is about a window with no memory store")
	}
	if a.home.ledger.learned != 0 || a.home.ledger.letGo != 0 {
		t.Fatalf("the ledger input was not read from the seam: %+v", a.home.ledger)
	}
	if strings.Contains(switchFrame(a), "learned") {
		t.Fatalf("a figure nobody can ask for was drawn:\n%s", switchFrame(a))
	}
}

// A STANDING ITEM IS STILL A STANDING ITEM ON THIS LIST: same row kind, same
// card, same two verbs.
func TestAWatchKeepsItsOwnRowKindInsideTheFlatList(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	work := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "here", work, now)
	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.stands.Items = func(string) []standing.Item {
		return []standing.Item{{ID: "w1", Words: "watch the repo", Status: standing.StatusActive,
			Workspace: work, NeedsPerson: "should I send the digest?", Updated: now.Add(-time.Hour)}}
	}
	a.openHome()
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeItem {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the watch has no row:\n%s", switchFrame(a))
	}
	a.home.cursor = at
	if !strings.Contains(switchFrame(a), "Watch the Repo") && !strings.Contains(switchFrame(a), "watch the repo") {
		t.Fatalf("the watch's words are not on the row:\n%s", switchFrame(a))
	}
}

// THE RIGHT MARGIN SAYS WHAT ENTER WILL DO, AT EVERY WIDTH.
//
// A conversation another window is holding and one whose folder is not there any
// more both refuse when they are pressed. Those two facts used to be on the card
// as well as on the row; the card only exists past a hundred and sixty columns
// now, so a list that left them to it would be silent about a door it has
// already decided against — which is exactly the trap [homeHeldShort] and
// [homeGoneShort] were written for.
func TestARowSaysWhenItsDoorWillRefuseWithoutACardToSayIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "here", lab.workspace("alpha"), now)
	lab.session("-beta", "bbbb000000000001", "somewhere else", filepath.Join(t.TempDir(), "deleted-since"), now.Add(-time.Hour))
	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.openHome()
	if _, right := homeColumns(a.width); right != 0 {
		t.Fatal("this test is about the width where there is no card")
	}
	if text := switchFrame(a); !strings.Contains(text, homeGoneShort) {
		t.Fatalf("a row whose folder is gone does not say so:\n%s", text)
	}
	// AND THE TWO DOORS THAT NEED THAT FOLDER ARE NOT OFFERED. A strip that named
	// them would be advertising two keystrokes the door has already refused —
	// the same law the card's old legend kept.
	for i, line := range a.home.lines {
		if line.kind != homeSession || line.row.Project == "" || !strings.Contains(line.row.Title, "somewhere") {
			continue
		}
		a.home.cursor = i
		words := ""
		for _, v := range a.homeRowVerbs() {
			words += string(v.key) + " " + v.word + " · "
		}
		if strings.Contains(words, "new in project") || strings.Contains(words, "open folder") {
			t.Fatalf("a gone row offered a door that cannot open: %s", words)
		}
		if !strings.Contains(words, "copy name") {
			t.Fatalf("a gone row lost the door that asks nothing of the disk: %s", words)
		}
		return
	}
	t.Fatalf("the row with the missing folder is not on the list:\n%s", switchFrame(a))
}

// openHomeFixtureTabs makes the layout fixture's quiet conversations actual tabs.
// Waiting conversations retain their needs-you row independently of this list.
func openHomeFixtureTabs(a *app) {
	_ = a.tabsRow(a.width)
	for _, row := range a.readWorld().Sessions() {
		if row.Transcript != a.file && !row.NeedsPerson() {
			rememberUnheldTab(a, row.Transcript, row.Workspace, homeName(row))
		}
	}
}
