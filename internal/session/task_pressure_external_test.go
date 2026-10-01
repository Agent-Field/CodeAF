package session

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestMachineAdmissionNoticesSettingsFromAnotherProcess(t *testing.T) {
	for _, road := range []string{"graph", "run", "default-graph", "default-run"} {
		t.Run(road, func(t *testing.T) {
			profile := t.TempDir()
			if strings.HasPrefix(road, "default-") {
				t.Setenv("CODEAF_HOME", profile)
				t.Setenv("CODEAF_PROFILE_DIR", "")
				profile = ""
			}
			path := config.BudgetConfigPath(profile)
			write := func(body string, tick int64) {
				t.Helper()
				generation := config.SettingsGeneration()
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(tick, 0)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				if config.SettingsGeneration() != generation {
					t.Fatal("external write bumped the local generation")
				}
			}
			write(`{"task.max_load":1.5,"task.min_free_mb":0}`, 1)
			var governor *admissionGovernor
			var poll func() bool
			if strings.HasSuffix(road, "graph") {
				agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
					cfg.ProfileDir, cfg.TaskMaxLoad, cfg.TaskMinFreeMB = profile, 1.5, 0
				})
				graph := agent.graph()
				governor = graph.governor
				poll = func() bool { graph.runFrontier(); return governor.admits(0) }
			} else {
				run := newRunAdmission(1.5, 0, profile, NewTaskLanes()).(*runAdmission)
				governor, poll = run.governor, run.MayStart
			}
			reading := loaded()
			reading.availableMB = 100
			governor.read = func() (machineReading, bool) { return reading, true }
			governor.now = func() time.Time { return time.Unix(10, 0) }
			if poll() {
				t.Fatal("loaded machine admitted work before the write")
			}
			write(`{"task.max_load":0.0,"task.min_free_mb":0}`, 2)
			if !poll() {
				t.Error("held task stayed blocked after an external write disabled the ceilings")
			}
			write(`{"task.max_load":0,"task.min_free_mb":200}`, 3)
			if poll() || governor.heldBy != config.KeyTaskMinFreeMB {
				t.Errorf("external memory ceiling was not applied: held by %q", governor.heldBy)
			}
			write(`{"task.max_load":0,"task.min_free_mb":0}`, 4)
			if !poll() {
				t.Error("held task stayed blocked after the external memory ceiling was removed")
			}
		})
	}
}

func TestNewAdmissionReadsSettingsChangedBeforeItsCreation(t *testing.T) {
	littleMemoryHost(t)
	for _, profileKind := range []string{"named", "default"} {
		t.Run(profileKind, func(t *testing.T) {
			profile := t.TempDir()
			if profileKind == "default" {
				t.Setenv("CODEAF_HOME", profile)
				t.Setenv("CODEAF_PROFILE_DIR", "")
				profile = ""
			}
			path := config.BudgetConfigPath(profile)
			if err := os.WriteFile(path, []byte(`{"task.min_free_mb":1099511627776}`), 0600); err != nil {
				t.Fatal(err)
			}
			gate := newRunAdmission(0, 0, profile, NewTaskLanes()).(*runAdmission)
			if gate.MayStart() || gate.HeldBy() != config.KeyTaskMinFreeMB {
				t.Fatal("new gate ignored memory setting written after session startup")
			}
			if gate.governor.maxLoad != 0 {
				t.Fatal("missing persisted load key replaced explicit startup zero")
			}
			if err := os.WriteFile(path, []byte(`{"task.max_load":0.001}`), 0600); err != nil {
				t.Fatal(err)
			}
			governor := newAdmissionGovernorForProfile(0, 0, profile)
			governor.read = func() (machineReading, bool) { return loaded(), true }
			governor.observe(0)
			if governor.admits(0) || governor.heldBy != config.KeyTaskMaxLoad || governor.minFreeMB != 0 {
				t.Fatal("new graph gate lost persisted load or explicit startup memory zero")
			}
		})
	}
}
