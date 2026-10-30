package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// unreadDisk is one conversation on disk under an isolated home, read by a
// backend that opens nothing, and the app file its marks are kept in.
type unreadDisk struct {
	t       *testing.T
	id, dir string
	state   string
	backend *appBackend
	rec     *appRecorder
	opens   int
	mu      sync.Mutex
}

func newUnreadDisk(t *testing.T) *unreadDisk {
	t.Helper()
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	d := &unreadDisk{t: t, id: session.NewSessionID(), state: filepath.Join(t.TempDir(), appStateName)}
	d.dir = filepath.Join(session.PlacesRoot(), "unread-fixtures", d.id)
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	spoke := time.Now().Add(-time.Hour)
	if err := session.SaveMeta(d.dir, session.Meta{ID: d.id, Title: "unread", Workspace: t.TempDir(), Created: spoke, LastUserAt: spoke}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.dir, "transcript.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.start()
	return d
}

// start is a new backend over the same disk and app file, the way the app is
// opened again after a quit. Its opener counts, so a test can prove nothing
// opened the conversation.
func (d *unreadDisk) start() {
	d.t.Helper()
	d.rec = newAppRecorder()
	d.backend = newFakeBackend(d.rec, func(appOpenAsk) (*appConn, error) {
		d.mu.Lock()
		d.opens++
		d.mu.Unlock()
		return nil, os.ErrPermission
	})
	backend := d.backend
	d.t.Cleanup(func() { backend.Close() })
	d.backend.state = &appState{path: d.state}
}

// replied stamps the answer time the session writes when a turn seals on one.
func (d *unreadDisk) replied(at time.Time) {
	d.t.Helper()
	meta, err := session.LoadMeta(d.dir)
	if err != nil {
		d.t.Fatal(err)
	}
	meta.LastReplyAt = at
	if err := session.SaveMeta(d.dir, meta); err != nil {
		d.t.Fatal(err)
	}
}

// asking writes the presence a conversation keeps while a question waits.
func (d *unreadDisk) asking(asked time.Time) {
	d.t.Helper()
	raw, err := json.Marshal(session.SessionPresence{
		Schema: 1, SessionID: d.id, PID: os.Getpid() + 1, UpdatedAt: time.Now(), State: session.PresenceWaiting,
		Reason: "which branch?", Question: session.PresenceQuestion{ID: 1, Text: "which branch?", Asked: asked},
	})
	if err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.dir, "presence.json"), raw, 0o600); err != nil {
		d.t.Fatal(err)
	}
}

// listed is the agent as agents.list says it, in any state.
func (d *unreadDisk) listed() appwire.Agent {
	d.t.Helper()
	all, err := d.backend.ListAgentsIn("", appwire.ListAll, "")
	if err != nil {
		d.t.Fatal(err)
	}
	for _, a := range all {
		if a.ID == d.id {
			return a
		}
	}
	d.t.Fatalf("agents.list does not have %s: %+v", d.id, all)
	return appwire.Agent{}
}

// A REPLY AFTER THE PERSON SAW AN AGENT MAKES IT UNREAD, SEEING IT CLEARS IT,
// AND BOTH SURVIVE A RESTART: the answer time is the session's own stamp and
// the mark is the app file's.
func TestAReplyAfterMarkSeenIsUnreadUntilSeenAndSurvivesARestart(t *testing.T) {
	d := newUnreadDisk(t)
	if d.listed().Unread {
		t.Fatal("an agent that has said nothing reads unread")
	}
	if err := d.backend.MarkSeen(d.id); err != nil {
		t.Fatal(err)
	}
	d.replied(time.Now().Add(time.Second))
	if !d.listed().Unread {
		t.Fatal("a reply after the seen mark does not read unread")
	}

	d.start()
	if !d.listed().Unread {
		t.Fatal("unread did not survive a restart")
	}
	if err := d.backend.MarkSeen(d.id); err != nil {
		t.Fatal(err)
	}
	d.rec.waitFor(t, 2*time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == d.id && !c.Agent.Unread
	})
	if d.listed().Unread {
		t.Fatal("agents.mark_seen did not clear unread")
	}

	d.start()
	if d.listed().Unread {
		t.Fatal("the seen mark did not survive a restart")
	}
	var seen bool
	_ = d.backend.state.read(func(f *appStateFile) { _, seen = f.Seen[d.id] })
	if !seen {
		t.Fatal("the app file keeps no seen mark")
	}
}

// A QUESTION WAITING ON THE PERSON IS OUTPUT: one put after the seen mark
// reads unread, read off the conversation's presence.
func TestAQuestionWaitingOnThePersonIsUnread(t *testing.T) {
	d := newUnreadDisk(t)
	if err := d.backend.MarkSeen(d.id); err != nil {
		t.Fatal(err)
	}
	d.asking(time.Now().Add(time.Second))
	got := d.listed()
	if got.State != appwire.StateNeedsYou || !got.Unread {
		t.Fatalf("a question after the seen mark = state %s unread %v", got.State, got.Unread)
	}
}

// NOBODY GETS A WALL OF DOTS ON UPGRADE: an agent that answered before this
// build kept unread, with no seen mark, reads read, and the moment unread began
// is kept so a restart agrees.
func TestUnreadStartsReadOnUpgrade(t *testing.T) {
	d := newUnreadDisk(t)
	d.replied(time.Now().Add(-time.Minute))
	if d.listed().Unread {
		t.Fatal("an agent that answered before the upgrade reads unread")
	}
	var from time.Time
	_ = d.backend.state.read(func(f *appStateFile) { from = f.UnreadFrom })
	if from.IsZero() {
		t.Fatal("the app file does not say when unread began")
	}
	d.start()
	if d.listed().Unread {
		t.Fatal("after a restart the old answer reads unread")
	}
	// AND OUTPUT AFTER THE UPGRADE STILL COUNTS for an agent never marked.
	d.replied(from.Add(time.Second))
	if !d.listed().Unread {
		t.Fatal("an answer after unread began does not read unread")
	}
}

// AGENTS.MARK_SEEN ONLY WRITES THE MARK: an agent in Closed or the archive
// stays there, is not opened or warmed, and says so when its unread clears.
func TestMarkSeenLeavesAClosedOrArchivedAgentWhereItIsAndCold(t *testing.T) {
	for name, mark := range map[string]func(*appStateFile, string, time.Time){
		"closed":   func(f *appStateFile, id string, at time.Time) { f.Closed = map[string]time.Time{id: at} },
		"archived": func(f *appStateFile, id string, at time.Time) { f.Archived = map[string]time.Time{id: at} },
	} {
		t.Run(name, func(t *testing.T) {
			d := newUnreadDisk(t)
			if err := d.backend.MarkSeen(d.id); err != nil {
				t.Fatal(err)
			}
			if err := d.backend.state.write(func(f *appStateFile) error { mark(f, d.id, time.Now()); return nil }); err != nil {
				t.Fatal(err)
			}
			if name == "archived" {
				// The archive mark itself is the conversation's (apptidy.go).
				meta, _ := session.LoadMeta(d.dir)
				meta.Archived = true
				if err := session.SaveMeta(d.dir, meta); err != nil {
					t.Fatal(err)
				}
			}
			d.replied(time.Now().Add(time.Second))
			before := d.listed()
			if !before.Unread || (before.State != appwire.StateClosed && before.State != appwire.StateArchived) {
				t.Fatalf("before = state %s unread %v", before.State, before.Unread)
			}
			if err := d.backend.MarkSeen(d.id); err != nil {
				t.Fatal(err)
			}
			d.rec.waitFor(t, 2*time.Second, func(_ string, data any) bool {
				c, ok := data.(appwire.AgentChanged)
				return ok && c.Agent.ID == d.id && !c.Agent.Unread && c.Agent.State == before.State
			})
			after := d.listed()
			if after.Unread || after.State != before.State || !d.backend.tidied(d.id) {
				t.Fatalf("after = state %s unread %v marked %v", after.State, after.Unread, d.backend.tidied(d.id))
			}
			d.mu.Lock()
			opens := d.opens
			d.mu.Unlock()
			if opens != 0 || d.backend.held(d.id) {
				t.Fatalf("agents.mark_seen opened it: opens=%d held=%v", opens, d.backend.held(d.id))
			}
		})
	}
}

// An unknown agent has nothing to mark.
func TestMarkSeenOnAnUnknownAgentIsNotFound(t *testing.T) {
	d := newUnreadDisk(t)
	err := d.backend.MarkSeen("no-such-agent")
	wire, ok := err.(*appwire.Error)
	if !ok || wire.Code != appwire.CodeNotFound {
		t.Fatalf("marking an unknown agent answered %v", err)
	}
}

// liveUnread is an open agent whose conversation folder is real, so the
// session's answer stamp can be written where the backend reads it.
func liveUnread(t *testing.T, conv *fakeConversation) (*appBackend, *appRecorder, appwire.Agent, string) {
	t.Helper()
	backend, rec, agent := workBackend(t, conv)
	a, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := session.SaveMeta(dir, session.Meta{ID: agent.ID}); err != nil {
		t.Fatal(err)
	}
	backend.locked(func() { a.transcript = filepath.Join(dir, "transcript.jsonl") })
	return backend, rec, agent, dir
}

// appUnreadChanged matches the agent said with the given unread.
func appUnreadChanged(id string, unread bool) func(string, any) bool {
	return func(_ string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == id && c.Agent.Unread == unread
	}
}

// A NEW AGENT STARTS READ, A TURN OF TOOL STEPS LEAVES IT READ, AND A TURN
// THAT ENDS ON AN ANSWER SAYS UNREAD LIVE: the turn's end reads the stamp the
// session wrote when it sealed, and agent.changed carries it.
func TestATurnThatAnswersSaysUnreadLiveAndToolStepsDoNot(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "read", Hint: "read go.mod"},
		{Kind: session.EventToolEnd, CallID: "c1", Tool: "read", Hint: "12 lines"},
		{Kind: session.EventTurnDone},
	}}
	backend, rec, agent, dir := liveUnread(t, conv)
	if agent.Unread {
		t.Fatal("a new agent starts unread")
	}
	if err := backend.MarkSeen(agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "look around", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateIdle
	})
	listed, err := backend.ListAgentsIn("", appwire.ListAll, "")
	if err != nil || len(listed) != 1 || listed[0].Unread {
		t.Fatalf("after tool steps alone: %+v, %v", listed, err)
	}
	rec.mu.Lock()
	for _, data := range rec.data {
		if c, ok := data.(appwire.AgentChanged); ok && c.Agent.Unread {
			rec.mu.Unlock()
			t.Fatal("tool steps alone said the agent unread")
		}
	}
	mark := len(rec.events)
	rec.mu.Unlock()

	conv.mu.Lock()
	conv.script = []session.Event{{Kind: session.EventTextDelta, Text: "done"}, {Kind: session.EventTurnDone}}
	conv.mu.Unlock()
	meta, _ := session.LoadMeta(dir)
	meta.LastReplyAt = time.Now().Add(time.Second)
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "and now?", ""); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 5*time.Second, appUnreadChanged(agent.ID, true))
	if i < mark {
		t.Fatalf("unread was said before the answering turn (%d < %d)", i, mark)
	}

	if err := backend.MarkSeen(agent.ID); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 2*time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && !c.Agent.Unread
	})
	listed, _ = backend.ListAgentsIn("", appwire.ListAll, "")
	if len(listed) != 1 || listed[0].Unread {
		t.Fatalf("after agents.mark_seen: %+v", listed)
	}
}

// A QUESTION PUT LIVE SAYS UNREAD at once, without a turn ending.
func TestALiveQuestionSaysUnread(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent, _ := liveUnread(t, conv)
	if err := backend.MarkSeen(agent.ID); err != nil {
		t.Fatal(err)
	}
	// The clock moves past the mark, as it does between a look and a question.
	backend.now = func() time.Time { return time.Now().Add(time.Second) }
	approval := session.Question{ConsentScopes: []string{"bash"}, ID: 4, Kind: session.QuestionConsent, Head: "bash rm -rf build", Reason: "bash asks first", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &approval})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.waitFor(t, 5*time.Second, appUnreadChanged(agent.ID, true))
}

// AN AGENT THE IDLE RULE SHOWS CLOSED STAYS CLOSED when a mark clears its
// unread: the event says it the way agents.list does.
func TestMarkSeenLeavesAnIdleClosedAgentClosed(t *testing.T) {
	d := newUnreadDisk(t)
	long := time.Now().Add(-72 * time.Hour)
	meta, _ := session.LoadMeta(d.dir)
	meta.LastUserAt, meta.Created = long, long
	if err := session.SaveMeta(d.dir, meta); err != nil {
		t.Fatal(err)
	}
	if err := d.backend.state.write(func(f *appStateFile) error { days := 1; f.IdleCloseDays = &days; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := d.backend.MarkSeen(d.id); err != nil {
		t.Fatal(err)
	}
	d.replied(time.Now().Add(time.Second))
	if before := d.listed(); before.State != appwire.StateClosed || !before.Unread {
		t.Fatalf("before = state %s unread %v", before.State, before.Unread)
	}
	if err := d.backend.MarkSeen(d.id); err != nil {
		t.Fatal(err)
	}
	d.rec.waitFor(t, 2*time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == d.id && !c.Agent.Unread && c.Agent.State == appwire.StateClosed
	})
}
