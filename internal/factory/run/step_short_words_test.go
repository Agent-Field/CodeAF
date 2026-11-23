package run

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

// A PERSON READS A SENTENCE, NEVER THE COMMAND. Task A of the third live run
// asked in its Needs you row: "test is not green after 3 rounds: verification
// blocked: cd /Users/ron/…/factory-demo-211 && echo "== bare go version:"; go
// version; …" — the raw shell lines its checks ran, paths and all. The
// question says how many checks were not confirmed to pass; the commands stay
// on the step's own record, where its chat and its next round read them.
func TestAStepOutOfRoundsOnItsChecksAsksInASentence(t *testing.T) {
	res := factory.StageResult{Notes: []string{
		verification.FailurePrefix + ` cd /Users/ron/work/factory-demo-211 && echo "== bare go version:"; go version · completed; no later measured covering pass`,
		verification.FailurePrefix + ` cd /Users/ron/work/factory-demo-211 && unset -f go && go test -count=1 ./... · completed; no later measured covering pass`,
	}}
	note, why := stepShort("test", factory.Stage{Name: "test", Until: "green", Max: 3}, 3, res)
	if note != "test is not green after 3 rounds" {
		t.Fatalf("note = %q", note)
	}
	for _, leak := range []string{"cd ", "&&", "/Users/", "go test", "unset"} {
		if strings.Contains(why, leak) {
			t.Fatalf("the person's question carries the command (%q): %q", leak, why)
		}
	}
	if why != "2 checks it ran were not confirmed to pass" {
		t.Fatalf("why = %q", why)
	}
	// The step's own record keeps the commands.
	if got := shortfall(res); !strings.Contains(got, "go test -count=1 ./...") {
		t.Fatalf("the record lost the commands: %q", got)
	}
	one := factory.StageResult{Notes: res.Notes[:1]}
	if _, why := stepShort("test", factory.Stage{Name: "test", Until: "green", Max: 3}, 3, one); why != "1 check it ran was not confirmed to pass" {
		t.Fatalf("one check reads %q", why)
	}
}
