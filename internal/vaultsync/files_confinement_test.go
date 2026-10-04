package vaultsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

func TestVaultFileRestoreRejectsExternalSymlinkParent(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		name := "write"
		if tombstone {
			name = "tombstone"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			a, b := r.machine(), r.machine()
			w := b.chatFolder(t)
			outside := t.TempDir()
			target := filepath.Join(outside, ".env")
			must(t, os.WriteFile(target, []byte("outside-original"), 0o600))
			must(t, os.Symlink(outside, filepath.Join(w.root, "nested")))
			id := fileScopePrefix + "chat/nested/.env"
			must(t, a.vault.PutAt(id, keys.Entry{Name: "nested/.env", Value: encodeFile(0o600, []byte("remote-value")), Scope: fileScopePrefix + "chat"}, time.Now()))
			if tombstone {
				must(t, a.vault.Delete(id))
			}
			must(t, a.Push(ctx))
			err := b.Pull(ctx)
			got, readErr := os.ReadFile(target)
			if readErr != nil || string(got) != "outside-original" {
				t.Fatalf("vault %s escaped workspace: outside=%q read error=%v (pull error=%v)", name, got, readErr, err)
			}
			if err == nil {
				t.Fatal("external symlink parent must report a confinement error")
			}
		})
	}
}

func TestFileMediumReadRejectsExternalSymlinkParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, ".env"), []byte("outside-secret"), 0o600))
	must(t, os.Symlink(outside, filepath.Join(root, "nested")))
	m := fileMedium{root: root, path: filepath.Join(root, "nested", ".env")}
	if data, _, err := m.Read(); err == nil {
		t.Fatalf("read escaped workspace and captured %q", data)
	}
}

func TestFileMediumConfinementPreservesNormalRestoreAndDelete(t *testing.T) {
	root := t.TempDir()
	m := fileMedium{root: root, path: filepath.Join(root, "new", "nested", ".env")}
	content := encodeFile(0o640, []byte("normal-value"))
	must(t, m.Write(content))
	got, _, err := m.Read()
	must(t, err)
	if got != content {
		t.Fatalf("restored %q, want %q", got, content)
	}
	must(t, m.Clear())
	if _, err := os.Stat(m.path); !os.IsNotExist(err) {
		t.Fatalf("normal delete failed: %v", err)
	}
	must(t, m.Clear())
}

// Pin the destination, then replace its name with an external symlink before
// mutation. This is the exact interleaving a preflight-lstat fix cannot secure.
func TestFileMediumPinnedParentDoesNotFollowReplacement(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	nested := filepath.Join(root, "nested")
	must(t, os.Mkdir(nested, 0o755))
	target := filepath.Join(outside, ".env")
	must(t, os.WriteFile(target, []byte("outside-original"), 0o600))
	m := fileMedium{root: root, path: filepath.Join(nested, ".env")}
	parent, name, close, err := m.parent(true)
	must(t, err)
	defer close()
	pinned := filepath.Join(root, "pinned")
	must(t, os.Rename(nested, pinned))
	must(t, os.Symlink(outside, nested))
	must(t, parent.WriteFile("temporary", []byte("inside"), 0o600))
	must(t, parent.Rename("temporary", name))
	if got := readFile(t, filepath.Join(pinned, ".env")); got != "inside" {
		t.Fatalf("write missed pinned directory: %q", got)
	}
	must(t, parent.Remove(name))
	if got := readFile(t, target); got != "outside-original" {
		t.Fatalf("parent replacement escaped root: %q", got)
	}
}
