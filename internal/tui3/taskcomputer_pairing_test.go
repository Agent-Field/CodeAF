package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestRunTasksOnNeedsAnotherPairedComputer(t *testing.T) {
	for _, paired := range []bool{false, true} {
		a, _ := sheetApp(t)
		a.taskComputers = surfaceTaskComputers{}
		a.addMachine.known, a.addMachine.size = true, 1
		if paired {
			a.addMachine.size = fleetEnough
		}
		drive(t, a, key("ctrl+,"))
		a.sheet.query.setText("Run tasks on")
		a.sheet.build()
		found := false
		for _, item := range a.sheet.items {
			found = found || item.row.Key == config.KeyTaskRunsOn
		}
		if found != paired {
			t.Fatalf("paired=%v: Run tasks on visible=%v", paired, found)
		}
		a.tookFleet(fleetMsg{size: 3 - a.addMachine.size})
		found = false
		for _, item := range a.sheet.items {
			found = found || item.row.Key == config.KeyTaskRunsOn
		}
		if found == paired {
			t.Fatal("open Settings did not adopt the new paired-computer count")
		}
	}
}
