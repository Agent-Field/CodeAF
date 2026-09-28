package session

import (
	"path/filepath"
	"reflect"
	"testing"
)

// Contract 4a: the belt must hand staging the real path, including whitespace
// and bytes that git quotes in its human status output.
func TestBeltTreeWorkKeepsGitQuotedNames(t *testing.T) {
	repo := newTestRepo(t)
	names := []string{" lead.txt", "odd name é'q.txt", "has\"quote.txt", "new\nline.txt", "a -> b.txt"}
	for _, name := range names {
		writeFile(t, filepath.Join(repo, name), "work\n")
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[literalPathspec+name] = true
	}
	got := beltTreeWork(repo)
	if len(got) != len(want) {
		t.Fatalf("beltTreeWork = %q, want %q", got, names)
	}
	for _, path := range got {
		if !want[path] {
			t.Errorf("beltTreeWork contains wrong path %q", path)
		}
	}
}

// Contract 4c: a leftover's name must be the same bytes as the file on disk.
func TestLeftBehindKeepsGitQuotedName(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "odd name é'q.txt"), "work\n")
	if got := leftBehind(repo); !reflect.DeepEqual(got, []string{"odd name é'q.txt"}) {
		t.Fatalf("leftBehind = %q", got)
	}
}

// Contract 4c: the staged path list must keep git-quoted names unchanged.
func TestStagedPathsKeepsGitQuotedName(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "odd name é'q.txt"), "work\n")
	mustGit(t, repo, "add", "odd name é'q.txt")
	got, problem := stagedPaths(repo)
	if problem != "" || !reflect.DeepEqual(got, []string{"odd name é'q.txt"}) {
		t.Fatalf("stagedPaths = %q, %q", got, problem)
	}
}

// Contract 4c: the porcelain reader must preserve the two names of a rename
// and must treat an arrow inside a filename as ordinary bytes.
func TestPorcelainEntriesReadRenameAndArrow(t *testing.T) {
	entries := porcelainEntries("R  new\x00old\x00?? a -> b.txt\x00")
	want := []porcelainEntry{{Code: "R ", Path: "new", From: "old"}, {Code: "??", Path: "a -> b.txt"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("porcelainEntries = %#v, want %#v", entries, want)
	}
}

// Contract 4a: paths separated by NUL must retain spaces at either edge.
func TestGitNULPathsKeepWhitespace(t *testing.T) {
	got := gitNULPaths(" lead.txt\x00trail.txt \x00\x00")
	want := []string{" lead.txt", "trail.txt "}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gitNULPaths = %q, want %q", got, want)
	}
}
