package pair

import (
	"strings"
	"testing"
)

// A pairing that reached no workspaces says only that it paired: a count of
// zero is never printed.
func TestJoinedFleetLineNeverPrintsZero(t *testing.T) {
	for workspaces, want := range map[int]string{
		0: JoinedLine, 1: JoinedLine, 3: JoinedLine,
	} {
		if got := JoinedFleetLine(workspaces); got != want {
			t.Errorf("JoinedFleetLine(%d) = %q, want %q", workspaces, got, want)
		}
	}
}

// The approving terminal names a system the way a person does, and leaves out
// one it cannot name rather than showing a raw word such as darwin.
func TestWantsToJoinLineNamesTheSystem(t *testing.T) {
	for platform, want := range map[string]string{
		"darwin": "laptop (Mac) wants to pair.",
		"linux":  "laptop (Linux) wants to pair.",
		"plan9":  "laptop wants to pair.",
		"":       "laptop wants to pair.",
	} {
		if got := WantsToJoinLine("laptop", platform); got != want {
			t.Errorf("WantsToJoinLine(%q) = %q, want %q", platform, got, want)
		}
	}
}

// V25: the approval sentence tells the person that a newly paired computer can
// see and control chats here and where to change that permission, without internal jargon.
func TestThePairedLineSaysTheNewDeviceCanControlChatsHere(t *testing.T) {
	const wantControl = "laptop can also see and control your chats here. Change that in /devices."
	if got := PairedControlLine("laptop"); got != wantControl {
		t.Fatalf("PairedControlLine(\"laptop\") = %q, want %q", got, wantControl)
	}
	gotApproved := ApprovedLine("laptop")
	if !strings.HasPrefix(gotApproved, "laptop is now paired. ") {
		t.Fatalf("ApprovedLine(\"laptop\") = %q, want prefix %q", gotApproved, "laptop is now paired. ")
	}
	if !strings.HasSuffix(gotApproved, wantControl) {
		t.Fatalf("ApprovedLine(\"laptop\") = %q, want suffix %q", gotApproved, wantControl)
	}
	if want := "laptop is now paired. " + wantControl; gotApproved != want {
		t.Fatalf("ApprovedLine(\"laptop\") = %q, want %q", gotApproved, want)
	}
	for _, banned := range []string{"relay", "take", "lease", "node", "manifest", "cell", "directory", "cas"} {
		for _, line := range []string{PairedControlLine("laptop"), ApprovedLine("laptop")} {
			if strings.Contains(strings.ToLower(line), banned) {
				t.Errorf("line %q contains banned word %q", line, banned)
			}
		}
	}
}
