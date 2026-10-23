package cell

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fileStamp struct {
	Mode fs.FileMode
	Time int64
	Sum  [32]byte
}

func copyFingerprint(t *testing.T, root string) map[string]fileStamp {
	t.Helper()
	out := map[string]fileStamp{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		var data []byte
		if !d.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		out[rel] = fileStamp{info.Mode(), info.ModTime().UnixNano(), sha256.Sum256(data)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func olderFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "older-chat")
	for path, body := range map[string]string{"transcript.jsonl": "whole conversation\n", "state.json": "dials", "tasks/a.jsonl": "task journal"} {
		name := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(name), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0640); err != nil {
			t.Fatal(err)
		}
		at := time.Unix(1234567890, 987654321)
		if err := os.Chtimes(name, at, at); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Unix(1234567890, 987654300)
	for _, dir := range []string{filepath.Join(root, "tasks"), root} {
		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestC29CopyNeverTouchesTheOriginal proves C29.1/C29.3: making and using the
// copy leaves every source file, mode and mtime unchanged and retries reuse it.
func TestC29CopyNeverTouchesTheOriginal(t *testing.T) {
	original := olderFixture(t)
	before := copyFingerprint(t, original)
	c, err := CopyLegacy(t.Context(), original, func(ctx context.Context, c Cell) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(c.Root, TranscriptPath), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("a new turn\n")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	again, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { t.Fatal("prepared twice"); return nil })
	if err != nil || again.Root != c.Root {
		t.Fatalf("duplicate copy: %v %v", again, err)
	}
	if !reflect.DeepEqual(before, copyFingerprint(t, original)) {
		t.Fatal("the original changed")
	}
	if !ValidID(c.ID) {
		t.Fatal("copy identity cannot move")
	}
}

// TestC29CopyFailureIsInvisibleAndRetryCompletes proves C29.6 at each public
// preparation boundary: failure leaves no visible copy and retry publishes one.
func TestC29CopyFailureIsInvisibleAndRetryCompletes(t *testing.T) {
	original := olderFixture(t)
	before := copyFingerprint(t, original)
	_, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return fmt.Errorf("interrupted before durable seal") })
	if err == nil || Copied(original) != original {
		t.Fatal("partial copy became visible")
	}
	if !reflect.DeepEqual(before, copyFingerprint(t, original)) {
		t.Fatal("original changed on interruption")
	}
	if _, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if Copied(original) == original {
		t.Fatal("retry did not finish")
	}
}

// TestC29KilledCopyRestarts proves C29.6 with an actual process kill between
// private construction and publication, including native Windows Process.Kill.
func TestC29KilledCopyRestarts(t *testing.T) {
	if root := os.Getenv("C6_COPY_KILL_ROOT"); root != "" {
		copyCheckpoint = func(step string) {
			if step == os.Getenv("C6_COPY_KILL_STEP") {
				p, _ := os.FindProcess(os.Getpid())
				_ = p.Kill()
				select {}
			}
		}
		_, _ = CopyLegacy(context.Background(), root, func(context.Context, Cell) error { return nil })
		os.Exit(3)
	}
	for round := 0; round < 3; round++ {
		for _, step := range []string{"source-locked", "file-created", "file-written", "copied", "migrated", "prepared", "published"} {
			t.Run(fmt.Sprintf("%d/%s", round, step), func(t *testing.T) {
				original := olderFixture(t)
				before := copyFingerprint(t, original)
				cmd := exec.Command(os.Args[0], "-test.run=^TestC29KilledCopyRestarts$")
				cmd.Env = append(os.Environ(), "C6_COPY_KILL_ROOT="+original, "C6_COPY_KILL_STEP="+step)
				if err := cmd.Run(); err == nil {
					t.Fatal("child was not killed")
				}
				after := copyFingerprint(t, original)
				if !reflect.DeepEqual(before, after) {
					for name, was := range before {
						if now := after[name]; now != was {
							t.Errorf("changed %s: before=%+v after=%+v", name, was, now)
						}
					}
					t.Fatal("kill changed original")
				}
				if _, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return nil }); err != nil {
					t.Fatal(err)
				}
				if Copied(original) == original {
					t.Fatal("retry did not publish a complete copy")
				}
			})
		}
	}
}

// TestC29LinkedFolderCannotAliasTheOriginal proves C29.1: a supplied folder
// link must never become the destination of migration writes through the copy.
func TestC29LinkedFolderCannotAliasTheOriginal(t *testing.T) {
	original := olderFixture(t)
	alias := filepath.Join(t.TempDir(), "linked-chat")
	if err := os.Symlink(original, alias); err != nil {
		t.Skipf("folder link fixture unavailable: %v", err)
	}
	before := copyFingerprint(t, original)
	if _, err := CopyLegacy(t.Context(), alias, func(context.Context, Cell) error { t.Fatal("linked original was prepared"); return nil }); err == nil {
		t.Fatal("folder link was copied as an alias")
	}
	if !reflect.DeepEqual(before, copyFingerprint(t, original)) {
		t.Fatal("copy through a folder link changed the original")
	}
}
