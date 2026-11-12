package praf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// THE REVIEW ISSUES ITS RECEIPT FROM THE BYTES IT WROTE: one, for the one
// report it declares, equal to the file on the disk; the markdown beside it is
// written as before and issued nothing; and a folder of "" writes and issues
// nothing.
func TestWriteReportIssuesTheReceiptOfTheBytesItWrote(t *testing.T) {
	folder := t.TempDir()
	files, receipts, err := writeReport(folder, Saved{Program: Name}, runFacts{})
	if err != nil || len(files) != 2 {
		t.Fatalf("files %v err %v", files, err)
	}
	on, err := os.ReadFile(filepath.Join(folder, reportJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0] != delegate.IssueReportReceipt(reportJSON, on) || !Program.DeclaresReport(receipts[0].Name) {
		t.Fatalf("receipts %+v", receipts)
	}
	if files, receipts, err := writeReport("", Saved{}, runFacts{}); files != nil || receipts != nil || err != nil {
		t.Fatalf("no folder: %v %v %v", files, receipts, err)
	}
}

// A FOLDER THAT CANNOT BE WRITTEN ISSUES NO RECEIPT.
func TestWriteReportIssuesNothingForAWriteThatFailed(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(folder, []byte("a file where the folder goes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, receipts, err := writeReport(folder, Saved{Program: Name}, runFacts{}); err == nil || receipts != nil {
		t.Fatalf("receipts %v err %v", receipts, err)
	}
}
