//go:build !windows

package compaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE DIRECTION GIVEN WHILE IT WORKED SURVIVES A SUMMARY: the steering file is
// read whole into a pin beside the task, its newest end kept when it is long,
// and nothing is pinned when there is no file.
func TestTheSteeringFileIsPinnedAndItsNewestEndKept(t *testing.T) {
	dir := t.TempDir()
	service := &Service{deps: Dependencies{Instance: InstanceContext{Directory: dir}}}
	if pin := BuildSteeringPin(service.steering()); pin != "" {
		t.Fatalf("no steering file pinned %q", pin)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".senior-dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{"- the person, 2026-09-30T10:00:00Z: the grader is in grade.sh"}
	for len(strings.Join(lines, "\n")) < steeringPinMost+500 {
		lines = append(lines, "- the conversation that handed you this work, 2026-09-30T10:05:00Z: an older padding line")
	}
	lines = append(lines, "- the person, 2026-09-30T11:00:00Z: THE NEWEST WORD")
	if err := os.WriteFile(filepath.Join(dir, ".senior-dev", "steering.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pin := BuildSteeringPin(service.steering())
	if !strings.Contains(pin, "# DIRECTION FROM THE PEOPLE YOU WORK FOR") || !strings.Contains(pin, "THE NEWEST WORD") {
		t.Fatalf("the pin lost the newest word:\n%s", pin[:200])
	}
	if strings.Contains(pin, "grade.sh") || len(pin) > steeringPinMost+400 {
		t.Fatalf("a long steering file was not cut to its newest end (%d bytes)", len(pin))
	}
}
