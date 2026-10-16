package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── two processes, lost and vanished claims, settles that fail, the read
// gate and what a handle can be shown to do. ──────────────────────────────

// second is ANOTHER PROCESS over the same HOME, the same conversations and the
// same producers: its own backend, its own admission, its own wire.
func (h *actionHarness) second() (*appBackend, *actionWire) {
	h.t.Helper()
	known := map[string]string{}
	h.b.locked(func() {
		for id, a := range h.b.agents {
			known[id] = a.transcript
		}
	})
	b := newAppBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		c := h.cvs[ask.Transcript]
		if c == nil {
			c = &actionConv{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, p: h.p}
			h.cvs[ask.Transcript] = c
		}
		return &appConn{Conv: c, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	for id, transcript := range known {
		b.locked(func() { b.agentLocked(id).transcript = transcript })
	}
	h.t.Cleanup(func() { b.Close() })
	return b, newActionWire(h.t, b)
}

// TWO PROCESSES, ONE CLIENT_ID, WHILE THE FIRST IS INSIDE THE PRODUCER. The
// second cannot see the first's flight, only its claim, so the same words are
// uncertain (not in_progress, not new), other words are a conflict, a read is
// uncertain, and once the first has settled the second replays its record.
// The producer runs once.
func TestTwoProcessesShareOneClaimAndRunTheProducerOnce(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.p.mu.Lock()
	h.p.hold, h.p.entered = make(chan struct{}), make(chan struct{}, 1)
	h.p.mu.Unlock()
	first := make(chan appwire.ActionReceipt, 1)
	go func() {
		r, e := h.w.act(taskAsk(agent, "mp-1", "do the thing"))
		if e != nil {
			t.Errorf("the first process: %+v", e)
		}
		first <- r
	}()
	<-h.p.entered
	_, w2 := h.second()

	same, e := w2.act(taskAsk(agent, "mp-1", "do the thing"))
	if e != nil || same.State != appwire.ActionUncertain || !same.Replayed || same.Note != appActionNoteIntent || same.Task != nil {
		t.Fatalf("the second process, same words, while the first runs: %+v %v", same, e)
	}
	_, e = w2.act(taskAsk(agent, "mp-1", "other words"))
	wantCode(t, e, appwire.CodeConflict)
	got, e := w2.get(agent, "mp-1")
	if e != nil || got.State != appwire.ActionUncertain {
		t.Fatalf("the second process reads while the first runs: %+v %v", got, e)
	}
	// The first process still answers its own repeat in_progress.
	if r, e := h.w.act(taskAsk(agent, "mp-1", "do the thing")); e != nil || r.State != appwire.ActionInProgress {
		t.Fatalf("the first process's own repeat: %+v %v", r, e)
	}

	close(h.p.hold)
	var done appwire.ActionReceipt
	select {
	case done = <-first:
	case <-time.After(10 * time.Second):
		t.Fatal("the first process never answered")
	}
	if done.State != appwire.ActionDone || done.Task == nil {
		t.Fatalf("the first process: %+v", done)
	}
	after, e := w2.act(taskAsk(agent, "mp-1", "do the thing"))
	if e != nil || after.State != appwire.ActionDone || !after.Replayed || after.Task == nil || after.Task.ID != done.Task.ID {
		t.Fatalf("the second process after the first settled: %+v %v", after, e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
}

// plantingConv runs plant while the engine reads its catalogue: after the
// lookups and the admission, before the claim — the window another process
// claims the same client_id in.
type plantingConv struct {
	*actionConv
	plant func()
}

func (c *plantingConv) Delegates() session.DelegateReport {
	if plant := c.plant; plant != nil {
		c.plant = nil
		plant()
	}
	return c.actionConv.Delegates()
}

// plantingBackend is a backend whose one conversation is a plantingConv.
func plantingBackend(t *testing.T) (*appBackend, *actionWire, *plantingConv, *actionProducers, string) {
	t.Helper()
	wakeEnvironment(t, resolvedTempDir(t))
	p := &actionProducers{kept: true}
	conv := &plantingConv{actionConv: &actionConv{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}, p: p}}
	b := newAppBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { b.Close() })
	a, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return b, newActionWire(t, b), conv, p, a.ID
}

// A LOST CLAIM RACE IS ANSWERED FROM THE WINNER'S RECORD: another process
// claims the client_id after this one looked and before it claimed; the
// exclusive link refuses this one, its producer is not called, and what the
// winner recorded — an attempt in flight — is uncertain.
func TestALostClaimRaceIsAnsweredFromTheWinnersRecord(t *testing.T) {
	b, w, conv, p, agent := plantingBackend(t)
	ask := programAsk(agent, "race-1", "review", "")
	conv.plant = func() {
		made, err := b.actionClaims().create(appActionClaim{V: appActionClaimV, ClientID: "race-1", Action: ask.Action, Agent: agent,
			Digest: appActionDigest(ask), State: appActionIntent, At: time.Now().UTC()})
		if err != nil || !made {
			t.Errorf("the other process's claim: %v %v", made, err)
		}
	}
	r, e := w.act(ask)
	if e != nil || r.State != appwire.ActionUncertain || !r.Replayed || r.Note != appActionNoteIntent {
		t.Fatalf("a lost race: %+v %v", r, e)
	}
	if _, d, _ := p.counts(); d != 0 {
		t.Fatalf("the loser called the producer %d times", d)
	}
}

// A LOST CLAIM WHOSE RECORD THEN VANISHED IS UNCERTAIN, NEVER AN EMPTY SUCCESS:
// the link was refused because the name was taken, so somebody claimed this
// client_id, and its record being gone a moment later — another process
// taking its own claim back after a failed sync, a hand on the folder — is not
// proof nothing was sent. Nothing ran here.
func TestALostClaimWhoseRecordVanishedIsUncertain(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	h.b.createAction = func(c appActionClaims, claim appActionClaim) (bool, error) {
		// Another process claims it first; this one's real link is refused;
		// then the winner's record is gone before it can be read.
		if made, err := c.create(claim); err != nil || !made {
			t.Errorf("the winner's claim: %v %v", made, err)
		}
		made, err := c.create(claim)
		_ = os.Remove(filepath.Join(c.dir, appClaimName(claim.ClientID)))
		return made, err
	}
	raw, e := h.w.call("session.act", taskAsk(agent, "gone-1", "x"))
	wantCode(t, e, appwire.CodeUncertain)
	if !strings.Contains(e.Message, "cannot say whether it ran") || len(raw) != 0 {
		t.Fatalf("a vanished claim answered %s / %+v", raw, e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 0 {
		t.Fatalf("the producer ran %d times", tasks)
	}
}

// A SETTLE THAT FAILS DOES NOT CLAIM TO KNOW WHAT THE RECORD SAYS. The caller
// gets the producer's real answer and a note that a later read MAY say
// uncertain; whether it does depends on whether the rewrite landed before the
// failure, and both are drawn here. Neither runs the producer again.
func TestASettleThatFailsSaysTheRecordMayReadUncertain(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	for _, c := range []struct {
		id, want string
		landed   bool
	}{
		{"settle-lost", appwire.ActionUncertain, false},
		{"settle-landed", appwire.ActionDone, true},
	} {
		h.b.settleAction = func(claims appActionClaims, claim appActionClaim) error {
			if c.landed {
				// The rename landed and the folder sync then failed.
				if err := claims.settle(claim); err != nil {
					t.Errorf("the rewrite: %v", err)
				}
			}
			return errors.New("the disk said no")
		}
		r, e := h.w.act(taskAsk(agent, c.id, "go"))
		if e != nil || r.State != appwire.ActionDone || r.Task == nil || r.Note != appActionNoteUnsettled {
			t.Fatalf("%s: %+v %v", c.id, r, e)
		}
		if strings.Contains(r.Note, "will") || !strings.Contains(r.Note, "may") {
			t.Fatalf("%s: the note claims to know what the record says: %q", c.id, r.Note)
		}
		h.b.settleAction = nil
		got, e := h.w.get(agent, c.id)
		if e != nil || got.State != c.want {
			t.Fatalf("%s: a later read: %+v %v", c.id, got, e)
		}
		again, e := h.w.act(taskAsk(agent, c.id, "go"))
		if e != nil || again.State != c.want || !again.Replayed {
			t.Fatalf("%s: a repeat: %+v %v", c.id, again, e)
		}
	}
	if tasks, _, _ := h.p.counts(); tasks != 2 {
		t.Fatalf("the producer ran %d times for two attempts", tasks)
	}
}

// SESSION.ACTIONS IS A BOUNDED READ, HELD UNTIL THE CATALOGUE HAS ANSWERED: it
// takes one of the read slots session.action.get shares, keeps it while the
// engine reads its programs, and answers busy at once when every slot is
// taken — nothing queues.
func TestSessionActionsHoldsAReadSlotThroughTheCatalogueRead(t *testing.T) {
	b, w, conv, _, agent := plantingBackend(t)
	reads := func() int {
		b.actions.mu.Lock()
		defer b.actions.mu.Unlock()
		return b.actions.reads
	}
	inside, leave := make(chan struct{}), make(chan struct{})
	conv.plant = func() {
		close(inside)
		<-leave
	}
	answered := make(chan *appwire.Error, 1)
	go func() {
		_, e := w.call("session.actions", appwire.ActionsParams{Agent: agent})
		answered <- e
	}()
	<-inside
	if got := reads(); got != 1 {
		t.Fatalf("while the catalogue is read, %d read slots are held; want 1", got)
	}
	var held []func()
	for i := 1; i < appActionReads; i++ {
		release, ok := b.actions.reading()
		if !ok {
			t.Fatalf("slot %d refused", i)
		}
		held = append(held, release)
	}
	_, e := w.call("session.actions", appwire.ActionsParams{Agent: agent})
	wantCode(t, e, appwire.CodeBusy)
	_, e = w.get(agent, "any")
	wantCode(t, e, appwire.CodeBusy)
	close(leave)
	if e := <-answered; e != nil {
		t.Fatalf("the held read: %+v", e)
	}
	for _, release := range held {
		release()
	}
	if got := reads(); got != 0 {
		t.Fatalf("%d read slots outlived their reads", got)
	}
	if _, e := w.call("session.actions", appwire.ActionsParams{Agent: agent}); e != nil {
		t.Fatalf("after the gate emptied: %+v", e)
	}
}

// emptyCatalogue is a conversation whose catalogue read answered nothing.
type emptyCatalogue struct{ *actionConv }

func (emptyCatalogue) Delegates() session.DelegateReport { return session.DelegateReport{} }

// WHAT A HANDLE CAN BE SHOWN TO DO IS WHAT IS OFFERED, AND WHAT IS REFUSED. A
// handle in this process answers for itself; a far handle's type proves
// nothing, so its effort is not offered (and refused before any claim), and an
// empty catalogue from it is unknown — never "carries no programs", never "no
// program named". Everywhere rides the same far door as a project line.
func TestFarDoorsOfferOnlyWhatTheyCanShow(t *testing.T) {
	b := &appBackend{}
	p := &actionProducers{kept: true}
	conv := &actionConv{fakeConversation: &fakeConversation{}, p: p}
	near := appActionDoorsOf(conv)
	far := near
	far.far, far.effort = true, nil

	if c := near.capabilities("a").Actions[0]; !c.Available || len(c.Efforts) != 2 {
		t.Fatalf("a handle in this process with the effort door: %+v", c)
	}
	if c := far.capabilities("a").Actions[0]; !c.Available || len(c.Efforts) != 0 {
		t.Fatalf("a far handle offers an effort nobody can show: %+v", c)
	}
	effort := taskAsk("a", "e", "go")
	effort.Task.Effort = appwire.EffortBest
	if _, err := b.actionRunWith(far, effort); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("an effort over a far handle: %v", err)
	}
	if m := far.capabilities("a").Actions[2]; !m.Available || len(m.Scopes) != 2 {
		t.Fatalf("a far handle that keeps memory keeps it everywhere through the same door: %+v", m)
	}

	far.program = emptyCatalogue{conv}
	if c := far.capabilities("a").Actions[1]; c.Available || c.Why != appActionNoCatalogue {
		t.Fatalf("an empty far catalogue: %+v", c)
	}
	if _, err := b.actionRunWith(far, programAsk("a", "p", "review", "")); wireCode(err) != appwire.CodeEngine ||
		strings.Contains(err.Error(), "no program named") {
		t.Fatalf("a program over an unknown far catalogue: %v", err)
	}
	near.program = emptyCatalogue{conv}
	if c := near.capabilities("a").Actions[1]; c.Available || c.Why != "this engine carries no programs" {
		t.Fatalf("an empty catalogue in this process is a fact: %+v", c)
	}
	if _, err := b.actionRunWith(near, programAsk("a", "p", "review", "")); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a program over an empty catalogue in this process: %v", err)
	}
	if tasks, d, r := p.counts(); tasks+d+r != 0 {
		t.Fatalf("a refusal called a producer: %d %d %d", tasks, d, r)
	}
}

// A PROJECT LINE STATES NO SCOPE; AN EVERYWHERE LINE STATES EVERYWHERE. The
// project door keeps the line for the project, or for the person everywhere
// when the conversation has none, and answers only a sentence, so the scope is
// absent on the answer, the replay and the read whatever that sentence says;
// the sentence itself is the producer's, unchanged. The everywhere method
// fixes where the line holds, so its scope is stated. A record an earlier
// build wrote with the ASKED scope reads with none.
func TestOnlyTheEverywhereMethodStatesWhereALineHolds(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	widened := "for you everywhere, since this conversation has no project of its own"
	h.p.mu.Lock()
	h.p.memReach = widened
	h.p.mu.Unlock()
	r, e := h.w.act(memoryAsk(agent, "w-1", "ship on Fridays", appwire.ScopeProject))
	if e != nil || r.State != appwire.ActionDone || r.Memory == nil || r.Memory.Scope != "" ||
		r.Memory.Receipt != "remembered · ship on Fridays · "+widened {
		t.Fatalf("a project line: %+v %v", r, e)
	}
	again, e := h.w.act(memoryAsk(agent, "w-1", "ship on Fridays", ""))
	if e != nil || !again.Replayed || again.Memory == nil || again.Memory.Scope != "" {
		t.Fatalf("the replay states a scope nobody knows: %+v %v", again, e)
	}
	if got, e := h.w.get(agent, "w-1"); e != nil || got.Memory == nil || got.Memory.Scope != "" {
		t.Fatalf("the read states a scope nobody knows: %+v %v", got, e)
	}
	every, e := h.w.act(memoryAsk(agent, "w-2", "tabs", appwire.ScopeEverywhere))
	if e != nil || every.Memory == nil || every.Memory.Scope != appwire.ScopeEverywhere {
		t.Fatalf("an everywhere line: %+v %v", every, e)
	}
	if got, e := h.w.get(agent, "w-2"); e != nil || got.Memory == nil || got.Memory.Scope != appwire.ScopeEverywhere {
		t.Fatalf("the read of an everywhere line: %+v %v", got, e)
	}

	old := memoryAsk(agent, "w-old", "an older record", appwire.ScopeProject)
	if made, err := h.b.actionClaims().create(appActionClaim{V: appActionClaimV, ClientID: "w-old", Action: old.Action, Agent: agent,
		Digest: appActionDigest(old), State: appActionDone, At: time.Now().UTC(),
		Memory: &appwire.ActionMemory{Scope: appwire.ScopeProject}}); err != nil || !made {
		t.Fatalf("the older record: %v %v", made, err)
	}
	if got, e := h.w.get(agent, "w-old"); e != nil || got.State != appwire.ActionDone || got.Memory == nil || got.Memory.Scope != "" {
		t.Fatalf("an older record's asked scope read as where the line holds: %+v %v", got, e)
	}
}

// EVERY CLAIM MAKES ITS FOLDER CHAIN DURABLE BEFORE THE CLAIM IS. The order is
// observed, not a power loss: when the state folder is synced the claims
// folder is in it and holds none of this claim yet, and then the state
// folder's parent is synced; a second claim in the same folder does both
// again, because a folder that exists may be another process's, not yet
// synced.
func TestEveryClaimSyncsItsFolderChainBeforeTheClaimIsMade(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	dir := filepath.Join(state, appActionClaimsName)
	var seen []string
	c := appActionClaims{appStartClaims: appStartClaims{dir: dir}, syncFolder: func(folder string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("the claims folder was not there when %s was synced: %v", folder, err)
		}
		seen = append(seen, folder+" with "+strconv.Itoa(len(entries)))
		return nil
	}}
	claim := appActionClaim{V: appActionClaimV, ClientID: "d-1", Action: appwire.ActionTaskStart, Agent: "a", Digest: "x", State: appActionIntent, At: time.Now().UTC()}
	if made, err := c.create(claim); err != nil || !made {
		t.Fatalf("%v %v", made, err)
	}
	claim.ClientID = "d-2"
	if made, err := c.create(claim); err != nil || !made {
		t.Fatalf("%v %v", made, err)
	}
	want := []string{state + " with 0", filepath.Dir(state) + " with 0", state + " with 1", filepath.Dir(state) + " with 1"}
	if strings.Join(seen, ";") != strings.Join(want, ";") {
		t.Fatalf("syncs %v, want exactly %v", seen, want)
	}
}

// A CLAIMS FOLDER ANOTHER PROCESS HAS MADE AND NOT YET SYNCED IS NOT TRUSTED.
// The other process made the folder and is stalled before its own sync; this
// one sees the folder, and its producer does not run, and no claim exists,
// until its own sync of the chain has answered.
func TestAFolderAnotherProcessHasNotSyncedHoldsTheProducerUntilTheSync(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	dir := h.b.actionClaims().dir
	if err := os.MkdirAll(dir, appStateFolderMode); err != nil {
		t.Fatal(err) // the other process's Mkdir, its sync never reached
	}
	entered, release := make(chan string, 4), make(chan struct{})
	// The request is let go and waited for before the test ends, whichever
	// way it ends, so it never touches the test after it has returned.
	var running sync.WaitGroup
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		running.Wait()
	})
	h.b.createAction = func(c appActionClaims, claim appActionClaim) (bool, error) {
		c.syncFolder = func(folder string) error {
			entered <- folder
			<-release
			return nil
		}
		return c.create(claim)
	}
	type answer struct {
		r appwire.ActionReceipt
		e *appwire.Error
	}
	done := make(chan answer, 1)
	running.Add(1)
	go func() {
		defer running.Done()
		r, e := h.w.act(taskAsk(agent, "seen-1", "go"))
		done <- answer{r, e}
	}()
	select {
	case got := <-entered:
		if got != filepath.Dir(dir) {
			t.Fatalf("the first sync was of %s, want the state folder %s", got, filepath.Dir(dir))
		}
	case a := <-done:
		t.Fatalf("the claim was made without syncing the folder it found: %+v %v", a.r, a.e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 0 {
		t.Fatalf("the producer ran %d times before the folder was synced", tasks)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("a claim was written before the folder was synced: %v %v", entries, err)
	}
	close(release)
	if a := <-done; a.e != nil || a.r.State != appwire.ActionDone {
		t.Fatalf("the claim after the sync: %+v %v", a.r, a.e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
}

// A FOLDER CHAIN THAT CANNOT BE SYNCED STOPS THE PRODUCER, BEFORE ANY CLAIM.
// The claim is not written (no record, nothing to read back as one), the wire
// answers engine "did not start it", and the same client_id runs once as soon
// as the disk answers, syncing the chain itself again.
func TestAFolderThatCannotBeMadeDurableStopsTheProducer(t *testing.T) {
	h := newActionHarness(t)
	agent := h.agent()
	fail, syncs := true, 0
	h.b.createAction = func(c appActionClaims, claim appActionClaim) (bool, error) {
		c.syncFolder = func(string) error {
			syncs++
			if fail {
				return errors.New("the disk said no")
			}
			return nil
		}
		return c.create(claim)
	}
	_, e := h.w.call("session.act", taskAsk(agent, "dur-1", "go"))
	wantCode(t, e, appwire.CodeEngine)
	if !strings.Contains(e.Message, "did not start it") {
		t.Fatalf("the refusal: %+v", e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 0 {
		t.Fatalf("the producer ran %d times on a folder the disk may not keep", tasks)
	}
	if _, found, err := h.b.actionClaims().read("dur-1"); found || err != nil {
		t.Fatalf("a claim exists after the failed sync: found=%v err=%v", found, err)
	}
	fail = false
	r, e := h.w.act(taskAsk(agent, "dur-1", "go"))
	if e != nil || r.State != appwire.ActionDone {
		t.Fatalf("the same id once the disk answers: %+v %v", r, e)
	}
	if tasks, _, _ := h.p.counts(); tasks != 1 {
		t.Fatalf("the producer ran %d times", tasks)
	}
	// One failed sync, then the retry's two: it did not rest on the folder
	// the failed attempt left.
	if syncs != 3 {
		t.Fatalf("the chain was synced %d times across a failure and its retry, want 3", syncs)
	}
}
