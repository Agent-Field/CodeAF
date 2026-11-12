package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConversationDeleteRemovesOwnedCheckReceiptsOnly(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "directory"
		if linked {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			root, profile, file := deletionFixture(t)
			receipts := file + ".checks"
			target := receipts
			if linked {
				target = t.TempDir()
			}
			call := filepath.Join(target, "call-first")
			if err := os.MkdirAll(call, 0700); err != nil {
				t.Fatal(err)
			}
			receipt := filepath.Join(call, "command.json")
			if err := os.WriteFile(receipt, []byte(`"go test ./..."`), 0600); err != nil {
				t.Fatal(err)
			}
			if linked {
				if err := os.Symlink(target, receipts); err != nil {
					t.Fatal(err)
				}
			}
			// A deliverable and another journal's receipts are not ours to erase.
			kept := []string{
				filepath.Join(filepath.Dir(file), "report.txt"),
				filepath.Join(file+".checks-other", "command.json"),
			}
			for _, path := range kept {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stopping := errors.New("owner has not stopped")
			if err := DeleteConversationUnder(root, profile, file, nil, func(string) error { return stopping }); !errors.Is(err, stopping) {
				t.Fatalf("owner stop refusal was lost: %v", err)
			}
			if _, err := os.Stat(receipt); err != nil {
				t.Fatalf("refused deletion removed verification history: %v", err)
			}
			if err := DeleteConversationUnder(root, profile, file, nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(receipts); !os.IsNotExist(err) {
				t.Fatalf("owned check receipts survived deletion: %v", err)
			}
			for _, path := range kept {
				if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
					t.Fatalf("unrelated data changed at %s: %q, %v", path, data, err)
				}
			}
			if linked {
				if _, err := os.Stat(receipt); err != nil {
					t.Fatalf("cleanup followed receipt symlink: %v", err)
				}
			}
			if err := removeDeletedConversationJournal(file); err != nil {
				t.Fatalf("repeated journal cleanup failed: %v", err)
			}
		})
	}
}
