package subharness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// watchingEnv is a capability surface that records the progress a run reports
// and has nothing behind any other door — which is honest here, because the page
// runner uses exactly one of them.
type watchingEnv struct {
	exec.UnwiredEnv
	said []string
}

func (e *watchingEnv) Log(_ context.Context, status string) error {
	e.said = append(e.said, status)
	return nil
}

// A PAGE IS DESCRIBED THE WAY EVERY OTHER SUBHARNESS IS: its own name, the line
// it was designed for, and one required field to run it on. What it does not
// claim is as load-bearing as what it does — no cues, no budget shape, no
// promised output — because a list that invented any of those would be speaking
// for a program that never said them.
func TestAPageIsDescribedAsTheSubharnessItIs(t *testing.T) {
	page := linear()
	page.Id.Desc = "one loop, checked"
	manifest := manifestOf(page)

	if manifest.Name != "linear" || manifest.Purpose != "one loop, checked" {
		t.Fatalf("the page is described as %+v", manifest.SubharnessInfo)
	}
	fields := manifest.Input.Fields()
	if len(fields) != 1 || fields[0].Name != BriefField || !fields[0].Required {
		t.Fatalf("the front door is not one required field: %+v", fields)
	}
	if strings.TrimSpace(fields[0].Description) == "" {
		t.Fatal("the one field a person is asked for says nothing about itself")
	}
	if len(manifest.Cues) != 0 || manifest.DeadlineFloor != 0 || !manifest.Output.Empty() {
		t.Fatalf("the manifest claims something the page never said: %+v", manifest)
	}
	// The provenance is the registry's to stamp and this must not pre-empt it.
	if manifest.Provenance != "" {
		t.Fatalf("the store stamped its own provenance: %q", manifest.Provenance)
	}
}

// RUNNING A ROW GOES THROUGH THE ONE RUNNER THE SURFACE ALREADY BUILT, with the
// card's one field as the request. What comes back is the run's own account, its
// bill, and a finish line something downstream can actually check.
func TestRunningAPageThreadsTheCardsOneFieldThrough(t *testing.T) {
	store := At(t.TempDir())
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	var ranAs, ranOn string
	source := store.Source(func(_ context.Context, name, text, _ string, step func(Trail)) (string, Usage, error) {
		ranAs, ranOn = name, text
		step(Trail{Step: 1, Id: "work", Kind: KindAgentLoop})
		return "it is done", Usage{Calls: 2, Input: 100, CostUSD: 0.5}, nil
	})
	runner, ok := source.Runner("linear")
	if !ok {
		t.Fatal("the store did not answer for a page it holds")
	}

	env := &watchingEnv{}
	result, err := runner.Run(context.Background(),
		json.RawMessage(`{"brief":"the october pictures"}`), env)
	if err != nil {
		t.Fatalf("the run could not be made to happen: %v", err)
	}
	if ranAs != "linear" || ranOn != "the october pictures" {
		t.Fatalf("the run was asked for %q on %q", ranAs, ranOn)
	}
	if result.Report != "it is done" || !result.Finished() {
		t.Fatalf("the run answered %+v", result)
	}
	if result.Spend.Calls != 2 || result.Spend.CostUSD != 0.5 {
		t.Fatalf("the bill did not come back: %+v", result.Spend)
	}
	if len(env.said) != 1 || env.said[0] != "work" {
		t.Fatalf("the steps were not said as they landed: %v", env.said)
	}
}

// A ROW WITH NOTHING TYPED INTO IT RUNS NOTHING. The card holds the required
// field before it gets here; this is the same answer said again at the door, so
// a program is never started on an empty request.
func TestAPageWithNoRequestRunsNothing(t *testing.T) {
	store := At(t.TempDir())
	if _, err := store.Save(linear()); err != nil {
		t.Fatal(err)
	}
	started := false
	source := store.Source(func(context.Context, string, string, string, func(Trail)) (string, Usage, error) {
		started = true
		return "", Usage{}, nil
	})
	runner, ok := source.Runner("linear")
	if !ok {
		t.Fatal("the store did not answer for a page it holds")
	}
	if _, err := runner.Run(context.Background(), json.RawMessage(`{"brief":"  "}`), &watchingEnv{}); err == nil {
		t.Fatal("an empty request started a run")
	}
	if started {
		t.Fatal("the program was entered on an empty request")
	}
}

// A STORE WITH NOTHING TO RUN ITS PAGES WITH IS NOT A PLACE PROGRAMS COME FROM.
// It is the codebase's law about a capability that cannot work, said at the
// earliest place it can be said.
func TestAStoreWithNoRunnerOffersNothing(t *testing.T) {
	if source := At(t.TempDir()).Source(nil); source != nil {
		t.Fatal("a store with nothing to run pages with answered as a source")
	}
}
