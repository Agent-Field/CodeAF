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
	if err := session.SaveMeta(filepath.Dir(file), session.Meta{ID: id, Workspace: workspace, Created: at.Add(-time.Hour), LastUserAt: at.Add(-time.Minute)}); err != nil {
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
		if e.Footer == appwire.FooterResumedAfterClose || e.Footer == appwire.FooterStoppedByClose || e.Footer == appwire.FooterResumedAfterQuit {
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
			// Reporting an old interruption avoids starting a new reply in this test.
			_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{r.id: 2}; return nil })
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
				if e, ok := d.(appwire.AgentChanged); ok && e.Agent.ID == r.id && e.Agent.State == appwire.StateIdle {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("corrected state events=%d", n)
			}
		})
	}
}

// C1: another conversation's slow opener cannot hold a dead claim's state
// correction past its deadline. The two due callbacks model independent timers.
func TestAppCloseC1SlowOpenDoesNotDelayAnotherCorrection(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	other, err := appMint(r.p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	id := appAgentID(other)
	if err = os.WriteFile(other, []byte(`{"type":"session","version":1,"id":"`+id+`"}`+"\n"+`{"type":"message","role":"user","content":"second reply"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := r.p
	p.SessionID = id
	raw, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(filepath.Dir(other), "presence.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{id: 2}; return nil })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	r.b.open = func(ask appOpenAsk) (*appConn, error) {
		close(entered)
		<-release
		return &appConn{Conv: r.conv, Transcript: ask.Transcript, Workspace: ask.Workspace, Assembled: true, Release: func() {}}, nil
	}
	r.b.snapshotCloseClaims()
	firstPath, secondPath := filepath.Dir(r.file), filepath.Dir(other)
	var first, second *appCloseClaim
	r.b.closeRecovery.locked(func() { first = r.b.closeRecovery.claims[firstPath]; second = r.b.closeRecovery.claims[secondPath] })
	finished1, finished2 := make(chan struct{}), make(chan struct{})
	go func() { defer close(finished1); r.b.checkCloseClaim(firstPath, first) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first conversation never reached the blocked opener")
	}
	go func() { defer close(finished2); r.b.checkCloseClaim(secondPath, second) }()
	r.rec.waitFor(t, time.Second, func(_ string, d any) bool {
		e, ok := d.(appwire.AgentChanged)
		return ok && e.Agent.ID == id && e.Agent.State == appwire.StateIdle
	})
	once.Do(func() { close(release) })
	for _, finished := range []chan struct{}{finished1, finished2} {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("due callback did not finish")
		}
	}
}

// C2: first seen mid-life, then killed while this engine stays up.
func TestAppCloseC2HolderDiesMidLife(t *testing.T) {
	r := newCloseRig(t, session.PresenceIdle, 0)
	r.start()
	r.p.State = session.PresenceWorking
	r.p.UpdatedAt = r.clock.now()
	r.write(t, r.p)
	_, _ = r.b.ListAgents("")
	r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
	if n, _ := r.conv.counts(); n != 1 {
		t.Fatalf("resumes=%d", n)
	}
	r.wantFooter(t, appwire.FooterResumedAfterClose, "Resumed after the app closed")
}

// C3: both kinds of left-behind mid-answer claim carry on automatically once.
func TestAppCloseC3WorkingAndWaitingCarryOn(t *testing.T) {
	for _, state := range []session.PresenceState{session.PresenceWorking, session.PresenceWaiting} {
		t.Run(string(state), func(t *testing.T) {
			r := newCloseRig(t, state, time.Minute)
			r.start()
			r.clock.advance(t, 0)
			if n, _ := r.conv.counts(); n != 1 {
				t.Fatalf("%s resumed %d times", state, n)
			}
			r.wantFooter(t, appwire.FooterResumedAfterClose, "Resumed after the app closed")
		})
	}
}

// C3: the real session and remote door start their own turn, with a private aside.
func TestAppCloseC3RealSessionCarriesOnOnce(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	model := newHeldModel(t)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(model.release) }) })
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.server.URL), Key: "test-key", Address: model.server.URL})
	r.b.open = func(ask appOpenAsk) (*appConn, error) {
		agent, err := session.New(session.Config{Workspace: r.p.Workspace, Place: session.Place{Dir: filepath.Dir(r.file)}, SessionFile: r.file, Model: "openai/gpt-4.1-mini", Sources: sources})
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
	select {
	case <-model.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("resumed turn never reached the model")
	}
	r.rec.waitFor(t, time.Second, func(name string, _ any) bool { return name == appwire.EventTurnStarted })
	r.wantFooter(t, appwire.FooterResumedAfterClose, "Resumed after the app closed")
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
	r.clock.advance(t, time.Minute)
	if len(closeFooters(r.entries(t))) != 1 {
		t.Fatal("the same death was resumed twice")
	}
	// C6: a normally completed real turn clears the persisted consecutive count.
	_ = r.b.state.read(func(f *appStateFile) {
		if f.CloseResumes[r.id] != 0 || f.ClosePending[r.id] != "" || !f.ClosePendingAt[r.id].IsZero() {
			t.Errorf("own continuation did not acknowledge its pending key or reset its count: %+v", f)
		}
	})
}

// C4: persisted running tasks continue only with a carried-on conversation.
func TestAppCloseC4TasksContinueOrReadInterrupted(t *testing.T) {
	for _, mode := range []string{"continue", "guard", "breaker", "old"} {
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
			if mode == "guard" {
				_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{r.id: 2}; return nil })
			}
			if mode == "breaker" {
				_ = r.b.state.write(func(f *appStateFile) error {
					f.CloseStarts = []time.Time{r.clock.now().Add(-2 * time.Minute), r.clock.now().Add(-time.Minute)}
					return nil
				})
			}
			r.start()
			r.clock.advance(t, 0)
			r.conv.muClose.Lock()
			n := len(r.conv.continued)
			r.conv.muClose.Unlock()
			if mode == "continue" {
				if n != 2 {
					t.Fatalf("continued tasks=%d", n)
				}
				return
			}
			if n != 0 {
				t.Fatal("blocked reply continued its tasks")
			}
			rows, err := r.b.ListTasks(r.id)
			if err != nil || len(rows) != 2 {
				t.Fatalf("tasks=%+v,%v", rows, err)
			}
			for _, row := range rows {
				if row.State != appwire.TaskFailed || row.Result != "Interrupted" {
					t.Fatalf("task=%+v", row)
				}
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

// C6, C13: two attempts survive fresh backends; a third death stops the loop.
func TestAppCloseC6TwoAttemptsAcrossRestarts(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	for attempt := 1; attempt <= 3; attempt++ {
		r.conv.beforeResume = func() {
			disk := &appState{path: r.b.state.where()}
			if err := disk.read(func(f *appStateFile) {
				if f.CloseResumes[r.id] != attempt || !f.CloseHandled[r.id].Equal(r.p.UpdatedAt) || len(f.CloseStarts) != 1 {
					t.Errorf("wake started before its durable record: %+v", f)
				}
			}); err != nil {
				t.Error(err)
			}
		}
		r.start()
		r.clock.advance(t, 0)
		if n, _ := r.conv.counts(); n != min(attempt, 2) {
			t.Fatalf("attempt %d: resumes=%d", attempt, n)
		}
		disk := &appState{path: r.b.state.where()}
		if err := disk.read(func(f *appStateFile) {
			if f.CloseResumes[r.id] != min(attempt, 2) || !f.CloseHandled[r.id].Equal(r.p.UpdatedAt) {
				t.Errorf("not durable before wake: %+v", f)
			}
		}); err != nil {
			t.Fatal(err)
		}
		if attempt < 3 {
			closeConsumeTurn(t, r, closeDisk(t, r).ClosePending[r.id], false)
			_ = r.b.Close()
			r.clock.advance(t, 11*time.Minute)
			r.p.UpdatedAt = r.clock.now().Add(-time.Minute)
			r.write(t, r.p)
			r.backend(t)
		}
	}
	footers := closeFooters(r.entries(t))
	if len(footers) != 3 || footers[2].Footer != appwire.FooterStoppedByClose || footers[2].Text != "The app closed while this was working · send a message to carry on" {
		t.Fatalf("footers=%+v", footers)
	}
	r.wantIdle(t)
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
				if f.CloseResumes[r.id] != 0 || f.ClosePending[r.id] != "" || !f.CloseResumedAt[r.id].IsZero() || !f.ClosePendingAt[r.id].IsZero() {
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

// C7: affected starts count once each, including the current third start;
// an unaffected start adds no stamp, and every reply on the third is stopped.
func TestAppCloseC7ThirdAffectedStartStopsEveryReply(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.CloseStarts = []time.Time{r.clock.now().Add(-8 * time.Minute), r.clock.now().Add(-4 * time.Minute)}
		return nil
	})
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
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("third affected start resumed work")
	}
	r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
	entries, _, err := r.b.Entries(otherID, "", 100)
	if err != nil || len(closeFooters(entries)) != 1 || closeFooters(entries)[0].Footer != appwire.FooterStoppedByClose {
		t.Fatalf("other entries=%+v,%v", entries, err)
	}
	_ = r.b.state.read(func(f *appStateFile) {
		if len(f.CloseStarts) != 3 {
			t.Errorf("counted conversations instead of starts: %v", f.CloseStarts)
		}
	})
	_ = r.b.Close()
	r.p.State = session.PresenceIdle
	r.write(t, r.p)
	_ = os.Remove(filepath.Join(filepath.Dir(other), "presence.json"))
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	_ = r.b.state.read(func(f *appStateFile) {
		if len(f.CloseStarts) != 3 {
			t.Errorf("unaffected start counted: %v", f.CloseStarts)
		}
	})
}

// C7: the delay needed to establish a dead claim cannot move a third start
// outside the ten-minute start window, and the breaker lasts for this start.
func TestAppCloseC7ExpiryDelayCannotEvadeTheBreaker(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, 2*time.Second)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.CloseStarts = []time.Time{r.clock.now().Add(-10*time.Minute + time.Second), r.clock.now().Add(-time.Minute)}
		return nil
	})
	r.start()
	r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("claim-expiry delay evaded third-start breaker")
	}
	r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
	_ = r.b.state.read(func(f *appStateFile) {
		for _, at := range f.CloseStarts {
			if at.Before(r.clock.now().Add(-appCloseBreakerWithin)) {
				t.Errorf("expired stamp retained: %v", at)
			}
		}
	})
	// Another claim found much later in this same process is still blocked.
	r.clock.advance(t, 11*time.Minute)
	r.p.UpdatedAt = r.clock.now()
	r.write(t, r.p)
	_, _ = r.b.ListAgents("")
	r.clock.advance(t, session.PresenceWindow+appClosePresenceMargin)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("breaker expired during its own start")
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

// C9: a claim older than one hour at this start is reported without replay.
func TestAppCloseC9OldDeathIsReported(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Hour+time.Nanosecond)
	r.start()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("old reply replayed")
	}
	r.wantFooter(t, appwire.FooterStoppedByClose, appCloseStoppedWord)
	r.wantIdle(t)
}

// C10: the same updatedAt encountered by a later process has no second effect.
func TestAppCloseC10SameClaimHandledOnce(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "resumed", true: "stopped"}[stopped], func(t *testing.T) {
			r := newCloseRig(t, session.PresenceWorking, time.Minute)
			if stopped {
				_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{r.id: 2}; return nil })
			}
			r.start()
			r.clock.advance(t, 0)
			first, _ := r.conv.counts()
			if !stopped {
				// The accepted fake wake must start a turn, as the real session
				// does. An unstarted wake deliberately remains pending for F3.
				a := r.b.agent(r.id)
				events := make(chan session.Event, 2)
				events <- session.Event{Kind: session.EventCloseContinued, Text: closeDisk(t, r).ClosePending[r.id]}
				events <- session.Event{Kind: session.EventError, Text: "unfinished test turn"}
				close(events)
				r.b.wokenTurn(a, r.b.liveOf(a), appTurnElsewhere{events: events})
			}
			_ = r.b.Close()
			r.backend(t)
			r.start()
			r.clock.advance(t, 0)
			if n, _ := r.conv.counts(); n != first {
				t.Fatal("same claim resumed again")
			}
			if len(closeFooters(r.entries(t))) != 1 {
				t.Fatal("same claim got another footer")
			}
		})
	}
}

// C11, C12: a half-finished quit owns its claim, and its old footer now persists.
func TestAppCloseC11QuitNeverDoublesAndKeepsItsFooter(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	_ = r.b.state.write(func(f *appStateFile) error {
		f.ResumeAfterQuit = []string{r.id}
		f.QuitReplies = []string{r.id}
		return nil
	})
	r.b.snapshotCloseClaims()
	r.b.resumeAfterQuit()
	r.rec.waitFor(t, time.Second, func(_ string, d any) bool {
		e, ok := d.(appwire.EntryAdded)
		return ok && e.Entry.Footer == appwire.FooterResumedAfterQuit
	})
	r.b.startCloseWatch()
	r.clock.advance(t, 0)
	r.wantFooter(t, appwire.FooterResumedAfterQuit, "Resumed after quit")
	if n, q := r.conv.counts(); n != 0 || q != 1 {
		t.Fatalf("close=%d quit=%d", n, q)
	}
	_ = r.b.Close()
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	r.wantFooter(t, appwire.FooterResumedAfterQuit, "Resumed after quit")
	if n, q := r.conv.counts(); n != 0 || q != 1 {
		t.Fatalf("later close=%d quit=%d", n, q)
	}
}

// C12: fresh backends keep both new footers in place, including equal timestamps.
func TestAppCloseC12FootersSurviveAFreshBackendInPlace(t *testing.T) {
	for _, kind := range []string{appwire.FooterResumedAfterClose, appwire.FooterStoppedByClose, appwire.FooterResumedAfterQuit} {
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
		t.Fatal("resumed without writing guard state")
	}
	_ = r.b.state.read(func(f *appStateFile) {
		if !f.CloseHandled[r.id].IsZero() {
			t.Error("failed write spent handled claim")
		}
	})
}

// C14: a malformed day limit blocks all paid-capable opens and leaves the
// claim available after the profile is repaired and the app is reopened.
func TestAppCloseC14DayLimitSettlesBeforeAnyResume(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	path := config.BudgetConfigPath(config.ProfileDir())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"daily_budget_usd":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	r.b.open = func(appOpenAsk) (*appConn, error) {
		t.Error("opened before budget settled")
		return nil, errors.New("blocked")
	}
	r.start()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("resumed with unknown day limit")
	}
	_ = r.b.state.read(func(f *appStateFile) {
		if !f.CloseHandled[r.id].IsZero() {
			t.Error("lost claim on budget failure")
		}
	})
	if err := os.WriteFile(path, []byte(`{"daily_budget_usd":20}`), 0600); err != nil {
		t.Fatal(err)
	}
	_ = r.b.Close()
	r.backend(t)
	r.start()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 1 {
		t.Fatal("repaired budget did not allow the saved reply to carry on")
	}
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
			if ours && n != 1 {
				t.Fatal("own opener erased the evidence")
			}
			if !ours && (n != 0 || len(closeFooters(r.entries(t))) != 0) {
				t.Fatal("drove the new holder")
			}
		})
	}
}

// C8: a new holder can acquire the journal after expiry but before the opener.
func TestAppCloseC8OwnershipChangesAtOpen(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, time.Minute)
	r.b.open = func(appOpenAsk) (*appConn, error) {
		return &appConn{Conv: r.conv, Transcript: r.file, Workspace: r.p.Workspace, Release: func() {}}, nil
	}
	r.start()
	r.clock.advance(t, 0)
	if n, _ := r.conv.counts(); n != 0 {
		t.Fatal("joined holder was resumed")
	}
	if len(closeFooters(r.entries(t))) != 0 {
		t.Fatal("joined holder received a close receipt")
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
