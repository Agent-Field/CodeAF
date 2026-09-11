package standing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// R2, THE RECEIPT IS THE FILE'S. What one item put at a path is what the next
// item reads there, whichever item it was, under a folder named for the path's
// hash and stamped with its schema.
func TestAReportPathsReceiptIsKeptUnderThePathForAnyItem(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const path = "/project/reports/r.md"
	if err := store.AtReport(path, "aaaaaaaaaaaaaaaa", "reports/r.md", func(last *Receipt) (*Receipt, error) {
		if last != nil {
			t.Fatalf("a path nothing was ever put at has a receipt: %+v", last)
		}
		return &Receipt{Class: ReceiptPublished, Item: "aaaaaaaaaaaaaaaa", SHA256: "first", Bytes: 5}, nil
	}); err != nil {
		t.Fatal(err)
	}
	file := store.receiptFile(path)
	if shard := filepath.Base(filepath.Dir(file)); len(shard) != 2 || !strings.HasPrefix(filepath.Base(file), shard) || filepath.Base(filepath.Dir(filepath.Dir(file))) != receiptsDir {
		t.Fatalf("the receipt is not fanned out by its path's hash: %s", file)
	}
	var seen *Receipt
	if err := store.AtReport(path, "bbbbbbbbbbbbbbbb", "reports/r.md", func(last *Receipt) (*Receipt, error) {
		seen = last
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.SHA256 != "first" || seen.Schema != ReceiptSchema || seen.Path != path || seen.Item != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("the next item read %+v at the path, want the first item's stamped receipt", seen)
	}
}

// L5, READ BOTH. An item that published before paths kept receipts finds its
// own last publication through the path, until its next one is kept there.
func TestAnItemsReceiptFromBeforePathsIsStillFoundThroughThePath(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const id = "0123456789abcdef"
	if err := os.MkdirAll(store.ItemDir(id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ItemDir(id), publicationsFile), []byte(`{"reports/r.md":{"path":"reports/r.md","sha256":"old","bytes":3}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen *Receipt
	if err := store.AtReport("/project/reports/r.md", id, "reports/r.md", func(last *Receipt) (*Receipt, error) {
		seen = last
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.SHA256 != "old" || seen.Item != id {
		t.Fatalf("the item's own receipt from before was not found: %+v", seen)
	}
	// Another item is not handed a receipt that was never the path's.
	if err := store.AtReport("/project/reports/r.md", "", "", func(last *Receipt) (*Receipt, error) {
		seen = last
		return nil, nil
	}); err != nil || seen != nil {
		t.Fatalf("a caller that is no item read %+v (%v)", seen, err)
	}
}

// L4, UNREADABLE STATE IS REPORTED AND NEVER WRITTEN OVER. A receipt that does
// not parse, or that a newer build wrote, stops the act before it happens.
func TestAReceiptThatCannotBeReadStopsTheActAndIsKept(t *testing.T) {
	for name, body := range map[string]string{"garbage": "not json", "newer": `{"schema":99,"sha256":"x"}`} {
		store, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		const path = "/project/reports/r.md"
		file := store.receiptFile(path)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		acted := false
		err = store.AtReport(path, "", "", func(*Receipt) (*Receipt, error) {
			acted = true
			return &Receipt{Class: ReceiptPublished, SHA256: "y"}, nil
		})
		raw, _ := os.ReadFile(file)
		if err == nil || acted || string(raw) != body {
			t.Fatalf("%s: a receipt that could not be read let the act run (%v, acted %v, now %q)", name, err, acted, raw)
		}
	}
}

// AND A RECEIPT THAT COULD NOT BE WRITTEN AFTER ITS ACT IS SAID AS THAT, so a
// report already placed is never reported as one that was not.
func TestAReceiptThatCouldNotBeKeptIsSaidAfterItsAct(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restore := syncFolder
	defer func() { syncFolder = restore }()
	syncFolder = func(*os.File) error { return syscall.EIO }
	acted := false
	err = store.AtReport("/project/reports/r.md", "", "", func(*Receipt) (*Receipt, error) {
		acted = true
		return &Receipt{Class: ReceiptPublished, SHA256: "y"}, nil
	})
	if !acted || !errors.Is(err, ErrReceiptNotKept) || !errors.Is(err, syscall.EIO) {
		t.Fatalf("acted %v, error %v; want the act done and the receipt said not kept", acted, err)
	}
}
