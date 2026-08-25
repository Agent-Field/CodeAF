package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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
	a.openHome()
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

// AT REST HOME IS ONE FLAT RANKED LIST: what needs you, then what is moving,
// then what is quiet, with the project as a tag on the row — and not one
// project heading, not one strip label, not one `elsewhere` rule (SCREEN 1a).
func TestHomeAtRestIsOneFlatRankedListWithNoTreeAndNoStrips(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 30)
	text := switchFrame(a)
	// THE LIST IS WHAT IS ASKED, AND THE PULSE IS NOT PART OF IT. The words below
	// are the old two-strip shape's own labels, and the frame's first row is the
	// pulse, which legitimately says `4 moving` because that is the count of what
	// is in flight on the whole machine (SCREEN 2b). Dropping row 0 before the
	// scan is what keeps this a test about the LIST rather than about any line
	// that happens to contain one of these words.
	body := text
	if at := strings.Index(text, "\n"); at >= 0 {
		body = text[at+1:]
	}
	for _, gone := range []string{"needs you", "moving", "elsewhere", "keeping an eye"} {
		if strings.Contains(body, gone) {
			t.Fatalf("the old shape survived (%q):\n%s", gone, text)
		}
	}
	if !strings.Contains(text, "what wants you first") {
		t.Fatalf("the switcher's own claim is missing:\n%s", text)
	}
	asking := strings.Index(text, "Swarm Task Splitting")
	moving := strings.Index(text, "Bounty Reward Companies")
	quiet := strings.Index(text, "Quiet Chat B")
	if asking < 0 || moving < 0 || quiet < 0 || !(asking < moving && moving < quiet) {
		t.Fatalf("needs-you, moving and quiet are not the sort order:\n%s", text)
	}
	// The project rides the row as a tag rather than as a heading over it.
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "Bounty Reward Companies") && !strings.Contains(line, "beta") {
			t.Fatalf("the project left the row:\n%s", line)
		}
	}
}

// EIGHT ROWS AND A DOOR OVER THE REST, and the door opens and folds back.
func TestHomesOneFoldIsADoorBothWays(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	text := switchFrame(a)
	if !strings.Contains(text, "more, quiet since") {
		t.Fatalf("no fold over the quiet tail:\n%s", text)
	}
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeSwitchFold {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the fold is not a line of the list")
	}
	a.home.cursor = at
	a.homeEnter()
	if !strings.Contains(switchFrame(a), "Quiet Chat I") {
		t.Fatalf("the fold did not open:\n%s", switchFrame(a))
	}
	for i, line := range a.home.lines {
		if line.kind == homeSwitchFold {
			a.home.cursor = i
		}
	}
	a.homeEnter()
	if strings.Contains(switchFrame(a), "Quiet Chat I") {
		t.Fatalf("the fold did not close again:\n%s", switchFrame(a))
	}
}

// alt+g GROUPS BY PROJECT AND alt+q HIDES THE QUIET ONES, and both survive the
// screen being closed and opened again.
func TestHomeGroupsAndHidesTheQuietOnesAndRemembersBoth(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	if !a.placeAlt('g') {
		t.Fatal("alt+g did nothing on home")
	}
	if !strings.Contains(switchFrame(a), "alpha") {
		t.Fatalf("grouping drew no project heading:\n%s", switchFrame(a))
	}
	if !a.placeAlt('q') {
		t.Fatal("alt+q did nothing on home")
	}
	if strings.Contains(switchFrame(a), "Quiet Chat A") {
		t.Fatalf("alt+q kept a quiet row:\n%s", switchFrame(a))
	}
	a.closeHome()
	a.openHome()
	if !a.home.grouped || !a.home.hideQuiet {
		t.Fatal("the two views were forgotten when home closed")
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
	if !strings.Contains(text, "since you left") || !strings.Contains(text, "1 task landed") {
		t.Fatalf("no ledger:\n%s", text)
	}
	doors := map[string]bool{}
	for i, line := range a.home.lines {
		if line.kind != homeLedger {
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
		// THE DOOR WAS WALKED THROUGH AND THE PLACE ANSWERED. This window holds
		// no agent with anything standing over it, so the standing place refuses
		// in its own words and the router puts home back (pages.go's [app.showPage])
		// — and that refusal is the proof the key reached the place at all.
		if !strings.Contains(homeNotes(a), standNothingWord) {
			t.Fatalf("the standing ledger line opened nothing: %q", homeNotes(a))
		}
	}
	if !doors["standing"] || !doors["tasks"] {
		t.Fatalf("the ledger drew %v, and both the watch and the landed work happened", doors)
	}
}

// THE CARD IS ONLY THERE PAST 160 COLUMNS, and below it the row's own note
// carries the fact the card was for (SCREEN 1a vs 1d).
func TestTheCardAppearsOnlyWhereTheWidthIsSpare(t *testing.T) {
	lab := newSwitchLab(t)
	for _, width := range []int{80, 120, homeCardMin - 1} {
		if left, right := homeColumns(width); right != 0 || left != width {
			t.Fatalf("%d columns drew a card: left %d right %d", width, left, right)
		}
	}
	left, right := homeColumns(homeCardMin)
	if right < homeCardCol || left+right+homeGutter != homeCardMin {
		t.Fatalf("the card tier does not add up: left %d right %d", left, right)
	}
	a := lab.open(200, 40)
	a.home.point(lab.mine)
	text := switchFrame(a)
	if !strings.Contains(text, homeVerbsWord) {
		t.Fatalf("the card lost the verbs hint:\n%s", text)
	}
	// And the row still carries the note at every width, because that is what
	// the card was for below the tier.
	narrow := lab.open(120, 40)
	if !strings.Contains(switchFrame(narrow), "task running") {
		t.Fatalf("the row lost its note at 120 columns:\n%s", switchFrame(narrow))
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
	found := false
	for _, v := range verbs {
		if v.key == 'y' {
			v.do()
			found = true
		}
	}
	if !found {
		t.Fatalf("the strip did not carry the question's own first option: %+v", verbs)
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
	if a.home.ledger != (switcherLedgerInput{}) {
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
		if strings.Contains(words, "new chat here") || strings.Contains(words, "open folder") {
			t.Fatalf("a gone row offered a door that cannot open: %s", words)
		}
		if !strings.Contains(words, "copy path") {
			t.Fatalf("a gone row lost the door that asks nothing of the disk: %s", words)
		}
		return
	}
	t.Fatalf("the row with the missing folder is not on the list:\n%s", switchFrame(a))
}

// THE ONE FOLD LEAVES THE CURSOR ON ITSELF, so the gesture that opened the list
// is the gesture that folds it back without walking anywhere. It is the law
// every other fold on this column keeps ([homeView.fold]).
func TestTheFoldLeavesTheCursorOnTheLineThatOpenedIt(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(120, 40)
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homeSwitchFold {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no fold to open:\n%s", switchFrame(a))
	}
	a.home.cursor = at
	a.homeEnter()
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSwitchFold {
		t.Fatalf("opening the fold walked off it, onto %v", line.kind)
	}
	a.homeEnter()
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeSwitchFold {
		t.Fatalf("folding it back walked off it, onto %v", line.kind)
	}
	if strings.Contains(switchFrame(a), "Quiet Chat I") {
		t.Fatalf("the second press did not fold the list back:\n%s", switchFrame(a))
	}
}
