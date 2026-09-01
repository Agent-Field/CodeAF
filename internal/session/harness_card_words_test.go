package session

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/video"
)

// A CARD IS THE PAGE SOMEBODY APPROVES A RECIPE FROM, so a verb that reaches the
// harness belt without reaching subharness's words for it prints its registered
// identifier there — `edit_video`, in a column of human phrases — and a person is
// being asked to approve a bound nobody wrote a sentence about. That is what
// happened when the cutting verb joined the belt.
//
// THE TWO HALVES CAN ONLY BE COMPARED ON THIS SIDE. internal/session says what a
// whitelist may legally hold ([HarnessBeltNames], harness_belt.go) and
// internal/subharness draws it (card.go's cardToolWords); the dependency runs one
// way, so the gate lives here rather than beside the table it guards.
//
// The probe is the FOOT'S OWN LINE and not a search of the page for the name,
// because every phrase in that table contains the name it stands for — "reads
// files" for `read`. Only an exact element of `can use · a · b · c` is evidence
// that a name fell through to itself.
func TestEveryVerbAHarnessMayNameHasWordsOnItsCard(t *testing.T) {
	names := HarnessBeltNames(t.TempDir(), HarnessBeltSeams{
		Media: &scriptedMedia{}, MediaModel: allMediaModels(), Seer: &scriptedCompleter{},
	})
	// The cutting verb travels on ffmpeg and on nothing else, so a machine
	// without it is a machine this gate cannot speak for — say so rather than
	// pass quietly and let the row it exists for go missing.
	if video.Available() && !names["edit_video"] {
		t.Fatal("edit_video is not on the harness belt on a machine with ffmpeg, so this test is about nothing")
	}
	whitelist := make([]string, 0, len(names))
	for name := range names {
		whitelist = append(whitelist, name)
	}
	sortStrings(whitelist)

	card := subharness.Card(subharness.Harness{
		Id: subharness.Id{Name: "everything", Version: 1},
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "work", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "work"}},
		}},
		Whitelist: whitelist,
	})
	const foot = "can use · "
	line := ""
	for _, drawn := range strings.Split(card, "\n") {
		if drawn = strings.TrimSpace(drawn); strings.HasPrefix(drawn, foot) {
			line = strings.TrimPrefix(drawn, foot)
		}
	}
	if line == "" {
		t.Fatalf("the card drew no %q line for a whitelist of %d verbs:\n%s", foot, len(whitelist), card)
	}
	spoken := map[string]bool{}
	for _, phrase := range strings.Split(line, " · ") {
		spoken[strings.TrimSpace(phrase)] = true
	}
	for _, name := range whitelist {
		if spoken[name] {
			t.Errorf("the card prints %q at a person rather than saying what it does — give it a row in subharness's cardToolWords", name)
		}
	}
}
