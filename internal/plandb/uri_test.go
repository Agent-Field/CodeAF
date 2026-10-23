package plandb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// N2 and N3 pins both writer handles' original Unix bytes and the four session
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

		}
		if runtime.GOOS != "windows" {
			if got, want := StoreURI(path, "mode=ro"), "file:"+path+"?mode=ro"; got != want {
				t.Fatalf("Unix session reader bytes: %q, want %q", got, want)
			}
		}
	}
}

// N2 and N3 prove both public readers reach the real writer for directories and file
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
			if err := s.AddSpend("work", "uri-model", RoleWork, 0.25, 10, 20); err != nil {
				t.Fatal(err)
			}
			preview, err := ReadRootPreview(path)
			if err != nil || preview.Title != "héllo" || preview.Description != "read the committed plan" || preview.CreatedAt.IsZero() {
				t.Fatalf("preview = %+v, %v", preview, err)
			}
		})
	}
}

// N2 uses the existing shared Windows URI door while keeping plan query bytes.
func TestWindowsPlanURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the shared formatter follows the host filesystem")
	}
	for _, tc := range []struct{ path, want string }{
		{`C:\plans\state %20 #.db`, "file:///C:/plans/state%20%2520%20%23.db?mode=ro"},
		{`\\server\share\state.db`, "file:////server/share/state.db?mode=ro"},
	} {
		if got := StoreURI(tc.path, "mode=ro"); got != tc.want {
			t.Fatalf("URI = %q, want %q", got, tc.want)
		}
	}
}
