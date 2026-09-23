package probe

// The fixture layer's own law, tested on the real disk: prepare is idempotent
// and deterministic, reset replays and reports, every fixture is private, and
// nothing here ever touches the real user's home.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// TestFixtureCleanPrepareIsIdempotentAndDeterministic: prepare in a fresh
// root, prepare again in ANOTHER fresh root, and the manifest hashes agree.
// Within one root a second prepare reuses the verified fixture and answers
// the same hash without rebuilding.
func TestFixtureCleanPrepareIsIdempotentAndDeterministic(t *testing.T) {
	first, err := FixturePrepare(t.TempDir(), "clean")
	if err != nil {
		t.Fatal(err)
	}
	second, err := FixturePrepare(t.TempDir(), "clean")
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("two fresh prepares disagree: %s vs %s", first.Hash, second.Hash)
	}

	root := t.TempDir()
	once, err := FixturePrepare(root, "clean")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := FixturePrepare(root, "clean")
	if err != nil {
		t.Fatal(err)
	}
	if once.Hash != twice.Hash || once.Home != twice.Home {
		t.Fatalf("prepare inside one root is not idempotent: %s/%s vs %s/%s", once.Hash, once.Home, twice.Hash, twice.Home)
	}
	if once.Seeded {
		t.Fatalf("the clean scenario seeded something: %+v", once)
	}
}

// TestFixturePrepareResetPrepareIdenticalManifest: the acceptance walk —
// prepare, reset, prepare again in fresh dirs, identical manifest hash. Reset
// replays the same steps and verifies its own tree, and a clean reset reports
// no drift.
func TestFixturePrepareResetPrepareIdenticalManifest(t *testing.T) {
	root := t.TempDir()
	a, err := FixturePrepare(root, "clean")
	if err != nil {
		t.Fatal(err)
	}
	b, err := FixtureReset(root, "clean")
	if err != nil {
		t.Fatal(err)
	}
	c, err := FixturePrepare(t.TempDir(), "clean")
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash || b.Hash != c.Hash {
		t.Fatalf("prepare/reset/prepare hashes disagree: %s %s %s", a.Hash, b.Hash, c.Hash)
	}
	if b.Drift != "" {
		t.Fatalf("a clean reset reported drift: %q", b.Drift)
	}
	if !b.Verified {
		t.Fatalf("reset did not verify its own tree")
	}
}

// TestFixtureReturningSeedsThroughTheProductWriter: the returning scenario's
// conversation identity is on the disk and readable back through the engine's
// own reader, session.LoadMeta — the surface reads its own output, not a
// second author's shape.
func TestFixtureReturningSeedsThroughTheProductWriter(t *testing.T) {
	f, err := FixturePrepare(t.TempDir(), "returning")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Seeded {
		t.Fatalf("the returning scenario did not seed: %+v", f)
	}
	meta, err := session.LoadMeta(returningBucket(f.Home))
	if err != nil {
		t.Fatalf("the seeded conversation does not read back through session.LoadMeta: %v", err)
	}
	if meta.Title == "" || meta.Workspace == "" {
		t.Fatalf("the seeded conversation has no title or workspace: %+v", meta)
	}
	if meta.Created.After(time.Now()) {
		t.Fatalf("the seeded conversation is stamped in the future: %v", meta.Created)
	}
}

// TestFixtureResetReplaysAfterDrift: mutate the tree, and reset reports the
// drift (verify-then-report, nothing silently repaired) and rebuilds a tree
// whose manifest hash matches a fresh prepare's.
func TestFixtureResetReplaysAfterDrift(t *testing.T) {
	root := t.TempDir()
	f, err := FixturePrepare(root, "returning")
	if err != nil {
		t.Fatal(err)
	}
	// Drift: a file the manifest never recorded, and a recorded file removed.
	if err := os.WriteFile(filepath.Join(f.Home, "stray.txt"), []byte("litter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.Home, "ledger", "notes.md")); err != nil {
		t.Fatal(err)
	}

	reset, err := FixtureReset(root, "returning")
	if err != nil {
		t.Fatal(err)
	}
	if reset.Drift == "" {
		t.Fatalf("reset did not report the drift it found")
	}
	// verifyTree names the first difference it finds, sorted; the missing
	// recorded file sorts before the stray one.
	if !strings.Contains(reset.Drift, "notes.md") {
		t.Fatalf("the drift report does not name the recorded file that went missing: %q", reset.Drift)
	}
	if !reset.Verified {
		t.Fatalf("reset did not verify its rebuilt tree")
	}

	fresh, err := FixturePrepare(t.TempDir(), "returning")
	if err != nil {
		t.Fatal(err)
	}
	if reset.Hash != fresh.Hash {
		t.Fatalf("the reset tree's hash %s does not match a fresh prepare's %s", reset.Hash, fresh.Hash)
	}
}

// TestFixtureNeverTouchesTheRealHome: with HOME standing at a sentinel
// directory OUTSIDE the probe root, prepare and reset leave that sentinel
// exactly as it was — no .codeaf appears, no file changes — and no manifest
// carries a path outside the root.
func TestFixtureNeverTouchesTheRealHome(t *testing.T) {
	sentinel := t.TempDir() // stands in for the real user's home
	before := walk(t, sentinel)

	root := filepath.Join(t.TempDir(), "probe-root")
	for _, name := range FixtureScenarios() {
		if _, err := FixturePrepare(root, name); err != nil {
			t.Fatal(err)
		}
		if _, err := FixtureReset(root, name); err != nil {
			t.Fatal(err)
		}
	}

	after := walk(t, sentinel)
	if before != after {
		t.Fatalf("the sentinel home changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, name := range FixtureScenarios() {
		dir := fixtureDir(root, name)
		raw, err := os.ReadFile(manifestPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("the %s manifest names the real home: %s", name, raw)
		}
		if _, err := os.Stat(filepath.Join(sentinel, ".codeaf")); !os.IsNotExist(err) {
			t.Fatalf("prepare created state in the sentinel home: %v", err)
		}
	}
}

// TestFixtureIsPrivate: the per-run root, every directory under it and every
// file in a prepared fixture are private — 0700 directories, 0600 files.
func TestFixtureIsPrivate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "probe-root")
	if _, err := FixturePrepare(root, "returning"); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if d.IsDir() && perm != 0o700 {
			t.Errorf("%s: directory mode %04o, want 0700", path, perm)
		}
		if !d.IsDir() && perm != 0o600 {
			t.Errorf("%s: file mode %04o, want 0600", path, perm)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFixtureEnvironIsSanitized: the environment a fixture hands a binary
// names the fixture home as HOME, drops inherited secrets, and never pins the
// state root outside the fixture.
func TestFixtureEnvironIsSanitized(t *testing.T) {
	t.Setenv("SOME_API_KEY", "secret-value")
	t.Setenv("CODEAF_HOME", "/elsewhere")
	env := FixtureEnviron("/fixture/home")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HOME=/fixture/home") {
		t.Fatalf("the environment does not set HOME to the fixture: %s", joined)
	}
	if strings.Contains(joined, "secret-value") {
		t.Fatalf("the environment carries an inherited credential: %s", joined)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "CODEAF_HOME=") {
			t.Fatalf("the environment pins CODEAF_HOME: %s", kv)
		}
	}
}

// TestFixtureUnknownScenarioIsRefused: an unknown name is refused by naming
// what IS known, never a silent fallthrough to clean.
func TestFixtureUnknownScenarioIsRefused(t *testing.T) {
	if _, err := FixturePrepare(t.TempDir(), "no-such-scenario"); err == nil {
		t.Fatalf("an unknown scenario was accepted")
	}
	if _, err := FixtureReset(t.TempDir(), "no-such-scenario"); err == nil {
		t.Fatalf("an unknown scenario was accepted by reset")
	}
}

// walk records a sentinel home's whole tree so the isolation test can compare
// it before and after: names and modes only, since mtimes move on their own.
func walk(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(rel + " " + info.Mode().String() + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
