package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

func TestTaskLaunchGateHelper(t *testing.T) {
	if os.Getenv("CODEAF_TASK_LAUNCH_HELPER") != "1" {
		return
	}
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	var args []string
	for n, arg := range os.Args {
		if arg == "--" {
			args = os.Args[n+1:]
			break
		}
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("CODEAF_HOME"), "entered"), nil, 0600)
	os.Exit(exitCodeOf(runDo(args)))
}

// Contract 12: the actual hidden command flag prevents work before its pid
// is durable. A launch abandoned during that window exits without tools.
func TestDoWaitsForDurableTaskAdmissionBeforeStarting(t *testing.T) {
	for _, mode := range []string{"ended", "registered"} {
		t.Run(mode, func(t *testing.T) {
			home, err := os.MkdirTemp("", "rn-l1-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(home)
			t.Setenv("HOME", home)
			t.Setenv("CODEAF_HOME", home)
			t.Setenv("CODEAF_TELEMETRY", "off")
			t.Setenv("DO_NOT_TRACK", "1")
			attempt := filepath.Join(home, "attempt")
			record := filepath.Join(attempt, "run")
			if err := os.MkdirAll(record, 0700); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(attempt, "state.json")
			writeState := func(state taskdoor.State) {
				t.Helper()
				raw, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				staged := statePath + ".new"
				if err := os.WriteFile(staged, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(staged, statePath); err != nil {
					t.Fatal(err)
				}
			}
			writeState(taskdoor.State{Phase: "running", LaunchStartedAt: time.Now()})
			out, err := os.Create(filepath.Join(attempt, "do.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			log, err := os.Create(filepath.Join(attempt, "do.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			// Omitting the brief makes even a broken gate fail before constructing a
			// provider, so this regression can never contact a real model endpoint.
			cmd := exec.Command(os.Args[0], "-test.run=^TestTaskLaunchGateHelper$", "--", "--task-store", record, "--json")
			cmd.Env = append(os.Environ(), "CODEAF_TASK_LAUNCH_HELPER=1")
			cmd.Stdout, cmd.Stderr = out, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(home, "entered")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("child did not reach launch gate")
				}
				time.Sleep(5 * time.Millisecond)
			}
			time.Sleep(20 * time.Millisecond)
			if info, err := out.Stat(); err != nil || info.Size() != 0 {
				t.Fatalf("child passed an undurable admission: %v, %v", info, err)
			}
			next := taskdoor.State{Phase: "interrupted"}
			want := "this task attempt has already ended"
			if mode == "registered" {
				next = taskdoor.State{Phase: "running", PID: cmd.Process.Pid, ProcessStart: "fixture-start"}
				want = "no goal given"
			}
			writeState(next)
			if err := cmd.Wait(); err == nil {
				t.Fatal("abandoned launch succeeded")
			}
			raw, err := os.ReadFile(out.Name())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), want) {
				t.Fatalf("wrong launch refusal: %s", raw)
			}
			if entries, err := os.ReadDir(record); err != nil || len(entries) != 0 {
				t.Fatalf("abandoned launch started a run: %v, %v", entries, err)
			}
		})
	}
}
