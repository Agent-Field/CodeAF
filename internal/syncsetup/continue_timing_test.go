package syncsetup

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
)

func TestAContinueLogsWhereItsTimeWent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("before the measured move")
	h.durable(h.cell.ID, h.cell)
	path := filepath.Join(t.TempDir(), "chat.log")
	file, err := os.Create(path)
	mustNil(t, err)
	before := log.Writer()
	log.SetOutput(file)
	defer func() { log.SetOutput(before); _ = file.Close() }()
	c := h.continuerB()
	phases := map[string]bool{}
	c.opt.OnPhase = func(p handoff.Phase) {
		if !p.Ended.IsZero() {
			phases[p.Name] = true
		}
	}
	_, err = c.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	_, err = h.continuerA().TakeSeen(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: "old-device", Fence: 1})
	if err == nil {
		t.Fatal("expected a failed continue")
	}
	raw, err := os.ReadFile(path)
	mustNil(t, err)
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "continue "+h.cell.ID+" phases ") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("wanted one line per attempt, got %v", lines)
	}
	for _, line := range lines {
		for _, name := range []string{"capture=", "directory=", "retirement=", "fetch=", "acquire=", "restore=", "hooks=", "filing=", "total="} {
			if !strings.Contains(line, name) {
				t.Errorf("missing %s in %s", name, line)
			}
		}
	}
	for _, name := range []string{"local capture", "directory", "retirement", "fetch", "acquire", "restore", "hooks", "filing"} {
		if !phases[name] {
			t.Errorf("observer missed %s", name)
		}
	}
}
