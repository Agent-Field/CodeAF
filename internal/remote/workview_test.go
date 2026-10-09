package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repoLoop(t *testing.T) (*Loop, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	loop, workspace, _ := browseLoop(t)
	gitIn(t, workspace, "init", "-q")
	write(t, filepath.Join(workspace, ".gitignore"), "ignored.txt\n")
	write(t, filepath.Join(workspace, "lexer.go"), "a\nb\nc\nd\ne\nf\ng\nh\n")
	gitIn(t, workspace, "add", ".")
	gitIn(t, workspace, "commit", "-q", "-m", "base")
	return loop, workspace
}

func TestReadTextConfinement(t *testing.T) {
	loop, workspace, folder := browseLoop(t)
	write(t, filepath.Join(workspace, "ok.go"), "package x\n")
	write(t, filepath.Join(folder, "secret.txt"), "no")
	outside := filepath.Join(filepath.Dir(workspace), "outside.txt")
	write(t, outside, "no")
	if err := os.Symlink(outside, filepath.Join(workspace, "link.txt")); err != nil {
		t.Fatal(err)
	}
	file, err := loop.Client.ReadText("ok.go")
	if err != nil || file.Text != "package x\n" || file.Lines != 1 || file.Path != "ok.go" || file.Language != "go" {
		t.Fatalf("ok: %+v %v", file, err)
	}
	for _, bad := range []string{"../outside.txt", outside, filepath.Join(folder, "secret.txt"), "link.txt", "missing.go", "", "."} {
		if got, err := loop.Client.ReadText(bad); err == nil {
			t.Errorf("%q was handed over: %+v", bad, got)
		}
	}
}

func TestReadTextRefusesBinaryAndLarge(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	write(t, filepath.Join(workspace, "b.bin"), "ab\x00cd")
	write(t, filepath.Join(workspace, "big.txt"), strings.Repeat("x", maxTextBytes+1))
	if f, err := loop.Client.ReadText("b.bin"); err != nil || f.Refusal != "binary" || f.Text != "" || f.Message == "" {
		t.Errorf("binary: %+v %v", f, err)
	}
	if f, err := loop.Client.ReadText("big.txt"); err != nil || f.Refusal != "too-large" || f.Text != "" {
		t.Errorf("large: %+v %v", f, err)
	}
}

func TestFindFilesIsGitignoreAware(t *testing.T) {
	loop, workspace := repoLoop(t)
	write(t, filepath.Join(workspace, "ignored.txt"), "x")
	write(t, filepath.Join(workspace, "lexer_test.go"), "x")
	found, err := loop.Client.FindFiles("lex", 10)
	if err != nil || len(found.Files) != 2 || found.Files[0].Path != "lexer.go" {
		t.Fatalf("find: %+v %v", found, err)
	}
	if none, _ := loop.Client.FindFiles("ignored", 10); len(none.Files) != 0 {
		t.Errorf("ignored file listed: %+v", none)
	}
	if none, _ := loop.Client.FindFiles("", 10); len(none.Files) != 0 {
		t.Errorf("empty query matched")
	}
}

func TestDiffChangesAndFile(t *testing.T) {
	loop, workspace := repoLoop(t)
	write(t, filepath.Join(workspace, "lexer.go"), "a\nb\nc\nX\nY\ne\nf\ng\nh\n")
	write(t, filepath.Join(workspace, "new.txt"), "one\ntwo\n")
	changes, err := loop.Client.DiffChanges(nil)
	if err != nil || !changes.Git || len(changes.Files) != 2 {
		t.Fatalf("changes: %+v %v", changes, err)
	}
	if lex := changes.Files[0]; lex.Path != "lexer.go" || lex.Added != 2 || lex.Deleted != 1 || lex.Status != "modified" {
		t.Errorf("lexer row: %+v", lex)
	}
	if n := changes.Files[1]; n.Status != "untracked" || n.Added != 2 {
		t.Errorf("new row: %+v", n)
	}
	if only, _ := loop.Client.DiffChanges([]string{"new.txt"}); len(only.Files) != 1 {
		t.Errorf("filter: %+v", only)
	}
	diff, err := loop.Client.DiffFile("lexer.go")
	if err != nil || len(diff.Hunks) != 1 || diff.Added != 2 || diff.Deleted != 1 || diff.Lines != 9 {
		t.Fatalf("diff: %+v %v", diff, err)
	}
	h := diff.Hunks[0]
	if h.OldStart != 1 || h.NewStart != 1 || len(h.Lines) == 0 {
		t.Errorf("hunk: %+v", h)
	}
	var del, add DiffLine
	for _, l := range h.Lines {
		switch l.Kind {
		case "del":
			del = l
		case "add":
			if add.Text == "" {
				add = l
			}
		}
	}
	if del.Old != 4 || del.Text != "d" || add.New != 4 || add.Text != "X" {
		t.Errorf("lines: del=%+v add=%+v", del, add)
	}
	nd, err := loop.Client.DiffFile("new.txt")
	if err != nil || nd.Status != "untracked" || nd.Added != 2 || len(nd.Hunks) != 1 {
		t.Errorf("untracked: %+v %v", nd, err)
	}
	if clean, err := loop.Client.DiffFile("ghost.txt"); err != nil || clean.Status != "clean" {
		t.Errorf("clean: %+v %v", clean, err)
	}
	_ = os.Remove(filepath.Join(workspace, "lexer.go"))
	if gone, err := loop.Client.DiffFile("lexer.go"); err != nil || gone.Status != "deleted" || gone.Deleted != 8 {
		t.Errorf("deleted: %+v %v", gone, err)
	}
}

func TestDiffRefusesEscapes(t *testing.T) {
	loop, workspace := repoLoop(t)
	outside := filepath.Join(filepath.Dir(workspace), "o.txt")
	write(t, outside, "x")
	_ = os.Symlink(filepath.Dir(workspace), filepath.Join(workspace, "up"))
	for _, bad := range []string{"../o.txt", outside, "up/o.txt", "../../etc/passwd"} {
		if d, err := loop.Client.DiffFile(bad); err == nil {
			t.Errorf("%q diffed: %+v", bad, d)
		}
	}
}

func TestDiffOutsideGit(t *testing.T) {
	loop, workspace, _ := browseLoop(t)
	write(t, filepath.Join(workspace, "a.txt"), "x")
	c, err := loop.Client.DiffChanges(nil)
	if err != nil || c.Git || len(c.Files) != 0 {
		t.Errorf("non-git: %+v %v", c, err)
	}
}
