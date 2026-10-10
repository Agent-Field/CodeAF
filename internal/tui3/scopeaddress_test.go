package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE RULE SAYS AN ADDRESS OR IT SAYS THIS WINDOW'S.
//
// `new conversation in` is where what you type will land, and a display name
// in that slot is not somewhere: `codeaf` cannot be told from a second checkout
// of the same name, and the row above it says `~/codeaf` about the same
// machine. The rule is one line: a row answers with a path or with nothing, and
// nothing falls through to the workspace this window is standing in.
func TestTheRuleTakesARowsAddressAndNeverItsName(t *testing.T) {
	const project = "/work/codeaf"
	for _, c := range []struct {
		what string
		line homeLine
		want string
	}{
		{"a conversation", homeLine{kind: homeSession, project: "codeaf",
			row: session.SessionRow{ProjectDir: project}}, project},
		{"a project heading", homeLine{kind: homeProject, project: "codeaf",
			proj: session.Project{Name: "codeaf", Path: project}}, project},
		// THE ROW THIS FIX IS ABOUT. It knows its project's NAME and nothing
		// else, and a name is not an answer to "where".
		{"a row that recorded no directory", homeLine{kind: homeLedger, project: "codeaf"}, ""},
	} {
		got := scopeAddress(c.line)
		if got != c.want {
			t.Errorf("%s: the rule's address is %q, want %q", c.what, got, c.want)
		}
	}
}
