package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

func TestClockMemoryMaintenanceStopsBeforeStoreCloses(t *testing.T) {
	memory, err := store.Open(filepath.Join(t.TempDir(), "mock-memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	stop := startClockMemoryMaintenance(context.Background(), memory, func(line string) { t.Log(line) })
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("memory maintenance did not join on stop")
	}
	if _, err := memory.ActiveMemoryCount(); err != nil {
		t.Fatalf("maintenance closed shared store: %v", err)
	}
}

func TestClockMemoryMaintenanceWithoutStoreIsAbsent(t *testing.T) {
	startClockMemoryMaintenance(context.Background(), nil, nil)()
}
