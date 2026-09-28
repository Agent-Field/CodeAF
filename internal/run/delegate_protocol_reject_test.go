//go:build !windows

package run_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/run"
)

func TestWrongProtocolLeavesNoChildStepsInTrajectory(t *testing.T) {
	store := runOpenStore(t)
	script := filepath.Join(t.TempDir(), "future.sh")
	wrong := delegate.ProtocolVersion + 1
	body := "#!/bin/sh\n" +
		"echo '{\"type\":\"hello\",\"protocol\":" + strconv.Itoa(wrong) + ",\"delegate\":\"fake\"}'\n" +
		"echo '{\"type\":\"step\",\"command\":\"bash: incompatible action\"}'\n" +
		"echo '{\"type\":\"terminal\",\"status\":\"pass\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	worker := run.NewDelegateWorker(store, t.TempDir(), delegate.Delegate{Name: "fake", Default: "run"}, run.DelegateSetup{Exe: script}, 0, 0)
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "version "+strconv.Itoa(wrong)) || !strings.Contains(err.Error(), "version "+strconv.Itoa(delegate.ProtocolVersion)) {
		t.Fatalf("mismatch error = %v", err)
	}
	for _, line := range rawTrajectory(t, filepath.Dir(store.Path()), store.RootID()) {
		if strings.Contains(line, "incompatible action") {
			t.Fatalf("wrong-protocol step entered trajectory: %s", line)
		}
	}
}
