package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// A workspace can have several filesystem names, including macOS's /var and
// /private/var. Compare the delivered file rather than rejecting an alias for it.
// A missing or different destination still fails the live report assertion.
func organizationSameFile(actual, expected string) bool {
	a, err := os.Stat(actual)
	if err != nil {
		return false
	}
	b, err := os.Stat(expected)
	return err == nil && os.SameFile(a, b)
}

func TestOrganizationReportAcceptsOnlyTheRequestedFile(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "report.json")
	if err := os.WriteFile(actual, []byte(`{"revision":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "another-report.json")
	if err := os.WriteFile(other, []byte(`{"revision":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !organizationSameFile(filepath.Join(alias, "report.json"), actual) {
		t.Fatal("an alias for the requested file was rejected")
	}
	if organizationSameFile(other, actual) || organizationSameFile(filepath.Join(dir, "missing.json"), actual) {
		t.Fatal("a different or missing report was accepted")
	}
}
