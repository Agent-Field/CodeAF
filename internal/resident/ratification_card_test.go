package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// draftForCard puts one proposed rule in the store and hands back the canonical
// charter the card is written from.
func draftForCard(t *testing.T, graph *store.Store, id string, spec store.CharterSpec) store.Charter {
	t.Helper()
	charter, err := graph.DraftCharter(id, "chat", 1, spec)
	if err != nil {
		t.Fatal(err)
	}
	return charter
}

// TestTheCardStatesAStatedScheduleAndAsksAboutAGuessedOne is the everyday
// simulation's worst moment, at the surface where it was visible: the card
// showed "fires: about every 2 minutes (cron:every 2 minutes)" — a guess, in an
// engine's spelling, presented as a fact.
func TestTheCardStatesAStatedScheduleAndAsksAboutAGuessedOne(t *testing.T) {
	graph := openStore(t)

	stated := draftForCard(t, graph, "plants", store.CharterSpec{
		Invariant: "remind me every sunday to water the plants",
		Watch: store.CharterWatch{
			Kind: store.WatchCron, Cadence: "every sunday",
			Spec: store.CadenceWatchSpec(store.WatchCron, "every sunday", "",
				"remind me every sunday to water the plants", time.Now()),
		},
		Sentinel: "Is it time?", Action: "Say: water the plants.", SayOnly: true,
		Rails: store.CharterSpecRails{
			EstimatedCostUSD: 0.15, MaxPerDay: 1,
			MaxPerDayJustification: "one a day is all a reminder needs", Expiry: "never",
		},
	})
	question, options := charterRatificationQuestion(stated, "")
	if !strings.Contains(question, "when: Sundays at 9am") {
		t.Fatalf("a schedule she stated is not stated back plainly:\n%s", question)
	}
	if strings.Contains(question, "Right?") || strings.Contains(question, "guess") {
		t.Fatalf("a stated schedule was asked about instead of stated:\n%s", question)
	}
	if len(options) != 3 || options[1].Value != "charter:cadence:"+stated.ID {
		t.Fatalf("card options = %+v", options)
	}

	guessedWatch := store.CadenceWatchSpec(store.WatchPoll, "about every 2 minutes", "",
		"watch for when the dyson v15 drops under 500", time.Now())
	guessedWatch.CadenceGuessed = true
	guessed := draftForCard(t, graph, "dyson", store.CharterSpec{
		Invariant: "watch for when the dyson v15 drops under 500",
		Watch: store.CharterWatch{
			Kind: store.WatchPoll, Cadence: "about every 2 minutes", Spec: guessedWatch,
		},
		Sentinel: "Has the price dropped?", Action: "Report the price.",
		Rails: store.CharterSpecRails{
			EstimatedCostUSD: 0.15, MaxPerDay: 10,
			MaxPerDayJustification: "caps the default worst day at about $1.50", Expiry: "never",
		},
	})
	asked, _ := charterRatificationQuestion(guessed, "caps the default worst day at about $1.50")
	if !strings.Contains(asked, "about every 2 minutes") || !strings.HasSuffix(
		strings.Split(asked, "\n")[1], "Right?") {
		t.Fatalf("a guessed schedule was asserted rather than asked:\n%s", asked)
	}

	// Neither card may speak the engine's spelling of a schedule.
	for _, body := range []string{question, asked} {
		for _, machinery := range []string{"cron:", "poll:", "charter", "/firing", "cadence"} {
			if strings.Contains(strings.ToLower(body), machinery) {
				t.Fatalf("ratification card leaks %q:\n%s", machinery, body)
			}
		}
	}
}

// A charter draft is the right answer in a conversation and a dead end on a
// surface with no mouth. `aforge do` already carries the answer to "standing or
// once?" in its verb, so a draft that reaches a one-shot errand is resolved as
// the caller already chose — journaled as a retired proposal, then run as work.
//
// The chat half of the same seam must not move: a resident SHOULD still ask.
func TestAOneShotErrandResolvesACharterDraftAsOnceAndStillRunsTheWork(t *testing.T) {
	spec := store.CharterSpec{
		Invariant: "Reconcile bank_export.csv against ledger.csv and flag every discrepancy",
		Watch: store.CharterWatch{
			Kind: store.WatchPoll, Cadence: "about every 2 minutes",
			Spec: store.CadenceWatchSpec(store.WatchPoll, "about every 2 minutes", "",
				"reconcile the ledgers", time.Now()),
		},
		Sentinel: "Have the ledgers diverged?", Action: "Reconcile the two ledgers.",
		Rails: store.CharterSpecRails{EstimatedCostUSD: 0.05, MaxPerDay: 10,
			MaxPerDayJustification: "caps the default worst day at about $0.50", Expiry: "never"},
	}
	compile := func(context.Context, string, string) (Compiled, error) {
		draft := spec
		return Compiled{Question: "Stand this rule up?", Charter: &draft}, nil
	}

	for _, surface := range []struct {
		name    string
		oneShot bool
	}{{"chat", false}, {"errand", true}} {
		t.Run(surface.name, func(t *testing.T) {
			graph := openStore(t)
			command, err := graph.RequestCommand(store.Command{
				SessionID: "s", Kind: store.CommandSplice,
				Instruction: spec.Invariant,
			})
			if err != nil {
				t.Fatal(err)
			}
			reconciler := New(graph, compile, nil)
			if surface.oneShot {
				reconciler = reconciler.WithOneShotErrands()
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			resolved, found, err := graph.CommandBySeq(command.Seq)
			if err != nil || !found {
				t.Fatalf("command found=%t err=%v", found, err)
			}
			nodes, err := graph.Nodes()
			if err != nil {
				t.Fatal(err)
			}
			// The charter draft is itself journaled as a node; work means the
			// thing the person asked for.
			work := 0
			for _, node := range nodes {
				if node.ID != store.RootID && !strings.HasPrefix(node.ID, "charter-") {
					work++
				}
			}

			if !surface.oneShot {
				if resolved.Status != store.CommandRejected {
					t.Fatalf("a chat window stopped asking for ratification: %s", resolved.Status)
				}
				if work != 0 {
					t.Fatalf("an unratified rule spliced %d nodes", work)
				}
				return
			}
			if resolved.Status != store.CommandApplied {
				t.Fatalf("the errand ended on a card it cannot answer: %s / %s",
					resolved.Status, resolved.Result)
			}
			if work == 0 {
				t.Fatal("the draft was resolved and the work still never existed")
			}
			// The record says what was proposed and what became of it.
			charter, found, err := graph.Charter(fmt.Sprintf("charter-%d", command.Seq))
			if err != nil || !found {
				t.Fatalf("the proposal was never journaled: found=%t err=%v", found, err)
			}
			if charter.Status != store.CharterRetired {
				t.Fatalf("a rule nobody ratified is %s, want retired", charter.Status)
			}
			// And nothing is left standing that could fire on a two-minute
			// cadence nobody ever asked for.
			active, err := graph.ActiveCharters()
			if err != nil {
				t.Fatal(err)
			}
			if len(active) != 0 {
				t.Fatalf("a one-shot errand stood up %d charters", len(active))
			}
		})
	}
}
