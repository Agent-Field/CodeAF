package tui3

import "testing"

func TestClearedDraftDoesNotReturnFromStaleTextExport(t *testing.T) {
	for _, keepTask := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty-record", true: "other-recipient-remains"}[keepTask], func(t *testing.T) {
			a, _ := keepLab(t)
			a.input.setText("already submitted")
			a.writeDraftsNow(a.leavingDraft())
			a.input.setText("")
			if keepTask {
				a.openRoom(7, "Fix the nil-map crash")
				a.input.setText("still unsent to task seven")
				a.closeRoom()
			}
			// Crash after the authoritative record commit, before its text export.
			if err := commitKeep(draftKeepPath(a.draftFile), a.draftKeepBuild("")); err != nil {
				t.Fatal(err)
			}
			next := reopen(t, a)
			if got := next.input.String(); got != "" {
				t.Fatalf("cleared draft resurrected from stale export: %q", got)
			}
			if keepTask {
				next.openRoom(7, "Fix the nil-map crash")
				if got := next.input.String(); got != "still unsent to task seven" {
					t.Fatalf("unrelated task draft lost: %q", got)
				}
			}
		})
	}
}
