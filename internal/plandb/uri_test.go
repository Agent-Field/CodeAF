package plandb

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Dev's literal-path contract replaces the old raw Unix URI spelling.
func TestStoreURIPreservesLiteralPath(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"relative plan%23 #?é.db", "file:relative%20plan%2523%20%23%3F%C3%A9.db"},
		{"relative/plan%23.db", "file:relative/plan%2523.db"},
		{"/tmp/dir with space é %20 #?/plan.db", "file:///tmp/dir%20with%20space%20%C3%A9%20%2520%20%23%3F/plan.db"},
	} {
		for _, query := range []string{
			"", "_txlock=immediate&_pragma=busy_timeout(5000)",
			"_pragma=busy_timeout(5000)", "mode=ro", "mode=ro&_pragma=busy_timeout(2000)",
		} {
			want := tc.want
			if query != "" {
				want += "?" + query
			}
			if got := StoreURI(tc.path, query); got != want {
				t.Fatalf("StoreURI(%q, %q) = %q, want %q", tc.path, query, got, want)
			}
		}
	}
}

// F1 proves both public readers reach the real writer for directories and file
// names containing spaces, Unicode, percent escapes and fragment characters.
func TestPlanReadersUseWriterURI(t *testing.T) {
	testPlanReadersUseWriterURI(t)
}

func testPlanReadersUseWriterURI(t *testing.T) {
	t.Helper()
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
			s, err := Open(path, "uri", "root", "héllo", "read the committed plan")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("literal database missing: %v", err)
			}
			if _, err := s.AddMany([]TaskSpec{{ID: "work", ParentID: "root", Title: "do the work"}}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddAnsweredSpend("work", "uri-model", RoleWork, 0.25, 10, 20); err != nil {
				t.Fatal(err)
			}
			tasks, err := ReadTasks(path)
			if err != nil || tasks.RootID != "root" || len(tasks.Tasks) != 2 || tasks.Tasks[1].ID != "work" || tasks.Tasks[1].Title != "do the work" {
				t.Fatalf("tasks = %+v, %v", tasks, err)
			}
			preview, err := ReadRootPreview(path)
			if err != nil || preview.Title != "héllo" || preview.Description != "read the committed plan" || preview.CreatedAt.IsZero() {
				t.Fatalf("preview = %+v, %v", preview, err)
			}
		})
	}
}
