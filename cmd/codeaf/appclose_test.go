package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type closeTimer struct {
	at      time.Time
	fn      func()
	stopped bool
}
type closeClock struct {
	mu     sync.Mutex
	at     time.Time
	timers []*closeTimer
}

func (c *closeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *closeClock) after(d time.Duration, fn func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &closeTimer{at: c.at.Add(d), fn: fn}
	c.timers = append(c.timers, timer)
	return func() { c.mu.Lock(); defer c.mu.Unlock(); timer.stopped = true }
}
func (c *closeClock) advance(t *testing.T, d time.Duration) {
	t.Helper()
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
	for n := 0; n < 100; n++ {
		c.mu.Lock()
		var fn func()
		for _, timer := range c.timers {
			if !timer.stopped && !timer.at.After(c.at) {
				timer.stopped = true
				fn = timer.fn
				break
			}
		}
		c.mu.Unlock()
		if fn == nil {
			return
		}
		fn()
	}
	t.Fatal("claim timer kept scheduling itself without advancing time")
}

type closeConversation struct {
	*fakeConversation
	muClose       sync.Mutex
	resumed, quit int
	beforeResume  func()
	continued     []string
	resumeErr     error
}

func (c *closeConversation) ResumeAfterCloseKey(key string) (bool, error) {
	if c.beforeResume != nil {
		c.beforeResume()
	}
	c.muClose.Lock()
	defer c.muClose.Unlock()
	c.resumed++
	return c.resumeErr == nil, c.resumeErr
}
func (c *closeConversation) ResumeAfterQuit() (bool, error) {
	c.muClose.Lock()
	defer c.muClose.Unlock()
	c.quit++
	return true, nil
}
func (c *closeConversation) AppTaskAction(i session.AppTaskIntent) error {
	c.muClose.Lock()
	defer c.muClose.Unlock()
	if i.Action == "continue" {
		c.continued = append(c.continued, i.ID)
	}
	return nil
}
func (c *closeConversation) counts() (int, int) {
	c.muClose.Lock()
	defer c.muClose.Unlock()
	return c.resumed, c.quit
}

type closeRig struct {
	b        *appBackend
	rec      *appRecorder
	clock    *closeClock
	conv     *closeConversation
	file, id string
	p        session.SessionPresence
}

func newCloseRig(t *testing.T, state session.PresenceState, age time.Duration) *closeRig {
	t.Helper()
	appDayProfile(t)
	workspace := t.TempDir()
	file, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	id := appAgentID(file)
	at := time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC)
	if err := session.SaveMeta(filepath.Dir(file), session.Meta{ID: id, Workspace: workspace, Created: at.Add(-time.Hour), LastUserAt: at.Add(-30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	raw := `{"type":"session","version":1,"id":"` + id + `"}` + "\n" + `{"type":"message","role":"user","content":"finish the work","timestamp":"2026-10-19T16:00:00Z"}` + "\n"
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	rig := &closeRig{clock: &closeClock{at: at}, conv: &closeConversation{fakeConversation: &fakeConversation{}}, file: file, id: id}
	rig.p = session.SessionPresence{Schema: 1, SessionID: id, Workspace: workspace, PID: 987654, UpdatedAt: at.Add(-age), State: state}
	rig.write(t, rig.p)
	rig.backend(t)
	return rig
}
func (r *closeRig) backend(t *testing.T) {
	t.Helper()
	r.rec = newAppRecorder()
	r.b = newAppBackend(r.rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: r.conv, Transcript: r.file, Workspace: r.p.Workspace, Assembled: true, Release: func() {}}, nil
	})
	r.b.now, r.b.after = r.clock.now, r.clock.after
	t.Cleanup(func() { _ = r.b.Close() })
}
func (r *closeRig) write(t *testing.T, p session.SessionPresence) {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(r.file)
	f, err := os.CreateTemp(dir, ".presence-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, "presence.json")); err != nil {
		t.Fatal(err)
	}
}
func (r *closeRig) start() { r.b.snapshotCloseClaims(); r.b.startCloseWatch() }
func (r *closeRig) entries(t *testing.T) []appwire.Entry {
	t.Helper()
	entries, _, err := r.b.Entries(r.id, "", 500)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
func closeFooters(entries []appwire.Entry) []appwire.Entry {
	var found []appwire.Entry
	for _, e := range entries {
		if e.Footer == appwire.FooterResumedAfterClose || e.Footer == appwire.FooterStoppedByClose || e.Footer == appwire.FooterResumedAfterQuit || e.Footer == appwire.FooterInterrupted {
			found = append(found, e)
		}
	}
	return found
}
func (r *closeRig) wantFooter(t *testing.T, kind, words string) {
	t.Helper()
	found := closeFooters(r.entries(t))
	if len(found) != 1 || found[0].Footer != kind || found[0].Text != words {
		t.Fatalf("footers=%+v", found)
	}
}

// wantInterrupted is the whole of what a death leaves: one Interrupted line,
// the mark that offers Continue, nothing resumed and nothing opened.
func (r *closeRig) wantInterrupted(t *testing.T) {
	t.Helper()
	r.wantFooter(t, appwire.FooterInterrupted, appInterruptedWord)
	if n, q := r.conv.counts(); n != 0 || q != 0 {
		t.Fatalf("carried on by itself: close=%d quit=%d", n, q)
	}
	if r.b.held(r.id) {
		t.Fatal("the interrupted chat was opened by itself")
	}
	if !r.b.interrupted(r.id) {
		t.Fatal("the chat is not marked for Continue")
	}
	rows, err := r.b.ListAgents("")
	if err != nil || len(rows) == 0 || !rows[0].Interrupted {
		t.Fatalf("list=%+v,%v", rows, err)
	}
}
func (r *closeRig) wantIdle(t *testing.T) {
	t.Helper()
	a, err := r.b.find(r.id)
	if err != nil {
		t.Fatal(err)
	}
	var state string
	r.b.locked(func() { state = string(a.view.State) })
	if state != string(appwire.StateIdle) {
		t.Fatalf("find state=%s", state)
	}
	rows, err := r.b.ListAgents("")
	if err != nil || len(rows) != 1 || rows[0].State != appwire.StateIdle {
		t.Fatalf("list=%+v,%v", rows, err)
	}
}

// C1: a dead working or waiting claim expires on the wire, including cached find.
func TestAppCloseC1DeadClaimExpiresOnTheWire(t *testing.T) {
	for _, state := range []session.PresenceState{session.PresenceWorking, session.PresenceWaiting} {
		t.Run(string(state), func(t *testing.T) {
			r := newCloseRig(t, state, 2*time.Second)
			r.start()
			rows, err := r.b.ListAgents("")
			if err != nil || len(rows) != 1 || rows[0].State == appwire.StateIdle {
				t.Fatalf("initial=%+v,%v", rows, err)
			}
			r.clock.advance(t, session.PresenceWindow-2*time.Second+appClosePresenceMargin)
			r.wantIdle(t)
			r.rec.waitFor(t, time.Second, func(_ string, data any) bool {
				e, ok := data.(appwire.AgentChanged)
				return ok && e.Agent.ID == r.id && e.Agent.State == appwire.StateIdle
			})
			r.rec.mu.Lock()
			defer r.rec.mu.Unlock()
			n := 0
			for _, d := range r.rec.data {
				// The correction is said once; the Interrupted mark after it is
				// its own change.
				if e, ok := d.(appwire.AgentChanged); ok && e.Agent.ID == r.id && e.Agent.State == appwire.StateIdle && !e.Agent.Interrupted {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("corrected state events=%d", n)
			}
		})
	}
}

// C2: a holder that dies while this app is open leaves the chat interrupted.
func TestAppCloseC2HolderDiesMidLife(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, 0)
	r.start()
	r.p.State = session.PresenceWorking
	r.p.UpdatedAt = r.clock.now()
	r.write(t, r.p)
	_, _ = r.b.ListAgents("")
	r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
	r.wantInterrupted(t)
}

// C3: both kinds of left-behind mid-answer claim wait for Continue; neither
// carries on by itself.
func TestAppCloseC3WorkingAndWaitingWaitForContinue(t *testing.T) {
	for _, state := range []session.PresenceState{session.PresenceWorking, session.PresenceWaiting} {
		t.Run(string(state), func(t *testing.T) {
			r := newCloseRig(t, state, time.Minute)
			r.start()
			r.clock.advance(t, 0)
			r.wantInterrupted(t)
		})
	}
}

// C3: with a real session behind the door, the start spends nothing — no
// request reaches the model — and the person's Continue carries the turn on
// exactly once, with the unexpected-close aside the model alone reads.
func TestAppCloseC3RealSessionContinuesOnceOnThePress(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	model := newHeldModel(t)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(model.release) }) })
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.server.URL), Key: "test-key", Address: model.server.URL})
	var held []bool
	r.b.open = func(ask appOpenAsk) (*appConn, error) {
		held = append(held, ask.HoldInterruptedWork)
		agent, err := session.New(session.Config{Workspace: r.p.Workspace, Place: session.Place{Dir: filepath.Dir(r.file)}, SessionFile: r.file, Model: "openai/gpt-4.1-mini", Sources: sources, HoldInterruptedWork: ask.HoldInterruptedWork})
		if err != nil {
			return nil, err
		}
		loop, err := remote.Loopback(remote.Hello{}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: agent, SessionFile: r.file, Workspace: r.p.Workspace}, nil
		}})
		if err != nil {
			_ = agent.Close()
			return nil, err
		}
		return appConnOf(loop.Client, func() { _ = loop.Close(); _ = agent.Close() }, true), nil
	}
	r.start()
	r.clock.advance(t, 0)
	r.wantFooter(t, appwire.FooterInterrupted, appInterruptedWord)
	if len(held) != 0 || r.b.held(r.id) {
		t.Fatalf("the start opened the interrupted chat: %v", held)
	}
	// Looking at the chat opens it held, and still nothing is asked of the model.
	if _, err := r.b.OpenAgent(r.id); err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || !held[0] {
		t.Fatalf("the open was not held: %v", held)
	}
	model.mu.Lock()
	asked := len(model.requests)
	model.mu.Unlock()
	if asked != 0 {
		t.Fatalf("an interrupted chat spent before Continue: %d requests", asked)
	}
	view, err := r.b.ContinueInterrupted(r.id)
	if err != nil || view.Interrupted {
		t.Fatalf("continue=%+v,%v", view, err)
	}
	select {
	case <-model.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the continued turn never reached the model")
	}
	r.rec.waitFor(t, time.Second, func(name string, _ any) bool { return name == appwire.EventTurnStarted })
	model.mu.Lock()
	said := false
	for _, request := range model.requests {
		if strings.Contains(strings.Join(request.texts(), "\n"), session.ResumeAfterCloseNote) {
			said = true
		}
	}
	model.mu.Unlock()
	if !said {
		t.Fatal("model did not receive the session's unexpected-close aside")
	}
	for _, e := range r.entries(t) {
		if strings.Contains(e.Text, session.ResumeAfterCloseNote) {
			t.Fatal("private aside became a conversation entry")
		}
	}
	release.Do(func() { close(model.release) })
	r.rec.waitFor(t, 10*time.Second, appTurnDone)
	// A second press is not a second turn.
	if _, err := r.b.ContinueInterrupted(r.id); err != nil {
		t.Fatal(err)
	}
	r.clock.advance(t, time.Minute)
	model.mu.Lock()
	turns := len(model.requests)
	model.mu.Unlock()
	if r.b.interrupted(r.id) || turns != 1 {
		t.Fatalf("continued more than once: requests=%d", turns)
	}
}

// C4: persisted running tasks are not continued by the start, at any age;
// the person's Continue carries every one of them on.
func TestAppCloseC4TasksWaitForContinue(t *testing.T) {
	for _, mode := range []string{"recent", "old"} {
		t.Run(mode, func(t *testing.T) {
			age := time.Minute
			if mode == "old" {
				age = time.Hour + time.Second
			}
			r := newCloseRig(t, session.PresenceWorking, age)
			tasks := `{"type":"tasks","version":1,"seq":2,"nodes":[{"id":1,"title":"finish tests","brief":"b","acceptance":"a","state":"running"}]}`
			if err := os.WriteFile(filepath.Join(filepath.Dir(r.file), "tasks.json"), []byte(tasks), 0600); err != nil {
				t.Fatal(err)
			}
			// A second task has only its project index row left, as older work can.
			raw, err := json.Marshal(session.TaskIndexEntry{ID: "2", SessionID: r.id, Title: "finish indexed work", Status: "running"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(session.TaskIndexPath(r.file), append(raw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			r.start()
			r.clock.advance(t, 0)
			r.conv.muClose.Lock()
			n := len(r.conv.continued)
			r.conv.muClose.Unlock()
			if n != 0 {
				t.Fatalf("the start continued %d tasks", n)
			}
			r.wantInterrupted(t)
			if _, err := r.b.ContinueInterrupted(r.id); err != nil {
				t.Fatal(err)
			}
			r.conv.muClose.Lock()
			n = len(r.conv.continued)
			r.conv.muClose.Unlock()
			if resumed, _ := r.conv.counts(); n != 2 || resumed != 1 {
				t.Fatalf("Continue carried on tasks=%d replies=%d", n, resumed)
			}
		})
	}
}

// C5: an idle claim is never a reason to start work or add a footer.
func TestAppCloseC5IdleIsLeftAlone(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, time.Minute)
	r.start()
	r.clock.advance(t, time.Hour)
	r.wantIdle(t)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("idle resumed")
	}
	if len(closeFooters(r.entries(t))) != 0 {
		t.Fatal("idle received a footer")
	}
}

// C6: restarts after a death never carry it on, however many there are; the
// chat keeps one Interrupted line until the person presses Continue.
func TestAppCloseC6RestartsNeverCarryOn(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	for attempt := 1; attempt <= 3; attempt++ {
		r.start()
		r.clock.advance(t, 0)
		r.wantInterrupted(t)
		_ = r.b.Close()
		r.clock.advance(t, 11*time.Minute)
		r.p.UpdatedAt = r.clock.now().Add(-time.Minute)
		r.write(t, r.p)
		r.backend(t)
	}
}

// C6: every person-facing direction door clears the guard's count.
func TestAppCloseC6PersonDirectionsResetCount(t *testing.T) {
	for _, door := range []string{"message", "marked message", "attachment send", "composed send", "steer", "queue", "marked queue", "answer", "rich answer"} {
		t.Run(door, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceIdle, 0)
			hold := make(chan struct{})
			r.conv.hold = hold
			t.Cleanup(func() { close(hold) })
			if door == "steer" || door == "queue" || door == "marked queue" || door == "answer" || door == "rich answer" {
				_, live, err := r.b.heldLive(r.id)
				if err != nil {
					t.Fatal(err)
				}
				if door == "steer" || door == "queue" || door == "marked queue" {
					r.b.locked(func() { live.running = true })
				}
				if door == "steer" {
					lane := make(chan session.Event)
					r.conv.steer = lane
					t.Cleanup(func() { close(lane) })
				}
				if door == "answer" || door == "rich answer" {
					r.b.locked(func() { live.questions["ask:7"] = session.Question{ID: 7, Kind: session.QuestionAsk} })
				}
			}
			if err := r.b.state.write(func(f *appStateFile) error {
				f.CloseResumes = map[string]int{r.id: 2}
				f.CloseResumedAt = map[string]time.Time{r.id: r.clock.now()}
				f.ClosePending = map[string]string{r.id: "owed"}
				f.ClosePendingAt = map[string]time.Time{r.id: r.clock.now()}
				f.markInterrupted(r.id, appInterrupted{At: r.clock.now(), Key: "interrupted", Road: appInterruptedQuit, Reply: true})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch door {
			case "message":
				_, err = r.b.Send(r.id, "carry on", "")
			case "marked message":
				_, err = r.b.SendMessage(r.id, appwire.AgentMessage{Text: "carry on"})
			case "attachment send":
				_, err = r.b.SendWith(r.id, "carry on", "", nil)
			case "composed send":
				_, err = r.b.SendComposed(r.id, appwire.AgentMessage{Text: "carry on"})
			case "steer":
				err = r.b.Steer(r.id, "try this", "")
			case "queue":
				_, err = r.b.Queue(r.id, "and this", "")
			case "marked queue":
				_, err = r.b.QueueMessage(r.id, appwire.AgentMessage{Text: "and this"})
			case "answer":
				err = r.b.AnswerAsk(r.id, "ask:7", "", "yes")
			case "rich answer":
				err = r.b.AnswerAskWith(r.id, "ask:7", appwire.AskAnswer{Text: "yes"})
			}
			if err != nil {
				t.Fatal(err)
			}
			disk := &appState{path: r.b.state.where()}
			_ = disk.read(func(f *appStateFile) {
				if _, marked := f.Interrupted[r.id]; marked || f.CloseResumes[r.id] != 0 || f.ClosePending[r.id] != "" || !f.CloseResumedAt[r.id].IsZero() || !f.ClosePendingAt[r.id].IsZero() {
					t.Errorf("%s kept automatic work=%+v", door, f)
				}
			})
			// A fake running flag has no turn to stop; don't make Close wait for one.
			if live := r.b.liveOf(r.b.agent(r.id)); live != nil {
				r.b.locked(func() {
					if live.pumpDone == nil {
						live.running = false
					}
				})
			}
		})
	}
}

// C6: a person's turn from another window resets the count immediately;
// the private wake used by automatic continuation keeps it until completion.
func TestAppCloseC6OtherWindowMessageResetsButPrivateWakeDoesNot(t *testing.T) {
	for _, said := range []string{"another message", ""} {
		t.Run(map[bool]string{true: "person", false: "private wake"}[said != ""], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceIdle, 0)
			a, live, err := r.b.heldLive(r.id)
			if err != nil {
				t.Fatal(err)
			}
			_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{r.id: 2}; return nil })
			events := make(chan session.Event)
			finished := make(chan struct{})
			go func() { defer close(finished); r.b.wokenTurn(a, live, appTurnElsewhere{said: said, events: events}) }()
			r.rec.waitFor(t, time.Second, func(name string, _ any) bool { return name == appwire.EventTurnStarted })
			want := 2
			if said != "" {
				want = 0
			}
			disk := &appState{path: r.b.state.where()}
			_ = disk.read(func(f *appStateFile) {
				if f.CloseResumes[r.id] != want {
					t.Errorf("count=%d, want %d", f.CloseResumes[r.id], want)
				}
			})
			close(events)
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("turn did not finish")
			}
		})
	}
}

// C7: every chat a death left working is marked, each on its own.
func TestAppCloseC7EveryAffectedChatIsMarked(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	other, err := appMint(r.p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	otherID := appAgentID(other)
	if err = os.WriteFile(other, []byte(`{"type":"session","version":1,"id":"`+otherID+`"}`+"\n"+`{"type":"message","role":"user","content":"other"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := r.p
	p.SessionID = otherID
	raw, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(filepath.Dir(other), "presence.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
	entries, _, err := r.b.Entries(otherID, "", 100)
	if err != nil || len(closeFooters(entries)) != 1 || closeFooters(entries)[0].Footer != appwire.FooterInterrupted || !r.b.interrupted(otherID) {
		t.Fatalf("other entries=%+v,%v", entries, err)
	}
}

// C8: freshness alone decides; a fake foreign pid keeps refreshing, changes
// owners, or removes its file when closing gracefully. None is resumed.
func TestAppCloseC8FreshForeignWindowIsNeverResumed(t *testing.T) {
	for _, ending := range []string{"refresh", "new holder", "gone", "idle"} {
		t.Run(ending, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, 2*time.Second)
			r.start()
			for n := 0; n < 4; n++ {
				r.clock.advance(t, 5*time.Second)
				r.p.UpdatedAt = r.clock.now()
				r.write(t, r.p)
			}
			switch ending {
			case "new holder":
				r.p.PID++
				r.write(t, r.p)
			case "gone":
				if err := os.Remove(filepath.Join(filepath.Dir(r.file), "presence.json")); err != nil {
					t.Fatal(err)
				}
			case "idle":
				r.p.State = session.PresenceIdle
				r.p.UpdatedAt = r.clock.now().Add(time.Millisecond)
				r.write(t, r.p)
			}
			if ending == "refresh" {
				r.clock.advance(t, 5*time.Second)
			} else {
				r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
			}
			if n, _ := r.conv.counts(); n != 0 {
				t.Fatalf("live window resumed %d times", n)
			}
			if len(closeFooters(r.entries(t))) != 0 {
				t.Fatal("live window got a close footer")
			}
		})
	}
}

// C9: a claim older than one hour reads the same as a recent one.
func TestAppCloseC9OldDeathIsInterruptedToo(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Hour+time.Nanosecond)
	r.start()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
}

// C10: the same updatedAt encountered by a later process has no second effect.
func TestAppCloseC10SameClaimHandledOnce(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	r.start()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
	_ = r.b.Close()
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
}

// C11, C12: a half-finished quit owns its claim: one Interrupted line across
// restarts, nothing carried on until Continue, and Continue carries it on once.
func TestAppCloseC11QuitNeverDoublesAndWaitsForContinue(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.ResumeAfterQuit = []string{r.id}
		f.QuitReplies = []string{r.id}
		return nil
	})
	r.b.snapshotCloseClaims()
	r.b.interruptAfterQuit()
	r.rec.waitFor(t, time.Second, func(_ string, d any) bool {
		e, ok := d.(appwire.EntryAdded)
		return ok && e.Entry.Footer == appwire.FooterInterrupted
	})
	r.b.startCloseWatch()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
	_ = r.b.Close()
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
	if _, err := r.b.ContinueInterrupted(r.id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.b.ContinueInterrupted(r.id); err != nil {
		t.Fatal(err)
	}
	if n, q := r.conv.counts(); n != 0 || q != 1 {
		t.Fatalf("after Continue close=%d quit=%d", n, q)
	}
	_ = r.b.Close()
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	if n, q := r.conv.counts(); n != 0 || q != 1 || r.b.interrupted(r.id) {
		t.Fatalf("a later start carried on again: close=%d quit=%d", n, q)
	}
}

// C12: fresh backends keep both new footers in place, including equal timestamps.
func TestAppCloseC12FootersSurviveAFreshBackendInPlace(t *testing.T) {
	for _, kind := range []string{appwire.FooterResumedAfterClose, appwire.FooterStoppedByClose, appwire.FooterResumedAfterQuit, appwire.FooterInterrupted} {
		t.Run(kind, func(t *testing.T) {
			r := newCloseRig(t, session.PresenceIdle, 0)
			a, err := r.b.find(r.id)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.b.keepAppFooter(a, "receipt", kind, "kept words"); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(r.file, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = file.WriteString(`{"type":"message","role":"assistant","content":"next reply"}` + "\n")
			_ = file.Close()
			if err != nil {
				t.Fatal(err)
			}
			_ = r.b.Close()
			r.backend(t)
			entries := r.entries(t)
			if len(entries) != 3 || entries[0].Kind != appwire.EntryUser || entries[1].Footer != kind || entries[2].Text != "next reply" {
				t.Fatalf("entries=%+v", entries)
			}
			older, more, err := r.b.Entries(r.id, "receipt", 1)
			if err != nil || more || len(older) != 1 || older[0].Kind != appwire.EntryUser {
				t.Fatalf("page before footer=%+v,%v,%v", older, more, err)
			}
		})
	}
}

// C13: prune timestamps and deleted conversations, while failed state writes
// cannot start a resumed turn or spend the handled mark in memory.
func TestAppCloseC13PruningAndWriteFailure(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.CloseStarts = []time.Time{r.clock.now().Add(-11 * time.Minute)}
		f.CloseResumes = map[string]int{"deleted": 2}
		f.CloseHandled = map[string]time.Time{"deleted": r.clock.now()}
		f.CloseResumedAt = map[string]time.Time{"deleted": r.clock.now()}
		f.ClosePending = map[string]string{"deleted": "owed"}
		f.ClosePendingAt = map[string]time.Time{"deleted": r.clock.now()}
		return nil
	})
	r.b.snapshotCloseClaims()
	_ = r.b.state.read(func(f *appStateFile) {
		if len(f.CloseStarts) != 0 || len(f.CloseResumes) != 0 || len(f.CloseHandled) != 0 || len(f.CloseResumedAt) != 0 || len(f.ClosePending) != 0 || len(f.ClosePendingAt) != 0 {
			t.Errorf("unpruned history=%+v", f)
		}
	})
	// An ordinary file in place of a parent directory deterministically refuses writes.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.b.state.mu.Lock()
	r.b.state.path = filepath.Join(blocker, "state.json")
	r.b.state.mu.Unlock()
	r.b.startCloseWatch()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("resumed without writing the mark")
	}
	_ = r.b.state.read(func(f *appStateFile) {
		if !f.CloseHandled[r.id].IsZero() {
			t.Error("failed write spent handled claim")
		}
	})
}

// C14: a start opens nothing at all for a dead claim, so a day limit it
// cannot read is never in question; the chat is marked all the same.
func TestAppCloseC14StartOpensNothing(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	path := config.BudgetConfigPath(config.ProfileDir())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"daily_budget_usd":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	r.b.open = func(appOpenAsk) (*appConn, error) {
		t.Error("the start opened an interrupted chat")
		return nil, errors.New("blocked")
	}
	r.start()
	r.clock.advance(t, 0)
	r.wantInterrupted(t)
}

// C8: the synchronous snapshot survives this process taking the journal and
// replacing the old claim. A different new owner is never driven instead.
func TestAppCloseC8SnapshotBeforeThisProcessHoldsTheJournal(t *testing.T) {
	for _, ours := range []bool{true, false} {
		t.Run(map[bool]string{true: "this process", false: "other owner"}[ours], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, 2*time.Second)
			r.b.snapshotCloseClaims()
			next := r.p
			next.UpdatedAt = r.clock.now()
			if ours {
				next.PID = os.Getpid()
				if _, _, err := r.b.heldLive(r.id); err != nil {
					t.Fatal(err)
				}
			} else {
				next.PID++
			}
			r.write(t, next)
			r.b.startCloseWatch()
			r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
			n, _ := r.conv.counts()
			if ours && (n != 0 || !r.b.interrupted(r.id)) {
				t.Fatal("own opener erased the evidence")
			}
			if !ours && (n != 0 || len(closeFooters(r.entries(t))) != 0) {
				t.Fatal("drove the new holder")
			}
		})
	}
}

// C12: another process, with no memory or cached state from the writer, reads
// all three notices and their exact placement through agents.entries.
func TestAppCloseC12EntriesInAnotherProcess(t *testing.T) {
	const childHome = "CODEAF_TEST_CLOSE_HOME"
	if root := os.Getenv(childHome); root != "" {
		t.Setenv("CODEAF_HOME", root)
		t.Setenv(config.ProfileDirEnv, os.Getenv("CODEAF_TEST_CLOSE_PROFILE"))
		b := newAppBackend(newAppRecorder(), nil)
		defer b.Close()
		entries, _, err := b.Entries(os.Getenv("CODEAF_TEST_CLOSE_ID"), "", 100)
		if err != nil || len(entries) != 5 {
			t.Fatalf("fresh process entries=%+v,%v", entries, err)
		}
		for n, kind := range []string{appwire.FooterResumedAfterClose, appwire.FooterStoppedByClose, appwire.FooterResumedAfterQuit} {
			if entries[n+1].Footer != kind {
				t.Fatalf("footer %d=%+v", n, entries[n+1])
			}
		}
		if entries[0].Kind != appwire.EntryUser || entries[4].Text != "the next reply" {
			t.Fatalf("placement=%+v", entries)
		}
		return
	}
	r := newCloseRig(t, session.PresenceIdle, 0)
	a, err := r.b.find(r.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{appwire.FooterResumedAfterClose, appwire.FooterStoppedByClose, appwire.FooterResumedAfterQuit} {
		if err := r.b.keepAppFooter(a, kind, kind, kind); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(r.file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"message","role":"assistant","content":"the next reply"}` + "\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	_ = r.b.Close()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAppCloseC12EntriesInAnotherProcess$", "-test.v")
	child.Env = append(os.Environ(), "CODEAF_HOME="+filepath.Join(filepath.Dir(r.file), "unused-child-start"), childHome+"="+os.Getenv("CODEAF_HOME"), "CODEAF_TEST_CLOSE_ID="+r.id, "CODEAF_TEST_CLOSE_PROFILE="+config.ProfileDir())
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("fresh process: %v\n%s", err, out)
	}
}
