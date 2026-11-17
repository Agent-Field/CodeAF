package subharness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSavedReadBudgetRefusesBeforeLoading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	b := savedReadBudget{bytes: SavedListReadBytes - 1}
	if _, err := b.read(path); !errors.Is(err, ErrSavedListTooLarge) || b.bytes != SavedListReadBytes-1 {
		t.Fatalf("aggregate byte budget: %d, %v", b.bytes, err)
	}
	visits := 0
	b = savedReadBudget{entries: SavedListEntries}
	if err := b.scan(filepath.Dir(path), func(os.DirEntry) error { visits++; return nil }); !errors.Is(err, ErrSavedListTooLarge) || visits != 0 {
		t.Fatalf("directory budget: visited=%d, %v", visits, err)
	}
}
