package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type reifyCall struct {
	dir string
	add []string
}

type mockReifier struct {
	calls []reifyCall
	node  *ReifiedNode
	err   error
}

func (m *mockReifier) Reify(_ context.Context, dir string, add []string) (*ReifiedNode, error) {
	m.calls = append(m.calls, reifyCall{dir: dir, add: append([]string(nil), add...)})
	return m.node, m.err
}

func TestNpmAddExistingAndReified(t *testing.T) {
	cache := t.TempDir()
	reifier := &mockReifier{}
	npm := NewNpm(cache, reifier)
	installed := filepath.Join(cache, "packages", "@scope", "pkg@1", "node_modules", "@scope", "pkg")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "package.json"), []byte(`{"main":"main.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "main.js"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := npm.Add(context.Background(), "@scope/pkg@1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Directory != installed || entry.Entrypoint == nil || len(reifier.calls) != 0 {
		t.Fatalf("entry=%+v calls=%v", entry, reifier.calls)
	}

	reifier.node = &ReifiedNode{Name: "foo", Path: filepath.Join(cache, "installed-foo")}
	entry, err = npm.Add(context.Background(), "foo@2")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Directory != reifier.node.Path || !reflect.DeepEqual(reifier.calls[0].add, []string{"foo@2"}) {
		t.Fatalf("entry=%+v calls=%v", entry, reifier.calls)
	}
}

func TestNpmInstallChecksNodeModulesAndLock(t *testing.T) {
	dir := t.TempDir()
	reifier := &mockReifier{}
	npm := NewNpm(t.TempDir(), reifier)
	if err := npm.Install(context.Background(), dir, []PackageRequest{{Name: "a", Version: "1"}}); err != nil {
		t.Fatal(err)
	}
	if len(reifier.calls) != 1 || !reflect.DeepEqual(reifier.calls[0].add, []string{"a@1"}) {
		t.Fatalf("initial calls: %#v", reifier.calls)
	}

	reifier.calls = nil
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"a":"1","b":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"packages":{"":{"dependencies":{"a":"1"}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := npm.Install(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if len(reifier.calls) != 1 {
		t.Fatalf("dirty calls: %#v", reifier.calls)
	}
}

func TestNpmWhichSelectionAndRepairFailure(t *testing.T) {
	cache := t.TempDir()
	reifier := &mockReifier{err: errors.New("offline")}
	npm := NewNpm(cache, reifier)
	dir := filepath.Join(cache, "packages", "pkg")
	binDir := filepath.Join(dir, "node_modules", ".bin")
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"other", "pkg"} {
		if err := os.WriteFile(filepath.Join(binDir, name), nil, 0o755); err != nil {
			if os.IsNotExist(err) {
				if mkdirErr := os.MkdirAll(binDir, 0o755); mkdirErr != nil {
					t.Fatal(mkdirErr)
				}
				if err := os.WriteFile(filepath.Join(binDir, name), nil, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "package.json"), []byte(`{"bin":{"pkg":"x","other":"y"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := npm.Which(context.Background(), "pkg")
	if !ok || got != filepath.Join(binDir, "pkg") {
		t.Fatalf("which = %q, %v", got, ok)
	}
	got, ok = npm.Which(context.Background(), "pkg", "other")
	if !ok || got != filepath.Join(binDir, "other") {
		t.Fatalf("which hint = %q, %v", got, ok)
	}
	if err := os.RemoveAll(binDir); err != nil {
		t.Fatal(err)
	}
	if _, ok := npm.Which(context.Background(), "missing"); ok {
		t.Fatal("missing package unexpectedly resolved")
	}
}

func TestSanitizeForWindows(t *testing.T) {
	if got := sanitizeForPlatform("a:b?c\x00d", "windows"); got != "a_b_c_d" {
		t.Fatalf("sanitize = %q", got)
	}
}
