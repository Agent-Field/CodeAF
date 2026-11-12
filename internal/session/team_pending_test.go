package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestLazyTeamWakeOpensAfterTrafficAndDeliversIt(t *testing.T) {
	for _, manager := range []bool{false, true} {
		name := "member"
		if manager {
			name = "manager"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newWakingTeamFixture(t)
			since := time.Now()
			path := fixture.web
			entry := teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "pending work"}
			if manager {
				path = fixture.manager
				entry = teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToManager, Text: "pending work"}
			}
			appendTraffic(t, fixture, entry)
			file, err := teams.Load(fixture.profile)
			if err != nil {
				t.Fatal(err)
			}
			if !TeamOpenPending(fixture.profile, path, file, teams.DefaultsAt(fixture.profile), since) {
				t.Fatal("unopened conversation lost its pending wake")
			}
			completer := oneAnswer(2)
			agent := teamAgent(t, fixture, path, completer, func(c *Config) { c.TeamListeningSince = since; c.teamWatchManual = true })
			// Advance the manager's coalescing deadline deterministically. Both
			// roles must run a real turn carrying the traffic that preceded assembly.
			now := time.Now()
			agent.teamWatchTick(fixture.profile, now)
			agent.teamWatchTick(fixture.profile, now.Add(teamWakeDelay()+time.Nanosecond))
			waitRequests(t, completer, 1)
			if words := userTextIn(completer.request(0)); !strings.Contains(words, "pending work") {
				t.Fatalf("opened turn lost traffic: %s", words)
			}
			waitIdle(t, agent)
			if TeamOpenPending(fixture.profile, path, file, teams.DefaultsAt(fixture.profile), since) {
				t.Fatal("delivered traffic still asks for an open")
			}
		})
	}
}

func TestLazyTeamTrafficOpensWithWakeOffWithoutStartingATurn(t *testing.T) {
	fixture := newTeamFixture(t, true)
	since := time.Now()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "wait"})
	file, _ := teams.Load(fixture.profile)
	if !TeamOpenPending(fixture.profile, fixture.web, file, teams.DefaultsAt(fixture.profile), since) {
		t.Fatal("wake-off crew lost traffic for an unopened member")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.web), placeTeamCursors)); !os.IsNotExist(err) {
		t.Fatalf("light probe wrote a delivery cursor: %v", err)
	}
	completer := oneAnswer(1)
	agent := teamAgent(t, fixture, fixture.web, completer, func(c *Config) { c.TeamListeningSince = since; c.teamWatchManual = true })
	agent.teamWatchTick(fixture.profile, time.Now())
	if got := completer.requests(); got != 0 {
		t.Fatalf("wake-off traffic started %d requests", got)
	}
	if news := agent.teamBoundary(); !strings.Contains(news, "wait") {
		t.Fatalf("wake-off traffic was not kept for delivery: %s", news)
	}
}

func TestLazyTaskRecoveryKeepsOwedAndParkedWork(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		noted, want bool
	}{
		{"parked", "queued", false, true}, {"running", "running", false, true},
		{"owed", "done", false, true}, {"settled", "done", true, false},
		{"pending decision", "unverified", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), placeTranscript)
			noted := "false"
			if tc.noted {
				noted = "true"
			}
			raw := `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"work","brief":"do it","kind":"quick","state":"` + tc.state + `","noted":` + noted + `}]}`
			if err := os.WriteFile(taskCheckpointPath(path), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := NeedsTaskRecovery(path); got != tc.want {
				t.Fatalf("needs open = %v, want %v", got, tc.want)
			}
		})
	}
}
