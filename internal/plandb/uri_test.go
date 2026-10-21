package plandb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
)

// F1 pins both writer handles' original Unix bytes and the four session
// readers' pre-branch Unix bytes, including characters SQLite interprets in a
// URI, and that Windows takes the escaped spelling instead.
func TestStoreURIKeepsWriterAndUnixReaderBytes(t *testing.T) {
	for _, path := range []string{
		"relative plan.db", "/tmp/dir with space é %20 #/plan %20 #.db",
		`C:\dir with space é %20 #\plan %20 #.db`, `\\server\share\plan.db`,
	} {
		for _, query := range []string{
			"", "_txlock=immediate&_pragma=busy_timeout(5000)",
			"_pragma=busy_timeout(5000)", "mode=ro", "mode=ro&_pragma=busy_timeout(2000)",
		} {
			want := "file:" + filepath.ToSlash(path)
			if query != "" {
				want += "?" + query
			}
			if got := storeURI("linux", path, query); got != want {
				t.Fatalf("writer bytes: %q, want %q", got, want)
			}
			// Windows never opened a raw percent or # path, so it takes the
			// escaped spelling the other stores use (StoreURI's comment).
			if runtime.GOOS == "windows" {
				if got, want := StoreURI(path, query), sqlitefile.URI(path, query); got != want {
					t.Fatalf("Windows plan URI: %q, want %q", got, want)
				}
			}
		}
		if runtime.GOOS != "windows" {
			if got, want := StoreURI(path, "mode=ro"), "file:"+path+"?mode=ro"; got != want {
				t.Fatalf("Unix session reader bytes: %q, want %q", got, want)
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
			s, err := Open(path, "uri", "root", "héllo", "read the committed plan")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
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
