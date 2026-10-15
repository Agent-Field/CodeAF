package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestAppQuietCrewSpendDoesNotEmitChangedSnapshots(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	backend, rec, _ := workBackend(t, &fakeConversation{})
	backend.profileDir = t.TempDir()
	const team = "aaaaaaaaaaaa"
	ids := []string{"0000000000000001", "0000000000000002", "0000000000000003", "0000000000000004"}
	members := []teamstore.Member{}
	for i, id := range ids {
		key := filepath.Join(t.TempDir(), id, "transcript.jsonl")
		members = append(members, teamstore.Member{Key: key, File: key, Handle: fmt.Sprintf("m%d", i)})
	}
	if err := teamstore.Save(backend.profileDir, []teamstore.Team{{ID: team, Name: "quiet", Manager: members[0].Key, Members: members, Made: time.Now(), ClosedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	ledger := teamstore.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(ledger), 0700); err != nil {
		t.Fatal(err)
	}
	text := ""
	for i, cost := range []float64{0.1, 0.2, 0.014895096, 1e-18} {
		text += fmt.Sprintf("{\"day\":%q,\"session\":%q,\"usd\":%.18g,\"calls\":1}\n", teamstore.Today(), ids[i], cost)
	}
	if err := os.WriteFile(ledger, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	run := &appCrewRun{prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{}, acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{}, held: map[string][]string{}, opening: map[string]bool{}}
	backend.crewTick(run, true)
	for i := 0; i < 256; i++ {
		backend.crewTick(run, false)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, event := range rec.events {
		if event == appwire.EventCrewChanged {
			t.Fatal("an unchanged ledger emitted crew.changed")
		}
	}
}
