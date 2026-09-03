package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	runErr := run()
	_ = write.Close()
	os.Stdout = previous
	body, readErr := io.ReadAll(read)
	_ = read.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(body), runErr
}

func TestServicesCommandListsAndStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "leaf", Brief: "serve", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, Intent: "run app", ServiceIntent: true}); err != nil {
		t.Fatal(err)
	}
	service, err := graph.PromoteService(store.Service{
		ID: "svc", Name: "dev-server", Command: "npm run dev", Dir: t.TempDir(), LogPath: "/tmp/dev.log",
		Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "5173"}, PID: 1_000_000_000,
		StartedAt:  time.Now().Add(-2 * time.Hour),
		Provenance: store.ServiceProvenance{OriginJobID: 1, LeafNodeID: "leaf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = graph.Close()

	listed, err := captureStdout(t, func() error { return runServices([]string{"--db", path}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dev-server", "running", "port:5173", "/tmp/dev.log"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("services list %q omitted %q", listed, want)
		}
	}
	stopped, err := captureStdout(t, func() error { return runServices([]string{"stop", "dev-server", "--db", path}) })
	if err != nil || stopped != "dev-server · stopped\n" {
		t.Fatalf("stop output=%q err=%v", stopped, err)
	}
	graph, _ = store.Open(path)
	defer graph.Close()
	got, _, _ := graph.Service(service.ID)
	if got.Status != store.ServiceStopped {
		t.Fatalf("headless stop status = %s", got.Status)
	}
}

// ── C22: RAW TAB-SEPARATED FIELDS, AND A HEADER NOBODY DREW ──────────────────
//
// With services running, this door emitted `name\tstatus\tage\thealth\tlogpath`
// — five fields with a tab between them, no header and no alignment, which is a
// machine's shape printed at a person and was the only listing in the binary
// that had it. `notebook` and `why` have drawn a headed, aligned table all
// along.
func TestServicesRowsAreAlignedUnderAHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "leaf", Brief: "serve", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, Intent: "run app", ServiceIntent: true}); err != nil {
		t.Fatal(err)
	}
	for _, service := range []store.Service{
		{ID: "svc-1", Name: "dev-server", Command: "npm run dev", LogPath: "/tmp/dev.log",
			Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "5173"}, PID: 1_000_000_001},
		{ID: "svc-2", Name: "a-much-longer-name", Command: "make watch", LogPath: "/tmp/watch.log",
			Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "8080"}, PID: 1_000_000_002},
	} {
		service.Dir = t.TempDir()
		service.StartedAt = time.Now().Add(-2 * time.Hour)
		service.Provenance = store.ServiceProvenance{OriginJobID: 1, LeafNodeID: "leaf"}
		if _, err := graph.PromoteService(service); err != nil {
			t.Fatal(err)
		}
	}
	_ = graph.Close()

	listed, err := captureStdout(t, func() error { return runServices([]string{"--db", path}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, "\t") {
		t.Fatalf("the rows are still raw tab-separated fields:\n%q", listed)
	}
	lines := strings.Split(strings.TrimRight(listed, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and two rows, got %d lines:\n%s", len(lines), listed)
	}
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "HEALTH") {
		t.Fatalf("there is no column header over the rows:\n%s", listed)
	}
	// ALIGNED MEANS THE COLUMNS LINE UP, which is the whole complaint: a name
	// twice as long as its neighbour must not shift every field after it.
	short, long := lines[1], lines[2]
	if strings.Index(short, "running") != strings.Index(long, "running") {
		t.Fatalf("the status column does not line up between rows:\n%s", listed)
	}
	if strings.Index(short, "/tmp/") != strings.Index(long, "/tmp/") {
		t.Fatalf("the log column does not line up between rows:\n%s", listed)
	}
}

// AND WITH NOTHING RUNNING THERE IS NO HEADER AND NO SILENCE — one sentence,
// which is the rule the streams lane wrote and this row's other half.
func TestServicesDrawsNoHeaderWhenNothingIsRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = graph.Close()

	listed, err := captureStdout(t, func() error { return runServices([]string{"--db", path}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, "NAME") || strings.Contains(listed, "HEALTH") {
		t.Fatalf("a column header was drawn over no rows:\n%s", listed)
	}
	if strings.TrimSpace(listed) != "nothing is being kept running." {
		t.Fatalf("services with nothing running said %q, want one short sentence", listed)
	}
}
