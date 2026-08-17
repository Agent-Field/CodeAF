package tui3

// Two windows, one directory: the draft half of it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// THE DEFECT: the draft was named after the directory alone, so two terminals in
// one project shared one file. Each window's debounce overwrote the other's
// half-sentence and whichever quit last decided what survived.
func TestTwoWindowsKeepTheirOwnDrafts(t *testing.T) {
	dir := t.TempDir()

	first := DraftFile(dir, "/tmp/lab")
	second := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	if first == second {
		t.Fatalf("both windows were given one draft file: %s", first)
	}

	writeDraft(first, "the first window's sentence")
	writeDraft(second, "the second window's sentence")
	if got := readDraft(first); got != "the first window's sentence" {
		t.Fatalf("the first window's draft reads %q", got)
	}
	if got := readDraft(second); got != "the second window's sentence" {
		t.Fatalf("the second window's draft reads %q", got)
	}

	// A submit in one window takes its own draft and nothing else.
	app := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/lab", DraftFile: first})
	app.dropDraft()
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("submit left this window's draft behind (%v)", err)
	}
	if got := readDraft(second); got != "the second window's sentence" {
		t.Fatalf("submit took the other window's sentence: %q", got)
	}
}

// A window's own name dies with it, so the sentence it left is an orphan — and
// an orphan is the person's words with nobody holding them.
func TestAnOrphanedDraftIsAdoptedByTheNextWindow(t *testing.T) {
	dir := t.TempDir()

	orphan := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	writeDraft(orphan, "what they were saying yesterday")

	own := DraftFile(dir, "/tmp/lab")
	app := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/lab", DraftFile: own})
	if got := app.input.String(); got != "what they were saying yesterday" {
		t.Fatalf("the orphan was not adopted: %q", got)
	}
	// Adopted MEANS taken over: the file is this window's now, so it is still on
	// disk if this window dies too and no second window offers it again.
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphan is still where it was (%v)", err)
	}
	if got := readDraft(own); got != "what they were saying yesterday" {
		t.Fatalf("this window's draft reads %q", got)
	}
}

// And the one guarantee that matters: a sentence somebody is still typing is
// never lifted out of their window.
func TestALiveWindowsDraftIsNeverAdopted(t *testing.T) {
	dir := t.TempDir()

	// os.Getpid is this test, which is as alive as a process gets.
	live := asWindow(t, os.Getpid(), func() string { return DraftFile(dir, "/tmp/lab") })
	writeDraft(live, "still being typed")

	own := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	if got := adoptDraft(own); got != "" {
		t.Fatalf("a live window's draft was adopted: %q", got)
	}
	if got := readDraft(live); got != "still being typed" {
		t.Fatalf("the live window's draft reads %q", got)
	}
}

// Only this workspace's drafts are candidates. Two projects open at once are two
// unrelated sentences, and the hash in the name is what keeps them apart.
func TestAnotherWorkspacesOrphanIsLeftAlone(t *testing.T) {
	dir := t.TempDir()

	elsewhere := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/other") })
	writeDraft(elsewhere, "about a different project")

	own := DraftFile(dir, "/tmp/lab")
	if got := adoptDraft(own); got != "" {
		t.Fatalf("another workspace's draft was adopted: %q", got)
	}
	if got := readDraft(elsewhere); got != "about a different project" {
		t.Fatalf("the other project's draft reads %q", got)
	}
}

// The oldest orphans wait rather than being swept: each window that opens
// rescues one more.
func TestOnlyTheNewestOrphanIsAdopted(t *testing.T) {
	dir := t.TempDir()

	older := filepath.Join(dir, draftPrefix("/tmp/lab")+strconv.Itoa(deadPid(t))+".txt")
	writeDraft(older, "the older sentence")
	newer := filepath.Join(dir, draftPrefix("/tmp/lab")+strconv.Itoa(deadPid(t))+".txt")
	writeDraft(newer, "the newer sentence")
	if older == newer {
		t.Skip("this machine handed out one pid twice")
	}
	// Two files written in the same millisecond would make "newest" a coin toss.
	touchOlder(t, older)

	if got := adoptDraft(DraftFile(dir, "/tmp/lab")); got != "the newer sentence" {
		t.Fatalf("adopted %q", got)
	}
	if got := readDraft(older); got != "the older sentence" {
		t.Fatalf("the older orphan reads %q", got)
	}
}

// asWindow runs one call as if this process were another window.
func asWindow(t *testing.T, pid int, call func() string) string {
	t.Helper()
	was := draftOwner
	draftOwner = pid
	defer func() { draftOwner = was }()
	return call()
}

// deadPid is the id of a process that has certainly exited: a real one, run and
// reaped, which is the only way to be sure the number was ever a process at all.
func deadPid(t *testing.T) int {
	t.Helper()
	command := exec.Command("sh", "-c", "exit 0")
	if err := command.Run(); err != nil {
		t.Skipf("could not run a process to retire: %v", err)
	}
	return command.ProcessState.Pid()
}

func touchOlder(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	older := info.ModTime().Add(-time.Minute)
	if err := os.Chtimes(path, older, older); err != nil {
		t.Fatal(err)
	}
}
