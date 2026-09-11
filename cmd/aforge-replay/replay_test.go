package main

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/callrows"
	"github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE INSTRUMENT'S OWN LAW ────────────────────────────────────────────────
//
// A replay is an argument about a counterfactual, and an argument about a
// counterfactual cannot be checked against the world. So it is checked against a
// log somebody WROTE, where what the best machine is, what the served policy
// cost, and what a deliberately wrong policy costs are all known before the
// program runs.
//
// `testdata/calls.jsonl` is fifty requests on one model over an hour, served
// round-robin by three machines whose behaviour is planted:
//
//	Steady    200 ms to the first token, 100 tokens a second, always
//	Bimodal   the same, alternating with 20 s and 2 tokens a second — the shape
//	          a median cannot see and a tail policy exists for
//	Slow      4 s and 5 tokens a second, always
//
// with five arms of a hedge that was cut off, one paced pool that names the
// machine that refused inside its own sentence and carries no `served` column,
// and one machine seen exactly once.

const fixture = "testdata/calls.jsonl"

func fixtureWorld(t *testing.T) (*world, []asked) {
	t.Helper()
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	requests, _ := requestsOf(rows)
	return newWorld(rows, defaultWindow), requests
}

// talkShape is what the fixture's requests ask for: a hundred tokens somebody
// reads, which is what every expectation below is computed over.
var talkShape = shape{visible: 100}

func TestTheOracleIsTheMachineThatWasPlantedFastest(t *testing.T) {
	measured, requests := fixtureWorld(t)
	picked := map[string]int{}
	for _, one := range requests {
		best, felt := measured.best(one.model, one.at, talkShape, true, one.id)
		if math.IsInf(felt, 0) {
			continue
		}
		picked[best]++
	}
	if picked["Steady"] == 0 {
		t.Fatalf("the oracle never picked the machine planted fastest: %v", picked)
	}
	for machine, times := range picked {
		if machine != "Steady" {
			t.Errorf("the oracle picked %s %d times; only Steady is ever the best machine here", machine, times)
		}
	}
}

func TestATriviallyWrongPolicyPaysTheRegretItWasBuiltToPay(t *testing.T) {
	measured, requests := fixtureWorld(t)
	// The planted difference, from lane.PerceivedSeconds over a hundred read
	// tokens: Steady is 0.2 s + nothing (it writes faster than anybody reads),
	// Slow is 4 s + 100·(1/5 − 1/18) = 18.4 s. A policy that demands Slow every
	// time is therefore owed about eighteen seconds a request and nothing else.
	var wrong, served []float64
	for _, one := range requests {
		best, bestFelt := measured.best(one.model, one.at, talkShape, true, one.id)
		if best == "" || math.IsInf(bestFelt, 0) {
			continue
		}
		if felt := measured.felt(lane.ID{Model: one.model, Lane: "Slow"}, one.at, talkShape, true, one.id); !math.IsInf(felt, 0) {
			wrong = append(wrong, felt-bestFelt)
		}
		if one.machine == "" {
			continue
		}
		if felt := measured.felt(lane.ID{Model: one.model, Lane: one.machine}, one.at, talkShape, true, one.id); !math.IsInf(felt, 0) {
			served = append(served, felt-bestFelt)
		}
	}
	// It is a band and not a point because the fixture's one paced pool is
	// credited to Slow, and the windows that contain it divide Slow's wait by the
	// share of requests it answered — the availability fold, which is part of the
	// quantity and not noise in it. The floor is the planted 18.2 s and the
	// ceiling is that figure over five answers in six.
	if got := mean(wrong); got < 18 || got > 23 {
		t.Errorf("a policy that always demands Slow: mean regret %.2fs, want between 18.2s and 22.2s", got)
	}
	if mean(served) >= mean(wrong) {
		t.Errorf("what actually served (%.2fs) is not cheaper than the worst machine (%.2fs)", mean(served), mean(wrong))
	}
	if mean(served) <= 0 {
		t.Errorf("what actually served paid no regret at all (%.2fs), and the fixture serves Slow a third of the time", mean(served))
	}
}

func TestAWatchedRoleIsPricedOnTheTailAndAnUnwatchedOneOnTheMedian(t *testing.T) {
	measured, requests := fixtureWorld(t)
	// The bimodal machine is the whole point of the fixture: its median is as
	// fast as Steady and half its answers take a minute. A role somebody reads
	// must be priced above a role nobody does, and by more than the difference
	// between the two plants.
	tail, middle := 0, 0
	for _, one := range requests {
		bimodal := lane.ID{Model: one.model, Lane: "Bimodal"}
		read := measured.felt(bimodal, one.at, talkShape, true, one.id)
		unread := measured.felt(bimodal, one.at, talkShape, false, one.id)
		if math.IsInf(read, 0) || math.IsInf(unread, 0) {
			continue
		}
		switch {
		case read > unread:
			tail++
		case read == unread:
			middle++
		default:
			t.Fatalf("a watched request was priced BELOW an unwatched one on the same machine: %.2f < %.2f", read, unread)
		}
	}
	if tail == 0 {
		t.Fatalf("the bimodal machine was never priced above its own median for a watched role")
	}
}

func TestAMachineIsNeverPricedFromTheAnswerBeingScored(t *testing.T) {
	measured, requests := fixtureWorld(t)
	// `Lonely` answered exactly once, and that answer IS the request below. With
	// its own row left out there is nothing to price it from, which is the whole
	// of the leave-one-out law: a machine priced on the very answer being scored
	// is a machine scored against itself.
	found := false
	for _, one := range requests {
		if one.machine != "Lonely" {
			continue
		}
		found = true
		id := lane.ID{Model: one.model, Lane: "Lonely"}
		if felt := measured.felt(id, one.at, talkShape, true, one.id); !math.IsInf(felt, 0) {
			t.Errorf("a machine with only the scored answer was priced at %.2fs; it must be unpriceable", felt)
		}
		if felt := measured.felt(id, one.at, talkShape, true, ""); math.IsInf(felt, 0) {
			t.Errorf("with nothing left out the same machine is unpriceable, so the exclusion is not what made it so")
		}
	}
	if !found {
		t.Fatal("the fixture no longer contains the machine seen exactly once")
	}
}

func TestACutStreamIsCountedAndNeverDropped(t *testing.T) {
	measured, _ := fixtureWorld(t)
	if measured.censored != 5 {
		t.Errorf("cut streams: got %d, want the fixture's 5", measured.censored)
	}
	if measured.refusals != 1 {
		t.Errorf("refusals: got %d, want the fixture's 1", measured.refusals)
	}
}

func TestARefusalIsCreditedToTheMachineItsOwnSentenceNames(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	// The paced row carries no `served` column at all and says `via Slow` in its
	// sentence. Filing it under `Steady` — the machine the preference asked for —
	// would credit a refusal to a machine that was never reached.
	for _, row := range rows {
		if row.Status != 429 {
			continue
		}
		if machine := answered(row); machine != "Slow" {
			t.Errorf("a refusal naming `via Slow` was credited to %q", machine)
		}
	}
}

func TestEveryTagTheBuildWritesResolvesToARoleSomebodyDeclared(t *testing.T) {
	// The one join this tool makes for itself. A tag that fell out of the table
	// silently becomes a background errand, which would move every number in the
	// watched half of the report without failing anything.
	for tag, want := range callSiteRoles {
		if got := roleOf(tag); got != want {
			t.Errorf("tag %q resolved to role %q, want %q", tag, got, want)
		}
		if !want.Known() {
			t.Errorf("tag %q names role %q, which is not in internal/lane's role table", tag, want)
		}
	}
	// And the rule the table stands beside: an errand tagged with its own role
	// word needs no row at all.
	for _, role := range lane.Roles() {
		if role == lane.RoleUnknown {
			continue
		}
		if got := roleOf(string(role)); got != role {
			t.Errorf("a call tagged with the role word %q resolved to %q", role, got)
		}
	}
	if got := roleOf("a tag nobody has written down"); got != lane.RoleUnknown {
		t.Errorf("an unrecognised tag resolved to %q; it must read as a background errand", got)
	}
}

func TestTheClassesAreExactlyWhatTheRoleTableCanProduce(t *testing.T) {
	// The report's two tables are keyed on this list, and the quantile candidate
	// keeps one picture per entry of it. A role added to internal/lane whose class
	// is not in the list would drop every call in it out of both silently.
	can := map[string]bool{}
	for _, role := range append(lane.Roles(), lane.RoleUnknown) {
		can[classOf(role)] = true
	}
	for _, class := range classes {
		if !can[class] {
			t.Errorf("the report prints a %q class no role can produce", class)
		}
		delete(can, class)
	}
	for class := range can {
		t.Errorf("the role table produces a %q class the report never prints", class)
	}
}

func TestTheHalfLifeFallsBackToTheBeliefsOwnWhenNothingMoves(t *testing.T) {
	measured, _ := fixtureWorld(t)
	// Nothing in the fixture drifts, so the variogram never reaches halfway and
	// the honest answer is the forgetting internal/lane already uses rather than
	// a figure invented to fill the hole.
	if got := changeHalfLife(measured); got <= 0 {
		t.Errorf("the measured half-life is %v; it may never be zero", got)
	}
	if got := processNoise(measured); got != 0 {
		t.Errorf("the fixture spans one day, so no day-to-day variance can be measured; got %.4f", got)
	}
}

func TestEveryCandidateAnswersTheSameQuestionAndTheTableSaysSo(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var out bytes.Buffer
	report(&out, settings{window: defaultWindow, min: 2, incidents: 2, log: fixture}, rows, nil)
	printed := out.String()
	for _, name := range order {
		if !strings.Contains(printed, "`"+name+"`") {
			t.Errorf("the report never names the %q candidate", name)
		}
	}
	for _, want := range []string{"## Regret", "## The moments that hurt", "## How good the estimate is", "Steady"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the report is missing %q", want)
		}
	}
}

func TestTheCommonSetIsTheSameSizeForEveryCandidate(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	measured := newWorld(rows, defaultWindow)
	requests, _ := requestsOf(rows)
	stream := momentsOf(rows, requests, nil)
	look := settings{window: defaultWindow, min: 2}
	said := map[string]answers{}
	for _, build := range candidates(lane.HalfLife, 0) {
		policy := build()
		said[policy.Name()] = pass(policy, stream, len(requests))
	}
	bills := bill(requests, said, measured, look, nil)
	// FOUR CANDIDATES SCORED ON FOUR DIFFERENT SETS CANNOT BE COMPARED, which is
	// the fault this law exists to keep out: a policy that declines the hard half
	// of the log would otherwise win every table by declining it.
	for _, class := range classes {
		want := -1
		for name, held := range bills {
			one := held[class]
			if one == nil {
				continue
			}
			if want < 0 {
				want = one.scored
				continue
			}
			if one.scored != want {
				t.Errorf("%s scored %d requests in the %s class; another candidate scored %d", name, one.scored, class, want)
			}
		}
	}
}

func TestAPolicyIsNeverTaughtAnAnswerItCouldNotHaveHadYet(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	requests, _ := requestsOf(rows)
	stream := momentsOf(rows, requests, nil)
	// THE ORDER IS THE WHOLE OF AN OFFLINE EVALUATION'S HONESTY. A stream that
	// handed a candidate an answer before the request that produced it would
	// flatter every policy that is good at hindsight, which is all of them.
	var last time.Time
	for _, one := range stream {
		if one.at.Before(last) {
			t.Fatalf("the stream goes backwards at %s", one.at)
		}
		last = one.at
	}
	for index, one := range stream {
		if one.saw == nil {
			continue
		}
		for _, before := range stream[:index] {
			if before.ask != nil && before.ask.id != "" && one.saw.At.Before(before.ask.at) {
				t.Fatalf("a sighting from %s was folded in after a request made at %s", one.saw.At, before.ask.at)
			}
		}
	}
}
