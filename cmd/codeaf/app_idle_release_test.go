package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// I1/I2 use the app's real opener, two homes and a loopback relay. Only the
// idle clock is advanced; ordinary watchers and admission remain installed.
func TestAppIdleReleaseI1I2OpenViewKeepsItsFence(t *testing.T) { appIdleReleaseI1I2(t, true) }
func TestAppIdleReleaseI1I2BackgroundChatStaysReleasedUntilSend(t *testing.T) {
	appIdleReleaseI1I2(t, false)
}

func TestAppBackgroundE13B1B3B4StoppedProcessStaysReleasedAcrossRestart(t *testing.T) {
	appIdleReleaseI1I2(t, false, true)
}

func TestAppBackgroundE13B2QueuedTaskAcquiresAndSettlesAtStartup(t *testing.T) {
	celltest.RequireEngine(t)
	var acquires atomic.Int32
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/acquire") {
				acquires.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	a, rec, _, workspace := appSyncBackend(t)
	homeA, _ := outageHomes(t, relay.URL)
	t.Cleanup(func() { _ = a.Close() })
	agent, err := mustCreate(t, a, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := a.Send(agent.ID, "! echo before-recovery", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	local := mustFind(t, a, agent.ID)
	var transcript string
	a.locked(func() { transcript = local.transcript })
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	seedAppE13StoppedProcess(t, transcript)
	checkpoint := filepath.Join(filepath.Dir(transcript), "tasks.json")
	raw, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	job := document["runs"].([]any)[0].(map[string]any)
	job["state"], job["stopped"] = "running", false
	delete(job, "endedAt")
	// This is genuinely runnable recovery, beyond the old command whose
	// process died with its engine. It must retain an automatic opener even
	// after that command's two saved records have been reconciled locally.
	document["seq"] = 2
	document["nodes"] = []any{map[string]any{
		"id": 2, "title": "B2 queued answer", "brief": "Reply with recovered.",
		"state": "queued", "acceptance": "An answer is recorded.",
	}}
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpoint, raw, 0600); err != nil {
		t.Fatal(err)
	}
	acquires.Store(0)
	restarted := e2StartApp(t, homeA)
	defer func() {
		if t.Failed() {
			n, found, err := session.ReadProcessRecord(transcript, 1)
			raw, _ := os.ReadFile(checkpoint)
			t.Logf("B2 startup: acquires=%d process=%+v found=%v err=%v checkpoint=%s", acquires.Load(), n, found, err, raw)
		}
	}()
	appSyncWait(t, "B2 startup recovery acquired and settled the queued task", func() bool {
		n, found, err := session.ReadProcessRecord(transcript, 1)
		if acquires.Load() != 1 || err != nil || !found || n.State != session.JobStopped {
			return false
		}
		for _, task := range session.ReadSessionTasks(transcript) {
			if task.ID == 2 && (task.State == session.TaskDone || task.State == session.TaskFailed || task.State == session.TaskUnverified) && !task.EndedAt.IsZero() {
				t.Logf("B2 queued task recovered as %s: %s", task.State, task.Report)
				return true
			}
		}
		return false
	})
	e2Quit(t, restarted)
}

func appIdleReleaseI1I2(t *testing.T, viewed bool, stopped ...bool) {
	celltest.RequireEngine(t)
	var acquires, releases, beats atomic.Int32
	var watches, devices, vaults atomic.Int32
	leftover := len(stopped) > 0 && stopped[0]
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/watch") {
				watches.Add(1)
			}
			if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/devices/") {
				devices.Add(1)
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/vault") {
				vaults.Add(1)
			}
			if strings.HasSuffix(r.URL.Path, "/acquire") {
				acquires.Add(1)
			}
			if strings.HasSuffix(r.URL.Path, "/release") {
				releases.Add(1)
			}
			if strings.HasSuffix(r.URL.Path, "/heartbeat") {
				beats.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	// Keep the relay setting alive through the parent's registered teardown.
	// outageHomes repeats these values after that teardown has been registered.
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	a, rec, _, workspace := appSyncBackend(t)
	_, homeB := outageHomes(t, relay.URL)
	var idleClock atomic.Int64
	if leftover {
		clock := a.now
		a.now = func() time.Time { return clock().Add(time.Duration(idleClock.Load())) }
	}
	// outageHomes stops spawned stores. Close the parent first so its final
	// save cannot restart a store while the fixture is being dismantled.
	t.Cleanup(func() { _ = a.Close() })
	homeA := home.Dir()
	_, peer, wire := appDeviceBackend(t, homeB)
	t.Setenv(home.EnvVar, homeA)
	a.snapshotCloseClaims()
	run := a.crews()
	a.startCloseWatch()
	agent, err := mustCreate(t, a, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := a.Send(agent.ID, "! echo before-idle", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	own := appSyncOwn(t)
	row := idleReleaseRow(t, wire, agent.ID, func(r appwire.ChatRow) bool { return r.Seen.Head != "" })
	appSyncWait(t, "I1 idle turn settled", func() bool {
		var idle bool
		a.locked(func() { idle = a.agents[agent.ID].live != nil && !a.agents[agent.ID].live.running })
		return idle
	})
	var transcript string
	if leftover {
		// Model the old engine's mismatch after its final checkpoint: the
		// stopped job is in tasks.json, but its process file still says running.
		func() {
			a := mustFind(t, a, agent.ID)
			a.gate.Lock()
			defer a.gate.Unlock()
			live := a.live
			transcript = a.transcript
			release := live.conn.Release
			live.conn.Release = func() {
				release()
				seedAppE13StoppedProcess(t, transcript)
			}
		}()
	}
	if viewed {
		if _, err := a.OpenAgent(agent.ID); err != nil {
			t.Fatal(err)
		}
	}
	peer.waitFor(t, 10*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.ChatChanged)
		return ok && changed.Chat.Cell == agent.ID && changed.Chat.Seen == row.Seen && changed.Chat.Online != nil && *changed.Chat.Online
	})
	peer.mu.Lock()
	cursor := len(peer.events)
	peer.mu.Unlock()
	before, err := own.Dir.Cell(t.Context(), agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	acquires.Store(0)
	releases.Store(0)
	network := [3]int32{watches.Load(), devices.Load(), vaults.Load()}
	for period := 0; period < 2; period++ {
		if leftover {
			idleClock.Add(int64(appAgentIdle + time.Second))
		} else {
			a.locked(func() {
				if live := a.agents[agent.ID].live; live != nil {
					live.lastUse = a.now().Add(-appAgentIdle - time.Second)
				}
			})
		}
		a.sweepOnce()
		a.crewTick(run, false)
		// Live read doors must keep using the existing open conversation.
		if viewed {
			if _, err := a.ListSkills(agent.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := a.GetAutonomy(agent.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := a.ConversationPoints(agent.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := a.ListAgents(""); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.Entries(agent.ID, "", appwire.MaxEntries); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ListTasks(agent.ID); err != nil {
			t.Fatal(err)
		}
		if !viewed && period == 0 {
			peer.waitFor(t, 10*time.Second, func(_ string, data any) bool {
				changed, ok := data.(appwire.ChatChanged)
				return ok && changed.Chat.Cell == agent.ID && changed.Chat.Status == "idle"
			})
		}
		// Give the automatic opener and directory follower time to react.
		time.Sleep(250 * time.Millisecond)
		v, err := own.Dir.Cell(t.Context(), agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		wantReleases := int32(1)
		if viewed {
			wantReleases = 0
		}
		live := v.Cell.Lease.Expires > v.Now
		if live != viewed || v.Cell.Lease.Fence != row.Seen.Fence || acquires.Load() != 0 || releases.Load() != wantReleases {
			t.Fatalf("I1/I2 period %d: lease=%+v acquires=%d releases=%d", period+1, v.Cell.Lease, acquires.Load(), releases.Load())
		}
		if leftover {
			if got := [3]int32{watches.Load(), devices.Load(), vaults.Load()}; got != network {
				t.Fatalf("B4 idle release restarted sync traffic: before=%v after=%v (watch/device/vault)", network, got)
			}
		}
	}
	if viewed {
		appSyncWait(t, "I1 open idle hold to renew beyond its original deadline", func() bool {
			v, err := own.Dir.Cell(t.Context(), agent.ID)
			return err == nil && v.Cell.Lease.Expires > before.Cell.Lease.Expires
		})
		if acquires.Load() != 0 || releases.Load() != 0 {
			t.Fatalf("I1 renewal acquired=%d released=%d", acquires.Load(), releases.Load())
		}
	}
	peer.mu.Lock()
	changes := 0
	for _, data := range peer.data[cursor:] {
		if changed, ok := data.(appwire.ChatChanged); ok && changed.Chat.Cell == agent.ID {
			changes++
		}
	}
	peer.mu.Unlock()
	wantChanges := 1
	if viewed {
		wantChanges = 0
	}
	if changes != wantChanges {
		t.Fatalf("I2 idle chat emitted %d changes, want %d", changes, wantChanges)
	}
	t.Logf("I1/I2 viewed=%v: idle acquires=%d releases=%d heartbeat requests=%d row changes=%d fence=%d", viewed, acquires.Load(), releases.Load(), beats.Load(), changes, row.Seen.Fence)
	if leftover {
		if session.NeedsProcessRecovery(transcript) {
			t.Fatal("B1 settled process still demands recovery after the sweep")
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		// A fresh engine must also leave the known ending closed without acquiring.
		seedAppE13StoppedProcess(t, transcript)
		restarted := e2StartApp(t, homeA)
		for range 2 {
			time.Sleep(250 * time.Millisecond)
			v, err := own.Dir.Cell(t.Context(), agent.ID)
			if err != nil || v.Cell.Lease.Fence != row.Seen.Fence || acquires.Load() != 0 || releases.Load() != 1 {
				t.Fatalf("B1/B3 restart acquired: lease=%+v acquires=%d releases=%d err=%v", v.Cell.Lease, acquires.Load(), releases.Load(), err)
			}
		}
		e2Turn(t, restarted, agent.ID, "send after background restart")
		newRow := idleReleaseRow(t, wire, agent.ID, func(r appwire.ChatRow) bool { return r.Seen.Fence == row.Seen.Fence+1 && r.Seen.Head != row.Seen.Head })
		if acquires.Load() != 1 || newRow.Holder == nil || newRow.Holder.ID != own.Device.ID() {
			t.Fatalf("B3 send after restart: row=%+v acquires=%d", newRow, acquires.Load())
		}
		return
	}
	turn, err = a.Send(agent.ID, "! echo after-idle", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	wantFence, wantAcquires := row.Seen.Fence+1, int32(1)
	if viewed {
		wantFence, wantAcquires = row.Seen.Fence, 0
	}
	newRow := idleReleaseRow(t, wire, agent.ID, func(r appwire.ChatRow) bool { return r.Seen.Fence == wantFence && r.Seen.Head != row.Seen.Head })
	if acquires.Load() != wantAcquires || newRow.Holder == nil || newRow.Holder.ID != own.Device.ID() {
		t.Fatalf("I1 send: row=%+v acquires=%d", newRow, acquires.Load())
	}
}

func seedAppE13StoppedProcess(t *testing.T, transcript string) {
	t.Helper()
	dir := filepath.Dir(transcript)
	if err := os.MkdirAll(filepath.Join(dir, "processes"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"processes/head.json": `{"epoch":"e13","high":1}`,
		"processes/1.json":    `{"ID":1,"Command":"say hi","Kind":"command","State":"running","Started":"2026-10-27T23:58:18Z","Elapsed":379625,"ToolCallID":"call_e13","StartedBy":"chat"}`,
		"tasks.json":          `{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"kind":"job","title":"say hi","state":"failed","stopped":true,"report":"job 1","job_command":"say hi","job_call_id":"call_e13","startedAt":"2026-10-27T23:58:18Z","endedAt":"2026-10-27T23:58:18.000379625Z"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func idleReleaseRow(t *testing.T, wire *actionWire, id string, ready func(appwire.ChatRow) bool) appwire.ChatRow {
	t.Helper()
	var found appwire.ChatRow
	appSyncWait(t, "I1/I2 peer row "+id, func() bool {
		for _, row := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if row.Cell == id && ready(row) {
				found = row
				return true
			}
		}
		return false
	})
	return found
}

func TestAppIdleReleaseI1WorkDoorsStillOpenTheConversation(t *testing.T) {
	for _, door := range []string{"send", "queue", "steer", "retry", "close then open"} {
		t.Run(door, func(t *testing.T) {
			var opens atomic.Int32
			conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
			rec := newAppRecorder()
			b := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				opens.Add(1)
				return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
			})
			t.Cleanup(func() { _ = b.Close() })
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if door == "retry" {
				conv.mu.Lock()
				conv.script = []session.Event{{Kind: session.EventError, Err: errors.New("try again")}}
				conv.mu.Unlock()
				turn, err := b.Send(agent.ID, "work", "")
				if err != nil {
					t.Fatal(err)
				}
				rec.waitFor(t, time.Second, func(_ string, data any) bool {
					done, ok := data.(appwire.TurnDone)
					return ok && done.Turn == turn && !done.OK
				})
				appSyncWait(t, "I1 failed turn idle", func() bool {
					var idle bool
					b.locked(func() { idle = !b.agents[agent.ID].live.running })
					return idle
				})
				conv.mu.Lock()
				conv.script = []session.Event{{Kind: session.EventTurnDone}}
				conv.mu.Unlock()
			}
			b.locked(func() { b.agents[agent.ID].live.lastUse = b.now().Add(-appAgentIdle - time.Second) })
			b.sweepOnce()
			if opens.Load() != 1 {
				t.Fatalf("I1 view reopened %d times", opens.Load())
			}
			switch door {
			case "send":
				_, err = b.Send(agent.ID, "work", "")
			case "queue":
				_, err = b.Queue(agent.ID, "work", "")
			case "steer":
				err = b.Steer(agent.ID, "work", "")
				// Steering requires a running turn; an idle refusal does not
				// admit work or need a writer.
				if wireCode(err) != appwire.CodeBusy || opens.Load() != 1 {
					t.Fatalf("I1 idle steer: %d opens, %v", opens.Load(), err)
				}
				return
			case "retry":
				_, err = b.Retry(agent.ID)
			case "close then open":
				err = b.CloseAgent(agent.ID)
				if err == nil {
					_, err = b.OpenAgent(agent.ID)
				}
			}
			if err != nil || opens.Load() != 2 {
				t.Fatalf("I1 %s: %d opens, %v", door, opens.Load(), err)
			}
		})
	}
}
