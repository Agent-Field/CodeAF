package session

import (
	"os"
	"path/filepath"
	"strings"
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
	// Keep metacharacters out of the subtest name: TempDir includes that name
	// before its unique suffix, which a raw URI fragment would otherwise cut off.
	for _, tc := range []struct{ name, directory string }{
		{"fragment in directory", "dir with space é %20 #"},
		{"fragment in filename", "dir with space é %20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.directory, "plan with space %20 #.db")
			// The raw writer decodes %20. Prepare that existing-store parent too,
			// so the second case reaches the filename's # rather than stopping
			// at the directory's #. Both spellings stay under this test's temp dir.
			if err := os.MkdirAll(strings.ReplaceAll(filepath.Dir(path), "%20", " "), 0o700); err != nil {
				t.Fatal(err)
			}
			s, err := plandb.Open(path, "uri", "root", "héllo", "read spending", "chat")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
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
