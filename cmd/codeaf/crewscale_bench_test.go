package main

// crewscale_bench_test.go: what the crews' hot paths cost as crews pile up.
//
// Every factory job adds a crew and none goes away, so a store a year old
// holds thousands. The paths below run on a beat or on every turn; each is
// timed over a store of 15 crews and one of 10,000, and should cost about the
// same in both:
//
//	go test -run '^$' -bench CrewScale -benchtime 20x ./cmd/codeaf/

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/teams/teamstest"
)

// crewScaleSizes are the stores the hot paths are timed over.
var crewScaleSizes = []int{15, 10_000}

// seedCrewScale fills a profile with n crews shaped like a working store:
// a dozen of the person's own, and the rest factory rooms, each with three
// stage chats, under the one factory team. The last three rooms are live jobs;
// every earlier one's job has ended, and its room is closed.
func seedCrewScale(tb testing.TB, profile string, n int) {
	tb.Helper()
	made := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	member := func(team, who string) teamstore.Member {
		// Every conversation has a folder of its own, named by its id.
		key := filepath.Join("/scale/state/v3/projects/-Users-someone-work-repo", team+"-"+who, "transcript.jsonl")
		return teamstore.Member{Key: key, File: key, Where: "/scale/work/repo", Word: who + " stage of the item", Handle: who, HandleBy: teamstore.HandleByTyped}
	}
	list := []teamstore.Team{{ID: "factory00000", Name: "factory", Made: made, FactoryManaged: true}}
	for i := 0; i < n-1; i++ {
		id := fmt.Sprintf("crew%08d", i)
		t := teamstore.Team{ID: id, Name: fmt.Sprintf("#%d · review", i), Made: made.Add(time.Duration(i) * time.Minute)}
		if i >= 12 {
			t.Parent, t.FactoryManaged = "factory00000", true
			if i < n-4 {
				t.State, t.ClosedAt = teamstore.TeamClosed, made
			}
		}
		for _, who := range []string{"build", "review", "land"} {
			t.Members = append(t.Members, member(id, who))
		}
		if i < 12 {
			t.Manager = t.Members[0].Key
		}
		t.SetHue(teamstore.HueSpec{Hue: float64(i % 360)})
		list = append(list, t)
	}
	teamstest.WriteTeams(tb, profile, list)
	// The rows were written as an earlier build would; one write names them.
	if err := teamstore.Update(profile, func(*teamstore.File) error { return nil }); err != nil {
		tb.Fatal(err)
	}
}

func BenchmarkCrewScale(b *testing.B) {
	for _, n := range crewScaleSizes {
		profile := b.TempDir()
		seedCrewScale(b, profile, n)
		backend := newFakeBackend(newAppRecorder(), nil)
		backend.profileDir = profile
		backend.factoryRooms.Do(func() {})
		// The watch as the app runs it, without its beat goroutine.
		run := &appCrewRun{
			ready: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
			prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{},
			acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{},
			held: map[string][]string{}, opening: map[string]bool{},
		}
		backend.crewTick(run, true)
		b.Run(fmt.Sprintf("crewTick/%d", n), func(b *testing.B) {
			for range b.N {
				backend.crewTick(run, false)
			}
		})
		b.Run(fmt.Sprintf("keeperPoll/%d", n), func(b *testing.B) {
			backend.crewRefresh(run)
			b.ResetTimer()
			for range b.N {
				backend.crewRefresh(run)
			}
		})
		b.Run(fmt.Sprintf("agentSeat/%d", n), func(b *testing.B) {
			agent := appMemberAgent(teamstore.Member{Key: filepath.Join("/scale/state/v3/projects/-Users-someone-work-repo", "crew00000003-review", "transcript.jsonl")})
			for range b.N {
				if _, _, ok := backend.crewSeatOf(agent); !ok {
					b.Fatal("no seat")
				}
			}
		})
		b.Run(fmt.Sprintf("overviewFirstPage/%d", n), func(b *testing.B) {
			for range b.N {
				if _, err := backend.CrewsOverview(context.Background(), appwire.OverviewAsk{}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("updateOneCrew/%d", n), func(b *testing.B) {
			for i := range b.N {
				if err := teamstore.UpdateOpen(profile, func(f *teamstore.File) error {
					f.Teams[teamstore.Index(f.Teams, "crew00000003")].Name = fmt.Sprint("renamed ", i)
					return nil
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
