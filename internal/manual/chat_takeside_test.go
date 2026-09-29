package manual

import (
	"strings"
	"testing"
)

// A person who has continued a chat on another machine asks what came with it,
// where the copy went and what happened to the secrets; each question must reach
// the home page's section that says so.
func TestTheContinuedChatQuestionsReachTheAnswer(t *testing.T) {
	for _, probe := range []struct{ asked, says string }{
		{"do I need git or a login on the other machine to continue a chat", "no git login or remote is needed"},
		{"what happens to my env file when I continue a chat on another computer", "put back as a `.env` in"},
		{"is my project folder changed when I continue a chat here", "left as it is"},
		{"does a continued chat keep the executable bit and binary files", "executable bit"},
		{"i continued a chat on a machine that already had a copy, what happens to my edits", "kept first,"},
		{"env keeps your own value for a variable", "never overwritten"},
		{"is there a merge for the turns of a branch", "Merge is\nnot offered on this build"},
	} {
		found := false
		for _, section := range Chat().Search(probe.asked, DefaultResults) {
			if section.Page == "home" && strings.Contains(section.Body, probe.says) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach a home section that says %q", probe.asked, probe.says)
		}
	}
}
