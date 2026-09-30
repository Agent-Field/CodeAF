package taskcopy

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// machine is one machine's view of a chat: the cell folder whose trees/ holds
// the task copies and the project they were cut from.
type machine struct {
	t       *testing.T
	cell    cell.Cell
	project string
}

// worktreeCutter is a Cutter that cuts a linked worktree. These tests are about
// what Restore asks of a Cutter, so they use the plainest one; the road a task
// really makes its copy by is driven in the session package's own tests.
type worktreeCutter struct{}

func (worktreeCutter) Cut(project, dest string, spec Spec) error {
	return exec.Command("git", "-C", project, "worktree", "add", "-q", "-b", spec.Branch, dest, spec.At).Run()
}

var restorer = Carry{Cutter: worktreeCutter{}}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	root := t.TempDir()
	m := &machine{t: t, cell: cell.Cell{ID: "c", Root: filepath.Join(root, "cell")}, project: filepath.Join(root, "project")}
	write(t, filepath.Join(m.project, "README.md"), "readme\n")
	write(t, filepath.Join(m.project, "keep.txt"), "keep\n")
	write(t, filepath.Join(m.project, ".gitignore"), "build/\n.env\n")
	run(t, m.project, "git", "init", "-q", "-b", "main")
	run(t, m.project, "git", "add", ".")
	run(t, m.project, "git", "commit", "-q", "-m", "base")
	return m
}

// task cuts a task copy the way a task does: a worktree on its own branch.
func (m *machine) task(name string) string {
	m.t.Helper()
	tree := filepath.Join(m.cell.Root, cell.TreesDir, name)
	run(m.t, m.project, "git", "worktree", "add", "-q", "-b", "task/"+name, tree)
	return tree
}

// moveTo is the chat arriving on a machine that shares nothing with this one: the
// carried copies and the project's repository arrive, the task folders do not.
func (m *machine) moveTo(to *machine) {
	m.t.Helper()
	run(m.t, filepath.Dir(m.project), "cp", "-a", m.project+"/.", to.project)
	if err := os.MkdirAll(filepath.Join(to.cell.Root, cell.StateDir), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if _, err := os.Stat(carriedRoot(m.cell)); err == nil {
		run(m.t, filepath.Dir(m.project), "cp", "-a", carriedRoot(m.cell), carriedRoot(to.cell))
	}
	if err := os.RemoveAll(filepath.Join(m.cell.Root, cell.TreesDir)); err != nil {
		m.t.Fatal(err)
	}
}

func (m *machine) seal() {
	m.t.Helper()
	if err := (Carry{}).Compose(m.cell); err != nil {
		m.t.Fatal(err)
	}
}

// takeOn seals this machine's chat and moves it onto a fresh machine that
// restores it.
func (m *machine) takeOn() (*machine, []string) {
	m.t.Helper()
	m.seal()
	b := newMachineNoRepo(m.t)
	m.moveTo(b)
	restored, err := restorer.Restore(b.cell, b.project)
	if err != nil {
		m.t.Fatal(err)
	}
	return b, restored
}

func newMachineNoRepo(t *testing.T) *machine {
	root := t.TempDir()
	return &machine{t: t, cell: cell.Cell{ID: "c", Root: filepath.Join(root, "cell")}, project: filepath.Join(root, "project")}
}

func (m *machine) read(name, rel string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(m.cell.Root, cell.TreesDir, name, rel))
	return string(raw), err == nil
}

func (m *machine) branch(name string) string {
	return run(m.t, filepath.Join(m.cell.Root, cell.TreesDir, name), "git", "rev-parse", "--abbrev-ref", "HEAD")
}

func TestTaskWithNoUncommittedEditsCarriesOnlyItsRecord(t *testing.T) {
	a := newMachine(t)
	a.task("1")
	a.seal()
	if _, err := os.Stat(filepath.Join(carriedRoot(a.cell), "1", filesDir)); err == nil {
		t.Fatal("a copy with no edits carried files")
	}
	b, restored := a.takeOn()
	if !slices.Equal(restored, []string{"1"}) || b.branch("1") != "task/1" {
		t.Fatalf("restored %v on %q, want copy 1 on task/1", restored, b.branch("1"))
	}
	if got, _ := b.read("1", "keep.txt"); got != "keep\n" {
		t.Fatalf("keep.txt = %q", got)
	}
}

func TestTaskWithEditsAndADeletedFileComesBack(t *testing.T) {
	a := newMachine(t)
	tree := a.task("1")
	write(t, filepath.Join(tree, "keep.txt"), "edited\n")
	write(t, filepath.Join(tree, "new/file.txt"), "untracked\n")
	if err := os.Remove(filepath.Join(tree, "README.md")); err != nil {
		t.Fatal(err)
	}
	b, _ := a.takeOn()
	if got, _ := b.read("1", "keep.txt"); got != "edited\n" {
		t.Errorf("keep.txt = %q, want the edit", got)
	}
	if got, _ := b.read("1", "new/file.txt"); got != "untracked\n" {
		t.Errorf("new/file.txt = %q, want the untracked file", got)
	}
	if _, ok := b.read("1", "README.md"); ok {
		t.Error("the deleted file is back")
	}
}

func TestTaskWhoseBranchWasAlreadyMergedComesBack(t *testing.T) {
	a := newMachine(t)
	tree := a.task("1")
	write(t, filepath.Join(tree, "done.txt"), "work\n")
	run(t, tree, "git", "add", ".")
	run(t, tree, "git", "commit", "-q", "-m", "task")
	run(t, a.project, "git", "merge", "-q", "task/1")
	write(t, filepath.Join(tree, "after.txt"), "after the merge\n")
	b, _ := a.takeOn()
	if got, _ := b.read("1", "after.txt"); got != "after the merge\n" {
		t.Errorf("after.txt = %q", got)
	}
	if got, _ := b.read("1", "done.txt"); got != "work\n" || b.branch("1") != "task/1" {
		t.Errorf("done.txt = %q on %q, want the merged commit on task/1", got, b.branch("1"))
	}
}

func TestTaskWhoseBranchIsGoneComesBackOnItsCommit(t *testing.T) {
	a := newMachine(t)
	tree := a.task("1")
	write(t, filepath.Join(tree, "wip.txt"), "wip\n")
	run(t, tree, "git", "checkout", "-q", "--detach")
	run(t, a.project, "git", "branch", "-q", "-D", "task/1")
	b, _ := a.takeOn()
	if got, _ := b.read("1", "wip.txt"); got != "wip\n" || b.branch("1") != "restored/1" {
		t.Errorf("wip.txt = %q on %q, want the edit on a branch made for it", got, b.branch("1"))
	}
}

func TestTwoTasksComeBackSeparately(t *testing.T) {
	a := newMachine(t)
	write(t, filepath.Join(a.task("1"), "one.txt"), "one\n")
	write(t, filepath.Join(a.task("2"), "two.txt"), "two\n")
	b, restored := a.takeOn()
	if !slices.Equal(restored, []string{"1", "2"}) {
		t.Fatalf("restored %v, want both", restored)
	}
	for name, file := range map[string]string{"1": "one.txt", "2": "two.txt"} {
		if _, ok := b.read(name, file); !ok {
			t.Errorf("copy %s lost %s", name, file)
		}
	}
	if _, ok := b.read("1", "two.txt"); ok {
		t.Error("copy 1 holds copy 2's file")
	}
}

func TestFinishedNodeLeavesNothingCarried(t *testing.T) {
	a := newMachine(t)
	write(t, filepath.Join(a.task("1"), "wip.txt"), "wip\n")
	a.seal()
	if got := Carried(a.cell); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("carried %v while the task ran", got)
	}
	run(t, a.project, "git", "worktree", "remove", "--force", filepath.Join(a.cell.Root, cell.TreesDir, "1"))
	a.seal()
	if got := Carried(a.cell); len(got) != 0 {
		t.Fatalf("carried %v after the task finished", got)
	}
	if _, restored := a.takeOn(); len(restored) != 0 {
		t.Fatalf("restored %v for a chat with no task copy", restored)
	}
}

func TestIgnoredFilesAndSecretsStayOut(t *testing.T) {
	a := newMachine(t)
	tree := a.task("1")
	write(t, filepath.Join(tree, "build/out.bin"), "ignored\n")
	write(t, filepath.Join(tree, ".env"), "TOKEN=x\n")
	write(t, filepath.Join(tree, "key.txt"), "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----\n")
	write(t, filepath.Join(tree, "fine.txt"), "fine\n")
	b, _ := a.takeOn()
	for _, rel := range []string{"build/out.bin", ".env", "key.txt"} {
		if _, ok := b.read("1", rel); ok {
			t.Errorf("%s travelled", rel)
		}
	}
	if _, ok := b.read("1", "fine.txt"); !ok {
		t.Error("fine.txt did not travel")
	}
}

func TestRestoreForgetsRegistrationsForPathsNotHere(t *testing.T) {
	a := newMachine(t)
	a.task("1")
	a.task("2")
	a.seal()
	b := newMachineNoRepo(t)
	a.moveTo(b)
	if !strings.Contains(run(t, b.project, "git", "worktree", "list", "--porcelain"), "prunable") {
		t.Fatal("the arriving repository has no stale registration to forget")
	}
	if _, err := restorer.Restore(b.cell, b.project); err != nil {
		t.Fatal(err)
	}
	list := run(t, b.project, "git", "worktree", "list", "--porcelain")
	if strings.Contains(list, "prunable") || strings.Count(list, "worktree ") != 3 {
		t.Fatalf("registrations after restore:\n%s", list)
	}
}

func TestRestoreLeavesALiveCopyItsOwnFiles(t *testing.T) {
	a := newMachine(t)
	tree := a.task("1")
	write(t, filepath.Join(tree, "wip.txt"), "wip\n")
	a.seal()
	write(t, filepath.Join(tree, "later.txt"), "written after the seal\n")
	if _, err := restorer.Restore(a.cell, a.project); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.read("1", "later.txt"); got != "written after the seal\n" {
		t.Fatalf("a restore on the machine the chat never left lost later.txt: %q", got)
	}
}

func TestACopyThatCannotBeCutStillGetsItsFiles(t *testing.T) {
	a := newMachine(t)
	write(t, filepath.Join(a.task("1"), "wip.txt"), "wip\n")
	a.seal()
	b := newMachineNoRepo(t)
	a.moveTo(b)
	if err := os.RemoveAll(b.project); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.project, 0o755); err != nil {
		t.Fatal(err)
	}
	restored, err := restorer.Restore(b.cell, b.project)
	if err == nil || len(restored) != 0 {
		t.Fatalf("restored %v, %v; want a refusal to cut the copy", restored, err)
	}
	if got, _ := b.read("1", "wip.txt"); got != "wip\n" {
		t.Fatalf("the edit is not where the task worked: %q", got)
	}
}

// A copy comes back with the permission bits it left with, whatever the folder
// it travelled through and the umask of the machine that put it back: the
// record holds them, so the copy compares equal to the first machine's.
func TestCopyFilesComeBackAtTheirModes(t *testing.T) {
	a := newMachine(t)
	tree := a.task("modes")
	for rel, mode := range map[string]os.FileMode{"note.txt": 0o664, "run.sh": 0o775, "private.txt": 0o600} {
		write(t, filepath.Join(tree, rel), rel+"\n")
		if err := os.Chmod(filepath.Join(tree, rel), mode); err != nil {
			t.Fatal(err)
		}
	}
	a.seal()
	b := newMachineNoRepo(t)
	a.moveTo(b)
	// The folder that travelled was rewritten on the way, as a machine with another umask does.
	err := filepath.WalkDir(filepath.Join(carriedRoot(b.cell), "modes", filesDir), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.Chmod(path, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restorer.Restore(b.cell, b.project); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]os.FileMode{"note.txt": 0o664, "run.sh": 0o775, "private.txt": 0o600} {
		info, err := os.Stat(filepath.Join(b.cell.Root, cell.TreesDir, "modes", rel))
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s came back as %v (%v), want %v", rel, info, err, want)
		}
	}
}
