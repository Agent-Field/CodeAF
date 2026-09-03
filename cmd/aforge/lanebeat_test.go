package main

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	homepkg "github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── THE HEADLESS DOOR LEARNS A SHEET TOO ────────────────────────────────────
//
// The lane ledger is ONE OBJECT every surface feeds and reads, and until issue
// #318 the fetching half of it lived on `session.Agent` alone. `aforge do`
// builds no session, so a headless install wrote sightings forever and never
// fetched an endpoints page: no cache under `v3/lanes/`, no sheet facts in the
// belief, and a chooser ranking the one lane the run happened to be served by.
// That is the "chooses between no lanes at all" condition, resurrected for one
// door.

// routedLanes are three machines that differ on speed AND on price, which is
// what leaves a frontier with something on it: three lanes where one is faster
// and no dearer are three lanes with one candidate.
func routedLanes() []lanestub.Lane {
	return []lanestub.Lane{
		{Name: "quicksilver", Profile: lanestub.Profile{
			TTFT: 20 * time.Millisecond, Rate: 400, Tokens: 8, Tools: true,
			PriceIn: 0.000003, PriceOut: 0.000006}},
		{Name: "brass", Profile: lanestub.Profile{
			TTFT: 30 * time.Millisecond, Rate: 300, Tokens: 8, Tools: true,
			PriceIn: 0.0000025, PriceOut: 0.000005}},
		{Name: "molasses", Profile: lanestub.Profile{
			TTFT: 40 * time.Millisecond, Rate: 200, Tokens: 8, Tools: true,
			PriceIn: 0.000002, PriceOut: 0.000004}},
	}
}

// beatingFor points the process beat at a lifetime this test can end, the way
// [execute] points it at the one exit every command shares.
func beatingFor(t *testing.T) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	restore := laneBeatCtx
	laneBeatCtx = ctx
	t.Cleanup(func() {
		stop()
		laneBeatCtx = restore
	})
}

// TestAHeadlessRunFetchesTheSheetForItsOwnModel is acceptance 1 and 2 of #318:
// one `do` leaves a sheet cache and sheet-known beliefs for every lane of its
// model, and the next run's chooser has more than one machine to rank.
func TestAHeadlessRunFetchesTheSheetForItsOwnModel(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()

	const routed = "openrouter/twin-lane"
	stub := lanestub.New(routed, routedLanes()...)
	defer stub.Close()

	// A state root of its own, so the cache and the belief file this asserts on
	// are this test's and never the developer's.
	state := t.TempDir()
	t.Setenv(homepkg.EnvVar, state)
	// THE STUB'S PLAIN LOOPBACK ADDRESS, AND NO HOSTNAME TRICK. This test used
	// to smuggle `openrouter.ai` into the URL's userinfo so that a substring
	// gate would let the beat run; the gate is gone (issue #373) and the base
	// is recognised by the endpoints page it answers with.
	t.Setenv("AFORGE_BASE_URL", stub.URL())
	t.Setenv(config.ModelEnv, routed)
	lanes.Default().Reset()
	t.Cleanup(lanes.Default().Reset)
	beatingFor(t)

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstderr:\n%s", err, stderr.String())
	}

	model := lanes.LedgerModel(routed)
	// The beat is a goroutine the run does not wait on, so the assertion waits
	// for it rather than racing it.
	waitForLanes(t, model, len(routedLanes()))

	for _, belief := range lanes.Default().Ledger().Beliefs(model) {
		if !belief.Facts.Known() {
			t.Fatalf("the sheet never spoke about %+v, so the gate is judging a lane it cannot see", belief.ID)
		}
	}

	// AND THE READING IS ON DISK, which is what the next process opens.
	cache := filepath.Join(state, "v3", "lanes", url.PathEscape(model)+".json")
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("no sheet cache for the run's own model: %v", err)
	}

	// A second run in the same home: the chooser ranks a frontier, which is the
	// durable form of "there is a machine to hedge to".
	var again, alsoStderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", timeout: 60 * time.Second,
		stdout: &again, stderr: &alsoStderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("the second errand did not settle cleanly: %v\nstderr:\n%s", err, alsoStderr.String())
	}
	choice := lanes.Default().Chooser().Choose(provider.LaneTalkAsk(routed, time.Now()))
	if len(choice.Frontier) < 2 {
		t.Fatalf("the chooser ranked %d lanes for a headless run's model, so no hedge could name an alternative",
			len(choice.Frontier))
	}
}

// waitForLanes waits until the ledger holds at least want lanes for model.
func waitForLanes(t *testing.T, model string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(lanes.Default().Ledger().Beliefs(model)) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the headless run left %d lanes in the ledger for %q, want %d: it never fetched a sheet",
		len(lanes.Default().Ledger().Beliefs(model)), model, want)
}
