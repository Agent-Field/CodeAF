package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

func unfinishedSweepCopy(t *testing.T, now time.Time) (string, string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	original := filepath.Join(root, "bucket", "older-chat")
	writeFile(t, filepath.Join(original, "transcript.jsonl"), "older conversation\n")
	copy := cell.CopyRoot(original)
	writeFile(t, filepath.Join(copy, "work", "private.txt"), "unfinished copy")
	writeFile(t, copy+".lock", "")
	stampTree(t, copy, now.Add(-8*24*time.Hour))
	return root, original, copy
}

func TestSweepReapsUnfinishedOlderChatCopyBeforeItsLock(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	root, original, copy := unfinishedSweepCopy(t, now)
	observed := false
	SweepPlaces(root, now, func(line string) {
		if _, err := os.Stat(copy); !os.IsNotExist(err) {
			t.Fatalf("folder must go before removal notice: %s (%v)", line, err)
		}
		lock, err := os.Open(copy + ".lock")
		if err != nil {
			t.Fatalf("lock removed before folder: %v", err)
		}
		defer lock.Close()
		if err := filelock.Lock(lock, true, true); err == nil {
			t.Fatal("copy lock not held across removal")
		}
		observed = true
	})
	if _, err := os.Stat(copy); !os.IsNotExist(err) {
		t.Fatalf("unfinished older-chat copy survived: %v", err)
	}
	if _, err := os.Stat(copy + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("copy lock survived: %v", err)
	}
	if !observed {
		t.Fatal("no removal notice")
	}
	if _, err := os.Stat(filepath.Join(original, "transcript.jsonl")); err != nil {
		t.Fatalf("original lost: %v", err)
	}
}

func TestSweepKeepsOlderChatCopyUnlessEveryConditionHolds(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, string, string, time.Time)
	}{
		{"missing lock", func(t *testing.T, original, copy string, now time.Time) {
			if err := os.Remove(copy + ".lock"); err != nil {
				t.Fatal(err)
			}
		}},
		{"held lock", func(t *testing.T, original, copy string, now time.Time) {
			lock, err := os.OpenFile(copy+".lock", os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { lock.Close() })
			if err := filelock.Lock(lock, true, true); err != nil {
				t.Fatal(err)
			}
		}},
		{"ready", func(t *testing.T, original, copy string, now time.Time) {
			writeFile(t, filepath.Join(copy, cell.CopyReadyPath), "")
		}},
		{"sealed turn", func(t *testing.T, original, copy string, now time.Time) {
			writeFile(t, filepath.Join(copy, cell.StateDir, "turns.jsonl"), "{\"id\":\"sealed\"}\n")
		}},
		{"sealed turn longer than a scanner line", func(t *testing.T, original, copy string, now time.Time) {
			writeFile(t, filepath.Join(copy, cell.StateDir, "turns.jsonl"), "{\"id\":\"sealed\",\"x\":\""+strings.Repeat("x", 128<<10)+"\"}\n")
		}},
		{"unparsable turns", func(t *testing.T, original, copy string, now time.Time) {
			writeFile(t, filepath.Join(copy, cell.StateDir, "turns.jsonl"), "not json\n")
		}},
		{"unparsable adopted head", func(t *testing.T, original, copy string, now time.Time) {
			writeFile(t, filepath.Join(copy, cell.StateDir, "adopted.json"), "not json")
		}},
		{"adopted head", func(t *testing.T, original, copy string, now time.Time) {
			writeFile(t, filepath.Join(copy, cell.StateDir, "adopted.json"), "{\"head\":\"adopted\"}")
		}},
		{"missing original", func(t *testing.T, original, copy string, now time.Time) {
			if err := os.RemoveAll(original); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing transcript", func(t *testing.T, original, copy string, now time.Time) {
			if err := os.Remove(filepath.Join(original, "transcript.jsonl")); err != nil {
				t.Fatal(err)
			}
		}},
		{"different original", func(t *testing.T, original, copy string, now time.Time) {
			if err := os.Rename(original, original+"-renamed"); err != nil {
				t.Fatal(err)
			}
		}},
		{"recent nested file", func(t *testing.T, original, copy string, now time.Time) {
			p := filepath.Join(copy, "work", "private.txt")
			if err := os.Chtimes(p, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
		}},
		{"exactly one week", func(t *testing.T, original, copy string, now time.Time) { stampTree(t, copy, now.Add(-7*24*time.Hour)) }},
		{"recent directory", func(t *testing.T, original, copy string, now time.Time) {
			if err := os.Chtimes(copy, now, now); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			root, original, copy := unfinishedSweepCopy(t, now)
			tc.change(t, original, copy, now)
			// Readiness and history must protect even an otherwise old tree.
			if tc.name != "recent nested file" && tc.name != "exactly one week" && tc.name != "recent directory" {
				stampTree(t, copy, now.Add(-8*24*time.Hour))
			}
			SweepPlaces(root, now, func(line string) { t.Fatalf("unexpected sweep notice: %s", line) })
			if _, err := os.Stat(filepath.Join(copy, "work", "private.txt")); err != nil {
				t.Fatalf("protected copy lost: %v", err)
			}
			if tc.name != "missing lock" {
				if _, err := os.Stat(copy + ".lock"); err != nil {
					t.Fatalf("protected lock lost: %v", err)
				}
			}
		})
	}
}

func TestSweepHomeContextReapsOnlyRecognizedUnfinishedCopies(t *testing.T) {
	now := time.Now()
	root, _, copy := unfinishedSweepCopy(t, now)
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	projects := filepath.Join(state, "v3", placesDirName)
	if err := os.MkdirAll(filepath.Dir(projects), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, projects); err != nil {
		t.Fatal(err)
	}
	copy = filepath.Join(projects, "bucket", ".moving", filepath.Base(copy))
	destination := cell.CopyRoot(filepath.Join(projects, "bucket", "older-chat"))
	if err := os.Rename(copy, destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(copy+".lock", destination+".lock"); err != nil {
		t.Fatal(err)
	}
	copy = destination
	unknown := filepath.Join(filepath.Dir(copy), "unknown-chat")
	writeFile(t, filepath.Join(unknown, "transcript.jsonl"), "standalone chat")
	writeFile(t, unknown+".lock", "")
	writeFile(t, filepath.Join(filepath.Dir(copy), cell.CopyFolderReadme), "keep this note")
	stampTree(t, unknown, now.Add(-8*24*time.Hour))
	SweepHomeContext(context.Background(), "", nil, nil)
	if _, err := os.Stat(copy); !os.IsNotExist(err) {
		t.Fatalf("launch left scratch copy: %v", err)
	}
	for _, kept := range []string{unknown, unknown + ".lock", filepath.Join(filepath.Dir(copy), cell.CopyFolderReadme)} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("launch touched unrelated moving entry: %v", err)
		}
	}
}
