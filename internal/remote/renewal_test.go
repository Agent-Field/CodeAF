package remote

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
)

type countedSealAgent struct {
	*session.Agent
	takes atomic.Int64
}

func (a *countedSealAgent) TakeSealNotice() string {
	a.takes.Add(1)
	return a.Agent.TakeSealNotice()
}

func idleSealAgent(t *testing.T, watch *cellstore.SealWatch) *countedSealAgent {
	t.Helper()
	a, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/no-model-call", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Fixture.", Seals: watch})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return &countedSealAgent{Agent: a}
}

func hostSealAgent(t *testing.T, a *countedSealAgent) *Client {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: a}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop.Client
}

func awaitSealNotice(t *testing.T, client *Client, line string, failing bool) string {
	t.Helper()
	var said string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		said += client.TakeNotice()
		if strings.Contains(said, line) && client.Agent().SealFailing() == failing {
			return said
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("notice=%q, want %q with failing=%v; got failing=%v", said, line, failing, client.Agent().SealFailing())
	return ""
}

func TestAnIdleHealthyChatAsksItsSealWatchNothing(t *testing.T) {
	a := idleSealAgent(t, &cellstore.SealWatch{})
	hostSealAgent(t, a)
	time.Sleep(200 * time.Millisecond)
	a.takes.Store(0)
	time.Sleep(2 * time.Second)
	takes := a.takes.Load()
	t.Logf("idle TakeSealNotice calls in 2 s: %d", takes)
	if takes > 1 {
		t.Fatalf("healthy idle chat asked its seal watch %d times, want at most 1", takes)
	}
}

func TestIdleSealNewsReachesTheWatchingSurfaceAtOnce(t *testing.T) {
	watch := &cellstore.SealWatch{}
	client := hostSealAgent(t, idleSealAgent(t, watch))
	note := "zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it"
	failure := "calls are not being sealed, so rewind cannot reach them: disk full"
	recovery := "sealing works again, and the calls made meanwhile are sealed now"
	watch.Note(note)
	said := awaitSealNotice(t, client, note, false)
	watch.Report(errors.New("disk full"))
	said += awaitSealNotice(t, client, failure, true)
	watch.Report(nil)
	said += awaitSealNotice(t, client, recovery, false)
	time.Sleep(200 * time.Millisecond)
	said += client.TakeNotice()
	for _, line := range []string{note, failure, recovery} {
		if count := strings.Count(said, line); count != 1 {
			t.Fatalf("%q appeared %d times in %q, want once", line, count, said)
		}
	}
}

func TestSealNewsQueuedBeforeAnyoneWatchesArrivesOnAttach(t *testing.T) {
	watch := &cellstore.SealWatch{}
	a := idleSealAgent(t, watch)
	line := "zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it"
	watch.Note(line)
	client := hostSealAgent(t, a)
	said := awaitSealNotice(t, client, line, false)
	if strings.Count(said, line) != 1 {
		t.Fatalf("queued notice=%q, want exactly one line", said)
	}
}
