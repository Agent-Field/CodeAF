package session

import (
	"strings"
	"testing"
)

// A declaration admitted by an older reader remains part of the checker's
// contract. The audit door must not silently erase it merely because the
// current proposal door would refuse its length.
func TestOverlongDeclaredCheckStillOpensTheAuditDoor(t *testing.T) {
	check := "printf " + strings.Repeat("x", auditCommandLimit)
	if len(check) <= auditCommandLimit {
		t.Fatalf("test check is only %d bytes", len(check))
	}

	got := runnableChecks([]string{check}, taskCopy{})
	if len(got) != 1 || got[0] != check {
		t.Fatalf("overlong declared check was absent from the audit door: %q", got)
	}
}
