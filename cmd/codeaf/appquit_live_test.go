package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The contract of these tests is what a quit writes down for the next start
// (appquit.go): the replies in the middle of their work to carry on, and the
// processes that were running as "stopped by quit". A chat THIS desktop runs is
// written down; a pure live view of a chat another computer runs is not,
// because quitting here stops nothing over there and its id means nothing
// after a restart. The state file is read back, never the backend's memory, so
// what is asserted is what a later start would find.

// quitJobsConversation is a conversation with one process running in it, as a
// quit finds a chat whose reply started something. Embedding the interface and
// not a concrete type keeps the process pager out of its method set, so the
// quit reads the jobs from JobNotices.
type quitJobsConversation struct {
	appConversation
	jobs []session.JobNotice
}

func (c *quitJobsConversation) JobNotices() ([]session.JobNotice, error) { return c.jobs, nil }

// quitJob is a process the model started and left running.
func quitJob() session.JobNotice {
	return session.JobNotice{ID: 3, Command: "sleep 600", State: session.JobRunning, Kind: session.JobKindCommand, Started: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)}
}

// quitMidReply puts a's conversation where a quit finds a reply: still
// running, with a process alive in it. The running flag is the one the turn's
// stream sets while a reply is being written.
func quitMidReply(b *appBackend, a *appAgent) {
	b.locked(func() {
		a.live.running = true
		a.live.conn.Conv = &quitJobsConversation{appConversation: a.live.conn.Conv, jobs: []session.JobNotice{quitJob()}}
	})
}

// quitLocalChat registers a chat this process built itself, in the middle of a
// reply with a process running.
func quitLocalChat(t *testing.T, b *appBackend, id string) *appAgent {
	t.Helper()
	a := b.agent(id)
	b.locked(func() {
		a.live = &appLive{box: appwire.NewOutbox(b.emit), conn: &appConn{Conv: &fakeConversation{}, Assembled: true, Release: func() {}}}
	})
	quitMidReply(b, a)
	return a
}

// quitState reads back what a quit left in the app's state file.
func quitState(t *testing.T, b *appBackend) (f appStateFile) {
	t.Helper()
	if err := b.state.read(func(s *appStateFile) { f = *s }); err != nil {
		t.Fatal(err)
	}
	return f
}

// wantNothingFor fails when the state file carries anything a later start
// would act on for id: a resume, a reply, a stopped process or an Interrupted
// mark. The mark is what interruptAfterQuit writes at the next start from the
// resume list, so it is read after that has run.
func wantNothingFor(t *testing.T, b *appBackend, id string) {
	t.Helper()
	f := quitState(t, b)
	for _, got := range [][]string{f.ResumeAfterQuit, f.QuitReplies, f.QuitTasks} {
		for _, name := range got {
			if name == id {
				t.Errorf("the quit wrote %q down to resume: resumes %v, replies %v, tasks %v", id, f.ResumeAfterQuit, f.QuitReplies, f.QuitTasks)
			}
		}
	}
	for _, p := range f.QuitStopped {
		if p.Agent == id {
			t.Errorf("the quit wrote a process of %q down as %q: %+v", id, appQuitStoppedWord, p)
		}
	}
	b.interruptAfterQuit()
	if _, marked := quitState(t, b).Interrupted[id]; marked {
		t.Errorf("a restart would hold an Interrupted mark for %q, an agent that will not exist", id)
	}
}

// Q1 (a quit with a pure live view open records nothing for it): the chat
// runs on another computer, a reply is in progress there and a process runs
// there. Quitting this desktop stops neither, so no resume, no "stopped by
// quit" process and, after a restart, no Interrupted mark for the view's id.
func TestQuitGuardQ1APureLiveViewIsNeverRecordedAtQuit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		assembled bool
	}{
		// The view's connection is built over the live door and is not marked
		// as built here (dialLive).
		{"as a live view is opened", false},
		// The guard must not rest on that mark alone: a desk-held chat is
		// marked built here and is recorded, so a view that ever carried the
		// mark must still be left out for being a view.
		{"even with its connection marked as built here", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLiveRig(t, true)
			r.open()
			a := r.b.agent(liveRigCell)
			var pure bool
			r.b.locked(func() { pure = a.liveOnly() })
			if !pure {
				t.Fatal("the rig's view has a folder here, so it is not the pure view this contract is about")
			}
			if tc.assembled {
				r.b.locked(func() { a.live.conn.Assembled = true })
			} else if built := r.b.liveOf(a).conn.Assembled; built {
				t.Fatal("a live view's connection is built over the live door, never marked as built here")
			}
			quitMidReply(r.b, a)
			r.b.markQuit([]*appAgent{a})
			wantNothingFor(t, r.b, liveRigCell)
		})
	}
}

// Q1 and Q2 together: a quit that walks a live view and a chat of this
// desktop's in the same pass records the chat and leaves the view out, and the
// view's running process does not borrow the chat's turn or the chat's the view's.
func TestQuitGuardQ1TheLocalChatBesideAPureLiveViewIsStillRecorded(t *testing.T) {
	r := newLiveRig(t, true)
	r.open()
	view := r.b.agent(liveRigCell)
	r.b.locked(func() { view.live.conn.Assembled = true })
	quitMidReply(r.b, view)
	local := quitLocalChat(t, r.b, "local-chat")
	// The view comes first, so a guard that stopped the whole walk would show.
	r.b.markQuit([]*appAgent{view, local})
	f := quitState(t, r.b)
	if !reflect.DeepEqual(f.ResumeAfterQuit, []string{"local-chat"}) || !reflect.DeepEqual(f.QuitReplies, []string{"local-chat"}) {
		t.Fatalf("resumes %v, replies %v, want only the desktop's own chat", f.ResumeAfterQuit, f.QuitReplies)
	}
	if len(f.QuitStopped) != 1 || f.QuitStopped[0].Agent != "local-chat" {
		t.Fatalf("stopped processes %+v, want the desktop's own chat's one", f.QuitStopped)
	}
	wantNothingFor(t, r.b, liveRigCell)
}

// wantRecorded fails unless a quit wrote id down exactly as it always has: a
// reply to carry on and the running process listed as stopped by quit.
func wantRecorded(t *testing.T, b *appBackend, id string) {
	t.Helper()
	f := quitState(t, b)
	if !reflect.DeepEqual(f.ResumeAfterQuit, []string{id}) || !reflect.DeepEqual(f.QuitReplies, []string{id}) {
		t.Errorf("resumes %v, replies %v, want %q alone in both", f.ResumeAfterQuit, f.QuitReplies, id)
	}
	if len(f.QuitStopped) != 1 {
		t.Fatalf("stopped processes %+v, want the one that was running", f.QuitStopped)
	}
	p := f.QuitStopped[0]
	if p.Agent != id || p.State != appwire.ProcessStopped || p.Reason != appQuitStoppedWord || p.EndedAt == nil {
		t.Errorf("the stopped process = %+v, want %q's, stopped with the reason %q", p, id, appQuitStoppedWord)
	}
}

// Q2 (a chat this desktop runs is recorded as before): in the middle of a
// reply with a process running, it is carried on after the restart and its
// process is listed as stopped by quit.
func TestQuitGuardQ2AChatThisDesktopRunsIsRecordedAtQuit(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { _ = b.Close() })
	a := quitLocalChat(t, b, "local-chat")
	b.markQuit([]*appAgent{a})
	wantRecorded(t, b, "local-chat")
}

// Q2, the desk-held host: a chat this desktop runs and another computer is
// watching through the holder door is the desktop's own, not a live view, so a
// quit records it as it records any other.
func TestQuitGuardQ2ADeskHeldChatAnotherComputerViewsIsStillRecorded(t *testing.T) {
	r := newDeskHeldRig(t, true)
	conn := r.open()
	t.Cleanup(conn.Release)
	socket, found := r.registry()
	if !found {
		t.Fatal("a chat the desktop opened with remote control on has no holder door")
	}
	if _, err := r.viewer(socket); err != nil {
		t.Fatalf("another computer could not join the chat through the door: %v", err)
	}
	if got := deskHost.viewers(r.file); got != 1 {
		t.Fatalf("one other computer is watching, the door says %d", got)
	}
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { _ = b.Close() })
	a := b.agent("desk-chat")
	b.locked(func() { a.live = &appLive{box: appwire.NewOutbox(b.emit), conn: conn} })
	quitMidReply(b, a)
	var pure bool
	b.locked(func() { pure = a.liveOnly() })
	if pure {
		t.Fatal("a chat the desktop runs is not a pure live view")
	}
	b.markQuit([]*appAgent{a})
	wantRecorded(t, b, "desk-chat")
}

// Q3 (remote control off changes nothing): there is no live door to open a
// view over, so every chat is this desktop's own and is recorded as before.
func TestQuitGuardQ3WithRemoteControlOffAQuitRecordsAsBefore(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	if config.RemoteControlOn() {
		t.Fatal("remote control must be off for this contract")
	}
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { _ = b.Close() })
	if b.ChatsLiveOffered() {
		t.Fatal("with remote control off no live view can be offered")
	}
	a := quitLocalChat(t, b, "local-chat")
	b.markQuit([]*appAgent{a})
	wantRecorded(t, b, "local-chat")
}
