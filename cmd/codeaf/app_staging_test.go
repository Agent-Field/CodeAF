package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// S6 gives the app a fetched staging tree with the sender's working presence.
// Listing, startup recovery and lazy opening must never admit it as an agent.
func TestAppStagingAdmissionS6(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	c, err := cell.CreateIn(filepath.Join(dir, "v3", "projects", "copies"), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err = session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "Staging only", LastUserAt: time.Now(), Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	presence, err := json.Marshal(session.SessionPresence{State: session.PresenceWorking, PID: 123, UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Root, "presence.json"), presence, 0600); err != nil {
		t.Fatal(err)
	}
	stage := c.Root + handoff.StagingSuffix
	if err = os.Rename(c.Root, stage); err != nil {
		t.Fatal(err)
	}
	b, _, _ := appDeviceBackend(t, dir)
	rows, err := b.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("S6 staging tree reached agents.list: %+v", rows)
	}
	run := &appCrewRun{}
	if got := b.lazyCandidates(run, appCrewRead{file: &teamstore.File{}}, true); len(got) != 0 {
		t.Errorf("S6 staging tree reached lazy open: %+v", got)
	}
	b.snapshotCloseClaims()
	if len(b.closeRecovery.claims) != 0 || appCloseConversationExists(stage) {
		t.Fatal("S6 staging tree reached close recovery")
	}
}
