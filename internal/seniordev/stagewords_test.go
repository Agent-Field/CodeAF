//go:build !windows

package seniordev

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/app"
)

// EVERY STAGE senior-dev CAN REPORT HAS A WORD A PERSON READS, and no word is
// machinery. app.Stages is the closed list of what the run can emit (its own
// test holds it to the source), so a stage added there without a word here
// fails now rather than showing its name on somebody's task page.
func TestEveryStageSeniorDevReportsHasAPersonsWord(t *testing.T) {
	banned := []string{"auditor", "verdict", "verified", "refuted", "runtime", "contract", "router"}
	for _, stage := range app.Stages {
		word := strings.TrimSpace(Program.StageWords[stage])
		if word == "" {
			t.Errorf("stage %q has no word a person reads", stage)
			continue
		}
		for _, bad := range banned {
			if strings.Contains(word, bad) {
				t.Errorf("stage %q reads %q, which says %q", stage, word, bad)
			}
		}
	}
	for stage := range Program.StageWords {
		if !contains(app.Stages, stage) {
			t.Errorf("a word is kept for %q, which senior-dev never reports", stage)
		}
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
