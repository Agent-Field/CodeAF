package cellstore

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/keys"
)

const fakeKey = "sk-or-v1-0123456789abcdef0123456789abcdef"

type guardRig struct {
	t      *testing.T
	tree   string
	cell   cell.Cell
	guard  Guard
	notice []string
}

func newRig(t *testing.T) *guardRig {
	r := &guardRig{t: t, tree: t.TempDir(), cell: newCell(t)}
	r.guard = Guard{Home: t.TempDir(), Notify: func(s string) { r.notice = append(r.notice, s) }}
	return r
}

// state is where the guard keeps its exclusions: the cell's directory.
func (r *guardRig) state() string { return stateDir(r.cell) }

func (r *guardRig) write(rel, text string) {
	r.t.Helper()
	path := filepath.Join(r.tree, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *guardRig) screen(changed ...string) {
	r.t.Helper()
	if err := r.guard.Screen(r.cell, r.tree, r.state(), changed); err != nil {
		r.t.Fatal(err)
	}
}

func (r *guardRig) policy() policyFile {
	r.t.Helper()
	p, err := readPolicy(r.tree, r.state())
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

func TestScreenWithholdsDotenvAndKeepsItInTheVault(t *testing.T) {
	r := newRig(t)
	r.write(".env", "OPENROUTER_API_KEY="+fakeKey+"\n")
	r.write("src/app.py", "print('hi')\n")
	r.screen()

	if got := r.policy().withheld; len(got) != 1 || got[0] != ".env" {
		t.Fatalf("withheld %v, want [.env]", got)
	}
	if _, err := os.Stat(filepath.Join(r.tree, policyName)); !os.IsNotExist(err) {
		t.Fatalf("the guard wrote into the workspace: %v", err)
	}
	v, err := keys.Open(r.guard.Home)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := v.Env(scopeOf(r.cell))
	if len(env) != 1 || env[0] != "OPENROUTER_API_KEY="+fakeKey {
		t.Fatalf("vault env %v", env)
	}
	if got := r.guard.Env(r.cell); len(got) != 1 {
		t.Fatalf("Env %v: the next tool call must see the variable", got)
	}
}

func TestScreenWithholdsAnyFileWithASecretAndSaysOnce(t *testing.T) {
	r := newRig(t)
	r.write("src/config.py", "KEY = '"+fakeKey+"'\n")
	r.screen("src/config.py")
	r.screen("src/config.py")
	r.screen()

	if got := r.policy().withheld; len(got) != 1 || got[0] != "src/config.py" {
		t.Fatalf("withheld %v", got)
	}
	if len(r.notice) != 1 || !strings.Contains(r.notice[0], "src/config.py") || strings.Contains(r.notice[0], fakeKey) {
		t.Fatalf("notices %q: want one, naming the file and never the secret", r.notice)
	}
}

func TestScreenReleasesAFileThatIsCleaned(t *testing.T) {
	r := newRig(t)
	r.write("src/config.py", "KEY = '"+fakeKey+"'\n")
	r.screen()
	r.write("src/config.py", "KEY = os.environ['KEY']\n")
	r.screen()
	if got := r.policy().withheld; len(got) != 0 {
		t.Fatalf("withheld %v after the secret was removed", got)
	}
	if _, err := os.Stat(filepath.Join(r.state(), policyName)); !os.IsNotExist(err) {
		t.Fatalf("an empty policy file is left behind: %v", err)
	}
}

func TestScreenLeavesTemplatesInTheSealButStillJudgesTheirContent(t *testing.T) {
	r := newRig(t)
	r.write(".env.example", "OPENROUTER_API_KEY=your-key-here\n")
	r.write(".env.sample", "TOKEN="+fakeKey+"\n")
	r.write("config.template", "x=1\n")
	r.screen()
	if got := r.policy().withheld; len(got) != 1 || got[0] != ".env.sample" {
		t.Fatalf("withheld %v: a placeholder template stays in the seal, one holding a real key does not", got)
	}
	if keys.Exists(r.guard.Home) {
		t.Fatal("a template was imported into the vault")
	}
}

func TestScreenKeepsWhatThePersonWrote(t *testing.T) {
	r := newRig(t)
	r.write(policyName, "exclude node_modules\n")
	r.write("node_modules/x/key.txt", fakeKey)
	r.write("app/config.py", fakeKey)
	r.screen()
	p := r.policy()
	if len(p.outside) != 1 || p.outside[0] != "exclude node_modules" || len(p.withheld) != 1 || p.withheld[0] != "app/config.py" {
		t.Fatalf("policy %+v: the person's line stays, an excluded tree is not scanned", p)
	}
}

func TestScreenWritesNothingWhenTheTreeIsClean(t *testing.T) {
	r := newRig(t)
	r.write("a.txt", "hello\n")
	r.screen()
	if _, err := os.Stat(filepath.Join(r.state(), policyName)); !os.IsNotExist(err) {
		t.Fatalf("clean tree got a policy file: %v", err)
	}
	if keys.Exists(r.guard.Home) {
		t.Fatal("a clean tree created a vault")
	}
}

func TestScopeIsTheNormalizedRemoteElseTheCell(t *testing.T) {
	same := []string{"https://github.com/Org/repo.git", "git@GitHub.com:Org/repo", "ssh://git@github.com/Org/repo.git/"}
	want := keys.NormalizeRemote(same[0])
	for _, s := range same {
		if got := keys.NormalizeRemote(s); got != want || strings.Contains(got, "@") {
			t.Fatalf("normalize(%q) = %q, want %q", s, got, want)
		}
	}
	c := newCell(t)
	if scopeOf(c) != c.ID {
		t.Fatalf("no remote: scope %q, want the cell id", scopeOf(c))
	}
}

// TestL3_SecretsNeverEnterTheStore proves C9's working-tree law as amended by
// Abir on 2026-10-21: committed history travels sealed, so this fixture commits
// only non-secret files. The secrets remain untracked rather than ignored to
// prove the guard, independently of Git's ignore rules. After two seals no
// store object holds the key, and the next tool call still gets the .env value.
func TestL3_SecretsNeverEnterTheStore(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e := realEngine(t)
		r := newRig(t)
		e.Workspace, e.Guard = r.tree, r.guard
		r.write(".env", "OPENROUTER_API_KEY="+fakeKey+"\n")
		r.write("src/config.py", "API = '"+fakeKey+"'\n")
		r.write("src/main.py", "MARKER_KEPT = 1\n")
		commitPaths(t, r.tree, "src/main.py")

		if _, err := e.Seal(context.Background(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		r.write("src/main.py", "MARKER_KEPT = 2\n")
		commitPaths(t, r.tree, "src/main.py")
		if _, err := e.Seal(context.Background(), r.cell, TurnInfo{Changed: []string{"src/main.py"}}); err != nil {
			t.Fatal(err)
		}

		if out, err := gitIn(r.tree, "status", "--porcelain", "--untracked-files=no"); err != nil || out != "" {
			t.Fatalf("the tracked workspace changed: %q, %v", out, err)
		}
		if out := gitStatus(t, r.tree); out != "?? .env\n?? src/config.py" {
			t.Fatalf("the working-tree secrets must remain untracked: %q", out)
		}
		if hits := grepStore(t, e.LocalDir(r.cell), fakeKey); len(hits) != 0 {
			t.Fatalf("the secret is in the store: %v", hits)
		}
		if len(grepStore(t, e.LocalDir(r.cell), "MARKER_KEPT")) == 0 {
			t.Skip("the store does not keep file text in the clear, so the absence above proves nothing here")
		}
		if got := r.guard.Env(r.cell); len(got) != 1 || got[0] != "OPENROUTER_API_KEY="+fakeKey {
			t.Fatalf("next tool call env %v", got)
		}
		_, withheld, err := e.Withheld(r.cell)
		if err != nil || len(withheld) != 2 {
			t.Fatalf("withheld files = %v, %v; want the environment and source file", withheld, err)
		}
	})
}

func grepStore(t *testing.T, dir, needle string) (hits []string) {
	t.Helper()
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte(needle)) {
				hits = append(hits, p)
			}
		}
		return nil
	})
	return hits
}

// TestSeatGivesTheToolTheVaultEnv checks the return path: a process the seat
// runs sees the variable although no .env was in its sealed tree.
func TestSeatGivesTheToolTheVaultEnv(t *testing.T) {
	r := newRig(t)
	r.write(".env", "TOOL_TOKEN=abc123\n")
	r.screen()
	local := executor.Local{Root: r.tree, Class: executor.HostBound, Secrets: vaultEnvIn{r.guard, r.cell}}
	res, err := local.Exec(context.Background(), executor.ExecRequest{Argv: []string{"sh", "-c", "printf %s \"$TOOL_TOKEN\""}}, nil)
	if err != nil || string(res.Stdout) != "abc123" {
		t.Fatalf("tool saw %q, %v", res.Stdout, err)
	}
}

type vaultEnvIn struct {
	g Guard
	c cell.Cell
}

func (v vaultEnvIn) Env() []string { return v.g.Env(v.c) }

// BenchmarkScreen10k is the guard's share of a seal on a 10,000-file tree: a
// walk when the caller cannot say what changed, one path when it can.
func BenchmarkScreen10k(b *testing.B) {
	tree := b.TempDir()
	fillTree(b, tree, 10000)
	c, err := cell.CreateIn(b.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		b.Fatal(err)
	}
	g := Guard{Home: b.TempDir(), Notify: func(string) {}, Ledger: keys.NewLedger()}
	for name, changed := range map[string][]string{"walk": nil, "changed": {"src/d005/f005.txt"}} {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if err := g.Screen(c, tree, stateDir(c), changed); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestScreenWithLedgerStillCatchesAnEditedFile(t *testing.T) {
	r := newRig(t)
	r.guard.Ledger = keys.NewLedger()
	r.write("a.py", "x = 1\n")
	r.screen()
	r.write("a.py", "x = '"+fakeKey+"'\n")
	r.screen()
	if got := r.policy().withheld; len(got) != 1 || got[0] != "a.py" {
		t.Fatalf("withheld %v: an edited file must be read again", got)
	}
}

// TestC9SecretAtTheEndOfALargeSourceFileIsWithheld proves C9 beyond the
// scanner's former first-MiB limit and across the streaming read boundary.
func TestC9SecretAtTheEndOfALargeSourceFileIsWithheld(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, r := realEngine(t), newRig(t)
		e.Workspace, e.Guard = r.tree, r.guard
		r.write("large.py", strings.Repeat("# padding\n", 120000)+"KEY = '"+fakeKey+"'\n")
		r.write("boundary.py", strings.Repeat(" ", (64<<10)-8)+"KEY = '"+fakeKey+"'\n")
		if _, err := e.Seal(context.Background(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		if hits := grepStore(t, e.LocalDir(r.cell), fakeKey); len(hits) != 0 {
			t.Fatalf("secret in local store: %v", hits)
		}
		if _, held, err := e.Withheld(r.cell); err != nil || len(held) != 2 {
			t.Fatalf("large or boundary source was not withheld: %v, %v", held, err)
		}
	})
}

// TestC9CaptureAndRewindKeepWorkingTreeSecretsOut proves C9 when capture,
// comparison and rewind can save an automatic undo snapshot between seals.
func TestC9CaptureAndRewindKeepWorkingTreeSecretsOut(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, r := realEngine(t), newRig(t)
		e.Workspace, e.Guard = r.tree, r.guard
		r.write("note.txt", "first")
		commitPaths(t, r.tree, "note.txt")
		first, err := e.Seal(context.Background(), r.cell, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		r.write(".env", "KEY="+fakeKey+"\n")
		r.write("config.py", "KEY = '"+fakeKey+"'\n")
		r.write("note.txt", "second")
		if dirty, err := e.Dirty(context.Background(), r.cell); err != nil || !dirty {
			t.Fatalf("edited workspace: dirty %v, %v", dirty, err)
		}
		if _, err := e.Capture(context.Background(), r.cell); err != nil {
			t.Fatal(err)
		}
		if err := e.Restore(context.Background(), r.cell, first.Turn.ID, nil); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(filepath.Join(r.tree, "note.txt")); err != nil || string(raw) != "first" {
			t.Fatalf("restored note %q, %v", raw, err)
		}
		for _, path := range []string{".env", "config.py"} {
			if raw, err := os.ReadFile(filepath.Join(r.tree, path)); err != nil || !bytes.Contains(raw, []byte(fakeKey)) {
				t.Fatalf("the local secret %s was lost: %q, %v", path, raw, err)
			}
		}
		if hits := grepStore(t, e.LocalDir(r.cell), fakeKey); len(hits) != 0 {
			t.Fatalf("secret in local store: %v", hits)
		}
	})
}

func commitPaths(t *testing.T, dir string, paths ...string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, append([]string{"add", "--"}, paths...), {"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-qm", "safe files"}} {
		if out, err := gitIn(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base"}} {
		if out, err := gitIn(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// gitStatus is `git status --porcelain` of dir.
func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := gitIn(dir, "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //codeaf:plumbing test fixture inspects the workspace with git
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestWithheldNamesTheFilesTheGuardKeptOutAndTheFolderTheyAreIn(t *testing.T) {
	r := newRig(t)
	r.write("web/client/.env.production", "K="+fakeKey+"\n")
	r.write("src/app.py", "print('hi')\n")
	r.screen()
	eng := Engine{Workspace: r.tree}
	tree, paths, err := eng.Withheld(r.cell)
	if err != nil || tree != r.tree || len(paths) != 1 || paths[0] != "web/client/.env.production" {
		t.Fatalf("Withheld = %q, %v, %v", tree, paths, err)
	}
}

// TestC9CertificateBundleIsSealedWhilePrivateKeysAreWithheld proves C9's
// distinction at the actual seal and restore boundary, on both engine transports.
func TestC9CertificateBundleIsSealedWhilePrivateKeysAreWithheld(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, r := realEngine(t), newRig(t)
		e.Workspace, e.Guard = r.tree, r.guard
		cert := "-----BEGIN CERTIFICATE-----\nPUBLIC_CA_MARKER\n-----END CERTIFICATE-----\n"
		key := "-----BEGIN PRIVATE KEY-----\nPRIVATE_KEY_MARKER\n-----END PRIVATE KEY-----\n"
		r.write(".venv/lib/site-packages/certifi/cacert.pem", cert+cert)
		r.write("private.pem", key)
		r.write("combined.pem", cert+key)
		sealed, err := e.Seal(context.Background(), r.cell, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if got := r.policy().withheld; len(got) != 2 || got[0] != "combined.pem" || got[1] != "private.pem" {
			t.Fatalf("withheld %v; want only the two files holding a private key", got)
		}
		if hits := grepStore(t, e.LocalDir(r.cell), "PRIVATE_KEY_MARKER"); len(hits) != 0 {
			t.Fatalf("private key reached the store: %v", hits)
		}
		path := filepath.Join(r.tree, ".venv/lib/site-packages/certifi/cacert.pem")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := e.Restore(context.Background(), r.cell, sealed.Turn.ID, nil); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != cert+cert {
			t.Fatalf("certificate bundle did not survive sealing: %q, %v", raw, err)
		}
	})
}
