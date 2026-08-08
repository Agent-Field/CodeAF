package resident

import (
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
