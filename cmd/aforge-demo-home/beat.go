package main

// beat.go keeps the demo home's two live conversations alive.
//
// A live conversation says so in a presence.json it rewrites every few seconds,
// and every reader believes that file for three heartbeats and no longer
// (internal/session's taskpresence.go). That rule is what stops a window
// somebody killed from sitting on home forever claiming to be running, and it
// is also why a seeded presence file is worth nothing fifteen seconds after it
// was written. So while the surface is open this restamps the same rows on the
// same cadence a real session does — the rows the seeding wrote, read back off
// the disk and given a fresh instant, never invented here.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// beatEvery is how often a live conversation refreshes its file. It is the
// engine's own heartbeat and it is deliberately the same number: beating slower
// than the reader's window would make the demo flicker between live and dead.
const beatEvery = 5 * time.Second

// startPresenceBeat restamps every presence file under the demo home until the
// returned function is called. It answers a no-op stopper when there is nothing
// to beat, which is the ordinary case for a home nobody has run yet.
func startPresenceBeat(dir string) func() {
	rows := livePresenceRows(dir)
	if len(rows) == 0 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(beatEvery)
		defer ticker.Stop()
		for {
			for _, row := range rows {
				row.row.UpdatedAt = time.Now()
				_ = writePresence(row.dir, row.row)
			}
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// livePresence is one conversation's claim about itself and the folder it is
// kept in.
type livePresence struct {
	dir string
	row session.SessionPresence
}

// livePresenceRows reads every presence.json under the demo home's projects
// root, IGNORING FRESHNESS — which is why it does not use
// [session.ReadSessionPresence]. That reader refuses a stale file on purpose,
// and every file here is stale by the time this runs: the whole job is to make
// them fresh again. The shape read is the engine's own struct, so a field that
// moves moves here too.
func livePresenceRows(dir string) []livePresence {
	root := filepath.Join(dir, ".aforge", "v3", "projects")
	buckets, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var rows []livePresence
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, bucket.Name()))
		if err != nil {
			continue
		}
		for _, entry := range sessions {
			if !entry.IsDir() {
				continue
			}
			folder := filepath.Join(root, bucket.Name(), entry.Name())
			raw, err := os.ReadFile(filepath.Join(folder, presenceFileName))
			if err != nil {
				continue
			}
			var row session.SessionPresence
			if json.Unmarshal(raw, &row) != nil || row.SessionID == "" {
				continue
			}
			rows = append(rows, livePresence{dir: folder, row: row})
		}
	}
	return rows
}

// presenceFileName is what a live session calls its file. The name is
// internal/session's (taskpresence.go's presenceName) and is not exported;
// spelling it here is the same bargain [writeTranscript] states, and the test
// beside this program is what stops the two drifting.
const presenceFileName = "presence.json"

// writePresence writes one conversation's claim, whole, through the engine's
// own struct.
func writePresence(dir string, row session.SessionPresence) error {
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, presenceFileName), append(raw, '\n'), 0o600)
}
