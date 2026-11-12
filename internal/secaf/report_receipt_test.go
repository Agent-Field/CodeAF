package secaf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// THE AUDIT ISSUES ITS RECEIPT FROM THE BYTES IT WROTE: one, for sec-report.json,
// equal to the file on the disk, and none for the markdown or the SARIF.
func TestReportFilesIssuesTheReceiptOfTheBytesItWrote(t *testing.T) {
	folder := t.TempDir()
	files, receipts, err := reportFiles(folder, schemas.SecurityAuditResult{}, false, runFacts{})
	if err != nil || len(files) == 0 {
		t.Fatalf("files %v err %v", files, err)
	}
	on, err := os.ReadFile(filepath.Join(folder, reportJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || receipts[0] != delegate.IssueReportReceipt(reportJSON, on) || !Program.DeclaresReport(receipts[0].Name) {
		t.Fatalf("receipts %+v", receipts)
	}
}
