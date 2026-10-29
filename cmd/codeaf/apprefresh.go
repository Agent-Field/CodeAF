package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/config"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// appInputStamp looks only at metadata. Capture it BEFORE reading the input,
// so a concurrent replacement is either included now or noticed next time.
func appInputStamp(paths ...string) string {
	var out strings.Builder
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(&out, "%s:%v|", path, err)
			continue
		}
		fmt.Fprintf(&out, "%s:%d:%d|", path, info.Size(), info.ModTime().UnixNano())
	}
	return out.String()
}

// appOrdersStamp includes the WAL: a committed SQLite change need not touch
// the database file until a checkpoint, including writes from another process.
func appOrdersStamp(store *automation.Store) string {
	path := filepath.Join(store.Root(), automation.DatabaseName)
	return appInputStamp(path, path+"-wal")
}

// crewInputs covers disk inputs and the live member facts that do not travel
// through disk. A day boundary also changes the spending view without a write.
func (b *appBackend) crewInputs(run *appCrewRun) string {
	profile := b.crewProfile()
	// One builder for the whole stamp: adding to a string per crew copied the
	// stamp so far each time, which grew with the square of the crews.
	var stamp strings.Builder
	stamp.WriteString(teamstore.Today() + "|" + appInputStamp(teamstore.Path(profile), config.ProfilePath(profile, "config.json"), b.crewLayer().path, b.state.where(), teamstore.UsageLedgerPath()))
	var teams []teamstore.Team
	run.locked(func() {
		teams = run.teams
		cursors, _ := json.Marshal(run.cursors)
		stamp.WriteString("|" + string(cursors))
	})
	for _, team := range teams {
		stamp.WriteString("|" + appInputStamp(teamstore.TrafficPath(profile, team.ID), teamstore.DecisionsPath(profile, team.ID)))
		if team.Parent != "" {
			stamp.WriteString("|" + appInputStamp(teamstore.TrafficPath(profile, team.Parent)))
		}
	}
	live := map[string]any{}
	b.locked(func() {
		for _, team := range teams {
			for _, member := range team.Members {
				id := appMemberAgent(member)
				if a := b.agents[id]; a != nil {
					live[id] = []any{a.view, a.failed, agentLineLocked(a, a.view.State), b.stoppedLocked(a), a.live != nil, a.opening}
				}
			}
		}
	})
	raw, _ := json.Marshal(live)
	return stamp.String() + "|" + string(raw)
}

// crewRefresh leaves the operational watch alive while the client is open,
// but does not rebuild a quiet view. Failed holds retain their existing retry
// schedule even when none of the view's files changed.
func (b *appBackend) crewRefresh(run *appCrewRun) bool {
	run.tick.Lock()
	defer run.tick.Unlock()
	stamp := b.crewInputs(run)
	var same, retry bool
	run.locked(func() {
		same = run.inputs == stamp
		for _, u := range run.unheld {
			if (!u.said && !u.quiet) || u.healed || !b.now().Before(u.next) {
				retry = true
			}
		}
	})
	if same && !retry {
		// Live stages and kept services can appear without a view write.
		// Changed inputs take crewTick's admission with the fresh defaults.
		if run.read.file != nil {
			b.holdMembers(run, b.neededMembers(run, run.read, false))
		}
		return false
	}
	if !b.crewTickLocked(run, false) {
		return false
	}
	run.locked(func() { run.inputs = stamp })
	return true
}

// ordersRefresh does no database reads when neither the database nor its WAL
// changed. The app's connected client owns this watch until backend.Close.
func (b *appBackend) ordersRefresh(store *automation.Store, stamp *string, cursor int64, seen map[string]string, open map[int64]appwire.Ask) (int64, bool) {
	next := appOrdersStamp(store) + "|" + b.now().Format("2006-01-02")
	if next == *stamp {
		return cursor, false
	}
	cursor, runs, ok := b.ordersRead(store, cursor, seen)
	if ok {
		b.sayOrderAsksFrom(store, open, runs)
		*stamp = next
	}
	return cursor, true
}
