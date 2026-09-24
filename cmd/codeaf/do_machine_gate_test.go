package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestDoRunHonoursProfileMachineFloor(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("host has no proc memory reading")
	}
	beltRunEnv(t)
	profile := config.ProfileDir()
	settings := config.NewSettings(config.SettingsOptions{ProfileDir: profile})
	memory, ok := settings.Row(config.KeyTaskMinFreeMB)
	if !ok {
		t.Fatal("task memory setting is absent")
	}
	if err := memory.Apply("1099511627776"); err != nil {
		t.Fatal(err)
	}
	load, ok := settings.Row(config.KeyTaskMaxLoad)
	if !ok {
		t.Fatal("task load setting is absent")
	}
	if err := load.Apply("0"); err != nil {
		t.Fatal(err)
	}
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{}
	var stderr strings.Builder
	outcome, err := runErrand(doRequest{
		task: "do the work", workspace: workspace, timeout: 120 * time.Millisecond,
		stderr: &stderr, newBeltCompleter: func(string) session.Completer { return seat },
	}, config.ResolveSeats(profile, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Nodes != 0 || seat.seen != 0 {
		t.Fatalf("held do launched %d workers and made %d model calls", outcome.Nodes, seat.seen)
	}
	if err := memory.Apply("0"); err != nil {
		t.Fatal(err)
	}
	seat = finishingSeat(0)
	stderr.Reset()
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	outcome, err = runErrand(doRequest{
		task: "do the work", workspace: workspace, timeout: 30 * time.Second,
		stderr: &stderr, newBeltCompleter: func(string) session.Completer { return seat },
	}, config.ResolveSeats(profile, "", ""))
	if err != nil || outcome.Nodes == 0 {
		t.Fatalf("zeroed machine gate did not start: nodes=%d err=%v", outcome.Nodes, err)
	}
}
