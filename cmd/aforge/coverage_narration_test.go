package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// `no check exercises` HAS TO REACH THE STREAM. igel s6 printed "acceptance — 17
// points from the request" at twenty-three seconds and never said another word
// about them, while three of the seventeen went to the end of the run exercised
// by nothing. It could not say it: the finding was a paragraph in the middle of
// the gate's `gap` string, and the line the stream prints is that string's FIRST
// line.
//
// FAILSAFE clause 3 — a fail-safe that does not propagate to the verdict the
// person reads is decoration.
func TestTheCoverageFindingReachesTheStream(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
		Pass: false,
		Gap: "The deliverable is a report about the work rather than the work.\n\n" +
			"The request asks for behaviours that no check exercises.\n" +
			"no check exercises: a rejected non-listed status does not close half-open state\n" +
			"no check exercises: a parse or hook failure is not retried",
		Unexercised: []string{
			"a rejected non-listed status does not close half-open state",
			"a parse or hook failure is not retried",
		},
	}); err != nil {
		t.Fatal(err)
	}
	line := narrated(t, watcher, said)
	if !strings.Contains(line, "gate: fail") {
		t.Fatalf("the verdict was not said:\n%s", line)
	}
	if !strings.Contains(line, "no check exercises") {
		t.Fatalf("the coverage finding never reached the stream:\n%s", line)
	}
	if !strings.Contains(line, "does not close half-open state") {
		t.Fatalf("the finding is named but says nothing:\n%s", line)
	}
	// One behaviour spelled out and the rest counted, which is the shape every
	// other list in this stream uses. Two lines per finding is two hundred lines
	// on a checklist of a hundred.
	if !strings.Contains(line, "and 1 more") {
		t.Fatalf("the finding printed its whole list instead of counting it:\n%s", line)
	}
	if strings.Count(line, "no check exercises") != 1 {
		t.Fatalf("the finding took more than one line:\n%s", line)
	}
}

// A PASS OVER A SUITE NOBODY COULD READ ENDS THE RUN SHORT, AND SAYS SO. ink s7
// journaled `npx ava --tap` killed at its ceiling of 1m53s, passed the round-two
// gate over a tree with no roster, and left with exit 0 at 13 of 25 hidden
// checks. FAILSAFE clause 5: a fail-safe has a floor that cannot deliver nothing
// as done.
func TestAPassOverAnUnreadableSuiteEndsShortAndSaysWhy(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	unreadable := store.DeliveryGate{
		Pass: true, Unreadable: true,
		Unmeasured: "nothing in this project's verification could be read: " +
			"`npx ava --tap` was killed at its ceiling of 1m53s without finishing",
	}
	if err := graph.RecordDeliveryGate("task-1", unreadable); err != nil {
		t.Fatal(err)
	}
	// The gate line still says pass, because the judge did pass — what changed
	// is what the run is willing to call whole.
	if line := narrated(t, watcher, said); !strings.Contains(line, "gate: pass") {
		t.Fatalf("the judge's own verdict was misreported:\n%s", line)
	}
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if watcher.deliveredWhole(node) {
		t.Fatal("a delivery nothing could check settled whole, which is exit 0 over " +
			"a suite nobody read")
	}
	said.Reset()
	watcher.sayStanding(node)
	standing := said.String()
	if !strings.Contains(standing, "partial — nothing in this project's verification could be read") {
		t.Fatalf("the run ends short and does not say why:\n%s", standing)
	}
	if !strings.Contains(standing, "killed at its ceiling") {
		t.Fatalf("the last line does not carry the reason nothing was read:\n%s", standing)
	}

	// And the other silence still delivers whole. A project that declares no
	// verification leaves the question unanswerable, and failing every such
	// delivery would fail every piece of prose this program writes.
	silent, watch2, _ := narrationFixture(t)
	if err := silent.RecordDeliveryGate("task-1", store.DeliveryGate{
		Pass: true, Unmeasured: "this project declares no verification this run could read",
	}); err != nil {
		t.Fatal(err)
	}
	quiet, ok, err := silent.Node("task-1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if !watch2.deliveredWhole(quiet) {
		t.Error("a project with no verification at all was charged for not having one")
	}
}
