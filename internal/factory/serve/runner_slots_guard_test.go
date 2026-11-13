package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func (c capacityRig) attachLive(t *testing.T, token, owner, machine string, live bool) {
	t.Helper()
	c.request(t, "POST", "/v1/runners/attach", token, api.RunnerAttach{Runner: machine, Name: machine, Owner: owner, Kind: api.RunnerMac, LiveCapacity: live,
		Checkouts: []api.Checkout{{Repo: "acme/api", Path: "/code/api"}}}, 201, nil)
}

func (c capacityRig) put(token, runner, body string) (int, string) {
	w := identityRequest(c.h, "PUT", "/v1/runners/"+runner, body, token, nil)
	return w.Code, w.Header().Get(api.PhaseHeader)
}

// THE DIRECTORY STATES THE RANGE AND THE CONDITION, from the server's own
// constant, so no client copies the number.
func TestRunnerDirectoryCarriesTheSlotsPolicy(t *testing.T) {
	c := capacitySetup(t)
	var list api.Runners
	c.request(t, "GET", "/v1/runners", c.priya, nil, 200, &list)
	if list.SlotsPolicy == nil || *list.SlotsPolicy != (api.RunnerSlotsPolicy{Min: 1, Max: api.MaxRunnerSlots, RequiresLive: true}) {
		t.Fatalf("policy %+v", list.SlotsPolicy)
	}
}

// A WORKER THAT CANNOT FOLLOW A CHANGE IS NEVER WRITTEN FOR, and a client that
// does not ask for the guard keeps the earlier behaviour exactly.
func TestRunnerSettingsRequireLiveRefusesAnOldWorkerWithNoWrite(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box", false)
	code, phase := c.put(c.priya, "box", `{"slots":5,"require_live_capacity":true}`)
	if code != 409 || phase != api.PhasePreMutation {
		t.Fatalf("old worker: %d %q", code, phase)
	}
	if got := c.runner(t, "box"); got.Slots != api.DefaultRunnerSlots || len(got.Checkouts) != 1 {
		t.Fatalf("a refused write changed the runner: %+v", got)
	}
	// An older client (no field) still writes, as before.
	if code, _ := c.put(c.priya, "box", `{"slots":5}`); code != 200 {
		t.Fatalf("legacy PUT = %d", code)
	}
	if got := c.runner(t, "box"); got.Slots != 5 {
		t.Fatalf("legacy PUT: %+v", got)
	}
}

func TestRunnerSettingsRequireLiveWritesForAnAttestedWorkerAndKeepsCheckouts(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box", true)
	code, phase := c.put(c.priya, "box", `{"slots":6,"require_live_capacity":true}`)
	if code != 200 || phase != "" {
		t.Fatalf("live worker: %d %q", code, phase)
	}
	got := c.runner(t, "box")
	if got.Slots != 6 || len(got.Checkouts) != 1 || got.Checkouts[0].Path != "/code/api" || !got.LiveCapacity {
		t.Fatalf("after the write: %+v", got)
	}
}

// THE CHECK IS MADE WHERE THE WRITE IS: a worker that went back to an older
// binary after the caller read the directory is the newest attach, and the
// write is refused before anything changes.
func TestRunnerSettingsRequireLiveSeesAWorkerThatRestartedOntoAnOlderBinary(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box", true)
	if !c.runner(t, "box").LiveCapacity {
		t.Fatal("the directory should have listed it live")
	}
	c.now = c.now.Add(time.Minute)
	c.attachLive(t, c.priya, "priya", "box", false) // the restart, after the read
	code, phase := c.put(c.priya, "box", `{"slots":7,"require_live_capacity":true}`)
	if code != 409 || phase != api.PhasePreMutation {
		t.Fatalf("stale attestation: %d %q", code, phase)
	}
	if got := c.runner(t, "box"); got.Slots != api.DefaultRunnerSlots || got.LiveCapacity {
		t.Fatalf("the directory must now say what the guard saw: %+v", got)
	}
	c.now = c.now.Add(time.Minute)
	c.attachLive(t, c.priya, "priya", "box", true)
	if code, _ := c.put(c.priya, "box", `{"slots":7,"require_live_capacity":true}`); code != 200 {
		t.Fatalf("the newest attach attests again: %d", code)
	}
}

// Attaches issued in the same instant cannot be ordered, so the write needs
// every one of them to attest: an ambiguity can only withhold a write.
func TestRunnerSettingsRequireLiveTiedAttachesMustAllAttest(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box", true)
	c.attachLive(t, c.priya, "priya", "box", false) // same fake instant
	if code, _ := c.put(c.priya, "box", `{"slots":4,"require_live_capacity":true}`); code != 409 {
		t.Fatalf("tie = %d", code)
	}
	if c.runner(t, "box").LiveCapacity {
		t.Fatal("the directory listed an ambiguous machine as live")
	}
}

// A REFUSAL BEFORE THE FIRST CHANGE CARRIES THE MARKER; owner-only is still a 404
// that cannot tell another person's computer from none.
func TestRunnerSettingsMarkEveryRefusalMadeBeforeTheFirstChange(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box", true)
	for name, tc := range map[string]struct {
		token, runner, body string
		status              int
	}{
		"no credential":   {"", "box", `{"slots":3}`, 401},
		"bad credential":  {"no-such-token", "box", `{"slots":3}`, 401},
		"slots too low":   {c.priya, "box", `{"slots":0,"require_live_capacity":true}`, 422},
		"slots too high":  {c.priya, "box", fmt.Sprintf(`{"slots":%d,"require_live_capacity":true}`, api.MaxRunnerSlots+1), 422},
		"unknown field":   {c.priya, "box", `{"slots":3,"path":"/x"}`, 422},
		"not an object":   {c.priya, "box", `3`, 422},
		"unknown runner":  {c.priya, "ghost", `{"slots":3,"require_live_capacity":true}`, 404},
		"another owner":   {c.raj, "box", `{"slots":3,"require_live_capacity":true}`, 404},
		"the operator":    {"operator-secret", "box", `{"slots":3,"require_live_capacity":true}`, 404},
		"operator legacy": {"operator-secret", "box", `{"slots":3}`, 404},
	} {
		t.Run(name, func(t *testing.T) {
			code, phase := c.put(tc.token, tc.runner, tc.body)
			if code != tc.status || phase != api.PhasePreMutation {
				t.Fatalf("%d %q, want %d marked", code, phase, tc.status)
			}
		})
	}
	if got := c.runner(t, "box"); got.Slots != api.DefaultRunnerSlots {
		t.Fatalf("a refusal changed capacity: %+v", got)
	}
	// The marker is only for the one-segment route.
	if w := identityRequest(c.h, "PUT", "/v1/runners/box/extra", `{"slots":3}`, c.priya, nil); w.Header().Get(api.PhaseHeader) != "" {
		t.Fatal("a deeper path is marked")
	}
	if w := identityRequest(c.h, "GET", "/v1/runners", "", c.priya, nil); w.Header().Get(api.PhaseHeader) != "" {
		t.Fatal("a read is marked")
	}
}

// A SAVE OR AN EVENT THAT FAILS AFTER THE NUMBER CHANGED IS NOT A REFUSAL: the
// marker is gone, so a client must call the outcome uncertain.
func TestRunnerSettingsFailureAfterTheChangeCarriesNoMarker(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box", true)
	// The event log cannot be read, so recording the change fails AFTER the
	// number was stored and saved.
	events := filepath.Join(c.st.Root(), "events.jsonl")
	_ = os.Remove(events)
	if err := os.Mkdir(events, 0o700); err != nil {
		t.Fatal(err)
	}
	code, phase := c.put(c.priya, "box", `{"slots":6,"require_live_capacity":true}`)
	if code < 400 || phase != "" {
		t.Fatalf("event failure = %d %q", code, phase)
	}
	if got := c.runner(t, "box"); got.Slots != 6 {
		t.Fatalf("the number was not stored before the failure: %+v", got)
	}
}

func TestRunnerSettingsRequireLiveWordsDoNotEchoTheRunner(t *testing.T) {
	c := capacitySetup(t)
	c.attachLive(t, c.priya, "priya", "box-secret-name", false)
	w := identityRequest(c.h, "PUT", "/v1/runners/box-secret-name", `{"slots":3,"require_live_capacity":true}`, c.priya, nil)
	if w.Code != 409 || strings.Contains(w.Body.String(), "box-secret-name") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
