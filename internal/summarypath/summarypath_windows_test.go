package summarypath

import "testing"

// Text contract C5: drive, forward-slash and UNC paths share native identity.
func TestTextContractSummaryWindowsPaths(t *testing.T) {
	for _, path := range []string{`C:\work\report.md`, `C:/work/report.md`, `\\server\share\report.md`} {
		if got := Parse("(" + path + ")."); got != path {
			t.Fatalf("path = %q, want %q", got, path)
		}
	}
	if Key(`C:\work\report.md`) != Key(`c:/work/report.md`) {
		t.Fatal("drive-letter case or separators changed the report identity")
	}
	if Key(`\\server\share\report.md`) != Key(`//server/share/report.md`) {
		t.Fatal("UNC separators changed the report identity")
	}
}
