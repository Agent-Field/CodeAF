package tui3

// Two windows, one directory: the draft half of it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	if got := adoptDraft(own, "/tmp/lab"); got != "" {
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
	if got := adoptDraft(own, "/tmp/lab"); got != "" {
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

	if got := adoptDraft(DraftFile(dir, "/tmp/lab"), "/tmp/lab"); got != "the newer sentence" {
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

// ── one process, two conversations in one project ───────────────────────────

// THE COLLISION THIS CLOSES: the name used to be the workspace and the pid,
// which is unique among the PROCESSES alive at one moment and was therefore
// enough while a process held one conversation per directory. /new twice in one
// repository now makes two live boxes, and both would have written one file.
func TestTwoConversationsInOneProjectKeepTheirOwnDrafts(t *testing.T) {
	dir := t.TempDir()
	first := DraftFile(dir, "/tmp/lab")
	second := DraftFile(dir, "/tmp/lab")
	if first == second {
		t.Fatalf("both conversations were given one draft file: %s", first)
	}
	writeDraft(first, "the first conversation's sentence")
	writeDraft(second, "the second conversation's sentence")
	if got := readDraft(first); got != "the first conversation's sentence" {
		t.Fatalf("the first conversation's draft reads %q", got)
	}
	if got := readDraft(second); got != "the second conversation's sentence" {
		t.Fatalf("the second conversation's draft reads %q", got)
	}
}

// AND THE PID STAYS THE LAST TOKEN, which is not decoration:
// [draftWindowAlive] parses it out to decide whether the window that left an
// orphan is gone, and it reads the token after the final dash.
func TestTheDraftNameKeepsThePidLast(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Base(DraftFile(dir, "/tmp/lab"))
	tail := strings.TrimSuffix(name, ".txt")
	if got := tail[strings.LastIndex(tail, "-")+1:]; got != strconv.Itoa(draftOwner) {
		t.Fatalf("the last token of %q is %q, not this window", name, got)
	}
	if !draftWindowAlive(filepath.Join(dir, name)) {
		t.Fatalf("this window's own draft was read as an orphan: %s", name)
	}
}

// The orphan hunt globs from the WORKSPACE's prefix, so a conversation whose
// ordinal is 1 still finds the sentence a dead single-conversation window left
// under ordinal 0 — which is the only case adoption exists for.
func TestAnOrphanIsAdoptedWhateverOrdinalItWore(t *testing.T) {
	dir := t.TempDir()
	orphan := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/attic") })
	writeDraft(orphan, "what they were saying yesterday")

	// Two conversations of this process on the same workspace: the second one
	// is the one doing the hunting.
	DraftFile(dir, "/tmp/attic")
	own := DraftFile(dir, "/tmp/attic")
	if got := adoptDraft(own, "/tmp/attic"); got != "what they were saying yesterday" {
		t.Fatalf("the orphan was not adopted: %q", got)
	}
}

// AND ONLY THE FIRST CONVERSATION ON A WORKSPACE HUNTS. Firstness cannot be
// inferred from the ordinal — the first conversation on workspace B may be the
// third of the process — so the process records which workspaces it has already
// looked on. A conversation opened from home an hour in must not paste a
// stranger's unfinished sentence into its box.
func TestTheSecondConversationOnAWorkspaceDoesNotAdopt(t *testing.T) {
	dir := t.TempDir()
	orphan := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/cellar") })
	writeDraft(orphan, "somebody else's unfinished sentence")

	first := newApp(t.Context(), Options{
		Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/cellar",
		DraftFile: DraftFile(dir, "/tmp/cellar"),
	})
	if first.input.String() != "somebody else's unfinished sentence" {
		t.Fatalf("the first conversation did not adopt: %q", first.input.String())
	}
	second := newApp(t.Context(), Options{
		Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/cellar",
		DraftFile: DraftFile(dir, "/tmp/cellar"),
	})
	if got := second.input.String(); got != "" {
		t.Fatalf("the second conversation adopted %q", got)
	}
}

// And a hunt with no workspace to glob from matches nothing rather than
// sweeping the directory.
func TestAnAdoptionWithNoWorkspaceMatchesNothing(t *testing.T) {
	dir := t.TempDir()
	orphan := asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/loft") })
	writeDraft(orphan, "not yours")
	if got := adoptDraft(DraftFile(dir, "/tmp/loft"), ""); got != "" {
		t.Fatalf("a hunt with no workspace adopted %q", got)
	}
}
