//go:build windows

package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// R2 reaches all four session read-only opens without constructing an agent:
// each must see spend written through the real plan store at a native path.
func TestWindowsSessionPlanReadersNativePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir with space é", "plandb.db")
	s, err := plandb.Open(path, "windows", "root", "héllo", "read spending", "chat")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddAnsweredSpend("root", "windows-model", plandb.RoleWork, 0.25, 10, 20); err != nil {
		t.Fatal(err)
	}
	if got := planSpendBySeat(path, "chat", time.Time{}); len(got) != 1 || got[0].Seat != plandb.RoleWork || got[0].Model != "windows-model" || got[0].USD != 0.25 || got[0].Calls != 1 {
		t.Fatalf("seat spend = %+v", got)
	}
	if got := planSpendByTask(path); got["root"] != 0.25 {
		t.Fatalf("task spend = %+v", got)
	}
	if got := planUsageByTask(path); got["root"].model != "windows-model" || got["root"].tokens != 30 {
		t.Fatalf("task usage = %+v", got)
	}
	if model, ok := planOneWorkModel(path, []string{"root"}); !ok || model != "windows-model" {
		t.Fatalf("work model = %q, %v", model, ok)
	}
}
