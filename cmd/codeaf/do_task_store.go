package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// doRunStore keeps the task door's record outside the copy and refuses reuse:
// a kept attempt can be followed after a crash but must never run twice.
func doRunStore(owned string) (string, error) {
	if owned == "" {
		_, dir, _, err := headlessStore("")
		return dir, err
	}
	if !filepath.IsAbs(owned) {
		return "", errors.New("the task's private record needs an absolute folder")
	}
	entries, err := os.ReadDir(owned)
	if err == nil && len(entries) > 0 {
		return "", errors.New("the task's private record already contains a run")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(owned, 0700); err != nil {
		return "", err
	}
	return owned, nil
}

// waitTaskLaunch lets a surviving child be found after a serve crash before
// any model call or tool can run. The door releases it by saving its pid.
func waitTaskLaunch(record string) error {
	if record == "" {
		return nil
	}
	for {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(record), "state.json"))
		if err != nil {
			return err
		}
		var state taskdoor.State
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		if state.Phase != "running" || state.Abandoned {
			return errors.New("this task attempt has already ended")
		}
		if state.PID == os.Getpid() && state.ProcessStart != "" {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func doTaskControls(record string) (taskdoor.Start, effort.Rung, error) {
	var request taskdoor.Start
	if record == "" {
		return request, effort.None, nil
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(record), "request.json"))
	// Direct in-process command tests can name an owned store without a door.
	// A real serve admission always wrote request.json before the child ran.
	if errors.Is(err, os.ErrNotExist) {
		return request, effort.None, nil
	}
	if err != nil {
		return request, effort.None, err
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return request, effort.None, err
	}
	if choice, ok := crewroute.ParseEffort(request.Effort); ok && choice != crewroute.EffortKnee {
		return request, effort.None, nil
	}
	rung, ok := effort.Parse(request.Effort)
	if !ok {
		return request, effort.None, errors.New("the task's reasoning effort is not readable")
	}
	return request, rung, nil
}
