package session

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// F1 proves every plan reader opens the store the real writer wrote, including
// the four session rollups that otherwise silently return no rows.
func TestSessionPlanReadersUseWriterURI(t *testing.T) {
	testSessionPlanReadersUseWriterURI(t)
}

func testSessionPlanReadersUseWriterURI(t *testing.T) {
	t.Helper()
	// Dev's literal-path contract supersedes the raw writer's decoded-path fixture.
	for _, tc := range []struct {
		name, directory string
		relative        bool
	}{
		{"fragment in directory", "dir with space é %20 #", false},
		{"fragment in filename", "dir with space é %20", false},
		{"relative path", "dir with space é %20 #", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "plan with space %20 #.db"
			if runtime.GOOS != "windows" {
				name = "plan with space %20 #?.db"
			}
			path := filepath.Join(t.TempDir(), tc.directory, name)
			if tc.relative {
				t.Chdir(t.TempDir())
				path = filepath.Join(tc.directory, name)
			}
			s, err := plandb.Open(path, "uri", "root", "héllo", "read spending", "chat")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("literal database missing: %v", err)
			}
			if _, err := s.AddMany([]plandb.TaskSpec{{ID: "work", ParentID: "root", Title: "do the work"}}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddAnsweredSpend("work", "uri-model", plandb.RoleWork, 0.25, 10, 20); err != nil {
				t.Fatal(err)
			}
			if got, err := plandb.ReadTasks(path); err != nil || len(got.Tasks) != 2 || got.Tasks[1].ID != "work" {
				t.Fatalf("tasks = %+v, %v", got, err)
			}
			if got, err := plandb.ReadRootPreview(path); err != nil || got.Title != "héllo" || got.Description != "read spending" || got.CreatedAt.IsZero() {
				t.Fatalf("preview = %+v, %v", got, err)
			}
			if got := planSpendBySeat(path, "chat", time.Time{}); len(got) != 1 || got[0].Seat != plandb.RoleWork || got[0].Model != "uri-model" || got[0].USD != 0.25 || got[0].Calls != 1 {
				t.Fatalf("seat spend = %+v", got)
			}
			if got := planSpendByTask(path); len(got) != 1 || got["work"] != 0.25 {
				t.Fatalf("task spend = %+v", got)
			}
			if got := planUsageByTask(path); len(got) != 1 || got["work"].model != "uri-model" || got["work"].tokens != 30 {
				t.Fatalf("task usage = %+v", got)
			}
			if model, ok := planOneWorkModel(path, []string{"work"}); !ok || model != "uri-model" {
				t.Fatalf("work model = %q, %v", model, ok)
			}
		})
	}
}
