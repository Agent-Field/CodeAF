package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/watchdog"
)

type fakeDoctorWatch struct {
	status watchdog.Status
	err    error
}

func (watch fakeDoctorWatch) Status() (watchdog.Status, error) { return watch.status, watch.err }

func TestDoctorShowsSharedCalmStatusRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := store.NewCharter("doctor-charter", "Keep releases documented.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for releases", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "doctor", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "doctor", Text: "Which release?", Urgency: store.QuestionWhenever,
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 3.4}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resident.lock"), []byte(`{"surface":"desktop","pid":4321}`), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	watch := fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-2 * time.Minute), NextDue: now.Add(3 * time.Minute),
	}}
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 20, watch); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		"brain", path, "desktop · pid 4321", "standing watch", "installed",
		"last wake 2m ago", "next check in 3m", "$3.40 today · rail $20.00",
		"1 active charter · 1 pending question",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"daemon", "launchd", "systemd", "service"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("doctor output contains %q:\n%s", forbidden, text)
		}
	}
}

func TestDoctorSaysWhenAnArrangedWatchStoppedWaking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-4 * time.Hour), NextDue: now.Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "checks look stalled") {
		t.Fatalf("stalled watch read as healthy:\n%s", output.String())
	}

	output.Reset()
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-2 * time.Minute), NextDue: now.Add(3 * time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "look stalled") {
		t.Fatalf("healthy watch reported as stalled:\n%s", output.String())
	}
}

func TestDoctorDoesNotCreateMissingBrainAndDegradesResidentCalmly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "graph.db")
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("doctor created missing brain: %v", err)
	}
	text := output.String()
	for _, want := range []string{
		path + " · not created", "this terminal while open", "not installed",
		"$0.00 today · rail unlimited", "0 active charters · 0 pending questions",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
}

// TestWatchGroundingNamesEachCharterRatherThanCountingThem is #35. The head's
// standing block said "3 active charters" and stopped there: doctor called
// ActiveCharters and kept only len(charters), so "what are you watching for me?"
// could be answered with a number and nothing a person would recognise as an
// answer — while the rail beside the conversation listed all three in full.
func TestWatchGroundingNamesEachCharterRatherThanCountingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	charter, err := store.NewCharter("release-notes", "Keep the release notes current.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for releases", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "doctor", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	charter.Watch.Cadence = "every hour"
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	grounding := watchGrounding(path, graph, fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-2 * time.Minute), NextDue: now.Add(3 * time.Minute),
	}}, 20)
	for _, want := range []string{
		// The calm status block is unchanged...
		"next check in 3m", "1 active charter",
		// ...and beside it, what the watch is actually for.
		"release-notes", "every hour", "Keep the release notes current.",
	} {
		if !strings.Contains(grounding, want) {
			t.Fatalf("standing grounding missing %q:\n%s", want, grounding)
		}
	}
	// The /standing slash command renders the same line, because two renderings
	// of one thing eventually disagree about it.
	lines, err := standingCharterLines(graph, 0)
	if err != nil || len(lines) != 1 || !strings.Contains(grounding, lines[0]) {
		t.Fatalf("grounding and /standing disagree: lines=%v err=%v\n%s", lines, err, grounding)
	}
}

// A graph with no charters at all adds nothing, so the block a quiet install
// sends is byte-for-byte what it always sent.
func TestWatchGroundingWithNoChartersIsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	watch := fakeDoctorWatch{status: watchdog.Status{Installed: true}}
	grounding := watchGrounding(path, graph, watch, 20)
	snapshot, err := collectDoctorSnapshot(path, graph, watch, 20, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if grounding != strings.TrimSpace(formatDoctor(snapshot)) {
		t.Fatalf("a charterless install gained bytes:\n%s", grounding)
	}
}
