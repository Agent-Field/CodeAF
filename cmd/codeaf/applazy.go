package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/fsnotify/fsnotify"
)

// subscribeCrewTraffic installs the shared watch before the first pending-work
// check. Directory watches survive log replacement; the existing crew beat
// remains the fallback after an overflow or a watch that could not be opened.
func (b *appBackend) subscribeCrewTraffic(run *appCrewRun) {
	run.tick.Lock()
	defer run.tick.Unlock()
	run.since = b.now()
	run.wakeStamps = map[string]string{}
	run.mu.Lock()
	run.recovering = map[string]bool{}
	// Recovery admits paid-capable work before opening its checkpoint. These
	// records stay on that road even if its timer and the crew look overlap.
	r := &b.closeRecovery
	r.mu.Lock()
	for path, claim := range r.claims {
		if !claim.presence.Fresh(b.now()) || !session.InUse(session.Place{Dir: path}.Transcript()) {
			run.recovering[filepath.Base(path)] = true
		}
	}
	r.mu.Unlock()
	_ = b.state.read(func(f *appStateFile) {
		for id := range f.ClosePending {
			run.recovering[id] = true
		}
		for _, id := range f.ResumeAfterQuit {
			run.recovering[id] = true
		}
	})
	run.mu.Unlock()
	run.watcher, _ = fsnotify.NewWatcher()
	if run.watcher != nil {
		_ = run.watcher.Add(filepath.Dir(teamstore.Path(b.crewProfile())))
		_ = run.watcher.Add(filepath.Join(filepath.Dir(teamstore.Path(b.crewProfile())), "teams"))
	}
}

// neededMembers is the one automatic-open decision. Membership alone keeps a
// light record; presence, recovery or unread wake traffic needs a conversation.
// The per-agent gate in ensure still arbitrates every automatic and UI open.
func (b *appBackend) neededMembers(run *appCrewRun, read appCrewRead, first bool) map[string][]string {
	candidates := b.lazyCandidates(run, read, first)
	needed := map[string][]string{}
	profile := b.crewProfile()
	shared := ""
	traffic := map[string]string{}
	for id, row := range candidates {
		var since time.Time
		var crewHeld bool
		b.locked(func() { since, crewHeld = b.agents[id].listening, b.agents[id].crewHeld })
		// A live stage and an established crew hold can change without a disk
		// write. Neither may wait for the persisted work stamp to move.
		_, staged := liveStageFor(row.Transcript)
		if staged || crewHeld && len(run.held[id]) > 0 || b.serviceKeeps(id) {
			needed[id] = run.held[id]
			continue
		}
		if shared == "" {
			shared = teamstore.Stamp(profile) + run.workStamp(config.BudgetConfigPath(profile))
		}
		for _, crew := range run.held[id] {
			if _, ok := traffic[crew]; !ok {
				traffic[crew] = teamstore.TrafficStamp(profile, crew)
			}
		}
		stamp := shared + run.lazyWorkStamp(row.Transcript, run.held[id], traffic)
		// A failed open remains due even when its input files did not change.
		retry := false
		run.locked(func() { retry = run.unheld[id] != nil })
		if run.wakeStamps[id] == stamp && !retry {
			continue
		}
		run.wakeStamps[id] = stamp
		if b.needsLazyOpen(run, read, row, since) {
			needed[id] = run.held[id]
		}
	}
	return needed
}

// needsLazyOpen keeps a recovery refusal settled. Only new traffic, a live
// foreign holder or a person's explicit open may revisit that conversation;
// its old working presence and checkpoint must not undo the crash breaker.
func (b *appBackend) needsLazyOpen(run *appCrewRun, read appCrewRead, row session.SessionRow, since time.Time) bool {
	if session.TeamOpenPending(b.crewProfile(), row.Transcript, read.file, read.defaults, since) {
		return true
	}
	recovered := false
	run.locked(func() { pending, known := run.recovering[row.ID]; recovered = known && !pending })
	presence, _ := session.ReadSessionPresenceAnyAge(row.Dir)
	if recovered && !(presence.Fresh(b.now()) && session.InUse(row.Transcript)) {
		return false
	}
	busy := presence.State == session.PresenceWorking || presence.NeedsPerson() || len(presence.RunningTasks) > 0 || len(presence.Jobs) > 0
	return busy || b.serviceKeeps(row.ID) || session.NeedsTaskRecovery(row.Transcript) || session.NeedsProcessRecovery(row.Transcript)
}

// lazyCandidates reuses the world and session index. A later crew look uses
// records already remembered here rather than walking the places again.
func (b *appBackend) lazyCandidates(run *appCrewRun, read appCrewRead, first bool) map[string]session.SessionRow {
	if first {
		for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
			if !row.DeletionPending {
				b.remember(row)
			}
		}
	}
	for _, team := range read.file.Teams {
		if team.Closed() {
			continue
		}
		if run.watcher != nil {
			_ = run.watcher.Add(filepath.Dir(teamstore.TrafficPath(b.crewProfile(), team.ID)))
		}
		for _, member := range team.Members {
			b.rememberLazyMember(appMemberAgent(member))
		}
	}
	startup := first || !run.lazyStarted
	run.lazyStarted = true
	now := b.now()
	hot := map[string]bool{}
	stageLends.mu.Lock()
	for agent := range stageLends.agents {
		hot[appAgentID(agent.SessionPath())] = true
	}
	stageLends.mu.Unlock()
	if svc, err := b.servicesOf(); err == nil {
		for _, row := range svc.sup.All() {
			if row.Running || row.Stopping || row.State == service.StateStarting || row.State == service.StateRestarting {
				hot[row.Owner] = true
			}
		}
	}
	recovering := map[string]bool{}
	run.locked(func() {
		for id, crews := range run.held {
			if len(crews) > 0 {
				hot[id] = true
			}
		}
		for id, u := range run.unheld {
			if u.healed || !now.Before(u.next) {
				hot[id] = true
			}
		}
		for id, waiting := range run.recovering {
			recovering[id] = waiting
		}
	})
	candidates := map[string]session.SessionRow{}
	var cold []string
	b.locked(func() {
		for id, a := range b.agents {
			if a.live != nil {
				if len(run.held[id]) > 0 {
					a.crewHeld = true
				}
			}
			if !a.lastLive.IsZero() && !now.After(a.lastLive.Add(appLazyHotWindow)) {
				hot[id] = true
			}
			if a.live != nil || a.transcript == "" || recovering[id] {
				continue
			}
			if a.listening.IsZero() {
				a.listening = run.since
			}
			if startup || hot[id] {
				candidates[id] = session.SessionRow{ID: id, Dir: session.DirOf(a.transcript), Transcript: a.transcript}
			} else {
				cold = append(cold, id)
			}
		}
		// A key cursor survives agents joining or leaving the cold set without
		// restarting the sweep at the beginning and starving later chats.
		sort.Strings(cold)
		start := sort.Search(len(cold), func(i int) bool { return cold[i] > run.coldAfter })
		beats := int(appLazyColdSweep / appCrewPoll)
		for i := 0; i < (len(cold)+beats-1)/beats; i++ {
			id := cold[(start+i)%len(cold)]
			a := b.agents[id]
			candidates[id] = session.SessionRow{ID: id, Dir: session.DirOf(a.transcript), Transcript: a.transcript}
			run.coldAfter = id
		}
	})
	return candidates
}

func (run *appCrewRun) workStamp(path string) string {
	if run.stamp != nil {
		return run.stamp(path)
	}
	return lazyStamp(path)
}

func (run *appCrewRun) lazyWorkStamp(transcript string, crews []string, traffic map[string]string) string {
	stamp := run.workStamp(filepath.Join(session.DirOf(transcript), "presence.json"))
	// Presence belongs to the folder; task and process records stay beside
	// the journal, including inside .cell/.
	truth := filepath.Dir(transcript)
	for _, name := range []string{"tasks.json", "team.json", "processes", "processes/head.json", "processes.json"} {
		stamp += run.workStamp(filepath.Join(truth, name))
	}
	for _, crew := range crews {
		stamp += traffic[crew]
	}
	return stamp
}

// lazyStamp changes on replacement as well as writes, without reading history.
func lazyStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return path + ":" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

func crewTrafficEvent(event fsnotify.Event) bool {
	if strings.HasSuffix(event.Name, "traffic.jsonl") || filepath.Base(event.Name) == teamstore.FileName {
		return true
	}
	if event.Has(fsnotify.Create) {
		info, err := os.Stat(event.Name)
		return err == nil && info.IsDir()
	}
	return false
}

// allowLazyOpen ends recovery's exclusive startup admission. Later traffic is
// still allowed to open a record whose recovery chose to leave it resting.
func (b *appBackend) allowLazyOpen(id string) {
	var run *appCrewRun
	b.locked(func() { run = b.crewRun })
	if run != nil {
		run.tick.Lock()
		defer run.tick.Unlock()
		run.locked(func() { run.recovering[id] = false })
		delete(run.wakeStamps, id)
	}
}

// rememberLazyMember consults the index once. An unchanged idle crew does not
// reread each member's presence before the work stamp has even been checked.
func (b *appBackend) rememberLazyMember(id string) {
	known := false
	b.locked(func() { known = b.agents[id] != nil && b.agents[id].transcript != "" })
	if !known {
		_, _ = b.find(id)
	}
}

// findCrewOwner restores the owner-backed answer an open crew had before its
// idle members became light records. Other closed conversations stay closed.
func (b *appBackend) findCrewOwner(id string) (*appAgent, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, err
	}
	// A conversation already held, or being opened, is not opened again: the
	// open holds the agent's gate for as long as it takes, and a caller that
	// only reads or stops answers from what is there instead of queueing.
	// At quit nothing is opened again either, and the gates are the wrap-up's.
	if b.crewKeeps(id) && !b.held(id) && !b.isClosing() {
		_, _, err = b.heldLive(id)
	}
	return a, err
}
