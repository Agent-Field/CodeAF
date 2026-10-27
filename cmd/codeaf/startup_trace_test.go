package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The solo factory's start writes its steps and its first answers to
// startup.log on its floor, each once, so a slow cold start can be read back.
func TestSoloFactoryStartWritesItsTimings(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &appFactoryHost{ctx: ctx, floor: floor, token: "owner-token"}
	mounted, err := host.mountSolo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer host.closeSoloForTest()
	srv := httptest.NewServer(mounted)
	defer srv.Close()
	for range 2 {
		var q api.Queue
		if err := appFactoryRequest(context.Background(), factoryTarget{srv.URL, "owner-token"}, "GET", "/v1/queue", nil, &q); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(floor.Root(), "startup.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{" ms factory: ready in ", "(discovery ", " ms factory: first answer GET /v1/queue in ", " ms factory: first queue GET /v1/queue in "} {
		if strings.Count(text, want) != 1 {
			t.Fatalf("startup.log has %q %d times:\n%s", want, strings.Count(text, want), text)
		}
	}
	if strings.Contains(text, "owner-token") {
		t.Fatal("the trace holds a token")
	}
}

// A trace keeps its last lines, so it never grows.
func TestStartupTraceKeepsItsLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.log")
	for i := range startupKeep + 20 {
		traceStartup(path, "step %d", i)
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != startupKeep || !strings.HasSuffix(lines[len(lines)-1], " ms step 419") {
		t.Fatalf("kept %d lines, last %q", len(lines), lines[len(lines)-1])
	}
}
