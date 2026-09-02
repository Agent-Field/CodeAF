package provider

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE SHIPPED DEFAULT, END TO END ─────────────────────────────────────────
//
// The model every install routes by default is a FLOATING ALIAS, and until this
// test nothing held the whole path to the consequence. The wire sends the alias
// and the router answers it; the endpoints page exists only under the concrete
// model it points at. So the beat asked for a sheet under one name, the sighting
// was filed under a second, and the machines that actually answered were a
// third's — one model, three ledger keys, and the default model of every install
// therefore chose between no lanes at all.
//
// It asserts on the FILE rather than on a double, because the file is what the
// next session opens.
//
// ITS TURN IS UNHEDGED, AND THAT IS A LIMIT WORTH NAMING. `internal/provider`
// has a third writer of the ledger's model id — hedge.go's race carries
// `config.Model` raw and its settle overwrites that with the answer's own
// `model` field, neither of them through [laneModel] — so a RACED call can
// still file a losing arm under a spelling nobody folded. That seam is
// hedge.go's and the general fold's, not this one's, and staging it here would
// assert something this change does not fix.

// storedLanes is every model name the belief file and its journal carry, read
// back the way another process would read them.
//
// BOTH HALVES, BECAUSE ONE OF THEM IS NOT ENOUGH. Since the store became a
// state file plus an append-only journal (`v3/lanes.json` and `v3/lanes.log`),
// the compacted half is written on the first record of a process and every
// journalLimit-th after, and everything since sits in the journal. A sighting
// filed a moment ago is therefore usually in the log and not in the state — so
// an assertion that read only the state would pass on a ledger that is still
// split, which is the whole thing this test exists to catch.
//
// Belief and Sighting carry no json tags, so the field names are the Go ones.
func storedLanes(t *testing.T) []string {
	t.Helper()
	var names []string
	state, err := os.ReadFile(lanes.StorePath())
	if err != nil {
		t.Fatalf("read the belief file: %v", err)
	}
	var held struct {
		Beliefs []struct {
			ID struct{ Model, Lane string }
		}
	}
	if err := json.Unmarshal(state, &held); err != nil {
		t.Fatalf("decode the belief file: %v", err)
	}
	for _, belief := range held.Beliefs {
		names = append(names, belief.ID.Model)
	}

	// The journal sits beside the state file under the same name.
	log, err := os.ReadFile(strings.TrimSuffix(lanes.StorePath(), ".json") + ".log")
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read the belief journal: %v", err)
	}
	for _, line := range strings.Split(string(log), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry struct {
			Sight *struct {
				ID struct{ Model, Lane string }
			}
			Out *struct {
				ID struct{ Model, Lane string }
			}
			Row *struct {
				ID struct{ Model, Lane string }
			}
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode a journal line: %v", err)
		}
		for _, seen := range []*struct {
			ID struct{ Model, Lane string }
		}{entry.Sight, entry.Out, entry.Row} {
			if seen != nil && seen.ID.Model != "" {
				names = append(names, seen.ID.Model)
			}
		}
	}
	return names
}

// TestTheShippedDefaultRoutesWithPriorsRatherThanUnderThreeNames is issue #289.
//
// EXPECTED RED until internal/provider/lanes.go's laneModel folds too. That file
// is frozen behind another change to the same seam, and this test is the honest
// record of what is still broken: with only the beat and the catalog fixed, the
// sheet lands under the servable id and the sighting still lands under the alias
// — two keys in the file, and the belief the chooser reads is the empty one.
func TestTheShippedDefaultRoutesWithPriorsRatherThanUnderThreeNames(t *testing.T) {
	// The alias is spelled without OpenRouter's "~" marker for one reason that
	// is worth writing down: [Client.isOpenRouter] reads `config.Model` RAW,
	// with no normalisation, so a configured "~openrouter/…" behind a base that
	// is not openrouter.ai turns the whole lane path off. It costs nothing in
	// production, where the base URL answers that question, and it is a separate
	// defect from this one. Both spellings reach the same fold — the catalog and
	// the lane seam are each held to that by their own tests.
	const alias = "openrouter/flash-latest"
	const servable = "openrouter/flash-0731"

	forgetLanes(t)
	// The fold the launch path installs, in miniature: the same answer
	// catalog.Servable gives for these rows, with the "~" stripped exactly as it
	// strips it.
	t.Cleanup(func() { lanes.UseServable(nil) })
	lanes.UseServable(func(model string) string {
		if strings.TrimPrefix(model, "~") == "openrouter/flash-latest" {
			return servable
		}
		return model
	})

	// A router that ANSWERS for the alias and PUBLISHES only under the concrete
	// id, which is what OpenRouter really does.
	// Three lanes that differ on speed AND on price, because that is what a real
	// endpoints page looks like and it is what leaves a frontier with something
	// on it: three lanes where one is faster and no dearer are three lanes with
	// one candidate, and then nothing could be hedged to whatever the ledger was
	// keyed on.
	server := lanestub.New(servable,
		lanestub.Lane{Name: "quicksilver", Profile: lanestub.Profile{
			TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 40, Tools: true,
			PriceIn: 0.000003, PriceOut: 0.000006}},
		lanestub.Lane{Name: "brass", Profile: lanestub.Profile{
			TTFT: 30 * time.Millisecond, Rate: 300, Tokens: 40, Tools: true,
			PriceIn: 0.0000025, PriceOut: 0.000005}},
		lanestub.Lane{Name: "molasses", Profile: lanestub.Profile{
			TTFT: 40 * time.Millisecond, Rate: 200, Tokens: 40, Tools: true,
			PriceIn: 0.000002, PriceOut: 0.000004}},
	)
	t.Cleanup(server.Close)
	server.Alias(alias, servable)
	if !lanes.WireSheet(server.URL(), "", sheetFetcher{}) {
		t.Fatal("the sheet would not take a base to fetch from")
	}

	// The beat, as internal/session runs it: the models this session means,
	// under the ledger's name for them.
	if err := lanes.Default().Sheet().Refresh(context.Background(), lanes.LedgerModel(alias)); err != nil {
		t.Fatalf("the beat could not fetch a sheet for the shipped default: %v", err)
	}
	for _, row := range lanes.Default().Sheet().Rows(servable) {
		lanes.Default().Ledger().Prime(row, 4)
	}

	client, err := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: server.URL(),
		Model:   alias,
		Routing: StaticRouting(RoutingLatency),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}

	// ONE MODEL, ONE KEY. The file is what the next session opens, so it is what
	// the assertion is made against.
	names := map[string]bool{}
	for _, model := range storedLanes(t) {
		names[model] = true
	}
	if len(names) != 1 {
		t.Fatalf("the belief file holds %d names for one model (%v); the ledger is still split", len(names), keysOf(names))
	}
	if !names[servable] {
		t.Fatalf("the belief file is keyed on %v, want the id the router actually serves (%q)", keysOf(names), servable)
	}

	// AND WHAT A NEXT SESSION ACTUALLY OPENS, which is the claim the file is
	// only evidence for: a registry built from scratch against the same home
	// restores the state and replays the journal, and it must find this model
	// under one name and nothing at all under the spelling nobody folded.
	lanes.Default().Reset()
	if stale := lanes.Default().Ledger().Beliefs(alias); len(stale) != 0 {
		t.Fatalf("a fresh session found %d lanes filed under %q, the id the router serves nothing under", len(stale), alias)
	}

	// And the beliefs under it are worth having: the sheet has spoken about the
	// lanes, so the gate can judge them.
	beliefs := lanes.Default().Ledger().Beliefs(servable)
	if len(beliefs) < 2 {
		t.Fatalf("the ledger holds %d lanes for the shipped default, so there is nothing to choose between", len(beliefs))
	}
	for _, belief := range beliefs {
		if !belief.Facts.Known() {
			t.Fatalf("the sheet never spoke about %+v, so the gate is judging a lane it cannot see", belief.ID)
		}
	}

	// A hedge has somewhere to go. The frontier is the durable form of that
	// question — it is the candidate set after the gate and the prune, and a
	// second lane on it is a second lane a slow stream can be raced against.
	choice := lanes.Default().Chooser().Choose(LaneTalkAsk(alias, time.Now()))
	if len(choice.Frontier) < 2 {
		t.Fatalf("the chooser ranked %d lanes for the shipped default, so no hedge could name an alternative", len(choice.Frontier))
	}
}

// keysOf names what a failure found, sorted, so a red run reads as a sentence.
func keysOf(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
