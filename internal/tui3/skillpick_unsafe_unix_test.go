//go:build unix

package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestSkillPickerDiskScanDoesNotHoldTheUpdateLoop(t *testing.T) {
	a, agent, project, _ := skillApp(t)
	agent.AttachSkills("held-skill")
	folder := filepath.Join(project, ".agents", "skills", "pipe")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(folder, "SKILL.md"), 0o600); err != nil {
		t.Skip(err)
	}
	typeInto(t, a, "/skill")
	answer := make(chan tea.Cmd, 1)
	go func() { _, cmd := a.Update(key(" ")); answer <- cmd }()
	select {
	case <-answer:
		if !a.skillPick.open || len(a.skillPick.rows) == 0 || a.skillPick.rows[0].name != "held-skill" {
			t.Fatalf("picker did not open at once with its held shelf row: %+v", a.skillPick)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening /skill blocked the update loop on a FIFO")
	}
}

func TestReadSkillNameRefusesPipeAtOnce(t *testing.T) {
	folder := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(folder, "SKILL.md"), 0o600); err != nil {
		t.Skip(err)
	}
	answer := make(chan error, 1)
	go func() { _, err := readSkillName(folder); answer <- err }()
	select {
	case err := <-answer:
		if err == nil || !strings.Contains(err.Error(), "SKILL.md") {
			t.Fatalf("pipe refusal = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readSkillName blocked on a FIFO")
	}
}

func TestFolderRowRefusesPipeSkillInOneLine(t *testing.T) {
	a, agent, _, homeDir := skillApp(t)
	folder := filepath.Join(homeDir, "pipe-skill")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(folder, "SKILL.md"), 0o600); err != nil {
		t.Skip(err)
	}
	typeInto(t, a, "/skill "+folder)
	drive(t, a, key("enter"))
	if len(agent.held) != 0 {
		t.Fatalf("pipe skill attached %v", agent.held)
	}
	if screen := strings.Join(strings.Fields(strings.Join(plainRows(a), "\n")), " "); !strings.Contains(screen, "SKILL.md is not a regular file in "+folder) {
		t.Fatalf("folder refusal = %q", screen)
	}
}
