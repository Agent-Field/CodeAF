package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// TestThreeFailedChecksRestartAndOnePassRecovers walks the board's main story:
// the first check ten seconds after start, three failures in a row make it
// fail, the policy restarts it after 2s, and one passing check makes it
// healthy again — all one episode.
func TestThreeFailedChecksRestartAndOnePassRecovers(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	if row.State != StateStarting || !row.Running || row.PID == 0 {
		t.Fatalf("a kept service starts running: %+v", row)
	}
	first := row.PID
	h.advance(9 * time.Second)
	if h.get(row.ID).Check != nil {
		t.Fatal("a check ran before ten seconds")
	}
	h.advance(time.Second)
	if got := h.get(row.ID); got.State != StateHealthy || got.Check == nil || !got.Check.OK {
		t.Fatalf("the first check at 10s should pass: %+v", got)
	}
	h.rt.fail("web", true)
	h.advance(20 * time.Second)
	if got := h.get(row.ID); got.State != StateHealthy || got.Check.Fails != 2 {
		t.Fatalf("two failures are not yet a failure: %+v", got)
	}
	h.advance(10 * time.Second)
	got := h.get(row.ID)
	if got.State != StateRestarting || got.BackoffUntil == nil || got.Restarts != 0 || !got.EpisodeOpen {
		t.Fatalf("three failures in a row restart it: %+v", got)
	}
	episode := got.Episode
	if want := h.clock.Now().Add(FirstBackoff); !got.BackoffUntil.Equal(want) {
		t.Fatalf("first backoff is 2s: %v, want %v", got.BackoffUntil, want)
	}
	h.rt.fail("web", false)
	h.advance(2 * time.Second)
	got = h.get(row.ID)
	if got.State != StateStarting || got.PID == first || got.Restarts != 1 || got.StartedHow != StartedByRestart {
		t.Fatalf("the restart starts a new process and counts 1 of 3: %+v", got)
	}
	if h.rt.proc(first).alive {
		t.Fatal("the unhealthy process was not ended before the restart")
	}
	h.advance(10 * time.Second)
	got = h.get(row.ID)
	if got.State != StateHealthy || got.EpisodeOpen || got.Episode != episode {
		t.Fatalf("one pass is healthy again and closes the same episode: %+v", got)
	}
}

// TestTheBudgetIsThreeRestartsInTenMinutes is 2s/4s/8s, then giving up, and the
// window sliding open again.
func TestTheBudgetIsThreeRestartsInTenMinutes(t *testing.T) {
	h := newHarness(t)
	row := h.keep("api", PolicyOnFailure)
	for i, backoff := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second} {
		pid := h.get(row.ID).PID
		h.rt.exit(t, h.s, pid, 1)
		got := h.get(row.ID)
		if got.State != StateRestarting || got.Reason != "exited 1" || got.Restarts != i {
			t.Fatalf("crash %d: %+v", i+1, got)
		}
		h.advance(backoff - time.Millisecond)
		if h.get(row.ID).State != StateRestarting {
			t.Fatalf("crash %d restarted before its %s backoff", i+1, backoff)
		}
		h.advance(time.Millisecond)
		if got := h.get(row.ID); got.State != StateStarting || got.Restarts != i+1 {
			t.Fatalf("crash %d: not restarted after %s: %+v", i+1, backoff, got)
		}
	}
	h.rt.exit(t, h.s, h.get(row.ID).PID, 1)
	got := h.get(row.ID)
	if got.State != StateGaveUp || got.Running || got.Reason != "stopped restarting after 3 failures in 10 minutes" {
		t.Fatalf("the fourth failure in ten minutes gives up: %+v", got)
	}
	starts := h.rt.starts
	h.advance(time.Hour)
	if h.rt.starts != starts {
		t.Fatal("a service that gave up was started again on its own")
	}
	if h.get(row.ID).Restarts != 0 {
		t.Fatal("the window did not slide")
	}
}

// TestTenHealthyMinutesStartTheCountAgain resets the budget after a long
// healthy run, which the window alone would also do; the reset is measured
// from healthy, not from the restart.
func TestTenHealthyMinutesStartTheCountAgain(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	h.rt.exit(t, h.s, h.get(row.ID).PID, 1)
	h.advance(2 * time.Second)
	h.advance(10 * time.Second)
	if got := h.get(row.ID); got.State != StateHealthy || got.Restarts != 1 {
		t.Fatalf("%+v", got)
	}
	h.advance(RestartWindow)
	if got := h.get(row.ID); got.Restarts != 0 {
		t.Fatalf("ten healthy minutes leave the count at %d", got.Restarts)
	}
}

// TestAPersonsRestartNeitherSpendsNorRefillsTheBudget is the board's "counts
// as yours, not toward 1 of 3".
func TestAPersonsRestartNeitherSpendsNorRefillsTheBudget(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	for _, backoff := range []time.Duration{2 * time.Second, 4 * time.Second} {
		h.rt.exit(t, h.s, h.get(row.ID).PID, 1)
		h.advance(backoff)
	}
	before := h.get(row.ID)
	if before.Restarts != 2 {
		t.Fatalf("%+v", before)
	}
	got, err := h.s.Restart("agent-a", row.ID)
	if err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	got = h.get(row.ID)
	if got.PID == before.PID || got.Restarts != 2 || got.StartedHow != StartedByPerson {
		t.Fatalf("a person's restart is a new process that counts nothing: %+v", got)
	}
	h.rt.exit(t, h.s, got.PID, 1)
	if got := h.get(row.ID); got.State != StateRestarting || !got.BackoffUntil.Equal(h.clock.Now().Add(8*time.Second)) {
		t.Fatalf("the next automatic restart counts on from 2: %+v", got)
	}
	h.advance(8 * time.Second)
	h.rt.exit(t, h.s, h.get(row.ID).PID, 1)
	if got := h.get(row.ID); got.State != StateGaveUp {
		t.Fatalf("the budget was refilled by a person's restart: %+v", got)
	}
}

// TestStopEndsItAndNothingRestartsIt is SIGTERM, the row stopped at once,
// the output kept, no restart after it, and Start again asking nothing.
func TestStopEndsItAndNothingRestartsIt(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyAlways)
	pid := row.PID
	if err := os.WriteFile(row.LogPath, []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopped, err := h.s.Stop("agent-a", row.ID)
	if err != nil || stopped.State != StateStopped || stopped.StoppedBy != StartedByPerson {
		t.Fatalf("stop: %+v %v", stopped, err)
	}
	settle(t, h.s)
	if h.rt.proc(pid).alive || len(h.rt.proc(pid).signals) != 1 || h.rt.proc(pid).signals[0] {
		t.Fatalf("stop sends one SIGTERM: %+v", h.rt.proc(pid))
	}
	starts := h.rt.starts
	h.advance(time.Hour)
	got := h.get(row.ID)
	if h.rt.starts != starts || got.State != StateStopped || got.Running {
		t.Fatalf("a stopped service came back: %+v", got)
	}
	if body, _ := os.ReadFile(got.LogPath); string(body) != "ready\n" {
		t.Fatal("stop did not keep the output")
	}
	if again, err := h.s.Stop("agent-a", row.ID); err != nil || again.State != StateStopped {
		t.Fatal("stopping a stopped service is not idempotent")
	}
	if _, err := h.s.Start("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if got := h.get(row.ID); got.State != StateStarting || !got.Running || got.Policy != PolicyAlways {
		t.Fatalf("start again uses the saved spec and policy: %+v", got)
	}
}

// TestAStopIgnoredSendsSigkillAfterTheGrace keeps the grace on the clock.
func TestAStopIgnoredSendsSigkillAfterTheGrace(t *testing.T) {
	h := newHarness(t)
	row := h.keep("stubborn", PolicyOnFailure)
	h.rt.proc(row.PID).ignoreTerm = true
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.get(row.ID).Stopping && time.Now().Before(deadline) {
		h.clock.fireDue(time.Second)
		time.Sleep(time.Millisecond)
	}
	p := h.rt.proc(row.PID)
	if p.alive || len(p.signals) != 2 || p.signals[0] || !p.signals[1] {
		t.Fatalf("TERM then KILL: %+v", p.signals)
	}
	if elapsed := h.clock.Now().Sub(row.Updated); elapsed < StopGrace {
		t.Fatalf("SIGKILL came after %s, before the %s grace", elapsed, StopGrace)
	}
}

// TestPolicyNeverShowsTheStateAndRestartsNothing covers both kinds of failure.
func TestPolicyNeverShowsTheStateAndRestartsNothing(t *testing.T) {
	h := newHarness(t)
	row := h.keep("mock", PolicyNever)
	h.rt.fail("mock", true)
	h.advance(30 * time.Second)
	got := h.get(row.ID)
	if got.State != StateUnhealthy || !got.Running || got.Reason != "port 5173 not open · 3 checks" {
		t.Fatalf("never: three failed checks are unhealthy and it keeps running: %+v", got)
	}
	h.rt.exit(t, h.s, got.PID, 1)
	got = h.get(row.ID)
	if got.State != StateFailed || got.Reason != "exited 1 · not restarted (policy: never)" || *got.ExitCode != 1 {
		t.Fatalf("never: exit 1 fails: %+v", got)
	}
	if h.rt.starts != 1 {
		t.Fatal("policy never restarted something")
	}
}

// TestACleanExitRestartsOnlyUnderAlways is the one difference between the two
// restarting policies.
func TestACleanExitRestartsOnlyUnderAlways(t *testing.T) {
	h := newHarness(t)
	onFailure := h.keep("build", PolicyOnFailure)
	always := h.keep("watch", PolicyAlways)
	h.rt.exit(t, h.s, onFailure.PID, 0)
	h.rt.exit(t, h.s, always.PID, 0)
	if got := h.get(onFailure.ID); got.State != StateNotRunning || got.Reason != "exited 0" {
		t.Fatalf("on_failure leaves a clean exit alone: %+v", got)
	}
	if got := h.get(always.ID); got.State != StateRestarting {
		t.Fatalf("always restarts a clean exit: %+v", got)
	}
}

// TestAPolicyChangeAppliesAtTheNextFailure and the agent cannot replace it.
func TestAPolicyChangeAppliesAtTheNextFailure(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	changed, err := h.s.SetPolicy("agent-a", row.ID, PolicyNever)
	if err != nil || changed.Policy != PolicyNever || changed.State != StateStarting || !changed.Running {
		t.Fatalf("a policy change touches nothing now: %+v %v", changed, err)
	}
	h.rt.exit(t, h.s, row.PID, 1)
	if got := h.get(row.ID); got.State != StateFailed {
		t.Fatalf("the next failure follows the new policy: %+v", got)
	}
	again, err := h.s.Keep("agent-a", webSpec("web", h.dir), PolicyAlways, Provenance{})
	if err != nil || again.Policy != PolicyNever {
		t.Fatalf("the agent asking again replaced the person's policy: %+v %v", again, err)
	}
	if _, err := h.s.SetPolicy("agent-a", row.ID, "sometimes"); err == nil {
		t.Fatal("an unknown policy was taken")
	}
}

// TestConsentIsForTheExactCommandFolderAndCheck: one yes covers one command
// in one folder with the check the person saw. A changed check — a new check
// command above all, which is a second command run every ten seconds — is
// asked about again, and the yes survives a restart of codeaf.
func TestConsentIsForTheExactCommandFolderAndCheck(t *testing.T) {
	h := newHarness(t)
	spec := Spec{Command: "docker compose up db", Dir: h.dir, Health: Health{Cmd: "pg_isready"}}
	if _, ok := h.s.Approved(spec); ok {
		t.Fatal("approved before anybody said yes")
	}
	if _, err := h.s.Keep("agent-a", spec, PolicyOnFailure, Provenance{}); err != nil {
		t.Fatal(err)
	}
	if policy, ok := h.s.Approved(spec); !ok || policy != PolicyOnFailure {
		t.Fatal("the yes was not banked")
	}
	for name, other := range map[string]Spec{
		"command":       {Command: "docker compose up", Dir: h.dir, Health: Health{Cmd: "pg_isready"}},
		"folder":        {Command: spec.Command, Dir: filepath.Join(h.dir, "other"), Health: Health{Cmd: "pg_isready"}},
		"check command": {Command: spec.Command, Dir: h.dir, Health: Health{Cmd: "pg_isready; curl evil"}},
		"check kind":    {Command: spec.Command, Dir: h.dir, Health: Health{Port: 5432}},
	} {
		if _, ok := h.s.Approved(other); ok {
			t.Fatalf("a different %s rode the same yes", name)
		}
	}
	h.reopen()
	if _, ok := h.s.Approved(spec); !ok {
		t.Fatal("the yes did not survive a restart of codeaf")
	}
}

// TestOneAgentNeverSeesOrTouchesAnothersServices.
func TestOneAgentNeverSeesOrTouchesAnothersServices(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	if rows := h.s.List("agent-b"); len(rows) != 0 {
		t.Fatalf("agent b sees %d services", len(rows))
	}
	for name, act := range map[string]func() error{
		"get":     func() error { _, err := h.s.Get("agent-b", row.ID); return err },
		"stop":    func() error { _, err := h.s.Stop("agent-b", row.ID); return err },
		"restart": func() error { _, err := h.s.Restart("agent-b", row.ID); return err },
		"policy":  func() error { _, err := h.s.SetPolicy("agent-b", row.ID, PolicyNever); return err },
	} {
		if err := act(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s from another agent: %v", name, err)
		}
	}
	if n, _ := h.s.StopOwner("agent-b", "agent closed"); n != 0 || !h.get(row.ID).Running {
		t.Fatal("closing another agent stopped this one's service")
	}
	if n, _ := h.s.StopOwner("agent-a", "agent closed"); n != 1 {
		t.Fatalf("closing the agent stopped %d", n)
	}
	settle(t, h.s)
	if got := h.get(row.ID); got.Running || got.StoppedBy != "agent closed" {
		t.Fatalf("%+v", got)
	}
}

// TestAQuitLeavesItRunningAndTheNextCodeafTakesItBack is re-adoption of the
// exact process, nothing restarted.
func TestAQuitLeavesItRunningAndTheNextCodeafTakesItBack(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	h.advance(10 * time.Second)
	h.reopen()
	got := h.get(row.ID)
	if !got.Running || got.PID != row.PID || !got.ReAdopted || got.State != StateHealthy || h.rt.starts != 1 {
		t.Fatalf("re-adopted the same process, starting nothing: %+v", got)
	}
	// A re-adopted process reports no exit; its check notices instead.
	h.rt.mu.Lock()
	h.rt.procs[row.PID].alive = false
	h.rt.mu.Unlock()
	h.advance(10 * time.Second)
	if got := h.get(row.ID); got.State != StateRestarting || got.Reason != "exited" {
		t.Fatalf("a re-adopted process that vanished is a failure: %+v", got)
	}
}

// TestARecycledPIDIsNeverTakenBackOrSignalled is the identity gate after a
// restart of codeaf: the same pid with another start identity is somebody
// else, left alone, and the service says it is not running.
func TestARecycledPIDIsNeverTakenBackOrSignalled(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyAlways)
	h.s.Close()
	h.rt.mu.Lock()
	stranger := &fakeProc{id: processgroup.Identity{PID: row.PID, Start: 999999, Boot: h.rt.boot}, alive: true}
	h.rt.procs[row.PID] = stranger
	h.rt.mu.Unlock()
	h.open()
	got := h.get(row.ID)
	if got.Running || got.State != StateNotRunning || got.Reason != "not running after restart" || !got.EpisodeOpen {
		t.Fatalf("a recycled pid was taken back: %+v", got)
	}
	if h.rt.starts != 1 {
		t.Fatal("not running after a codeaf restart was started again on its own")
	}
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Remove("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if !stranger.alive || len(stranger.signals) != 0 {
		t.Fatal("the stranger holding the recycled pid was signalled")
	}
}

// TestMissingIdentityIsNotRunning is a record with no identity at all.
func TestMissingIdentityIsNotRunning(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	h.s.Close()
	h.rt.mu.Lock()
	delete(h.rt.procs, row.PID)
	h.rt.mu.Unlock()
	h.open()
	if got := h.get(row.ID); got.State != StateNotRunning || got.Running {
		t.Fatalf("%+v", got)
	}
}

// TestAfterARebootOnlyARestartingPolicyStartsAgain.
func TestAfterARebootOnlyARestartingPolicyStartsAgain(t *testing.T) {
	h := newHarness(t)
	restarting := h.keep("web", PolicyOnFailure)
	never := h.keep("db", PolicyNever)
	stopped := h.keep("seed", PolicyAlways)
	if _, err := h.s.Stop("agent-a", stopped.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	h.s.Close()
	h.rt.mu.Lock()
	h.rt.boot = "boot-2"
	h.rt.procs = map[int]*fakeProc{}
	h.rt.mu.Unlock()
	h.open()
	if got := h.get(restarting.ID); !got.Running || got.StartedHow != StartedAfterRestart {
		t.Fatalf("on_failure after a reboot is started again in its folder: %+v", got)
	}
	if got := h.get(never.ID); got.Running || got.State != StateNotRunning {
		t.Fatalf("never after a reboot is not started: %+v", got)
	}
	if got := h.get(stopped.ID); got.Running || got.State != StateStopped {
		t.Fatalf("a stopped service stays stopped after a reboot: %+v", got)
	}
}

// TestAPortAlreadyAnsweringIsSaidAndNothingIsStarted.
func TestAPortAlreadyAnsweringIsSaidAndNothingIsStarted(t *testing.T) {
	h := newHarness(t)
	h.rt.portsUsed[5173] = true
	row := h.keep("web", PolicyOnFailure)
	if row.State != StateFailed || !row.PortConflict || row.Running || row.Reason != "port 5173 is used by another program" {
		t.Fatalf("%+v", row)
	}
	if h.rt.starts != 0 {
		t.Fatal("a service was started onto a port another program holds")
	}
	h.rt.portsUsed[5173] = false
	if _, err := h.s.Start("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if got := h.get(row.ID); !got.Running || got.PortConflict {
		t.Fatalf("%+v", got)
	}
}

// TestRemoveDropsOnlyAServiceWithNoProcess, and keeps its output: the board's
// "Remove drops it from Processes; its output stays in the chat's notice".
func TestRemoveDropsOnlyAServiceWithNoProcess(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	if err := h.s.Remove("agent-a", row.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("removing a running service: %v", err)
	}
	if err := os.WriteFile(row.LogPath, []byte("ready\nserving\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Stop("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	if err := h.s.Remove("agent-a", row.ID); err != nil {
		t.Fatal(err)
	}
	if len(h.s.List("agent-a")) != 0 {
		t.Fatal("the record survived remove")
	}
	if err := h.s.Remove("agent-a", row.ID); err != nil {
		t.Fatal("removing twice is not idempotent")
	}
	page, err := h.s.Output("agent-a", row.ID, 0, 10)
	if err != nil || len(page.Lines) != 2 || page.Lines[1] != "serving" {
		t.Fatalf("the removed service's output: %+v %v", page, err)
	}
	h.reopen()
	if page, err := h.s.Output("agent-a", row.ID, 0, 10); err != nil || len(page.Lines) != 2 {
		t.Fatalf("the output after a restart of codeaf: %+v %v", page, err)
	}
	if a, err := h.s.Archive("agent-a", row.ID); err != nil || a.Name != "web" {
		t.Fatalf("the archive: %+v %v", a, err)
	}
	for _, wrong := range [][2]string{{"agent-b", row.ID}, {"agent-a", "s99"}, {"agent-a", "../services"}, {"agent-a", "s1/../../x"}, {"", row.ID}} {
		if _, err := h.s.Output(wrong[0], wrong[1], 0, 10); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v read someone's output: %v", wrong, err)
		}
	}
	// The id is never handed out again, so nothing writes over that output.
	next := h.keep("api", PolicyNever)
	if next.ID == row.ID || next.LogPath == row.LogPath {
		t.Fatalf("a removed service's id was reused: %s", next.ID)
	}
}

// TestTheStoreIsPrivateAndHasOneSupervisor.
func TestTheStoreIsPrivateAndHasOneSupervisor(t *testing.T) {
	h := newHarness(t)
	h.keep("web", PolicyOnFailure)
	info, err := os.Stat(filepath.Join(h.dir, storeFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the services file is %v %v", info.Mode(), err)
	}
	dir, _ := os.Stat(h.dir)
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("the services folder is %v", dir.Mode())
	}
	if _, err := Open(Options{Dir: h.dir, Runtime: h.rt, Clock: h.clock}); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second supervisor opened the same store: %v", err)
	}
}

// TestTheSupervisorKeepsOneTimerForEveryService.
func TestTheSupervisorKeepsOneTimerForEveryService(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		h.keep(name, PolicyOnFailure)
	}
	for i := 0; i < 30; i++ {
		h.advance(10 * time.Second)
		if n := h.clock.pending(); n > 1 {
			t.Fatalf("%d timers pending for six services", n)
		}
	}
	if _, err := h.s.Keep("agent-a", Spec{Command: "x", Dir: h.dir, Health: Health{Port: 1}}, PolicyNever, Provenance{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPerOwner; i++ {
		_, _ = h.s.Keep("agent-a", Spec{Command: "y" + string(rune('a'+i)), Dir: h.dir, Health: Health{Port: 2}}, PolicyNever, Provenance{})
	}
	if len(h.s.List("agent-a")) != maxPerOwner {
		t.Fatalf("one agent kept %d services", len(h.s.List("agent-a")))
	}
}

// TestConcurrentActionsSettleConsistently runs every verb at once, many times.
// It is a -race test as much as an outcome test: whatever interleaving wins,
// a stopped service has no process and a running one has exactly one.
func TestConcurrentActionsSettleConsistently(t *testing.T) {
	h := newHarness(t)
	row := h.keep("web", PolicyOnFailure)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(5)
		go func() { defer wg.Done(); _, _ = h.s.Restart("agent-a", row.ID) }()
		go func() { defer wg.Done(); _, _ = h.s.Stop("agent-a", row.ID) }()
		go func() { defer wg.Done(); _, _ = h.s.Start("agent-a", row.ID) }()
		go func() { defer wg.Done(); _, _ = h.s.SetPolicy("agent-a", row.ID, PolicyAlways) }()
		go func() { defer wg.Done(); _ = h.s.List("agent-a"); _ = h.s.Remove("agent-a", row.ID) }()
	}
	wg.Wait()
	settle(t, h.s)
	h.advance(time.Minute)
	rows := h.s.List("agent-a")
	running := h.rt.running()
	switch {
	case len(rows) == 0:
		if len(running) != 0 {
			t.Fatalf("a removed service left %v running", running)
		}
	case rows[0].Running:
		if len(running) != 1 || running[0] != rows[0].PID {
			t.Fatalf("one running service, processes %v, row %+v", running, rows[0])
		}
	default:
		if len(running) != 0 {
			t.Fatalf("a service with no process left %v running: %+v", running, rows[0])
		}
	}
}

// TestCloseLeavesServicesRunningAndStopAllEndsThem is quit's two answers.
func TestCloseLeavesServicesRunningAndStopAllEndsThem(t *testing.T) {
	h := newHarness(t)
	a := h.keep("a", PolicyOnFailure)
	b := h.keep("b", PolicyNever)
	if h.s.Running() != 2 {
		t.Fatal(h.s.Running())
	}
	h.reopen()
	if len(h.rt.running()) != 2 {
		t.Fatal("a quit stopped services")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.s.StopAll(ctx, "quit"); err != nil {
		t.Fatal(err)
	}
	if len(h.rt.running()) != 0 || h.get(a.ID).State != StateStopped || h.get(b.ID).StoppedBy != "quit" {
		t.Fatal("stop all left something running")
	}
}

// TestEventsCarryNoCredentialAndOneLinePerEpisode.
func TestEventsCarryNoCredentialAndOneLinePerEpisode(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var events []Event
	h.s.Subscribe(func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() })
	secret := "sk-or-v1-" + "0123456789abcdef0123456789abcdef0123456789abcdef"
	row, err := h.s.Keep("agent-a", Spec{Name: "api", Command: "OPENROUTER_API_KEY=" + secret + " node api.js", Dir: h.dir, Health: Health{Port: 4010}}, PolicyOnFailure, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	h.rt.exit(t, h.s, row.PID, 1)
	h.advance(2 * time.Second)
	h.rt.exit(t, h.s, h.get(row.ID).PID, 1)
	h.advance(4 * time.Second)
	h.advance(10 * time.Second)
	h.s.Close()
	mu.Lock()
	defer mu.Unlock()
	episodes := map[string]bool{}
	for _, e := range events {
		if strings.Contains(e.Row.Command, secret) {
			t.Fatal("an event carried the command's credential")
		}
		if e.Row.Episode != "" {
			episodes[e.Row.Episode] = true
		}
	}
	if len(episodes) != 1 {
		t.Fatalf("two crashes and a recovery were %d episodes", len(episodes))
	}
	if last := events[len(events)-1].Row; last.State != StateHealthy || last.EpisodeOpen || last.Restarts != 2 {
		t.Fatalf("the last event is the recovery, restarted 2 of 3: %+v", last)
	}
}
