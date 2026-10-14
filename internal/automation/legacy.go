package automation

// legacy.go removes the operating-system timers the feature this replaces
// installed. Automations run only while codeaf is open, so nothing of codeaf's
// may be left scheduled on the machine — and a timer left behind goes on
// running `codeaf tick` against a store that no longer exists, or, on a machine
// where a test once claimed it, fails forever with nobody told (the 2026-10-09
// audit of the standing machinery found exactly that).
//
// IT RUNS ON EVERY START, NOT ONCE. An older build on the same machine — another
// worktree's binary, a session host that has not retired yet — could put a
// timer back, and four stat calls are the whole cost of finding none.
//
// IT REMOVES THEM WHOEVER INSTALLED THEM. The old timer was one per login,
// shared by every home and build, so "ours" was never a safe question to ask of
// it; the answer now is that codeaf schedules nothing.
//
// IT ONLY EVER ACTS ON A FILE IT FOUND. Each timer is unloaded through the
// definition file under this login's home, never by its label alone, so a
// process whose home was redirected — a test — finds nothing and touches
// nothing. launchctl and systemctl are found on the PATH like any command.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// The labels and units the old features installed.
var (
	legacyDarwinLabels = []string{
		"ai.agentfield.codeaf.tick",
		"ai.agentfield.aforge.tick", // legacy-name
		"ai.agentfield.codeaf.wake",
		"ai.agentfield.aforge.wake", // legacy-name
	}
	legacyLinuxTimers = []string{
		"codeaf-tick.timer",
		"aforge-tick.timer", // legacy-name
		"codeaf-wake.timer",
		"aforge-wake.timer", // legacy-name
	}
)

// RemoveOldTimers takes the old timers off this login. It is quiet: a person
// who never had one is told nothing, and one that could not be removed is tried
// again on the next start.
func RemoveOldTimers() {
	if testing.Testing() {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		removeDarwinTimers(home)
	case "linux":
		removeLinuxTimers(home)
	}
}

func removeDarwinTimers(home string) {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	for _, label := range legacyDarwinLabels {
		plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		if _, err := os.Stat(plist); err != nil {
			continue
		}
		// THE FILE FIRST, THE JOB SECOND, and the job named by its label. This
		// can be running inside `codeaf tick` that launchd itself started, and
		// unloading the job kills that process: done first, it would end the
		// removal before the file went, and the timer would be loaded again at
		// the next login. The label form names the job without its file.
		_ = os.Remove(plist)
		_ = os.Remove(plist + ".lock")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = exec.CommandContext(ctx, "launchctl", "bootout", domain+"/"+label).Run()
		cancel()
	}
}

func removeLinuxTimers(home string) {
	units := filepath.Join(home, ".config", "systemd", "user")
	touched := false
	for _, timer := range legacyLinuxTimers {
		service := timer[:len(timer)-len(".timer")] + ".service"
		paths := []string{
			filepath.Join(units, timer),
			filepath.Join(units, service),
			filepath.Join(units, "timers.target.wants", timer),
		}
		found := false
		for _, path := range paths {
			if _, err := os.Lstat(path); err == nil {
				found = true
			}
		}
		if !found {
			continue
		}
		touched = true
		// THE FILES FIRST, for the same reason as on macOS: a removal running
		// inside the service the timer started must not be stopped before the
		// files that would start it again are gone. The timer is then stopped by
		// name, and the reload forgets the units.
		for _, path := range paths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				continue
			}
		}
		_ = os.Remove(filepath.Join(units, timer) + ".lock")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = exec.CommandContext(ctx, "systemctl", "--user", "stop", timer).Run()
		cancel()
	}
	if touched {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run()
		cancel()
	}
}
