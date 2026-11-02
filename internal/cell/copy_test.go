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
	"strings"
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

// TestTheCopyFolderSaysItIsTheLiveStore pins #2072: the folder holding moved
// chats is named for a move in progress, so it says, to anyone who looks, that
// it holds the chats themselves and must not be removed.
func TestTheCopyFolderSaysItIsTheLiveStore(t *testing.T) {
	original := olderFixture(t)
	if _, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(CopyRoot(original)), CopyFolderReadme))
	if err != nil || !strings.Contains(string(raw), "Do not delete it") {
		t.Fatalf("the copy folder does not explain itself: %q, %v", raw, err)
	}
}

// TestCopyLegacyRefusesToWipePopulatedDestination verifies the second line of
// defence when a destination copy already holds sealed turns (either in
// turns.jsonl or adopted.json) without copy-ready: CopyLegacy opens and
// re-stamps a valid cell without rerunning prepare or removing the destination,
// and fails loudly without removing the destination if OpenAt fails.
func TestCopyLegacyRefusesToWipePopulatedDestination(t *testing.T) {
	for _, marker := range []string{"turns.jsonl", "adopted.json"} {
		t.Run(marker, func(t *testing.T) {
			original := olderFixture(t)
			c, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			// Append precious post-copy content and record a sealed turn marker.
			transcriptPath := filepath.Join(c.Root, filepath.FromSlash(TranscriptPath))
			if err := os.WriteFile(transcriptPath, []byte("whole conversation\nprecious turn\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch marker {
			case "turns.jsonl":
				if err := os.WriteFile(filepath.Join(c.Root, StateDir, "turns.jsonl"),
					[]byte(`{"V":1,"id":"1111111111111111111111111111111111111111111111111111111111111111"}`+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "adopted.json":
				if err := os.WriteFile(filepath.Join(c.Root, StateDir, "adopted.json"),
					[]byte(`{"V":1,"head":"2222222222222222222222222222222222222222222222222222222222222222"}`+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Remove copy-ready to simulate an older build's restore or take-back stripping it.
			readyFile := filepath.Join(c.Root, StateDir, "copy-ready")
			if err := os.Remove(readyFile); err != nil {
				t.Fatal(err)
			}

			reopened, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error {
				t.Fatal("CopyLegacy re-ran prepare on an already-populated copy")
				return nil
			})
			if err != nil {
				t.Fatalf("CopyLegacy failed instead of opening populated copy: %v", err)
			}
			if reopened.Root != c.Root {
				t.Fatalf("CopyLegacy returned root %s, want %s", reopened.Root, c.Root)
			}
			after, err := os.ReadFile(transcriptPath)
			if err != nil || !strings.Contains(string(after), "precious turn") {
				t.Fatalf("populated copy transcript lost precious turn: err=%v body=%q", err, string(after))
			}
			if Copied(original) != c.Root {
				t.Fatalf("Copied(original) = %s, want %s", Copied(original), c.Root)
			}
		})
	}

	t.Run("unopenable_populated_destination_fails_loudly_without_removal", func(t *testing.T) {
		original := olderFixture(t)
		c, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		transcriptPath := filepath.Join(c.Root, filepath.FromSlash(TranscriptPath))
		if err := os.WriteFile(transcriptPath, []byte("precious unopenable turn\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.Root, StateDir, "turns.jsonl"),
			[]byte(`{"V":1,"id":"1111111111111111111111111111111111111111111111111111111111111111"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(c.Root, StateDir, "copy-ready")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.Root, filepath.FromSlash(MetaPath)), []byte("{corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := CopyLegacy(t.Context(), original, func(context.Context, Cell) error { return nil }); err == nil {
			t.Fatal("expected CopyLegacy to fail loudly when populated destination cannot be opened")
		}
		after, err := os.ReadFile(transcriptPath)
		if err != nil || !strings.Contains(string(after), "precious unopenable turn") {
			t.Fatalf("CopyLegacy deleted or altered populated unopenable destination: err=%v body=%q", err, string(after))
		}
	})
}
