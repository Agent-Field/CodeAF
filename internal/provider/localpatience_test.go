package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/control"
)

// Derived from the validation contract for slow-local-model patience. The
// package pins the process-wide `response.patience` to one for every other test
// (localpatience_pin_test.go), so each test here states the figure it is about:
// zero is `auto`, which is ten for a loopback server and one for a hosted address.

func patientClient(t *testing.T, base, source string) *Client {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: base, Model: "sim/model", SourceID: source})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	return client
}

// CONTRACT: a client is held to the patience of where it sends — one for a
// hosted address under auto, the local multiple for a local one and for the
// Ollama row wherever it runs, and a written number for every address.
func TestAClientIsHeldToThePatienceOfWhereItSends(t *testing.T) {
	hosted := patientClient(t, "https://openrouter.ai/api/v1", "")
	local := patientClient(t, "http://localhost:11434/v1", "")
	lan := patientClient(t, "http://192.168.1.20:8000/v1", "")
	ollamaRow := patientClient(t, "https://ollama.example.com/v1", "ollama")

	usePatience(t, 0)
	for name, tc := range map[string]struct {
		client *Client
		want   float64
	}{"hosted": {hosted, 1}, "localhost": {local, lanes.LocalPatience}, "lan": {lan, lanes.LocalPatience}, "ollama row": {ollamaRow, lanes.LocalPatience}} {
		if got := tc.client.patience(); got != tc.want {
			t.Errorf("auto, %s: patience = %v, want %v", name, got, tc.want)
		}
	}

	usePatience(t, 3)
	for name, client := range map[string]*Client{"hosted": hosted, "localhost": local, "lan": lan, "ollama row": ollamaRow} {
		if got := client.patience(); got != 3 {
			t.Errorf("3, %s: patience = %v, want exactly 3", name, got)
		}
	}

	usePatience(t, 1)
	if got := local.patience(); got != 1 {
		t.Errorf("a person who wrote 1 gets %v on a local endpoint, want today's behaviour", got)
	}
	var none *Client
	if got := none.patience(); got != 1 {
		t.Errorf("a nil client's patience = %v", got)
	}
}

// CONTRACT: every silence bound is the hosted bound at one, and that many times
// the hosted bound otherwise — for every role, in the same proportion, so the
// transport stays strictly outside the controller at every factor.
func TestEveryStallBoundIsTheHostedBoundAtOneAndMultipliedOtherwise(t *testing.T) {
	for _, role := range []lanes.Role{lanes.RoleTalk, lanes.RoleLeafAttached, lanes.RoleLeafUnattended, lanes.RoleUnknown} {
		hosted := boundsFor(role)
		if got := boundsForPatience(role, 1); got != hosted {
			t.Errorf("%s: factor 1 = %+v, hosted = %+v; they must be identical", role, got, hosted)
		}
		for _, bad := range []float64{0, 0.5, -1} {
			if got := boundsForPatience(role, bad); got != hosted {
				t.Errorf("%s: factor %v = %+v, want the hosted bounds untouched", role, bad, got)
			}
		}
		for _, factor := range []float64{3, 10} {
			got := boundsForPatience(role, factor)
			want := stallBounds{
				first:    time.Duration(float64(hosted.first) * factor),
				gap:      time.Duration(float64(hosted.gap) * factor),
				buffered: time.Duration(float64(hosted.buffered) * factor),
				floor:    time.Duration(float64(hosted.floor) * factor),
			}
			if got != want {
				t.Errorf("%s: factor %v = %+v, want %+v", role, factor, got, want)
			}
		}
	}
	// And the figures the manual states for a watched turn, at the shipped
	// constants: the 45-second gap is 7m30s on a local endpoint.
	talk := boundsForPatience(lanes.RoleTalk, lanes.LocalPatience)
	if talk.gap != 450*time.Second || talk.first != 900*time.Second || talk.buffered != 1500*time.Second {
		t.Errorf("a watched turn on a local endpoint: %+v, want gap 7m30s, first 15m, buffered 25m", talk)
	}
}

// CONTRACT: the header deadline of the streaming transport is the hosted two
// minutes for a hosted address and for a local one under 1, and ten times that
// for a local one under auto — which is what lets a model that says nothing for
// five minutes while it reads a long prompt answer at all.
func TestTheHeaderDeadlineFollowsThePatience(t *testing.T) {
	headerDeadline := func(client *Client) time.Duration {
		t.Helper()
		transport, ok := client.clientFor("sim/model", true, 0).Transport.(*http.Transport)
		if !ok {
			t.Fatalf("a streaming client's transport is %T", client.clientFor("sim/model", true, 0).Transport)
		}
		return transport.ResponseHeaderTimeout
	}
	hosted := patientClient(t, "https://openrouter.ai/api/v1", "")
	local := patientClient(t, "http://localhost:11434/v1", "")

	usePatience(t, 0)
	if got := headerDeadline(hosted); got != 2*time.Minute {
		t.Errorf("hosted under auto: header deadline %v, want exactly 2m", got)
	}
	if got := hosted.clientFor("sim/model", true, 0).Transport; got != streamTransport() {
		t.Error("a hosted call under auto is not on the original streaming transport")
	}
	if got := headerDeadline(local); got != 20*time.Minute {
		t.Errorf("local under auto: header deadline %v, want 20m (it was cut at 2m0s before)", got)
	}
	if got := headerDeadline(local); got < 5*time.Minute {
		t.Errorf("a local model that says nothing for five minutes is cut at %v", got)
	}

	usePatience(t, 3)
	if got := headerDeadline(hosted); got != 6*time.Minute {
		t.Errorf("hosted under 3: header deadline %v, want 6m", got)
	}
	if got := headerDeadline(local); got != 6*time.Minute {
		t.Errorf("local under 3: header deadline %v, want 6m", got)
	}

	usePatience(t, 1)
	if got := headerDeadline(local); got != 2*time.Minute {
		t.Errorf("local under 1: header deadline %v, want today's 2m", got)
	}
	if streamTransport().ResponseHeaderTimeout != 2*time.Minute {
		t.Errorf("the shared streaming transport was changed to %v", streamTransport().ResponseHeaderTimeout)
	}
}

// CONTRACT: a client the caller brought its own HTTP client for keeps it.
func TestACallersOwnTransportIsNotReplaced(t *testing.T) {
	usePatience(t, 0)
	own := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("unused") })}
	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://localhost:11434/v1", Model: "sim/model", HTTPClient: own})
	if err != nil {
		t.Fatal(err)
	}
	if _, swapped := client.clientFor("sim/model", true, 0).Transport.(*http.Transport); swapped {
		t.Error("a caller's own transport was replaced by the streaming pool")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// CONTRACT: a non-streamed answer is held to the hosted deadline times the
// patience, and a measured lane's wall (the 2m30s that cut a local model once it
// had answered once) is in the same proportion.
func TestANonStreamedAnswerIsHeldToTheStretchedDeadline(t *testing.T) {
	local := patientClient(t, "http://localhost:11434/v1", "")
	usePatience(t, 1)
	base := local.clientFor("sim/model", false, 4096).Timeout
	if base <= 0 {
		t.Fatalf("hosted non-streamed deadline = %v", base)
	}
	usePatience(t, 0)
	if got := local.clientFor("sim/model", false, 4096).Timeout; got != 10*base {
		t.Errorf("local under auto: %v, want ten times %v", got, base)
	}

	local.noteRun("sim/model", "", 5*time.Second)
	usePatience(t, 1)
	wall1, measured := local.completionWall("sim/model")
	if !measured || wall1 != stallWallMeasured {
		t.Fatalf("a measured lane's wall under 1 = %v, %v; want %v", wall1, measured, stallWallMeasured)
	}
	usePatience(t, 0)
	if wall10, _ := local.completionWall("sim/model"); wall10 != 10*wall1 {
		t.Errorf("a measured lane's wall under auto = %v, want ten times %v", wall10, wall1)
	}
}

// CONTRACT: a stream writing about a token a second is keeping pace under auto
// on a local endpoint, and is not under the hosted bound; the wall is the
// hosted one times the patience.
func TestASlowButSteadyLocalStreamKeepsPace(t *testing.T) {
	local := patientClient(t, "http://localhost:11434/v1", "")
	usePatience(t, 1)
	hostedPace := local.streamPace("sim/model", "")
	hostedWall := local.streamWall("sim/model", "")
	usePatience(t, 0)
	pace := local.streamPace("sim/model", "")
	wall := local.streamWall("sim/model", "")
	if pace != hostedPace/lanes.LocalPatience {
		t.Errorf("pace = %v, want %v / %v", pace, hostedPace, lanes.LocalPatience)
	}
	if wall != time.Duration(float64(hostedWall)*lanes.LocalPatience) {
		t.Errorf("wall = %v, want ten times %v", wall, hostedWall)
	}

	// A minute of a token a second, as a stream's own counters see it.
	oneTokenASecond := func(pace float64) bool {
		now := time.Now()
		watch := newPatientStallWatch(context.Background(), func() {}, wall, pace, 1)
		defer watch.stop()
		watch.spoken = true
		watch.paceFrom, watch.paceMark = now.Add(-time.Minute), 0
		watch.written = 60 * charsPerToken
		return watch.keptPace(now)
	}
	if oneTokenASecond(hostedPace) {
		t.Fatalf("test premise: one token a second must NOT keep the hosted pace of %v", hostedPace)
	}
	if !oneTokenASecond(pace) {
		t.Errorf("one token a second does not keep the local pace of %v", pace)
	}
}

// CONTRACT: the controller's ceiling and the turn's deadline are stretched with
// the rest and the floor is not; at one the plan is the very same value.
func TestAPlanIsStretchedAtItsCeilingAndDeadlineButNotItsFloor(t *testing.T) {
	began := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	plan := control.Plan{Began: began, Ceiling: 10 * time.Second, Floor: 800 * time.Millisecond, Deadline: began.Add(90 * time.Second)}
	local := patientClient(t, "http://localhost:11434/v1", "")
	hosted := patientClient(t, "https://api.openai.com/v1", "")

	usePatience(t, 0)
	if got := hosted.stretchPlan(plan); !reflect.DeepEqual(got, plan) {
		t.Errorf("a hosted plan under auto changed: %+v", got)
	}
	got := local.stretchPlan(plan)
	if got.Ceiling != 100*time.Second || got.Deadline != began.Add(15*time.Minute) || got.Floor != plan.Floor {
		t.Errorf("local plan = ceiling %v, deadline +%v, floor %v; want 100s, +15m, floor unchanged", got.Ceiling, got.Deadline.Sub(began), got.Floor)
	}
	noDeadline := plan
	noDeadline.Deadline = time.Time{}
	if got := local.stretchPlan(noDeadline); !got.Deadline.IsZero() {
		t.Errorf("a plan with no deadline grew one: %v", got.Deadline)
	}
	usePatience(t, 3)
	if got := hosted.stretchPlan(plan); got.Ceiling != 30*time.Second || got.Deadline != began.Add(270*time.Second) {
		t.Errorf("hosted under 3: ceiling %v, deadline +%v; want 30s, +4m30s", got.Ceiling, got.Deadline.Sub(began))
	}
}

// CONTRACT: a local endpoint is never sent a duplicate of a slow request, and a
// hosted one still can be.
func TestALocalEndpointIsNeverOfferedARescueRequest(t *testing.T) {
	before := LaneGuardOn()
	t.Cleanup(func() { SetLaneGuard(before) })
	SetLaneGuard(true)
	usePatience(t, 0)

	hosted := patientClient(t, "https://openrouter.ai/api/v1", "")
	local := patientClient(t, "http://localhost:11434/v1", "")
	ollamaRow := patientClient(t, "https://ollama.example.com/v1", "ollama")

	if plan := hosted.planFor(context.Background(), lanes.Choice{}, "sim/model", 0); !plan.Purse.Allows(0.0001, hosted.clock()) {
		t.Fatal("the hosted purse refused a rescue with the guard on: hosted behaviour moved")
	}
	for name, client := range map[string]*Client{"localhost": local, "the ollama row": ollamaRow} {
		if plan := client.planFor(context.Background(), lanes.Choice{}, "sim/model", 0); plan.Purse.Allows(0.0001, client.clock()) {
			t.Errorf("%s: the purse allowed a second request to the same machine", name)
		}
	}
	// A person who wrote a number keeps the same promise: it changes how long
	// a local model is waited on, never whether it is asked twice.
	usePatience(t, 3)
	if plan := local.planFor(context.Background(), lanes.Choice{}, "sim/model", 0); plan.Purse.Allows(0.0001, local.clock()) {
		t.Error("a written patience re-enabled duplicate requests to a local endpoint")
	}
}

// CONTRACT: the refusal is about having nowhere else to go. A local gateway
// that has shown it routes among several hosts is still rescued there, because
// that is a real rescue and not a second request to the same machine.
func TestALocalGatewayThatNamesAnotherLaneMayStillBeRescued(t *testing.T) {
	client, _, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
	)
	before := LaneGuardOn()
	t.Cleanup(func() { SetLaneGuard(before) })
	SetLaneGuard(true)
	usePatience(t, 0)

	if !client.localEndpoint() {
		t.Fatal("test premise: the stub router is on loopback")
	}
	if !client.prefsProven() {
		t.Fatal("test premise: the stub router has shown it routes among several hosts")
	}
	plan := client.planFor(context.Background(), lanes.Choice{}, model, 0)
	if !plan.Purse.Allows(0.0001, client.clock()) {
		t.Error("a rescue to another lane of a local gateway was refused")
	}
}

// ── on the wire, with the bounds shortened to milliseconds ──────────────────

// streamingServer answers with `script` and counts the requests it got.
func streamingServer(t *testing.T, script func(w http.ResponseWriter, flush func())) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush := func() { w.(http.Flusher).Flush() }
		flush()
		script(w, flush)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func finish(w http.ResponseWriter, flush func()) {
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush()
}

// CONTRACT: a loopback server that goes quiet for several times the bound the
// test set is waited on under auto and answers on its FIRST request; the same
// server under 1 is cut at the bound.
func TestASilentLocalServerIsWaitedOnAndAskedOnce(t *testing.T) {
	defer shortenStallBounds(t, 100*time.Millisecond, 100*time.Millisecond)()
	server, requests := streamingServer(t, func(w http.ResponseWriter, flush func()) {
		time.Sleep(350 * time.Millisecond)
		fmt.Fprint(w, "data: "+deltaChunk("the prompt is read")+"\n\n")
		finish(w, flush)
	})

	usePatience(t, 0)
	response, err := streamAgainst(t, server.URL, nil)
	if err != nil {
		t.Fatalf("a local server silent for 3.5x the bound was cut: %v", err)
	}
	if !strings.Contains(response.Text(), "the prompt is read") {
		t.Errorf("text = %q", response.Text())
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the server saw %d requests, want exactly one", got)
	}

	usePatience(t, 1)
	_, err = streamAgainst(t, server.URL, nil)
	if cut, ok := CutFrom(err); !ok || cut.Reason != CutSilent {
		t.Errorf("under 1 the same server was not cut as silent: %v", err)
	}
}

// CONTRACT: the same for a reply that goes quiet part of the way through.
func TestALocalServerThatPausesMidReplyIsWaitedOnAndAskedOnce(t *testing.T) {
	defer shortenStallBounds(t, 5*time.Second, 100*time.Millisecond)()
	server, requests := streamingServer(t, func(w http.ResponseWriter, flush func()) {
		fmt.Fprint(w, "data: "+deltaChunk("first half, ")+"\n\n")
		flush()
		time.Sleep(350 * time.Millisecond)
		fmt.Fprint(w, "data: "+deltaChunk("second half")+"\n\n")
		finish(w, flush)
	})

	usePatience(t, 0)
	response, err := streamAgainst(t, server.URL, nil)
	if err != nil {
		t.Fatalf("a local server that paused 3.5x the gap bound mid-reply was cut: %v", err)
	}
	if !strings.Contains(response.Text(), "first half, second half") {
		t.Errorf("text = %q", response.Text())
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the server saw %d requests, want exactly one", got)
	}

	usePatience(t, 1)
	_, err = streamAgainst(t, server.URL, nil)
	if cut, ok := CutFrom(err); !ok || cut.Reason != CutStalled {
		t.Errorf("under 1 the same pause was not cut as a stall: %v", err)
	}
}

// CONTRACT: waiting longer is not waiting forever — a local server that is
// quiet past the stretched bound is still cut and named.
func TestALocalServerQuietPastTheStretchedBoundIsStillCut(t *testing.T) {
	defer shortenStallBounds(t, 40*time.Millisecond, 40*time.Millisecond)()
	server, _ := streamingServer(t, func(w http.ResponseWriter, flush func()) {
		time.Sleep(2 * time.Second)
		finish(w, flush)
	})
	usePatience(t, 3)
	started := time.Now()
	_, err := streamAgainst(t, server.URL, nil)
	if cut, ok := CutFrom(err); !ok || cut.Reason != CutSilent {
		t.Fatalf("err = %v, want a silent cut", err)
	}
	if took := time.Since(started); took < 100*time.Millisecond || took > time.Second {
		t.Errorf("cut after %v, want after about 3 x 40ms and long before the server answered", took)
	}
}
