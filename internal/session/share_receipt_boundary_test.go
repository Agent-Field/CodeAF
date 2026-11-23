package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShareReceiptRejectsOversizedAndLinkedFiles(t *testing.T) {
	root := t.TempDir()
	claim, err := AdmitShareOperation(root, "session", "operation", []string{"/w/a.png"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	claim.Release()
	_, receipt := ShareOpPaths(root, "session", "operation")
	if err := os.WriteFile(receipt, []byte(strings.Repeat(" ", shareOpReceiptMax+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadShareReceipt(root, "session", "operation"); err == nil {
		t.Fatal("oversized receipt trusted")
	}
	target := filepath.Join(root, "target")
	if err := os.Rename(receipt, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, receipt); err != nil {
		t.Skip(err)
	}
	if _, err := LoadShareReceipt(root, "session", "operation"); err == nil {
		t.Fatal("linked receipt trusted")
	}
}

func TestShareReceiptRejectsCorruptIdentityAndSchema(t *testing.T) {
	for _, mutation := range []string{"session", "operation", "version", "status", "digest", "path", "duplicate", "recorded", "provenance"} {
		t.Run(mutation, func(t *testing.T) {
			root := t.TempDir()
			c, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png", "/w/b.png"}, ShareProvenance{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			r := c.Receipt
			c.Release()
			switch mutation {
			case "session":
				r.Session = "other"
				r.Provenance.Session = "other"
			case "operation":
				r.OperationID = "other"
			case "version":
				r.Version++
			case "status":
				r.Status = "success"
			case "digest":
				r.PayloadDigest = "bad"
			case "path":
				r.Planned[0].Path = "a/../a.png"
			case "duplicate":
				r.Planned[1].ID = r.Planned[0].ID
			case "recorded":
				r.RecordedIDs = []string{NewDeliverableID()}
			case "provenance":
				r.Provenance.Session = "other"
			}
			raw, _ := json.Marshal(r)
			_, path := ShareOpPaths(root, "s", "op")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadShareReceipt(root, "s", "op"); err == nil {
				t.Fatal("corrupt receipt trusted")
			}
			if got, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png", "/w/b.png"}, ShareProvenance{}, time.Time{}); err == nil {
				got.Release()
				t.Fatal("corrupt receipt replayed")
			}
		})
	}
}

func TestShareReceiptUnknownCanReconcileWithoutNewIDs(t *testing.T) {
	root := t.TempDir()
	c, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, ShareProvenance{Call: "original"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	id := c.Receipt.Planned[0].ID
	planShareForTest(t, c)
	if err := c.Settle(ShareOpUnknown, nil); err != nil {
		t.Fatal(err)
	}
	replay, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, ShareProvenance{Call: "later"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Receipt.Planned[0].ID != id || replay.Receipt.Provenance.Call != "original" {
		t.Fatal("unknown replay changed authority")
	}
	if err := replay.Settle(ShareOpAccepted, []string{id}); err != nil {
		t.Fatal(err)
	}
	r, err := LoadShareReceipt(root, "s", "op")
	if err != nil || r.Status != ShareOpAccepted {
		t.Fatalf("reconciled=%+v err=%v", r, err)
	}
}

func TestShareReceiptDeadlineCannotBeRemoved(t *testing.T) {
	for _, requested := range []time.Time{{}, time.Now().Add(time.Hour)} {
		before := time.Now()
		got := boundedShareDeadline(requested)
		if got.Before(before) || got.After(time.Now().Add(shareOpTimeout)) {
			t.Fatalf("unbounded deadline %v", got)
		}
	}
}

func TestShareReceiptRefusesALinkedLock(t *testing.T) {
	root := t.TempDir()
	c, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	c.Release()
	lock, _ := ShareOpPaths(root, "s", "op")
	target := filepath.Join(root, "target-lock")
	if err := os.Rename(lock, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lock); err != nil {
		t.Skip(err)
	}
	if c, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, ShareProvenance{}, time.Time{}); err == nil {
		c.Release()
		t.Fatal("linked lock admitted")
	}
}
