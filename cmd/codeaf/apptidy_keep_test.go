package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// keptWork gives the second of lazyBackend's agents a task the crew keeper
// opens a conversation to recover, the way a quick task never noted does.
func keptWork(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "tasks.json"), []byte(`{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"parked work","brief":"carry on","kind":"quick","state":"queued"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// presenceLike makes b's conversations keep a presence file the way a real one
// does: written when it is opened and again when it is let go. A close changes
// what the keeper reads, so it looks at the agent again.
func presenceLike(t *testing.T, b *appBackend) {
	t.Helper()
	opener := b.open
	b.open = func(ask appOpenAsk) (*appConn, error) {
		conn, err := opener(ask)
		if err != nil {
			return conn, err
		}
		presence := filepath.Join(filepath.Dir(ask.Transcript), "presence.json")
		say := func(state string) {
			if err := os.WriteFile(presence, []byte(`{"schema":1,"state":"`+state+`","pid":`+strconv.Itoa(os.Getpid())+`}`), 0o600); err != nil {
				t.Error(err)
			}
		}
		say("idle")
		release := conn.Release
		conn.Release = func() { say("let go"); release() }
		return conn, nil
	}
}

// keptClosed says the agent is still marked as the person left it and nothing
// holds it, after the keeper has looked again.
func keptClosed(t *testing.T, b *appBackend, run *appCrewRun, id string) {
	t.Helper()
	for i := 0; i < 3; i++ {
		b.crewTick(run, false)
		time.Sleep(20 * time.Millisecond)
	}
	if !b.tidied(id) || b.held(id) {
		t.Fatalf("after the keeper looked: marked=%v held=%v", b.tidied(id), b.held(id))
	}
}

// A CLOSED AGENT IS NEVER OPENED BY THE CREW KEEPER. It holds open every
// agent with work to recover, and an open takes an agent out of Closed, so an
// agent with such work came back the moment it was closed: its conversation
// was let go, the keeper saw an agent let go a moment ago with work in it, and
// opened it again.
func TestTheCrewKeeperLeavesAClosedAgentClosed(t *testing.T) {
	for name, verb := range tidyVerbs {
		t.Run(name+" while held", func(t *testing.T) {
			b, rec, paths, opens := lazyBackend(t, 2)
			presenceLike(t, b)
			keptWork(t, paths[1])
			if err := teamstore.Update(b.profileDir, func(f *teamstore.File) error { f.Teams = nil; return nil }); err != nil {
				t.Fatal(err)
			}
			id := appAgentID(paths[1])
			run := b.crews()
			rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
				changed, ok := data.(appwire.AgentChanged)
				return ok && changed.Agent.ID == id
			})
			if !b.held(id) || opens.Load() != 1 {
				t.Fatalf("the keeper did not hold the agent with work: held=%v opens=%d", b.held(id), opens.Load())
			}
			if err := verb(b, id); err != nil {
				t.Fatal(err)
			}
			keptClosed(t, b, run, id)
			if got := opens.Load(); got != 1 {
				t.Fatalf("the keeper opened the %s agent again: %d opens", name, got)
			}
			// The person's own open still takes it out.
			if _, err := b.OpenAgent(id); err != nil || b.tidied(id) {
				t.Fatalf("a person's open = %v, marked=%v", err, b.tidied(id))
			}
		})
		t.Run(name+" before the keeper starts", func(t *testing.T) {
			b, _, paths, opens := lazyBackend(t, 2)
			keptWork(t, paths[1])
			if err := teamstore.Update(b.profileDir, func(f *teamstore.File) error { f.Teams = nil; return nil }); err != nil {
				t.Fatal(err)
			}
			id := appAgentID(paths[1])
			if err := verb(b, id); err != nil {
				t.Fatal(err)
			}
			keptClosed(t, b, b.crews(), id)
			if got := opens.Load(); got != 0 {
				t.Fatalf("the keeper's first look opened the %s agent: %d opens", name, got)
			}
		})
	}
}

// A closed crew member is answered from what is there: reading its tasks or
// asks never opens it, as an open would take it out of Closed.
func TestReadingAClosedCrewMemberLeavesItClosed(t *testing.T) {
	b, _, paths, opens := lazyBackend(t, 2)
	run := b.crews()
	id := appAgentID(paths[1])
	if !b.crewKeeps(id) {
		t.Fatal("the member is not kept by its crew")
	}
	if err := b.MoveToClosed([]string{id}); err != nil {
		t.Fatal(err)
	}
	before := opens.Load()
	if _, err := b.ListTasks(id); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ListAsks(id); err != nil {
		t.Fatal(err)
	}
	bucket := filepath.Base(filepath.Dir(filepath.Dir(paths[1])))
	if _, err := b.crewOwnerPage(t.Context(), crewOwner{id: id, bucket: bucket, file: paths[1]}, 10); err != nil {
		t.Fatal(err)
	}
	keptClosed(t, b, run, id)
	if got := opens.Load(); got != before {
		t.Fatalf("reading a closed member opened it: %d opens, was %d", got, before)
	}
}

// Work a quit left owed is not carried on in an agent the person has closed
// since: carrying it on would open it, and so take it out of Closed.
func TestAQuitsResumeLeavesAClosedAgentClosed(t *testing.T) {
	b, _, paths, opens := lazyBackend(t, 1)
	id := appAgentID(paths[0])
	if err := b.MoveToClosed([]string{id}); err != nil {
		t.Fatal(err)
	}
	b.resumeOne(id, true, nil)
	if !b.tidied(id) || b.held(id) || opens.Load() != 0 {
		t.Fatalf("after the resume: marked=%v held=%v opens=%d", b.tidied(id), b.held(id), opens.Load())
	}
}

// agents.create's posture outlives a first open that could not set it: the
// retry's open sets it, so the agent never runs without what was asked.
func TestCreateDialsOutliveAFailedFirstOpen(t *testing.T) {
	b, _, paths, _ := lazyBackend(t, 1)
	a, err := b.find(appAgentID(paths[0]))
	if err != nil {
		t.Fatal(err)
	}
	b.locked(func() { a.dials = &appDials{posture: "ask"} })
	if _, _, err := b.ensure(a); err == nil {
		t.Fatal("a conversation with no posture door opened as if it took the posture")
	}
	var kept *appDials
	b.locked(func() { kept = a.dials })
	if kept == nil {
		t.Fatal("the failed open dropped the posture agents.create asked for")
	}
	conv := &postureConversation{}
	b.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, UserMessages: 1, Release: func() {}}, nil
	}
	if _, _, err := b.ensure(a); err != nil {
		t.Fatal(err)
	}
	b.locked(func() { kept = a.dials })
	if conv.posture != "ask" || kept != nil {
		t.Fatalf("after the retry's open: posture %q, still pending %+v", conv.posture, kept)
	}
}

// postureConversation is a conversation with a posture door.
type postureConversation struct {
	fakeConversation
	posture string
}

func (c *postureConversation) ResolvedApprovalPosture() string { return c.posture }

func (c *postureConversation) SetApprovalPosture(posture string) error {
	c.posture = posture
	return nil
}
