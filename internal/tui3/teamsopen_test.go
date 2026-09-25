package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE MANAGER BROUGHT INTO THE PANE (teamsopen.go) ────────────────────────

// teamsOpenLab is the teams page lab with orbit given a manager this window is
// NOT holding: a transcript in a folder of its own, which is not the window's
// (`/tmp/lab`). made says whether the transcript is on the disk. The open door
// is a fake that records what it was asked and answers with err when set.
type teamsOpenLab struct {
	a             *app
	harbor, orbit string
	file, where   string
	key           string
	asked         []string
	err           error
}

func newTeamsOpenLab(t *testing.T, made bool) *teamsOpenLab {
	t.Helper()
	l := &teamsOpenLab{}
	l.a, l.harbor, l.orbit = teamsPlaceLabIDs(t)
	l.where = t.TempDir()
	l.file = filepath.Join(l.where, "manager.jsonl")
	if made {
		if err := os.WriteFile(l.file, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l.key = l.a.convKey(l.file)
	l.a.open = func(where, file string) (Conversation, error) {
		l.asked = append(l.asked, where+" "+file)
		if l.err != nil {
			return Conversation{}, l.err
		}
		return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: where, SessionFile: file}, nil
	}
	if err := l.a.teamEdit(func(f *teamstore.File) error {
		if err := f.AddMember(l.orbit, teamstore.Member{Key: l.key, File: l.file, Where: l.where, Word: "run orbit", Handle: "boss"}); err != nil {
			return err
		}
		return f.SetManager(l.orbit, l.key)
	}); err != nil {
		t.Fatal(err)
	}
	return l
}

// selectOrbit chooses orbit on the rail, as a press does, and runs what that
// asked for.
func (l *teamsOpenLab) selectOrbit(t *testing.T) {
	t.Helper()
	drive(t, l.a, runCmd(l.a.teamsSelect(l.orbit))...)
}

// A MANAGER THIS WINDOW IS NOT HOLDING OPENS IN THE PANE, AND THE PERSON STAYS
// ON THE PAGE. It is opened in its own folder, not the window's, off the loop,
// and the pane hosts it; the conversation that was in front stays open behind.
// Before the fix the open went through the switcher's door, which steps off
// the place standing, and the person was taken off the page to it.
func TestTeamsManagerNotHeldOpensInThePane(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	was := l.a.frontTabKey()
	l.selectOrbit(t)
	if len(l.asked) != 1 || l.asked[0] != l.where+" "+l.file {
		t.Fatalf("the door was asked %q, want the manager in its own folder %q", l.asked, l.where)
	}
	if !l.a.at(pageTeams) {
		t.Fatalf("opening the manager took the person off the page, to %q", l.a.page.word())
	}
	if l.a.frontTabKey() != l.key || !l.a.teamsHosting() {
		t.Fatalf("the pane does not host the manager (front %q):\n%s", l.a.frontTabKey(), teamsFrameText(l.a))
	}
	if !l.a.trafficHeld(was) {
		t.Fatal("the conversation that was in front is not held behind")
	}
	if strings.Contains(teamsFrameText(l.a), "opening") {
		t.Fatalf("the hosted pane still says opening:\n%s", teamsFrameText(l.a))
	}
}

// A MANAGER HELD BEHIND COMES FORWARD at once, with no door asked.
func TestTeamsManagerHeldBehindComesForward(t *testing.T) {
	a, _, orbit := teamsPlaceLabIDs(t)
	m := mustTeam(t, a, orbit).Members[0]
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager(orbit, m.Key) }); err != nil {
		t.Fatal(err)
	}
	asked := 0
	a.open = func(where, file string) (Conversation, error) {
		asked++
		return Conversation{}, errors.New("not asked")
	}
	drive(t, a, runCmd(a.teamsSelect(orbit))...)
	if asked != 0 || a.frontTabKey() != m.Key || !a.teamsHosting() {
		t.Fatalf("the held manager did not come forward (asked %d, front %q)", asked, a.frontTabKey())
	}
}

// A MANAGER SET WHILE THE PAGE STANDS IS BROUGHT IN without anybody choosing
// the team again: a session naming one on the file, or the teams arriving
// after the page opened. The pane used to say `opening` with nothing behind it.
func TestTeamsManagerSetWhileThePageStandsIsBroughtIn(t *testing.T) {
	a, _, orbit := teamsPlaceLabIDs(t)
	drive(t, a, runCmd(a.teamsSelect(orbit))...)
	where := t.TempDir()
	file := filepath.Join(where, "later.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.open = func(w, f string) (Conversation, error) {
		return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: w, SessionFile: f}, nil
	}
	key := a.convKey(file)
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.AddMember(orbit, teamstore.Member{Key: key, File: file, Where: where, Word: "later", Handle: "later"}); err != nil {
			return err
		}
		return f.SetManager(orbit, key)
	}); err != nil {
		t.Fatal(err)
	}
	// Any message: the page settles after every one.
	drive(t, a, tea.WindowSizeMsg{Width: a.width, Height: a.height})
	if a.frontTabKey() != key || !a.teamsHosting() || !a.at(pageTeams) {
		t.Fatalf("the manager set on the file was not brought in (front %q):\n%s", a.frontTabKey(), teamsFrameText(a))
	}
}

// A REFUSAL IS SAID ON THE PANE WITH ITS REASON, and offers Retry and
// Open in chats. Retry asks again and hosts it; the refusal is not asked
// again on every beat.
func TestTeamsManagerRefusedSaysWhyAndRetries(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.err = errors.New("the engine said no")
	l.selectOrbit(t)
	drive(t, l.a, tea.WindowSizeMsg{Width: l.a.width, Height: l.a.height})
	text := teamsFrameText(l.a)
	for _, want := range []string{"couldn't open " + teamManagerGlyph + " orbit's manager: the engine said no", "Retry", teamsOpenInChatsWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("the pane does not say %q:\n%s", want, text)
		}
	}
	if len(l.asked) != 1 {
		t.Fatalf("the refused open was asked %d times without a Retry", len(l.asked))
	}
	if !l.a.at(pageTeams) {
		t.Fatal("a refusal took the person off the page")
	}
	l.err = nil
	drive(t, l.a, runCmd(l.a.teamsDo(teamsTargetOf(t, l.a, teamsActRetryManager, l.orbit)))...)
	if l.a.frontTabKey() != l.key || !l.a.teamsHosting() {
		t.Fatalf("Retry did not host the manager:\n%s", teamsFrameText(l.a))
	}
}

// AN OPEN THE ENGINE HAS NOT ANSWERED IS SAID AFTER THE BOUND: `opening` for
// the beat it takes, then the reason and the way forward. Open in chats goes
// through the chat surface's own door, off the page.
func TestTeamsManagerSlowOpenIsSaidAfterTheBound(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	now := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)
	l.a.clock = func() time.Time { return now }
	// The ask is left out: the engine has not answered.
	_ = l.a.teamsSelect(l.orbit)
	text := teamsFrameText(l.a)
	if !strings.Contains(text, "opening "+teamManagerGlyph+" orbit's manager") || strings.Contains(text, "Retry") {
		t.Fatalf("the pane does not say opening for the beat:\n%s", text)
	}
	now = now.Add(teamsOpenBound + time.Second)
	l.a.tp.top = teamsTopCache{}
	text = teamsFrameText(l.a)
	for _, want := range []string{"couldn't open " + teamManagerGlyph + " orbit's manager: no answer in 4s", "Retry", teamsOpenInChatsWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("after the bound the pane does not say %q:\n%s", want, text)
		}
	}
	drive(t, l.a, runCmd(l.a.teamsDo(teamsTargetOf(t, l.a, teamsActOpenInChats, l.orbit)))...)
	if l.a.at(pageTeams) || l.a.frontTabKey() != l.key {
		t.Fatalf("Open in chats did not open the manager off the page (page %q, front %q)", l.a.page.word(), l.a.frontTabKey())
	}
}

// A MANAGER WHOSE TRANSCRIPT IS GONE SAYS SO AND OFFERS `+ Manager`, which
// makes a new conversation the team's manager and hosts it.
func TestTeamsManagerGoneOffersANewManager(t *testing.T) {
	l := newTeamsOpenLab(t, false)
	l.selectOrbit(t)
	text := teamsFrameText(l.a)
	for _, want := range []string{"couldn't open " + teamManagerGlyph + " orbit's manager: " + teamsManagerGoneWord, teamManagerSlotWord, "Retry"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the pane does not say %q:\n%s", want, text)
		}
	}
	if len(l.asked) != 0 {
		t.Fatalf("the door was asked for a transcript that is not there: %q", l.asked)
	}
	fresh := filepath.Join(t.TempDir(), "fresh.jsonl")
	l.a.start = func(where string) (Conversation, error) {
		return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: where, SessionFile: fresh}, nil
	}
	drive(t, l.a, runCmd(l.a.teamsDo(teamsTargetOf(t, l.a, teamsActManager, l.orbit)))...)
	teamsFlush(t, l.a)
	if got := mustTeam(t, l.a, l.orbit).Manager; got != l.a.convKey(fresh) {
		t.Fatalf("+ Manager did not replace the gone manager: %q", got)
	}
	if !l.a.teamsHosting() || !l.a.at(pageTeams) {
		t.Fatalf("the new manager is not in the pane:\n%s", teamsFrameText(l.a))
	}
}

// A PRESS ON A TRAFFIC ROW IN THE HOSTED MANAGER GOES THROUGH THE CHAT'S OWN
// DOOR and takes the person to that member, as a press on a member row does,
// rather than leaving the page over a conversation it does not host with the
// pane saying `opening`.
func TestTeamsHostedTrafficRowGoesToTheMember(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "prices are in"})
	trafficReadNow(t, a)
	if cmd := a.showPage(pageTeams); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	drive(t, a, runCmd(a.teamsSelect(harbor))...)
	a.tp.traffic = true
	a.teamsSync()
	if !a.teamsHosting() {
		t.Fatalf("the pane does not host harbor's manager:\n%s", teamsFrameText(a))
	}
	// The note answers nothing, so it is a line under General, laid open.
	a.sideToggleThread(sideThreadKey(harbor, sideGeneral))
	frame, _, _ := a.frame()
	rows := strings.Split(ansi.Strip(frame), "\n")
	cols := a.railWidth()
	// THE RAIL IS THREADS: the member's handle heads the thread and opens it,
	// and the words under it are a door of their own.
	at, x := -1, -1
	for y, r := range rows {
		cells := plainCells(r, a.width-cols, a.width)
		if i := strings.Index(cells, "@"+price); i >= 0 {
			at, x = y, a.width-cols+len([]rune(cells[:i]))+1
			break
		}
	}
	if at < 0 {
		t.Fatalf("no Traffic row in the hosted pane:\n%s", strings.Join(rows, "\n"))
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: at, Button: tea.MouseLeft})
	if a.frontTabKey() != priceKey {
		t.Fatalf("the row went to %q, want %q", a.frontTabKey(), priceKey)
	}
	if a.at(pageTeams) {
		t.Fatalf("the page stayed over a conversation it does not host:\n%s", teamsFrameText(a))
	}
}

// lockingOpen makes the lab's door behave like the real one about the
// transcript's lock: the first open of a file takes it, and every later open of
// the same file while it is held is refused as another window's.
func (l *teamsOpenLab) lockingOpen() {
	held := map[string]bool{}
	l.a.open = func(where, file string) (Conversation, error) {
		l.asked = append(l.asked, where+" "+file)
		if held[file] {
			return Conversation{}, session.ErrSessionLocked
		}
		held[file] = true
		return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: where, SessionFile: file}, nil
	}
}

// A TEAM CHOSEN TWICE WHILE ITS MANAGER IS OPENING ASKS THE DOOR ONCE, AND THE
// MANAGER COMES IN. The second choice used to ask again: the first answer came
// back as a stale attempt and was put behind, the second met its lock, and the
// pane said the manager was open in another window until Retry was pressed.
// The two answers are run one after the other, first ask first, which is the
// order that stuck.
func TestTeamsManagerChosenTwiceWhileOpeningAsksOnceAndComesIn(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.lockingOpen()
	first := l.a.teamsSelect(l.orbit)
	second := l.a.teamsSelect(l.orbit)
	drive(t, l.a, runCmd(first)...)
	drive(t, l.a, runCmd(second)...)
	if len(l.asked) != 1 {
		t.Fatalf("the door was asked %d times for one manager: %q", len(l.asked), l.asked)
	}
	if l.a.frontTabKey() != l.key || !l.a.teamsHosting() || l.a.tp.open.why != "" {
		t.Fatalf("the manager is not in the pane (front %q, pane says %q):\n%s", l.a.frontTabKey(), l.a.tp.open.why, teamsFrameText(l.a))
	}
}

// A RETRY PRESSED WHILE THE FIRST OPEN IS STILL OUT ASKS AGAIN, and whichever
// answer lands the manager is the page's: the first one, which the Retry made
// stale, brings the manager in, and the Retry's refusal about a conversation
// this window now holds is no refusal.
func TestTeamsManagerRetryRacingTheFirstOpenStillComesIn(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.lockingOpen()
	first := l.a.teamsSelect(l.orbit)
	again := l.a.teamsRetryManager()
	drive(t, l.a, runCmd(first)...)
	drive(t, l.a, runCmd(again)...)
	if len(l.asked) != 2 {
		t.Fatalf("Retry did not ask the door again: %q", l.asked)
	}
	if l.a.frontTabKey() != l.key || !l.a.teamsHosting() || l.a.tp.open.why != "" {
		t.Fatalf("the manager is not in the pane (front %q, pane says %q):\n%s", l.a.frontTabKey(), l.a.tp.open.why, teamsFrameText(l.a))
	}
	if strings.Contains(teamsFrameText(l.a), sessionBusyWord) {
		t.Fatalf("the pane says the manager is busy:\n%s", teamsFrameText(l.a))
	}
}

// OVER A CONNECTION THAT HOLDS ONE CONVERSATION AT A TIME THE SWAP IS NOT MADE
// FROM UPDATE. Choosing the team asks nothing on the keystroke; the swap is a
// command, and when it answers the manager is the conversation in front.
func TestTeamsManagerSwapOverASharedConnectionIsAskedOffTheLoop(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.a.shared = true
	l.a.open = nil
	swapped := 0
	l.a.resume = func(file string) (Agent, error) {
		swapped++
		return &fakeAgent{model: "m"}, nil
	}
	cmd := l.a.teamsSelect(l.orbit)
	if swapped != 0 {
		t.Fatal("the swap was asked on the keystroke, from Update")
	}
	drive(t, l.a, runCmd(cmd)...)
	if swapped != 1 {
		t.Fatalf("the swap was asked %d times", swapped)
	}
	if l.a.frontTabKey() != l.key || !l.a.teamsHosting() {
		t.Fatalf("the swapped manager is not in the pane (front %q):\n%s", l.a.frontTabKey(), teamsFrameText(l.a))
	}
}

// A SWAP THAT MEETS THIS WINDOW'S OWN LOCK, WHILE THE MANAGER IS HELD BEHIND,
// IS NO REFUSAL. The local open forgives that case. The swap used to say the
// conversation was open in another window about one this window already holds.
// (A manager already in front is a different road: the page settles to done
// and the refusal does not stay on the pane.)
func TestTeamsSwappedLockWhileThisWindowHoldsItIsNoRefusal(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.a.shared = true
	if l.a.behind == nil {
		l.a.behind = map[string]*kept{}
	}
	l.a.tp.sel = l.orbit
	l.a.behind[l.key] = &kept{conv: Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: l.file}}
	if !l.a.holding(l.file) || l.a.frontTabKey() == l.key {
		t.Fatal("the manager is not held behind")
	}
	l.a.tp.openSeq++
	gen := l.a.tp.openSeq
	l.a.tp.open = teamsOpen{key: l.key, gen: gen, at: l.a.now(), out: true}
	if selected, ok := l.a.teamsSelected(); !ok || selected.Manager != l.key || !l.a.at(pageTeams) {
		t.Fatalf("the page does not want this manager (ok %v, manager %q, key %q, on page %v)", ok, selected.Manager, l.key, l.a.at(pageTeams))
	}
	cmd := l.a.teamsSwapped(gen, l.key, Conversation{SessionFile: l.file}, session.ErrSessionLocked)
	drive(t, l.a, tea.WindowSizeMsg{Width: l.a.width, Height: l.a.height})
	if cmd != nil {
		drive(t, l.a, runCmd(cmd)...)
	}
	if l.a.tp.open.why != "" || strings.Contains(teamsFrameText(l.a), sessionBusyWord) {
		t.Fatalf("a lock on a conversation this window holds was said (why %q):\n%s", l.a.tp.open.why, teamsFrameText(l.a))
	}
	if l.a.frontTabKey() != l.key || !l.a.teamsHosting() {
		t.Fatalf("the held manager was not brought forward (front %q):\n%s", l.a.frontTabKey(), teamsFrameText(l.a))
	}
}
