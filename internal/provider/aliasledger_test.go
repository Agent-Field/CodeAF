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
// ITS TURN IS UNHEDGED, AND A RACED ONE IS THE TEST BELOW IT. `internal/provider`
// had a third writer of the ledger's model id — hedge.go's settle overwrote the
// spelling with the answer's own `model` field, not through [laneModel] — so a
// raced call filed its losing arm under a spelling nobody folded.
// [TestARacedArmIsFiledUnderTheOneNameTheLedgerKeys] is that door, staged on a
// stalled lane, because a loser only teaches the ledger anything once it has
// named itself.

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

// TestTheShippedDefaultRoutesWithPriorsRatherThanUnderThreeNames is issue #289:
// the sheet, the sighting and the file all name the model the router serves,
// and the chooser therefore has something to rank.
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

	// THE STATE FILE IS WRITTEN BY THE WRITER AND NOT BY THE TURN (issue #264):
	// an observation appends to the journal, and the compaction that folds the
	// journal into `lanes.json` happens off the send path. This is the door a
	// real process runs at its own exit (`cmd/aforge`'s execute).
	lanes.Flush()

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
	// restores the state and replays the journal, and asking it under EITHER
	// spelling asks about one model. That is what one key means, and it is the
	// difference between this and the quick win it grew out of: the alias no
	// longer names an empty ledger, it names the same one.
	lanes.Default().Reset()
	underAlias := lanes.Default().Ledger().Beliefs(alias)
	underServed := lanes.Default().Ledger().Beliefs(servable)
	if len(underAlias) != len(underServed) {
		t.Fatalf("the alias answered %d lanes and the served id %d; one model, two ledgers", len(underAlias), len(underServed))
	}
	for i := range underAlias {
		if underAlias[i].ID.Model != servable {
			t.Fatalf("a lane asked for under the alias came back keyed on %q", underAlias[i].ID.Model)
		}
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
	now := time.Now()
	choice := lanes.Default().Chooser().Choose(LaneTalkAsk(alias, now))
	if len(choice.Frontier) < 2 {
		t.Fatalf("the chooser ranked %d lanes for the shipped default, so no hedge could name an alternative", len(choice.Frontier))
	}
	// AND THE PLAN A WATCH IS BUILT WITH CAN NAME ONE. This is issue #289's
	// third acceptance said in code: `verdict()` returns nothing while the
	// alternative is empty, and it was empty because the identity the frontier
	// was solved from held no sheet prior. It is not structural any more — it is
	// the same fold, asked one layer up.
	plan := lanes.PlanFor(choice, lanes.PaceFor(lanes.ID{Model: alias, Lane: lanes.HeadOf(choice)}, now), RoleFrom(context.Background()), now)
	if len(plan.Alts) == 0 {
		t.Fatal("the plan named no alternative, so a slow stream on the shipped default has nowhere to be raced to")
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

// TestARacedArmIsFiledUnderTheOneNameTheLedgerKeys is door 3 of issue #289.
//
// A LOSER IS ONLY EVER SEEN BY THE RACE. Every finished stream reaches the
// ledger through the ordinary path, but the arm that was cancelled reaches it
// from [hedgeRace.settle] and from nowhere else — and settle wrote the model as
// the ANSWER spelled it, straight in, past the fold every other door applies.
// So on the model this build ships as its default, the cheapest measurement
// there is of the lane nobody chose was filed under a name no sheet and no
// belief was ever keyed on.
//
// The ledger here is the rig's scripted one, which files what it is handed
// without folding anything, so what is asserted is the DOOR and not the
// ledger's own normalisation behind it.
func TestARacedArmIsFiledUnderTheOneNameTheLedgerKeys(t *testing.T) {
	const alias = "openrouter/flash-latest"
	// The rig names its router after the id the alias resolves to, and the
	// client below is pointed at the alias — the asymmetry a real install has.
	rig := newLaneRig(t, "flash-0731",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallFor: 200 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	servable := rig.model
	rig.believes("A", 2, 250)
	// AND THE SAME BELIEF UNDER THE SPELLING THE RACE CARRIES. `raceFor` hands
	// `config.Model` to the plan as the operator wrote it, and in a shipped
	// build that is harmless because the ledger folds every id at its own door
	// ([lane.ID.key]) — but this rig's ledger is a script that files exactly
	// what it is handed, so the plan would find nothing to be surprised about
	// and the race under test would never start.
	rig.ledger.mu.Lock()
	rig.ledger.beliefs[lanes.ID{Model: alias, Lane: "A"}] = rig.ledger.beliefs[lanes.ID{Model: servable, Lane: "A"}]
	rig.ledger.mu.Unlock()
	rig.server.Alias(alias, servable)
	t.Cleanup(func() { lanes.UseServable(nil) })
	lanes.UseServable(func(model string) string {
		if strings.TrimPrefix(model, "~") == alias {
			return servable
		}
		return model
	})

	client, err := NewClient(Config{APIKey: "test-key", BaseURL: rig.server.URL(), Model: alias})
	if err != nil {
		t.Fatal(err)
	}
	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(servable, 12*time.Millisecond))
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("the stalled lane was never raced, so the settle door was never opened")
	}
	// Both arms taught the ledger something — the winner by the ordinary path,
	// the loser by settle — and that is what makes this test about the loser.
	waitFor(t, func() bool { return len(rig.ledger.noted()) == 2 })
	stalled, ok := rig.ledger.sightingFor("A")
	if !ok {
		t.Fatalf("the stalled lane taught the ledger nothing: %+v", rig.ledger.noted())
	}
	if stalled.ID.Model != servable {
		t.Errorf("the losing arm was filed under %q, want the one name the ledger keys (%q)", stalled.ID.Model, servable)
	}
	for _, sighting := range rig.ledger.noted() {
		if sighting.ID.Model != servable {
			t.Errorf("a raced sighting reached the ledger as %q; one model, one name", sighting.ID.Model)
		}
	}
}

// TestAPickerSpellingReachesTheBeatAsTheModelTheRouterServes is door 4, at the
// seam a surface really uses: [lanes.WantSheet] takes whatever a person picked,
// and the router publishes an endpoints page only under the id it resolves to.
func TestAPickerSpellingReachesTheBeatAsTheModelTheRouterServes(t *testing.T) {
	const alias = "openrouter/flash-latest"
	const servable = "openrouter/flash-0731"
	forgetLanes(t)
	t.Cleanup(func() { lanes.UseServable(nil) })
	lanes.UseServable(func(model string) string {
		if strings.TrimPrefix(model, "~") == alias {
			return servable
		}
		return model
	})
	server := lanestub.New(servable,
		lanestub.Lane{Name: "quicksilver", Profile: lanestub.Profile{
			TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 40, Tools: true,
			PriceIn: 0.000003, PriceOut: 0.000006}},
		lanestub.Lane{Name: "brass", Profile: lanestub.Profile{
			TTFT: 30 * time.Millisecond, Rate: 300, Tokens: 40, Tools: true,
			PriceIn: 0.0000025, PriceOut: 0.000005}},
	)
	t.Cleanup(server.Close)
	server.Alias(alias, servable)
	if !lanes.WireSheet(server.URL(), "", sheetFetcher{}) {
		t.Fatal("the sheet would not take a base to fetch from")
	}

	// One round of the beat over exactly what the picker asked for, which is
	// what [lanes.Beat] does with the names [lanes.WantSheet] leaves behind.
	lanes.WantSheet("~" + alias)
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	go lanes.Beat(ctx, lanes.Default().Sheet(), nil, 50*time.Millisecond)
	waitFor(t, func() bool { return len(lanes.Default().Ledger().Beliefs(alias)) == 2 })

	for _, belief := range lanes.Default().Ledger().Beliefs(alias) {
		if belief.ID.Model != servable {
			t.Fatalf("a row the picker's spelling fetched was primed under %q", belief.ID.Model)
		}
		if !belief.Facts.Known() {
			t.Fatalf("the sheet was fetched under a page nobody publishes: %+v", belief)
		}
	}
}
