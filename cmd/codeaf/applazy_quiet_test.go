package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/service/servicetest"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/fsnotify/fsnotify"
)

func quietLazyRun(t *testing.T, count int) (*appBackend, *appCrewRun, appCrewRead, *time.Time, map[string]int, *appRecorder) {
	t.Helper()
	b, rec, paths, _ := lazyBackend(t, 1)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	root := filepath.Dir(filepath.Dir(paths[0]))
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("%016x", i+1)
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		b.locked(func() { b.agentLocked(id).transcript = filepath.Join(dir, "transcript.jsonl") })
	}
	file, err := teamstore.Load(b.crewProfile())
	if err != nil {
		t.Fatal(err)
	}
	// Base watches are only installable when the team directories exist.
	if err := os.MkdirAll(filepath.Dir(teamstore.TrafficPath(b.crewProfile(), file.Teams[0].ID)), 0700); err != nil {
		t.Fatal(err)
	}
	run := &appCrewRun{held: map[string][]string{}}
	b.subscribeCrewTraffic(run)
	if run.watcher != nil {
		watcher := run.watcher
		t.Cleanup(func() { _ = watcher.Close() })
	}
	counts := map[string]int{}
	run.stamp = func(path string) string {
		counts[path]++
		return lazyStamp(path)
	}
	return b, run, appCrewRead{file: &teamstore.File{}}, &now, counts, rec
}

func quietLazyVisits(counts map[string]int) map[string]int {
	visits := map[string]int{}
	for path, n := range counts {
		if filepath.Base(path) == "presence.json" {
			visits[filepath.Base(filepath.Dir(path))] += n
		}
	}
	return visits
}

func quietLazyPath(b *appBackend, id string) string {
	var path string
	b.locked(func() { path = b.agents[id].transcript })
	return path
}

// B1/B3/B4: count each startup visit and each bounded cold beat, and require
// complete coverage in every sweep window rather than timing filesystem work.
func TestAppLazyQuietColdSweepBoundAndCoverage(t *testing.T) {
	for _, count := range []int{13, 100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, run, read, now, counts, _ := quietLazyRun(t, count)
			if got := b.neededMembers(run, read, true); len(got) != 0 {
				t.Fatalf("quiet startup admitted %d chats", len(got))
			}
			visits := quietLazyVisits(counts)
			if len(visits) != count {
				t.Fatalf("startup visited %d chats, want %d", len(visits), count)
			}
			for id, n := range visits {
				if n != 1 {
					t.Fatalf("startup stamped %s %d times", id, n)
				}
			}
			budget := config.BudgetConfigPath(b.crewProfile())
			if counts[budget] != 1 || len(counts) != 6*count+1 {
				t.Fatalf("startup: %d budget probes, %d paths; want 1 and %d", counts[budget], len(counts), 6*count+1)
			}
			run.read, run.inputs = read, b.crewInputs(run)
			bound := (count + 29) / 30
			for window := 0; window < 2; window++ {
				seen := map[string]bool{}
				for tick := 0; tick < 30; tick++ {
					clear(counts)
					*now = now.Add(2 * time.Second)
					if b.crewRefresh(run) {
						t.Fatal("quiet tick rebuilt the crew view")
					}
					visits = quietLazyVisits(counts)
					if len(visits) != bound || counts[budget] != 1 || len(counts) != 6*bound+1 {
						t.Fatalf("tick %d: %d chats, %d budget probes, %d paths; want %d, 1, %d", tick, len(visits), counts[budget], len(counts), bound, 6*bound+1)
					}
					for id, n := range visits {
						if n != 1 {
							t.Fatalf("tick stamped %s %d times", id, n)
						}
						seen[id] = true
					}
				}
				if len(seen) != count {
					t.Fatalf("sweep %d visited %d chats, want %d", window, len(seen), count)
				}
			}
			t.Logf("%d cold chats: startup %d local/budget probes; each tick %d; full coverage in 30 ticks", count, 6*count+1, 6*bound+1)
		})
	}
}

// B2/B4: existing crew holds are hot even when no engine was established.
// Cold work is additional and the budget input is still shared once per pass.
func TestAppLazyHeldMembersStayHotWithBoundedColdWork(t *testing.T) {
	b, run, read, now, counts, _ := quietLazyRun(t, 100)
	b.neededMembers(run, read, true)
	for _, id := range []string{"0000000000000001", "0000000000000064"} {
		run.held[id] = []string{"aaaaaaaaaaaa"}
	}
	for range 31 {
		clear(counts)
		*now = now.Add(2 * time.Second)
		b.neededMembers(run, read, false)
		visits := quietLazyVisits(counts)
		if visits["0000000000000001"] != 1 || visits["0000000000000064"] != 1 || len(visits) != 2+4 {
			t.Fatalf("hot + cold visits = %v, want both held chats and 4 cold", visits)
		}
		if counts[config.BudgetConfigPath(b.crewProfile())] != 1 {
			t.Fatal("budget input was not shared")
		}
	}
}

// B2: refresh last-live on every tick; closing stays hot through ten minutes
// after the latest observation, then returns to the bounded cold sweep.
func TestAppLazyRecentlyLiveStaysHotForTenMinutes(t *testing.T) {
	b, run, read, now, counts, _ := quietLazyRun(t, 100)
	b.neededMembers(run, read, true)
	const id = "0000000000000064"
	b.locked(func() { b.agents[id].live = &appLive{} })
	b.neededMembers(run, read, false)
	*now = now.Add(4 * time.Minute)
	b.neededMembers(run, read, false)
	b.locked(func() { b.agents[id].live = nil })
	for range 300 {
		clear(counts)
		*now = now.Add(2 * time.Second)
		b.neededMembers(run, read, false)
		if quietLazyVisits(counts)[id] != 1 {
			t.Fatalf("recently live chat skipped at %s", now)
		}
	}
	visits := 0
	for range 30 {
		clear(counts)
		*now = now.Add(2 * time.Second)
		b.neededMembers(run, read, false)
		visits += quietLazyVisits(counts)[id]
		if len(quietLazyVisits(counts)) != 4 {
			t.Fatal("expired hot chat did not return to the cold bound")
		}
	}
	if visits == 0 || visits == 30 {
		t.Fatalf("expired chat visited %d times, want round-robin cold visits", visits)
	}
}

// B2: failed opens whose work stamp did not change are reconsidered at the
// deadline, including when the chat otherwise belongs to the cold set.
func TestAppLazyRetryDueWithoutFileChanges(t *testing.T) {
	b, run, read, now, counts, _ := quietLazyRun(t, 100)
	const id = "0000000000000064"
	path := quietLazyPath(b, id)
	if err := os.WriteFile(filepath.Join(session.DirOf(path), "presence.json"), []byte(`{"schema":1,"state":"waiting on you","pid":98765}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := b.neededMembers(run, read, true); len(got) != 1 {
		t.Fatal("startup work was not admitted")
	}
	run.unheld = map[string]*appUnheld{id: {next: now.Add(4 * time.Second)}}
	*now = now.Add(2 * time.Second)
	if got := b.neededMembers(run, read, false); len(got) != 0 {
		t.Fatal("failed open retried before its deadline")
	}
	clear(counts)
	*now = now.Add(2 * time.Second)
	if got := b.neededMembers(run, read, false); len(got) != 1 || quietLazyVisits(counts)[id] != 1 {
		t.Fatal("failed open did not retry at its unchanged-stamp deadline")
	}
}

// B3/B5: real recovery work outside a crew waits for its next cold visit,
// even with no watcher or a removed and recreated truth directory.
func TestAppLazyColdSweepAdmitsWorkWithoutFileEvents(t *testing.T) {
	for _, fault := range []string{"missed event", "unavailable watcher", "recreated directory"} {
		t.Run(fault, func(t *testing.T) {
			b, run, read, now, counts, _ := quietLazyRun(t, 100)
			b.neededMembers(run, read, true)
			if fault == "unavailable watcher" && run.watcher != nil {
				_ = run.watcher.Close()
				run.watcher = nil
			}
			const id = "0000000000000064"
			truth := filepath.Dir(quietLazyPath(b, id))
			if fault == "recreated directory" {
				if err := os.RemoveAll(truth); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(truth, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(truth, "tasks.json"), []byte(`{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"recover","brief":"continue","kind":"quick","state":"queued"}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			for tick := 1; tick <= 30; tick++ {
				clear(counts)
				*now = now.Add(2 * time.Second)
				got := b.neededMembers(run, read, false)
				if len(quietLazyVisits(counts)) > 4 {
					t.Fatal("fallback performed a full cold scan")
				}
				if quietLazyVisits(counts)[id] == 0 {
					if len(got) != 0 {
						t.Fatal("work was admitted before its cold visit")
					}
					continue
				}
				if _, ok := got[id]; !ok || len(got) != 1 {
					t.Fatalf("next sweep visit admitted %v, want only %s", got, id)
				}
				return
			}
			t.Fatal("cold work was not admitted within one minute")
		})
	}
}

// B6: history adds no watches, even when every work directory exists. Only
// the base profile, teams directory and open team's traffic directory remain.
func TestAppLazyWatchSetDoesNotGrowWithChatHistory(t *testing.T) {
	for _, count := range []int{13, 100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, run, read, _, _, _ := quietLazyRun(t, count)
			if run.watcher == nil {
				t.Skip("filesystem watching unavailable")
			}
			for _, a := range b.agents {
				if err := os.MkdirAll(filepath.Join(filepath.Dir(a.transcript), "processes"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			file, err := teamstore.Load(b.crewProfile())
			if err != nil {
				t.Fatal(err)
			}
			read.file = file
			b.neededMembers(run, read, true)
			want := map[string]bool{
				filepath.Dir(teamstore.Path(b.crewProfile())):                          true,
				filepath.Join(filepath.Dir(teamstore.Path(b.crewProfile())), "teams"):  true,
				filepath.Dir(teamstore.TrafficPath(b.crewProfile(), file.Teams[0].ID)): true,
			}
			got := run.watcher.WatchList()
			if len(got) != len(want) {
				t.Fatalf("%d chats installed %d watches, want %d", count, len(got), len(want))
			}
			for _, dir := range got {
				if !want[dir] {
					t.Fatalf("unexpected watch: %s", dir)
				}
			}
		})
	}
}

// B1/B3: initializing the view fingerprint on the first heartbeat must
// perform only one cold batch, rather than repeat the startup scan.
func TestAppLazyFirstRefreshUsesOnlyOneColdBatch(t *testing.T) {
	b, run, read, now, counts, _ := quietLazyRun(t, 100)
	b.neededMembers(run, read, true)
	clear(counts)
	run.prints, run.members, run.cursors = map[string]string{}, map[string]string{}, map[string]string{}
	run.acted, run.packets, run.asks = map[string]bool{}, map[string]teamstore.Packet{}, map[string]appwire.Ask{}
	run.opening = map[string]bool{}
	*now = now.Add(2 * time.Second)
	b.crewRefresh(run)
	// The real crew contains one member, leaving 99 cold candidates.
	if got := len(quietLazyVisits(counts)); got != 1+4 {
		t.Fatalf("first heartbeat stamped %d chats, want 1 hot + 4 cold", got)
	}
}

// B2: factory stages and kept services outside a crew must not depend on
// a cold visit or a crew-view metadata change to be selected and admitted.
func TestAppLazyStageAndServiceAreHotOutsideCrews(t *testing.T) {
	for _, signal := range []string{"stage", "service"} {
		t.Run(signal, func(t *testing.T) {
			b, run, read, now, counts, _ := quietLazyRun(t, 100)
			b.neededMembers(run, read, true)
			const id = "0000000000000064"
			if signal == "stage" {
				sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Key: "test-key", Address: "http://127.0.0.1:1"})
				stage, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: quietLazyPath(b, id), Model: "openai/gpt-4.1-mini", Sources: sources, System: "SYSTEM"})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = stage.Close() })
				rememberStageAgent(stage)
				defer forgetStageAgent(stage)
			} else {
				w := &svcWorld{backend: b, rt: servicetest.New(), clock: servicetest.NewClock(*now), dir: t.TempDir(), work: t.TempDir()}
				w.open(t)
				w.keep(t, id, service.PolicyNever)
			}
			for range 2 {
				clear(counts)
				*now = now.Add(2 * time.Second)
				got := b.neededMembers(run, read, false)
				// Admitted directly, as on the base beat: it needs no work stamp.
				if _, ok := got[id]; !ok {
					t.Fatalf("%s was not admitted on the next hot tick", signal)
				}
				// The other 99 stay cold: ceil(99/30) visits per beat.
				if len(quietLazyVisits(counts)) != 4 {
					t.Fatal("hot signal changed the cold work bound")
				}
			}
		})
	}
}

// B5: each old local work input still invalidates the same admission cache,
// for both ordinary journals and journals kept under .cell/.
func TestAppLazyWorkInputsInvalidateHotAndColdAdmission(t *testing.T) {
	for _, cell := range []bool{false, true} {
		for _, hot := range []bool{false, true} {
			for _, name := range []string{"presence.json", "tasks.json", "team.json", "processes", "processes/head.json", "processes.json"} {
				t.Run(fmt.Sprintf("cell=%t/hot=%t/%s", cell, hot, name), func(t *testing.T) {
					b, run, read, now, counts, _ := quietLazyRun(t, 13)
					const id = "000000000000000d"
					if cell {
						b.locked(func() {
							a := b.agents[id]
							a.transcript = filepath.Join(filepath.Dir(a.transcript), ".cell", "transcript.jsonl")
						})
					}
					path := quietLazyPath(b, id)
					changedPath := filepath.Join(filepath.Dir(path), name)
					if name == "presence.json" {
						changedPath = filepath.Join(session.DirOf(path), name)
					}
					changed := false
					run.stamp = func(path string) string {
						counts[path]++
						if changed && path == changedPath {
							return path + ":changed"
						}
						return path
					}
					b.neededMembers(run, read, true)
					if hot {
						run.held[id] = []string{"aaaaaaaaaaaa"}
					}
					// Cache the hot crew's traffic stamp before isolating one input.
					if hot {
						b.neededMembers(run, read, false)
					}
					if err := os.WriteFile(filepath.Join(session.DirOf(path), "presence.json"), []byte(`{"schema":1,"state":"waiting on you","pid":98765}`), 0600); err != nil {
						t.Fatal(err)
					}
					for tick := 0; tick < 30; tick++ {
						*now = now.Add(2 * time.Second)
						if got := b.neededMembers(run, read, false); len(got) != 0 {
							t.Fatal("unchanged stamp bypassed admission cache")
						}
					}
					changed = true
					limit := 30
					if hot {
						limit = 1
					}
					for tick := 0; tick < limit; tick++ {
						clear(counts)
						*now = now.Add(2 * time.Second)
						got := b.neededMembers(run, read, false)
						if quietLazyVisits(counts)[id] == 0 {
							continue
						}
						if _, ok := got[id]; !ok || len(got) != 1 {
							t.Fatalf("changed %s admitted %v, want only %s", name, got, id)
						}
						return
					}
					t.Fatal("changed work input was not visited by its deadline")
				})
			}
		}
	}
}

// B7: an error from the existing watcher still rebuilds the crew view. A
// watcher with no subscriptions isolates this signal from ordinary events.
func TestAppLazyWatcherErrorsStillReconcileCrews(t *testing.T) {
	b, run, _, _, _, rec := quietLazyRun(t, 13)
	run.prints, run.members, run.cursors = map[string]string{}, map[string]string{}, map[string]string{}
	run.acted, run.packets, run.asks = map[string]bool{}, map[string]teamstore.Packet{}, map[string]appwire.Ask{}
	run.opening = map[string]bool{}
	b.crewTick(run, true)
	if run.watcher != nil {
		_ = run.watcher.Close()
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skip("filesystem watching unavailable")
	}
	run.watcher = watcher
	for id := range run.prints {
		run.prints[id] = "stale view"
	}
	run.stop, run.done = make(chan struct{}), make(chan struct{})
	go b.crewLoop(run)
	defer run.close()
	select {
	case watcher.Errors <- fsnotify.ErrEventOverflow:
	case <-time.After(time.Second):
		t.Fatal("crew loop did not consume watcher error")
	}
	rec.waitFor(t, time.Second, func(event string, _ any) bool {
		return event == appwire.EventCrewChanged
	})
}
