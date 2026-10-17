package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A COLD ROW NAMES ITS PROGRAM, ITS ENDING AND ITS BRANCH FROM WHAT THE ROW
// PERSISTED, on the live notice and on the list and page read alike, and says
// nothing for a row that recorded none. A stop rides beside "failed" and never
// replaces it, and an old failure is never reclassified.
func TestAppTaskCarriesTheColdProgramFactsTheRowPersistedAndNothingElse(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

	live, ok := appTaskOf("a1", &session.TaskNotice{ID: 3, Title: "senior-dev run", State: session.TaskFailed, Program: "senior-dev", Branch: "task/fix-nil-map", Stopped: true}, false, "", at)
	if !ok || live.Program != "senior-dev" || live.Ending != "stopped" || live.Branch != "task/fix-nil-map" || live.State != appwire.TaskFailed {
		t.Fatalf("live = %+v", live)
	}
	typed, _ := appTaskOf("a1", &session.TaskNotice{ID: 4, Title: "t", State: session.TaskFailed, Ending: session.TaskEndingProgram}, false, "", at)
	if typed.Ending != "program" || typed.Program != "" || typed.Branch != "" {
		t.Fatalf("typed = %+v", typed)
	}

	cold := appTaskOfView("a1", session.AppTaskView{ID: "7", Title: "t", State: "stopped", RunProgram: "review", Ending: "stopped", Branch: "task/x"})
	if cold.Program != "review" || cold.Ending != "stopped" || cold.Branch != "task/x" || cold.State != appwire.TaskFailed {
		t.Fatalf("cold = %+v", cold)
	}

	// A legacy failure with no typed ending stays a plain failure, and a program's
	// name is never read out of the title.
	for _, task := range []appwire.Task{
		appTaskOfView("a1", session.AppTaskView{ID: "8", Title: "senior-dev: fix it", State: "failed"}),
		func() appwire.Task {
			legacy, _ := appTaskOf("a1", &session.TaskNotice{ID: 9, Title: "senior-dev: fix it", State: session.TaskFailed}, false, "", at)
			return legacy
		}(),
	} {
		raw, _ := json.Marshal(task)
		if task.State != appwire.TaskFailed || task.Program != "" || task.Ending != "" || task.Branch != "" ||
			strings.Contains(string(raw), `"program"`) || strings.Contains(string(raw), `"ending"`) || strings.Contains(string(raw), `"branch"`) {
			t.Fatalf("a legacy row says %s", raw)
		}
	}
}
