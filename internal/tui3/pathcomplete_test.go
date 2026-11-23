package tui3

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// pathTree lays down a small tree under a fresh directory: two folders, two
// files, a dot folder, node_modules, and a file whose name only HOLDS "e".
func pathTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"internal/tui3", "docs", ".git", "node_modules/left", "Extras"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"internal/tui3/files.go", "internal/tui3/folderpick.go", "README.md", "go.mod", ".env", "internal/edit.go"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func pathsOf(listed PathListing) []string {
	out := make([]string, 0, len(listed.Entries))
	for _, entry := range listed.Entries {
		out = append(out, entry.Path)
	}
	return out
}

// A TYPED PATH IS COMPLETED FROM EXACTLY WHERE IT SAYS, ONE DIRECTORY AT A
// TIME. The folder a path names is listed — folders first with a separator
// after them, then files — the half-typed name narrows it, and every offered
// path keeps the spelling the person typed rather than becoming an absolute
// one they did not write.
func TestCompletePathListsTheFolderAPathNamesSpelledAsTyped(t *testing.T) {
	root := pathTree(t)

	got := CompletePath("internal/", root, 0)
	if want := []string{"internal/tui3/", "internal/edit.go"}; strings.Join(pathsOf(got), " ") != strings.Join(want, " ") {
		t.Fatalf("internal/ offered %v, want %v", pathsOf(got), want)
	}
	if got.Dir != filepath.Join(root, "internal") || got.Missing || got.Unreadable {
		t.Fatalf("internal/ = %+v", got)
	}
	if !got.Entries[0].Dir || got.Entries[0].Name != "tui3" || got.Entries[1].Dir {
		t.Fatalf("the kinds are wrong: %+v", got.Entries)
	}

	// The name after the last separator narrows, case-blind: names that begin
	// with it first, then names that only hold it.
	if got := pathsOf(CompletePath("internal/tui3/F", root, 0)); strings.Join(got, " ") != "internal/tui3/files.go internal/tui3/folderpick.go" {
		t.Fatalf("internal/tui3/F offered %v", got)
	}
	if got := pathsOf(CompletePath("./e", root, 0)); strings.Join(got, " ") != "./Extras/ ./internal/ ./README.md" {
		t.Fatalf("./e offered %v, want Extras (begins) before internal and README.md (hold)", got)
	}

	// An absolute path is listed where it is, whatever the root.
	abs := CompletePath(filepath.Join(root, "do"), t.TempDir(), 0)
	if want := filepath.Join(root, "docs") + "/"; len(abs.Entries) != 1 || abs.Entries[0].Path != want {
		t.Fatalf("an absolute path offered %v, want %s", pathsOf(abs), want)
	}

	// `..` climbs out of the root, and `.` alone is the root itself.
	up := CompletePath("../", filepath.Join(root, "internal"), 0)
	if up.Dir != root || len(up.Entries) == 0 || up.Entries[0].Path != "../docs/" {
		t.Fatalf("../ = %+v", up)
	}
	if dot := CompletePath(".", root, 0); dot.Dir != root || !strings.HasPrefix(dot.Entries[0].Path, "./") {
		t.Fatalf(". = %+v", dot)
	}
}

// HOME IS `~`, typed alone or leading a path, and a root that is empty means
// home too — which is where an app agent with no workspace starts.
func TestCompletePathReadsTildeAsHome(t *testing.T) {
	home := pathTree(t)
	t.Setenv("HOME", home)
	if got := CompletePath("~/do", t.TempDir(), 0); len(got.Entries) != 1 || got.Entries[0].Path != "~/docs/" || got.Dir != home {
		t.Fatalf("~/do = %+v", got)
	}
	if got := CompletePath("~", t.TempDir(), 0); got.Dir != home || !strings.HasPrefix(got.Entries[0].Path, "~/") {
		t.Fatalf("~ = %+v", got)
	}
	if got := CompletePath("go", "", 0); got.Dir != home || len(got.Entries) != 1 || got.Entries[0].Path != "go.mod" {
		t.Fatalf("a bare name with no root = %+v", got)
	}
}

// DOT NAMES AND THE FOLDERS NOBODY BROWSES TO STAY OUT until the half-typed
// name itself begins with a dot — the shell's rule and the folder browser's.
func TestCompletePathHidesDotNamesUntilADotIsTyped(t *testing.T) {
	root := pathTree(t)
	for _, entry := range CompletePath("", root, 0).Entries {
		if strings.HasPrefix(entry.Name, ".") || entry.Name == "node_modules" {
			t.Fatalf("a bare listing offered %s", entry.Path)
		}
	}
	if got := pathsOf(CompletePath(".", root, 0)); strings.Contains(strings.Join(got, " "), ".git") {
		t.Fatalf("`.` alone is the folder, not a dot name: %v", got)
	}
	if got := pathsOf(CompletePath("./.", root, 0)); len(got) < 2 || strings.Join(got[:2], " ") != "./.git/ ./.env" {
		t.Fatalf("./. offered %v, want the dot names first", got)
	}
}

// A FOLDER THAT IS NOT THERE AND ONE THAT CANNOT BE READ ARE SAID, NEVER
// DRAWN AS EMPTY; and a limit bounds the entries and says more were left off.
func TestCompletePathSaysMissingUnreadableAndMore(t *testing.T) {
	root := pathTree(t)
	if got := CompletePath("nowhere/", root, 0); !got.Missing || got.Unreadable || len(got.Entries) != 0 {
		t.Fatalf("a missing folder = %+v", got)
	}
	if got := CompletePath("go.mod/", root, 0); !got.Missing {
		t.Fatalf("a file named as a folder = %+v", got)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		closed := filepath.Join(root, "closed")
		if err := os.Mkdir(closed, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })
		if got := CompletePath("closed/", root, 0); !got.Unreadable || got.Missing {
			t.Fatalf("an unreadable folder = %+v", got)
		}
	}
	cut := CompletePath("", root, 2)
	if len(cut.Entries) != 2 || !cut.More {
		t.Fatalf("limit 2 = %+v", cut)
	}
	if whole := CompletePath("internal/", root, 2); whole.More {
		t.Fatalf("an answer that fits says more: %+v", whole)
	}
}
