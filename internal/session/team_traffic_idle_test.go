package session

import (
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
)

// C1: idle chats share the timer's lifetime rather than each keeping a goroutine.
func TestTeamTrafficNoTeamChatsShareOneWatch(t *testing.T) {
	profile := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		before := runtime.NumGoroutine()
		var agents []*Agent
		defer func() {
			for _, a := range agents {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			}
			time.Sleep(2 * teamWatchInterval())
			synctest.Wait()
			if extra := runtime.NumGoroutine() - before; extra > 0 {
				t.Errorf("closed chats retained %d watch goroutines", extra)
			}
		}()
		for i := range 10 {
			a := &Agent{config: Config{ProfileDir: profile, SessionFile: filepath.Join(profile, string(rune('a'+i)), "transcript.jsonl")}}
			agents = append(agents, a)
			a.watchTeamTraffic(profile)
		}
		synctest.Wait()
		if extra := runtime.NumGoroutine() - before; extra > 1 {
			t.Fatalf("ten no-team chats retained %d watch goroutines; want at most one shared watch", extra)
		}
	})
}

// C2: membership made by another window still reaches an already open chat.
func TestTeamTrafficMembershipAddedWhileOpenStillWakes(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	restore := teamstest.Hide(t, fixture.profile)
	completer := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, completer, nil)
	quietFor()
	restore()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Check the header."})
	waitRequests(t, completer, 1)
}

// C2: each affected conversation keeps its own progress when another is blocked.
func TestTeamTrafficBlockedMemberDoesNotDelayAnother(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	blocked := teamAgent(t, fixture, fixture.web, oneAnswer(1), nil)
	other := oneAnswer(1)
	teamAgent(t, fixture, fixture.parser, other, nil)
	blocked.mu.Lock()
	defer blocked.mu.Unlock()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "parser", Text: "Check the parser."})
	waitRequests(t, other, 1)
}

// F4: the shared watch reports a deadline within the unchanged one-second bound.
func TestTeamTrafficWrapDeadlineWithinCadence(t *testing.T) {
	stubCapSpend(t, &capSpend{stamp: "deadline"})
	synctest.Test(t, func(t *testing.T) {
		fixture := newTeamFixture(t, true)
		started := time.Now()
		bound := 1500 * time.Millisecond
		if err := teams.Update(fixture.profile, func(f *teams.File) error { return f.SetWrap(fixture.teamID, started, bound) }); err != nil {
			t.Fatal(err)
		}
		manager := &Agent{config: Config{ProfileDir: fixture.profile, SessionFile: fixture.manager}}
		manager.watchTeamTraffic(fixture.profile)
		defer manager.Close()
		synctest.Wait()
		time.Sleep(bound - time.Nanosecond)
		synctest.Wait()
		if packets, _, err := teams.OpenPackets(fixture.profile, teams.Person); err != nil || len(packets) != 0 {
			t.Fatalf("early report: %d, %v", len(packets), err)
		}
		time.Sleep(time.Second + time.Nanosecond)
		synctest.Wait()
		p := onlyPacket(t, fixture.profile, teams.Person)
		if p.Kind != teams.PacketClosing || p.Report == nil || !p.Report.Incomplete {
			t.Fatalf("missing incomplete report by deadline + 1 s: %+v", p)
		}
	})
}

// F4: a closed subscriber's in-flight check cannot strand its replacement.
func TestTeamTrafficCloseReopenDuringCheck(t *testing.T) {
	stubCapSpend(t, &capSpend{stamp: "reopen"})
	synctest.Test(t, func(t *testing.T) {
		fixture := newTeamFixture(t, true)
		started := time.Now()
		bound := 1500 * time.Millisecond
		if err := teams.Update(fixture.profile, func(f *teams.File) error { return f.SetWrap(fixture.teamID, started, bound) }); err != nil {
			t.Fatal(err)
		}
		old := &Agent{config: Config{ProfileDir: fixture.profile, SessionFile: fixture.manager}}
		old.watchTeamTraffic(fixture.profile)
		defer old.Close()
		synctest.Wait()
		entered, resume := make(chan struct{}), make(chan struct{})
		original := teamSpendStamp
		var block atomic.Bool
		teamSpendStamp = func(profile, team, day string) string {
			if block.CompareAndSwap(false, true) {
				close(entered)
				<-resume
			}
			return "changed|" + team
		}
		defer func() { teamSpendStamp = original }()
		time.Sleep(time.Second)
		synctest.Wait()
		select {
		case <-entered:
		default:
			t.Fatal("shared watch did not begin its check")
		}
		if err := old.Close(); err != nil {
			t.Fatal(err)
		}
		reopened := &Agent{config: Config{ProfileDir: fixture.profile, SessionFile: fixture.manager}}
		reopened.watchTeamTraffic(fixture.profile)
		defer reopened.Close()
		synctest.Wait()
		// Keep the old check blocked while the replacement crosses the deadline.
		time.Sleep(bound)
		synctest.Wait()
		p := onlyPacket(t, fixture.profile, teams.Person)
		if p.Report == nil || !p.Report.Incomplete {
			t.Fatalf("reopened watch missed deadline + 1 s: %+v", p)
		}
		close(resume)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if packets, _, err := teams.OpenPackets(fixture.profile, teams.Person); err != nil || len(packets) != 1 {
			t.Fatalf("stale check duplicated report: %d, %v", len(packets), err)
		}
		teamTraffic.mu.Lock()
		subscribed := teamTraffic.run != nil && teamTraffic.run.clients[reopened] != nil
		teamTraffic.mu.Unlock()
		if !subscribed {
			t.Fatal("old check removed reopened subscriber")
		}
	})
}
