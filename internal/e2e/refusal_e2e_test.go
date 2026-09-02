//go:build e2e

package e2e

// refusal_e2e_test.go is issue #266's replication, driven end to end: the real
// binary, a real terminal, and a router that PUBLISHES a machine on its
// endpoints page and refuses to serve the model from it.
//
// ── WHY IT IS A STUB AND NOT THE LIVE ROUTER ────────────────────────────────
//
// Every other subtest beside this one talks to a real model, and this one
// cannot: the defect is a DISAGREEMENT between two answers the router gives —
// the endpoints page under one id, the completion under another — and a live
// router cannot be asked to disagree with itself on demand. The reported run
// collected six 404s of it in ten minutes and nobody could reproduce it.
//
// So the router is `internal/lane/lanestub` with [lanestub.Lane.SheetOnly] set,
// which publishes a lane and will not serve it, and the refusal it answers with
// is the live body copied word for word:
//
//	Providers serving <model>: …, but your request's provider.only preference
//	permits only: ghost
//
// Everything above that is real: the binary a person runs, its own chooser, its
// own race, its own ladder, and the screen read back with capture-pane.
//
// ── WHAT IS ASSERTED AND WHAT IS GLIMPSED ───────────────────────────────────
//
// The four laws are asserted off the STUB'S OWN REQUEST LOG, which is a record
// of what the binary really did and is not subject to a race with a redraw: the
// refused machine is asked once, the next request leaves within two seconds
// demanding somebody else, no gap opens while the turn is working, and the turn
// still ends in an answer.
//
// The sentence on the status row is GLIMPSED, for the reason tuiphase_test.go
// gives about its own two sentences: while a request is actually in flight the
// phase clock owns that segment (render.go's servedRider), so `refused` is
// drawn in the moments between arms rather than held there, and a subtest that
// waited twenty seconds for it would be a subtest that fails on a fast machine.
// Caught, it is asserted whole; missed, it is written down as a FINDING and the
// four laws above still stand on their own.

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// refusalModel is the model the staged router serves. It is spelled
// `openrouter/…` because that is how the transport recognises a router at all
// when the base URL is a loopback address (lanestub's own header says so).
const refusalModel = "openrouter/refusal-flash"

// refusedLane is the machine this run's own routing demands and the router
// will not serve: published on the endpoints page, absent from the serving set.
//
// IT IS NOT A PERSON'S PIN, and that distinction is the point. A pin is a
// standing instruction — somebody named the machine, and a build that quietly
// stopped honouring it would be breaking a promise the manual makes. What this
// test is about is the demand THIS PROCESS makes on its own account: the second
// request of a race names one lane so the pair cannot both land on the machine
// that is already stalling, and that is the `provider.only` the reported run
// died on, six times over.
const refusedLane = "Phantom"

// lanePrice is what EVERY lane in these scenarios charges, and it is one figure
// on purpose.
//
// The chooser drops a lane whose tariff is above the model's own list price
// times a quarter again (internal/lane's underPriceCeiling), and the staged
// router publishes its FIRST lane's price as the model's list price. A scenario
// whose machines charged differently therefore lost most of its frontier before
// anything about a refusal happened — which cost this file an afternoon and is
// worth writing down rather than rediscovering.
const lanePrice = 0.2e-6

// TestTUIRefusedLane is the replication and the acceptance of issue #266.
func TestTUIRefusedLane(t *testing.T) {
	// NO API KEY IS NEEDED AND NONE IS ASKED FOR, which is what separates this
	// subtest from every other one in the suite: the router it talks to is
	// staged in this process, so the run costs nothing and refuses on demand.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this test drives the real binary in a real terminal")
	}
	binary(t)
	// Five tool-capable machines on the sheet, and two of them the router will
	// not serve. The sheet calls the two ghosts by far the quickest, which is
	// what puts them at the head of a cold frontier — and a cold frontier is
	// the state of every model somebody picks after launch.
	//
	// THE SHEET IS WRONG ON PURPOSE AND THAT IS THE SCENARIO. A machine the
	// endpoints page praises and the completion endpoint has never heard of is
	// exactly the disagreement the reported run was made of.
	ghost := lanestub.Profile{
		Tools: true, Quant: "fp8", Context: 128000, MaxOut: 8000, Uptime: 100,
		PriceIn: lanePrice, PriceOut: 2 * lanePrice,
		TTFTms: [4]float64{20, 30, 40, 60}, Rates: [4]float64{900, 1000, 1100, 1200},
	}
	// The machines that really answer: slow enough on the first token that the
	// race has a reason to send a second request, and quick enough afterwards
	// that the turn finishes inside the suite's patience.
	real := lanestub.Profile{
		TTFT: 1500 * time.Millisecond, Rate: 40, Tokens: 12,
		Tools: true, Quant: "fp8", Context: 128000, MaxOut: 8000, Uptime: 100,
		PriceIn: lanePrice, PriceOut: 2 * lanePrice,
		TTFTms: [4]float64{1500, 2000, 2600, 4000}, Rates: [4]float64{40, 45, 50, 60},
	}
	server := lanestub.New(refusalModel,
		lanestub.Lane{Name: "Ghost", SheetOnly: true, Profile: ghost},
		lanestub.Lane{Name: refusedLane, SheetOnly: true, Profile: ghost},
		lanestub.Lane{Name: "Haven", Profile: real},
		lanestub.Lane{Name: "Harbor", Profile: real},
		lanestub.Lane{Name: "Hollow", Profile: real},
	)
	t.Cleanup(server.Close)

	home := newHome(t, map[string]any{
		"model.talk": refusalModel,
		// THE SETUP IS ALREADY BEHIND THIS MACHINE. A state root with no
		// `setup_seen_at` opens on the three-step first run and never reaches a
		// composer, which is a correct product and the wrong subject: this test
		// is about what happens when a request is refused, not about a machine
		// that has never been configured.
		"setup_seen_at": time.Now().UTC().Format(time.RFC3339Nano),
		// AND NOBODY HAS PINNED ANYTHING. The lane is left on `auto`, so every
		// `provider.only` that goes out on this run is one this build decided
		// to send — which is what makes the refusal it earns this build's to
		// act on.
		"lane.talk": "auto",
	})
	ws := newWorkspace(t, "refusalws", false)
	r := startWithEnv(t,
		[]string{"OPENROUTER_API_KEY=stub-key", "AFORGE_BASE_URL=" + server.RouterURL()},
		"afe2e_refused", home, ws, tuiWide, 40)

	// THE FRONTIER IS WAITED FOR RATHER THAN ASSUMED. A race with fewer than
	// two beliefs has no alternative to name, so it hedges nothing and demands
	// nothing — which is a correct cold start and would make this test about
	// silence. The beat fetches the endpoints page on session open; the run
	// starts talking once the router has been asked for it.
	if !waitForSheet(server, 30*time.Second) {
		t.Fatalf("the endpoints page was never fetched, so nothing could rank the lanes")
	}

	began := time.Now()
	r.lit("say hello")
	r.keys("Enter")

	// THE SENTENCE, CAUGHT IF THE RUN IS SLOW ENOUGH TO SAY IT. Two spellings,
	// because a walk that has somewhere to go and a walk that has run out say
	// different halves of the same fact.
	screen, caught := r.glimpse(20*time.Second, say(t, "laneRefusedTrying"), say(t, "laneRefusedTail"))
	if caught {
		if strings.Contains(screen, say(t, "laneSlowTrying")) {
			t.Errorf("the status row called a refusal slow:\n%s", screen)
		}
		t.Logf("the status row said what the wire said:\n%s", screen)
	} else {
		t.Logf("FINDING: the run never sat still long enough to draw %q — "+
			"the four laws below are asserted off the router's own request log instead",
			say(t, "laneRefusedTrying"))
	}

	// The turn still ends in an answer: a refusal is terminal for a LANE, not
	// for the question. The words are the staged router's own — it writes a
	// numbered token stream rather than prose — so what is waited for is the
	// first of them.
	r.waitFor(90*time.Second, "t0")

	asks := server.Asks()
	if len(asks) < 2 {
		t.Fatalf("%d requests reached the router, want the demand and what followed it", len(asks))
	}

	// WHICH of the two unservable machines this run's own routing demanded is
	// the chooser's business and not this test's, so it is READ rather than
	// assumed — and every law below holds of whichever it was.
	first, refused := firstDemand(asks)
	if first < 0 {
		t.Fatalf("nothing demanded a machine at all, so no refusal was staged:\n%s", askLog(asks))
	}
	if !sheetOnly[refused] {
		t.Fatalf("the run demanded %q, which the router serves — the scenario did not stage a refusal:\n%s",
			refused, askLog(asks))
	}

	// 1. THE REFUSED MACHINE IS NEVER ASKED AGAIN. In the reported run it was
	//    chosen three separate times.
	if asked := server.Requests(refused); asked != 1 {
		t.Errorf("%s was asked %d times, want exactly one", refused, asked)
	}

	// 2. THE NEXT REQUEST LEFT AT ONCE AND DEMANDED SOMEBODY ELSE.
	if first+1 >= len(asks) {
		t.Fatalf("nothing followed the refusal at all:\n%s", askLog(asks))
	}
	if gap := asks[first+1].At.Sub(asks[first].At); gap > 2*time.Second {
		t.Errorf("the next request took %v to leave, want under two seconds", gap)
	}
	for _, later := range asks[first+1:] {
		if namesLane(later.Only, refused) {
			t.Errorf("a request after the refusal still demanded %s: %v", refused, later.Only)
		}
	}

	// 3. NOTHING SAT ANYWHERE WHILE THE TURN WAS WORKING. The reported run had
	//    a wait clock counting twenty-six minutes and forty-seven seconds of a
	//    request that was never sent.
	for index := 1; index < len(asks); index++ {
		if gap := asks[index].At.Sub(asks[index-1].At); gap > 5*time.Second {
			t.Errorf("a %v gap opened between requests %d and %d", gap, index-1, index)
		}
	}
	t.Logf("the router's own log, %d requests over %v:\n%s", len(asks), time.Since(began), askLog(asks))
}

// sheetOnly names the machines the staged router publishes and will not serve.
var sheetOnly = map[string]bool{"Ghost": true, refusedLane: true}

// firstDemand is the first request that named ONE machine, and the machine it
// named. It is read rather than assumed because which of the two unservable
// lanes a cold frontier ranks first is the chooser's decision and this test is
// not about the chooser.
func firstDemand(asks []lanestub.Ask) (int, string) {
	for index, ask := range asks {
		if len(ask.Only) == 1 {
			return index, strings.TrimSpace(ask.Only[0])
		}
	}
	return -1, ""
}

// namesLane reports whether a preference list names one machine.
func namesLane(list []string, lane string) bool {
	for _, name := range list {
		if strings.EqualFold(strings.TrimSpace(name), lane) {
			return true
		}
	}
	return false
}

// askLog is the router's request log as evidence a person can read: when each
// request arrived, and what it demanded.
func askLog(asks []lanestub.Ask) string {
	var b strings.Builder
	for index, ask := range asks {
		gap := time.Duration(0)
		if index > 0 {
			gap = ask.At.Sub(asks[index-1].At)
		}
		demand := "—"
		if len(ask.Only) > 0 {
			demand = strings.Join(ask.Only, ",")
		}
		b.WriteString("  +")
		b.WriteString(gap.Round(time.Millisecond).String())
		b.WriteString("  only=")
		b.WriteString(demand)
		b.WriteString("\n")
	}
	return b.String()
}

// waitForSheet blocks until the running binary has asked the staged router for
// the model's endpoints page, which is the moment its frontier stops being
// empty.
func waitForSheet(server *lanestub.Server, within time.Duration) bool {
	// The page is fetched, decoded and primed on the beat's own goroutine, so
	// [waitForSheetOf] waits a moment past the fetch: it is the difference
	// between a frontier that exists and one that is halfway through being
	// written.
	return waitForSheetOf(server, refusalModel, within)
}

// ── THE FRAME: A TURN THAT STOPPED ON A REFUSAL ─────────────────────────────
//
// TestTUIRefusedLane above proves what the routing DOES about a refusal, off
// the router's own request log. It cannot prove what the row SAYS, and the
// reason is a precedence rule in the surface rather than anything about
// refusals: while a request is in flight the phase clock owns that cell
// (render.go's servedRider), and once a turn lands the answer's own news
// replaces whatever the rescue put there. So the sentence is on the screen
// exactly when a turn STOPS on the refusal and nothing follows it.
//
// WHICH IS THE REPORTED RUN EXACTLY. Twenty-six minutes with nothing on the
// wire and `· slow · trying nextbit…` left standing on the row until a
// ten-minute window aged it out. This subtest stages that ending — a router
// that publishes five machines and serves none of them — and reads the row.
const voidModel = "openrouter/refusal-void"

func TestTUIRefusedLaneRowAfterTheTurnStops(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this test drives the real binary in a real terminal")
	}
	binary(t)
	published := lanestub.Profile{
		Tools: true, Quant: "fp8", Context: 128000, MaxOut: 8000, Uptime: 100,
		PriceIn: lanePrice, PriceOut: 2 * lanePrice,
		TTFTms: [4]float64{20, 30, 40, 60}, Rates: [4]float64{900, 1000, 1100, 1200},
	}
	// Five machines on the endpoints page and not one of them in the serving
	// set. Every request this run makes is refused, the walk spends itself
	// moving between them, and the turn ends with the refusal as the last thing
	// anybody was told.
	server := lanestub.New(voidModel,
		lanestub.Lane{Name: "Ghost", SheetOnly: true, Profile: published},
		lanestub.Lane{Name: "Phantom", SheetOnly: true, Profile: published},
		lanestub.Lane{Name: "Spectre", SheetOnly: true, Profile: published},
		lanestub.Lane{Name: "Wraith", SheetOnly: true, Profile: published},
		lanestub.Lane{Name: "Shade", SheetOnly: true, Profile: published},
	)
	t.Cleanup(server.Close)

	home := newHome(t, map[string]any{
		"model.talk":    voidModel,
		"setup_seen_at": time.Now().UTC().Format(time.RFC3339Nano),
		"lane.talk":     "auto",
	})
	ws := newWorkspace(t, "voidws", false)
	r := startWithEnv(t,
		[]string{"OPENROUTER_API_KEY=stub-key", "AFORGE_BASE_URL=" + server.RouterURL()},
		"afe2e_refused_row", home, ws, tuiWide, 40)

	if !waitForSheetOf(server, voidModel, 30*time.Second) {
		t.Fatalf("the endpoints page was never fetched, so nothing could rank the lanes")
	}
	r.lit("say hello")
	r.keys("Enter")

	// THE ROW, ONCE THE TURN HAS STOPPED. `refused` is the word, and `slow` is
	// the word this row used to say about exactly this 404.
	screen := r.waitFor(60*time.Second, say(t, "laneRefusedTail"))
	if strings.Contains(screen, say(t, "laneSlowTrying")) {
		t.Errorf("the status row called a refusal slow:\n%s", screen)
	}
	// AND THE PROMISE IS GONE. A row still saying `trying …` after every arm
	// has died is the retraction failing, which is the half of #266 a person
	// stared at for ten minutes.
	if strings.Contains(screen, say(t, "laneRefusedTrying")) {
		t.Errorf("the row is still promising a rescue that has already failed:\n%s", screen)
	}
	t.Logf("the row after a turn that stopped on a refusal:\n%s", screen)

	// Every machine it demanded, it demanded once.
	for _, ghost := range []string{"Ghost", "Phantom", "Spectre", "Wraith", "Shade"} {
		if asked := server.Requests(ghost); asked > 1 {
			t.Errorf("%s was asked %d times, want at most one", ghost, asked)
		}
	}
	t.Logf("the router's own log, %d requests:\n%s", len(server.Asks()), askLog(server.Asks()))
}

// waitForSheetOf is [waitForSheet] for a named model.
func waitForSheetOf(server *lanestub.Server, model string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if server.Sheets(model) > 0 {
			time.Sleep(2 * time.Second)
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
