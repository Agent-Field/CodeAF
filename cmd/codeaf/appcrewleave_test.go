package main

import (
	"context"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// EOF must arm the same process bound as a signal before Close can wait on
// the backend lock. A crew opening or watcher must not keep that bound unarmed.
func TestAppEOFArmsLeaveBeforeBackendLock(t *testing.T) {
	ctx, leaving := context.WithCancel(context.Background())
	defer leaving()
	server := appwire.NewServer(io.Discard)
	b := newFakeBackend(server, nil)
	b.leaving = leaving
	b.warmed = make(chan struct{})
	close(b.warmed)
	b.mu.Lock()
	returned := make(chan struct{})
	served := make(chan error, 1)
	go func() {
		served <- server.Serve(ctx, strings.NewReader(""), appwire.Hello{}, b)
		close(returned)
	}()
	ended := make(chan struct{})
	go leaveWithin(ctx, b.left, returned, time.Millisecond, func() { close(ended) })
	select {
	case <-ended:
	case <-time.After(time.Second):
		stack := make([]byte, 64<<10)
		t.Errorf("EOF left the leave bound unarmed while Close waited for the backend lock:\n%s", stack[:runtime.Stack(stack, true)])
	}
	b.mu.Unlock()
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the app did not finish after the backend lock was released")
	}
}

type quitCrewConversation struct {
	*fakeConversation
	stop sync.Once
}

func (c *quitCrewConversation) Interrupt() {
	c.fakeConversation.Interrupt()
	c.stop.Do(func() { close(c.hold) })
}

func TestAppCloseHeldCrewKeepsMemberResumeFacts(t *testing.T) {
	c := newAppMemberCrew(t)
	a := c.backend.agent(c.member)
	a.gate.Lock()
	c.conv.hold = make(chan struct{})
	conv := &quitCrewConversation{fakeConversation: c.conv}
	released := make(chan struct{})
	c.backend.locked(func() {
		a.live.conn.Conv = conv
		a.live.conn.Assembled = true
		a.live.conn.Release = func() { close(released) }
	})
	a.gate.Unlock()
	defer conv.Interrupt()
	if _, err := c.backend.Send(c.member, "continue the member's work", ""); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- c.backend.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("closing a held crew waited for its member's turn")
	}
	select {
	case <-released:
	default:
		t.Error("the held member was not released")
	}
	select {
	case <-c.run.done:
	default:
		t.Error("the crew watcher outlived Close")
	}
	conv.mu.Lock()
	interrupted := conv.interrupted
	conv.mu.Unlock()
	if interrupted != 1 {
		t.Errorf("member interrupted %d times, want once", interrupted)
	}
	if err := c.backend.state.read(func(f *appStateFile) {
		if !reflect.DeepEqual(f.ResumeAfterQuit, []string{c.member}) || !reflect.DeepEqual(f.QuitReplies, []string{c.member}) {
			t.Errorf("member quit facts = resumes %v, replies %v", f.ResumeAfterQuit, f.QuitReplies)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAppCloseDoesNotWaitForCrewMemberOpening(t *testing.T) {
	b, _, paths, _ := lazyBackend(t, 2)
	run := b.crews()
	entered, release := make(chan struct{}), make(chan struct{})
	member := appAgentID(paths[1])
	defer func() {
		close(release)
		a := b.agent(member)
		a.gate.Lock()
		a.gate.Unlock()
	}()
	before, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	b.open = func(appOpenAsk) (*appConn, error) {
		close(entered)
		<-release
		return nil, appwire.Errorf(appwire.CodeClosed, "the opener stopped")
	}
	b.holdMembers(run, map[string][]string{member: {run.teams[0].ID}})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("the crew did not try to open its member")
	}
	closed := make(chan error, 1)
	go func() { closed <- b.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		stack := make([]byte, 128<<10)
		t.Fatalf("Close waited for a crew member's pending open:\n%s", stack[:runtime.Stack(stack, true)])
	}
	if after, err := os.ReadFile(paths[1]); err != nil || string(after) != string(before) {
		t.Errorf("closing changed the opening member's record: %v", err)
	}
}
