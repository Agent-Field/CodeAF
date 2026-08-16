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
	if err != nil || stopped != "dev-server\tstopped\n" {
		t.Fatalf("stop output=%q err=%v", stopped, err)
	}
	graph, _ = store.Open(path)
	defer graph.Close()
	got, _, _ := graph.Service(service.ID)
	if got.Status != store.ServiceStopped {
		t.Fatalf("headless stop status = %s", got.Status)
	}
}
