package tui3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeExec is an in-memory Exec seam. freeze panics on any call so a test
// can prove View and a cursor move never read the store.
type fakeExec struct {
	mu       sync.Mutex
	frozen   bool
	works    []ExecWork
	paused   []string
	stopped  []string
	stateErr error
	pauseErr error
	stopErr  error
	reads    int
}

func (f *fakeExec) freeze() { f.mu.Lock(); f.frozen = true; f.mu.Unlock() }

func (f *fakeExec) touch(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frozen {
		panic("Exec." + op + " called after freeze (View or cursor must not read the store)")
	}
	f.reads++
}

func (f *fakeExec) LaunchState(_ context.Context, conversationID string) ([]ExecWork, error) {
	f.touch("LaunchState")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	var out []ExecWork
	for _, work := range f.works {
		if execWorkBelongs(work, conversationID) || strings.TrimSpace(work.SourceRef) == "" {
			out = append(out, work)
		}
	}
	return append([]ExecWork(nil), out...), nil
}

func (f *fakeExec) PauseCoordination(_ context.Context, coordinatorID string) error {
	f.touch("PauseCoordination")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pauseErr != nil {
		return f.pauseErr
	}
	f.paused = append(f.paused, coordinatorID)
	return nil
}

func (f *fakeExec) StopWork(_ context.Context, workID string) error {
	f.touch("StopWork")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped = append(f.stopped, workID)
	return nil
}

func execLab(t *testing.T) (*app, *fakeExec) {
	t.Helper()
	fake := &fakeExec{}
	a := newLiveLab(t).open()
	a.exec = fake
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.readCollab()
	a.readExec()
	a.home.build()
	homeText(a)
	return a, fake
}

func TestLaunchStateIsSoftwareDerivedOnTheBeat(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		Road: "session-task", SourceRef: "aaaa000000000001", Joined: true,
	}}
	a.readExec()
	a.home.build()
	homeText(a)
	fake.freeze()
	homeText(a)
	a.home.move(1)
	homeText(a)
	a.closeHome()
	_ = frame(a)
}

func TestLaunchStatePaintsFolderAndDiscussionPreview(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		Road: "session-task", SourceRef: "aaaa000000000001", Joined: true,
	}}
	a.readExec()
	a.home.build()
	a.pointFolderID("col-billing")
	a.enterFolder("col-billing")
	home := homeText(a)
	for _, want := range []string{execJoinWord, "running"} {
		if !strings.Contains(home, want) {
			t.Fatalf("folder preview hid %q:\n%s", want, home)
		}
	}
	if strings.Contains(home, execZeroRunsWord) || strings.Contains(home, execFakeDoneWord) {
		t.Fatalf("folder preview invented a roll-up:\n%s", home)
	}
	a.closeHome()
	a.readExec()
	a.touch()
	got := plain(frame(a))
	for _, want := range []string{execJoinWord, "running", execSourceWord, "readme comment", "session-task"} {
		if !strings.Contains(got, want) {
			t.Fatalf("discussion preview hid %q:\n%s", want, got)
		}
	}
}

func TestLaunchStateEmptyDrawsNothingNotZeroRuns(t *testing.T) {
	a, _ := execLab(t)
	a.closeHome()
	a.readExec()
	a.touch()
	got := strings.ToLower(plain(frame(a)))
	if strings.Contains(got, execZeroRunsWord) || strings.Contains(got, "0 runs") {
		t.Fatalf("empty launch state broke the emptiness law:\n%s", got)
	}
	if strings.Contains(got, execJoinWord) || strings.Contains(got, execUnavailableWord) {
		t.Fatalf("empty launch state painted chrome:\n%s", got)
	}
}

func TestLaunchStateUnavailableDoesNotLookEmpty(t *testing.T) {
	a, fake := execLab(t)
	fake.stateErr = errExecProbe
	a.readExec()
	a.home.build()
	a.pointFolderID("col-billing")
	a.enterFolder("col-billing")
	home := homeText(a)
	if !strings.Contains(home, "folders") {
		t.Fatalf("unavailable launch state dropped the folders heading:\n%s", home)
	}
	if !strings.Contains(home, execUnavailableWord) {
		t.Fatalf("unavailable launch state looked empty:\n%s", home)
	}
	if strings.Contains(home, execZeroRunsWord) || strings.Contains(home, execFakeDoneWord) {
		t.Fatalf("unavailable launch state masqueraded as empty work:\n%s", home)
	}
	a.closeHome()
	a.touch()
	got := plain(frame(a))
	if !strings.Contains(got, execUnavailableWord) {
		t.Fatalf("discussion hid unavailable launch state:\n%s", got)
	}
}

func TestLaunchStateKeepsLastWorkOnFailedRefresh(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "running",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	fake.stateErr = errExecProbe
	fake.works = nil
	a.readExec()
	a.closeHome()
	a.touch()
	got := plain(frame(a))
	if !strings.Contains(got, "readme comment") {
		t.Fatalf("failed refresh dropped last good launch state:\n%s", got)
	}
}

func TestLaunchStateEightyColumnsStayReadable(t *testing.T) {
	a, fake := execLab(t)
	a.width, a.height = 80, 40
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "Porting the Resume Picker", State: "bound",
		Road: "bash-run", SourceRef: "aaaa000000000001", Joined: true,
	}}
	a.readExec()
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	a.enterFolder("col-billing")
	home := homeText(a)
	if a.home.cols != 1 || homeGridCols(a.width) != 1 {
		t.Fatalf("80-col launch browse grew %d columns", a.home.cols)
	}
	if !strings.Contains(home, execJoinWord) {
		t.Fatalf("80-col folder preview hid launch-or-join:\n%s", home)
	}
	a.closeHome()
	a.readExec()
	a.touch()
	a.width = 80
	chat := plain(frame(a))
	if !strings.Contains(chat, execJoinWord) {
		t.Fatalf("80-col discussion hid launch-or-join:\n%s", chat)
	}
	for _, line := range strings.Split(chat, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Fatalf("80-col launch-state row is %d cells: %q", w, line)
		}
	}
}

func TestLaunchStateDoesNotJumpSelection(t *testing.T) {
	a, fake := execLab(t)
	a.pointFolderID("col-billing")
	want, ok := a.home.focusedLine()
	if !ok || want.kind != homeFolderRow || want.dir != "col-billing" {
		t.Fatalf("cursor was not on Billing: %+v", want)
	}
	a.home.box.setText("keep this sentence")
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001", Joined: true,
	}}
	a.readExec()
	a.home.build()
	homeText(a)
	if a.home.box.String() != "keep this sentence" {
		t.Fatalf("binding updates wiped the composer: %q", a.home.box.String())
	}
	a.home.box.setText("")
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	got, ok := a.home.focusedLine()
	if !ok || got.kind != homeFolderRow || got.dir != "col-billing" {
		t.Fatalf("binding updates dropped Billing: %+v", got)
	}
	a.closeHome()
	a.input.setText("keep the draft")
	sel := a.sel
	fake.works = append(fake.works, ExecWork{
		WorkID: "w-other", Title: "second", State: "running", SourceRef: "aaaa000000000001",
	})
	a.readExec()
	a.touch()
	_ = frame(a)
	if a.input.String() != "keep the draft" {
		t.Fatalf("binding updates wiped the chat composer: %q", a.input.String())
	}
	if a.sel != sel {
		t.Fatalf("binding updates jumped chat selection from %d to %d", sel, a.sel)
	}
}

func TestPauseCoordinationAndStopWorkAreTwoVerbs(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	folder := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	pauseKey, stopKey := rune(0), rune(0)
	for _, v := range a.folderVerbs(folder) {
		switch v.word {
		case execPauseWord:
			pauseKey = v.key
		case execStopWord:
			t.Fatal("folder row offered stop work; pause and stop must not share a chord")
		}
	}
	if pauseKey != 'p' {
		t.Fatalf("pause coordination key %q, want p", pauseKey)
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	for _, v := range a.folderVerbs(member) {
		switch v.word {
		case execPauseWord:
			pauseKey = v.key
		case execStopWord:
			stopKey = v.key
		}
	}
	if pauseKey == 0 || stopKey == 0 {
		t.Fatal("member with work hid pause coordination or stop work")
	}
	if pauseKey == stopKey {
		t.Fatalf("pause coordination and stop work shared chord %q", pauseKey)
	}
	if pauseKey != 'p' || stopKey != 's' {
		t.Fatalf("pause/stop keys %q/%q, want p/s", pauseKey, stopKey)
	}
}

func TestPauseCoordinationDoesNotStopWork(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	if cmd := a.pauseCoordination(); cmd != nil {
		t.Fatal("pause returned a command")
	}
	if len(fake.paused) != 1 || fake.paused[0] != "aaaa000000000001" {
		t.Fatalf("paused %v, want the existing chat", fake.paused)
	}
	if len(fake.stopped) != 0 {
		t.Fatalf("pause coordination stopped work: %v", fake.stopped)
	}
}

func TestStopWorkDoesNotPauseCoordination(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	if cmd := a.stopExecWork("w-readme"); cmd != nil {
		t.Fatal("stop work returned a command")
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != "w-readme" {
		t.Fatalf("stopped %v, want w-readme", fake.stopped)
	}
	if len(fake.paused) != 0 {
		t.Fatalf("stop work paused coordination: %v", fake.paused)
	}
}

func TestStopWorkTabCloseIsStopNotPause(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	if err := a.stopConversation(a.frontChatTab()); err != nil {
		t.Fatal(err)
	}
	if len(fake.paused) != 0 {
		t.Fatalf("tab-close stop work paused coordination: %v", fake.paused)
	}
	if len(fake.stopped) != 1 || fake.stopped[0] != "w-readme" {
		t.Fatalf("tab-close stop work stopped %v", fake.stopped)
	}
}

func TestClosingFolderViewDoesNotPauseCoordination(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	a.closeHome()
	if len(fake.paused) != 0 || len(fake.stopped) != 0 {
		t.Fatalf("closing a view paused=%v stopped=%v", fake.paused, fake.stopped)
	}
}

func TestFolderNilExecHasNoLaunchChrome(t *testing.T) {
	a := newLiveLab(t).open()
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.readCollab()
	a.readExec()
	a.home.build()
	homeText(a)
	row := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	keys := ""
	for _, v := range a.folderVerbs(row) {
		keys += string(v.key)
		if v.word == execPauseWord || v.word == execStopWord {
			t.Fatalf("nil Exec offered %q", v.word)
		}
	}
	if keys != "nfei" {
		t.Fatalf("nil Exec folder verbs %q, want nfei", keys)
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	keys = ""
	for _, v := range a.folderVerbs(member) {
		keys += string(v.key)
	}
	if keys != "nfmwx" {
		t.Fatalf("nil Exec member verbs %q, want nfmwx", keys)
	}
	a.closeHome()
	a.touch()
	got := plain(frame(a))
	if strings.Contains(got, execJoinWord) || strings.Contains(got, execPauseWord) || strings.Contains(got, execUnavailableWord) {
		t.Fatalf("nil Exec painted launch chrome:\n%s", got)
	}
}

func TestFolderWave1VerbsStayWithLaunchWired(t *testing.T) {
	a, fake := execLab(t)
	fake.works = []ExecWork{{
		WorkID: "w-readme", Title: "readme comment", State: "bound",
		SourceRef: "aaaa000000000001",
	}}
	a.readExec()
	row := homeLine{kind: homeFolderRow, dir: "col-billing", project: "Billing"}
	keys := ""
	for _, v := range a.folderVerbs(row) {
		keys += string(v.key)
	}
	if keys != "nfeip" {
		t.Fatalf("launch-wired folder verbs %q, want nfeip", keys)
	}
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	keys = ""
	for _, v := range a.folderVerbs(member) {
		keys += string(v.key)
	}
	if keys != "nfmwxps" {
		t.Fatalf("launch-wired member verbs %q, want nfmwxps", keys)
	}
}

func TestFolderLaunchAddsNoSlash(t *testing.T) {
	for _, c := range commands {
		if c.name == "launch" || c.name == "pause" || c.name == "launch-or-join" {
			t.Fatalf("new slash /%s; Wave 4 adds no slash command", c.name)
		}
	}
}

var errExecProbe = errors.New("exec probe")
