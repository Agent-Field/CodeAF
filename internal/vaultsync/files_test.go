package vaultsync

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// workspace is one machine's folder of a chat, and the files its seals withhold.
type workspace struct {
	root     string
	withheld []string
}

// files attaches a workspace to a machine: the Files carrier of a chat whose
// withheld paths are whatever the test lists.
func (m *machine) files(t *testing.T, w *workspace) {
	t.Helper()
	m.Carry = append(m.Carry, Files{
		Root: w.root, Chat: "chat",
		Withheld: func() ([]string, error) { return w.withheld, nil },
	})
}

func (m *machine) chatFolder(t *testing.T) *workspace {
	t.Helper()
	w := &workspace{root: t.TempDir()}
	m.files(t, w)
	return w
}

// save writes a withheld file at mode and dates it, so a later edit is newer.
func (w *workspace) save(t *testing.T, rel, content string, mode os.FileMode, at time.Time) {
	t.Helper()
	path := filepath.Join(w.root, filepath.FromSlash(rel))
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(content), mode))
	must(t, os.Chmod(path, mode))
	must(t, os.Chtimes(path, at, at))
	for _, have := range w.withheld {
		if have == rel {
			return
		}
	}
	w.withheld = append(w.withheld, rel)
}

func (w *workspace) drop(t *testing.T, rel string) {
	t.Helper()
	must(t, os.Remove(filepath.Join(w.root, filepath.FromSlash(rel))))
	var kept []string
	for _, have := range w.withheld {
		if have != rel {
			kept = append(kept, have)
		}
	}
	w.withheld = kept
}

func (w *workspace) want(t *testing.T, rel, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(w.root, filepath.FromSlash(rel))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s is missing: %v", rel, err)
	}
	if got := readFile(t, path); got != content {
		t.Fatalf("%s holds %q, want %q", rel, got, content)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s has mode %v, want %v", rel, info.Mode().Perm(), mode)
	}
}

func (w *workspace) wantGone(t *testing.T, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(w.root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("%s is still there: %v", rel, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	must(t, err)
	return string(raw)
}

// carry sends a's files to b: a pushes, b pulls.
func carry(t *testing.T, a, b *machine) {
	t.Helper()
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
}

func TestFileKeepsCommentsOrderAndQuotingByteForByte(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	body := "# my file\nZED='quoted value'\n\nexport A=\"x y\"\nZED=again"
	wa.save(t, ".env", body, 0o644, time.Now())
	carry(t, a, b)
	wb.want(t, ".env", body, 0o644)
}

func TestFileKeepsItsMode(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	wa.save(t, "open.env", "A=1\n", 0o644, time.Now())
	wa.save(t, "private.env", "B=2\n", 0o600, time.Now())
	wa.save(t, "run.env", "C=3\n", 0o755, time.Now())
	carry(t, a, b)
	wb.want(t, "open.env", "A=1\n", 0o644)
	wb.want(t, "private.env", "B=2\n", 0o600)
	wb.want(t, "run.env", "C=3\n", 0o755)
}

func TestFileBytesThatAreNotTextSurvive(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	binary := "\x00\xff\xfe key \x80\r\n"
	wa.save(t, "deploy.p12", binary, 0o600, time.Now())
	carry(t, a, b)
	wb.want(t, "deploy.p12", binary, 0o600)
}

func TestNestedDotenvAtDepthThreeIsRestored(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	wa.save(t, "control-plane/web/client/.env.production", "API=prod\n", 0o640, time.Now())
	carry(t, a, b)
	wb.want(t, "control-plane/web/client/.env.production", "API=prod\n", 0o640)
}

func TestFileDeletedOnOneMachineIsDeletedOnTheOther(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	wa.save(t, ".env", "A=1\n", 0o600, time.Now().Add(-time.Hour))
	carry(t, a, b)
	wb.want(t, ".env", "A=1\n", 0o600)

	wa.drop(t, ".env")
	carry(t, a, b)
	wb.wantGone(t, ".env")

	// b's own push must not bring the file back.
	must(t, b.Push(ctx))
	must(t, a.Pull(ctx))
	wa.wantGone(t, ".env")
}

// A window that only started up has merged the vault already, so the slot is
// in B's vault when the chat it did not have arrives with its list of withheld
// paths and no files. The pull must bring the files, and B's next push must not
// carry a removal of them back to A.
func TestPullOfAChatWhoseSlotsWereMergedEarlierStillWritesTheFiles(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa := a.chatFolder(t)
	wa.save(t, ".env", "A=1\n", 0o644, time.Now().Add(-time.Hour))
	wa.save(t, "svc/api/.env", "B=2\n", 0o640, time.Now().Add(-time.Hour))
	wa.save(t, "web/client/app/.env.local", "C=3\n", 0o600, time.Now().Add(-time.Hour))
	must(t, a.Push(ctx))
	must(t, b.Push(ctx)) // B's own window syncs at start: the slots are merged, no file is written

	wb := b.chatFolder(t)
	wb.withheld = append([]string(nil), wa.withheld...) // the taken chat's policy names them
	must(t, b.Pull(ctx))
	wb.want(t, ".env", "A=1\n", 0o644)
	wb.want(t, "svc/api/.env", "B=2\n", 0o640)
	wb.want(t, "web/client/app/.env.local", "C=3\n", 0o600)

	must(t, b.Push(ctx))
	must(t, a.Pull(ctx))
	wa.want(t, ".env", "A=1\n", 0o644)
}

func TestTwoMachinesEditingDifferentFilesKeepBoth(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	old := time.Now().Add(-time.Hour)
	wa.save(t, "one.env", "one=old\n", 0o600, old)
	wa.save(t, "two.env", "two=old\n", 0o600, old)
	carry(t, a, b)

	wa.save(t, "one.env", "one=from-a\n", 0o600, time.Now())
	wb.save(t, "two.env", "two=from-b\n", 0o600, time.Now())
	must(t, a.Push(ctx))
	must(t, b.Push(ctx))
	must(t, a.Pull(ctx))
	for _, w := range []*workspace{wa, wb} {
		w.want(t, "one.env", "one=from-a\n", 0o600)
		w.want(t, "two.env", "two=from-b\n", 0o600)
	}
}

func TestTwoMachinesEditingTheSameFileKeepTheNewestWhole(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	wa.save(t, ".env", "X=old\n", 0o600, time.Now().Add(-time.Hour))
	carry(t, a, b)
	wa.save(t, ".env", "X=a\nA=1\n", 0o600, time.Now().Add(-time.Minute))
	wb.save(t, ".env", "X=b\n", 0o644, time.Now())
	must(t, a.Push(ctx))
	must(t, b.Push(ctx))
	must(t, a.Pull(ctx))
	wa.want(t, ".env", "X=b\n", 0o644)
	wb.want(t, ".env", "X=b\n", 0o644)
}

func TestAFileNamedOutsideTheWorkspaceIsNeverWritten(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wb := b.chatFolder(t)
	outside := filepath.Join(filepath.Dir(wb.root), "escaped.env")
	must(t, a.vault.PutAt(fileScopePrefix+"chat/../escaped.env", keys.Entry{Name: "../escaped.env", Value: encodeFile(0o600, []byte("X=1")), Scope: fileScopePrefix + "chat"}, time.Now()))
	carry(t, a, b)
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a vault name escaped the workspace")
	}
}

func TestNoFileContentIsEverLogged(t *testing.T) {
	const secret = "s3cr3t-value-7f3a"
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa := a.chatFolder(t)
	b.chatFolder(t)
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)

	wa.save(t, ".env", "TOKEN="+secret+"\n", 0o600, time.Now())
	for _, err := range []error{a.Push(ctx), b.Pull(ctx)} {
		if err != nil {
			out.WriteString(err.Error() + "\n")
		}
	}
	b.Store = swapped{Store: r.store, rid: currentRID(t, a), obj: append(bytes.Clone(magic), secret...)}
	if err := b.Pull(ctx); err != nil {
		out.WriteString(err.Error())
	}
	if out.Len() == 0 {
		t.Fatal("the capture saw nothing, so it proves nothing")
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("a secret value leaked: %q", out.String())
	}
}
