//go:build !windows

package seniordev

import "testing"

// THE FOLDER CODEAF MOVES OUT OF A PLAIN FOLDER IS THE ONE SENIOR-DEV WRITES
// ITS RECORDS TO: its brief, checklist, session database and conversation all
// live under app's seniorDevDataDirectory, which is this name.
func TestSeniorDevNamesTheFolderItKeepsItsRecordsIn(t *testing.T) {
	if Program.Notes != ".senior-dev" {
		t.Fatalf("senior-dev's notes folder = %q, want .senior-dev", Program.Notes)
	}
}
