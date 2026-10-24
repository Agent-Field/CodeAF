package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── a stub factory: scripted /v1/me, /v1/runners and runner PUT ────────────
//
// httptest only: no model, no worker, no GitHub. It logs every request.

type runnerStub struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex
	log []string
	// the PUT's bearer and body, per PUT
	auths, bodies []string
	me            api.Me
	dir           api.Runners
	dirStatus     int
	dirBody       string
	put           func(w http.ResponseWriter, r *http.Request)
	onMe, onDir   func()
	onPut         func()
}

func liveRunner(id, owner string, slots int, live bool) api.Runner {
	return api.Runner{ID: id, Name: id, Owner: owner, Kind: api.RunnerMac, Slots: slots, State: api.RunnerIdle, LiveCapacity: live,
		Checkouts: []api.Checkout{{Repo: "octo/hello", Path: "/home/x/hello"}}, Jobs: []int{}}
}

func newRunnerStub(t *testing.T, me api.Me, runners ...api.Runner) *runnerStub {
	t.Helper()
	s := &runnerStub{t: t, me: me, dirStatus: http.StatusOK, dir: api.Runners{Runners: runners, SlotsPolicy: api.CurrentRunnerSlotsPolicy()}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.log = append(s.log, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			s.auths = append(s.auths, r.Header.Get("Authorization"))
			s.bodies = append(s.bodies, string(body))
		}
		me, dir, status, dirBody, put := s.me, s.dir, s.dirStatus, s.dirBody, s.put
		onMe, onDir, onPut := s.onMe, s.onDir, s.onPut
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/me":
			if onMe != nil {
				onMe()
			}
			_ = json.NewEncoder(w).Encode(me)
		case r.URL.Path == "/v1/runners" && r.Method == http.MethodGet:
			if onDir != nil {
				onDir()
			}
			if status != http.StatusOK {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"runner mbp-secret-host leaked"}`)
				return
			}
			if dirBody != "" {
				_, _ = io.WriteString(w, dirBody)
				return
			}
			_ = json.NewEncoder(w).Encode(dir)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/runners/"):
			if onPut != nil {
				onPut()
			}
			if put != nil {
				put(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"factory":{}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *runnerStub) count(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (s *runnerStub) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

func runnerMarked(status int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"/v1/runners/mbp-secret-host is echoed"}`)
	}
}

func runnerBare(status int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"/v1/runners/mbp-secret-host is echoed"}`)
	}
}

const runnerToken = "tok_held_for_runners_0123456789"

type runnerWire struct {
	*factoryWire
	hub *appFactoryHub
	id  string
}

func newRunnerWire(t *testing.T, stub *runnerStub, token string) *runnerWire {
	t.Helper()
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	hub.resume = nil
	id := appFactoryID(stub.srv.URL)
	hub.mu.Lock()
	hub.rows = []appFactoryRow{{ID: id, URL: stub.srv.URL, Name: "Acme", Mode: api.FactoryTeam}}
	hub.loaded = true
	hub.mu.Unlock()
	if token != "" {
		hub.hold(id, token, 0)
	}
	return &runnerWire{factoryWire: w, hub: hub, id: id}
}

func (r *runnerWire) slots(runner string, n int) (appwire.FactoryRunnerSlotsDone, error) {
	return r.backend.FactoryRunnerSlots(context.Background(), appwire.FactoryRunnerSlotsParams{Factory: r.id, Runner: runner, Slots: &n})
}

func alice() api.Me {
	return api.Me{Mode: api.FactoryTeam, Operator: false, Profile: api.Profile{Handle: "alice"}}
}

func wantRunnerCode(t *testing.T, err error, code string) *appwire.Error {
	t.Helper()
	if got := wireCodeOf(t, err); got != code {
		t.Fatalf("code %q (%v), want %q", got, err, code)
	}
	var w *appwire.Error
	asWire(err, &w)
	return w
}

// the words an answer may never carry: another computer's id, the factory's
// own prose, the credential.
func (r *runnerWire) leakless(t *testing.T, err error) {
	t.Helper()
	text := ""
	if err != nil {
		text = err.Error()
	}
	for _, bad := range []string{"mbp-secret-host", runnerToken, "/v1/runners", "bob-laptop"} {
		if strings.Contains(text, bad) {
			t.Fatalf("answer %q carries %q", text, bad)
		}
	}
}

// ── the yes path ───────────────────────────────────────────────────────────

func TestRunnerSlotsOwnComputerSendsOnePutAndAnswersTheExactReceipt(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true), liveRunner("bob-laptop", "bob", 2, true))
		stub.put = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }
		r := newRunnerWire(t, stub, runnerToken)
		got, err := r.slots("mbp", 5)
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if got != (appwire.FactoryRunnerSlotsDone{Factory: r.id, Runner: "mbp", Slots: 5}) {
			t.Fatalf("receipt %+v", got)
		}
		want := "GET /v1/me|GET /v1/runners|PUT /v1/runners/mbp"
		if log := strings.Join(stub.requests(), "|"); log != want {
			t.Fatalf("requests %s, want %s", log, want)
		}
		// Exactly two fields: no checkouts can go with a capacity change.
		if stub.auths[0] != "Bearer "+runnerToken || stub.bodies[0] != `{"slots":5,"require_live_capacity":true}` {
			t.Fatalf("PUT sent %q with %q", stub.auths[0], stub.bodies[0])
		}
	}
}

func TestRunnerSlotsOverTheWireAnswersExactlyTheReceiptAndRefusesExtras(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	var out map[string]any
	r.must("factory.runner_slots", map[string]any{"factory": r.id, "runner": "mbp", "slots": 3}, &out)
	if len(out) != 3 || out["factory"] != r.id || out["runner"] != "mbp" || out["slots"] != float64(3) {
		t.Fatalf("receipt %v", out)
	}
	before := len(stub.requests())
	for _, params := range []map[string]any{
		{"factory": r.id, "runner": "mbp"},
		{"factory": r.id, "slots": 2},
		{"factory": r.id, "runner": "mbp", "slots": 0},
		{"factory": r.id, "runner": "mbp", "slots": api.MaxRunnerSlots + 1},
		{"factory": r.id, "runner": "mbp", "slots": -1},
		{"factory": r.id, "runner": "mbp", "slots": 2.5},
		{"factory": r.id, "runner": "mbp", "slots": "2"},
		{"factory": r.id, "runner": "mbp", "slots": nil},
		{"factory": r.id, "runner": "mbp", "slots": 2, "checkouts": []string{}},
		{"factory": r.id, "runner": "mbp", "slots": 2, "path": "/v1/runners/x"},
		{"factory": r.id, "runner": "mbp", "slots": 2, "token": "tok_x"},
		{"factory": r.id, "runner": "mbp", "slots": 2, "verb": "DELETE"},
		{"factory": r.id, "runner": "", "slots": 2},
		{"factory": r.id, "runner": "..", "slots": 2},
		{"factory": r.id, "runner": ".", "slots": 2},
		{"factory": r.id, "runner": "a/b", "slots": 2},
		{"factory": r.id, "runner": "a\\b", "slots": 2},
		{"factory": r.id, "runner": " mbp", "slots": 2},
		{"factory": r.id, "runner": "mb\np", "slots": 2},
		{"factory": r.id, "runner": "mb‮p", "slots": 2},
		{"factory": r.id, "runner": strings.Repeat("m", appwire.RunnerIDMax+1), "slots": 2},
	} {
		r.mustFail("factory.runner_slots", params, appwire.CodeBadParams)
	}
	if len(stub.requests()) != before {
		t.Fatalf("a bad request reached the factory: %v", stub.requests()[before:])
	}
	r.neverSaid(runnerToken)
}

func TestRunnerSlotsRangeIsTheServersAndTheLastValueIsAllowed(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	for _, n := range []int{1, api.MaxRunnerSlots} {
		if got, err := r.slots("mbp", n); err != nil || got.Slots != n {
			t.Fatalf("%d: %+v %v", n, got, err)
		}
	}
}

func TestRunnerSlotsEscapesAnOpaqueIDAsOnePathSegment(t *testing.T) {
	id := "Sam's MacBook ? #1 100%"
	stub := newRunnerStub(t, alice(), liveRunner(id, "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	if _, err := r.slots(id, 2); err != nil {
		t.Fatal(err)
	}
	log := stub.requests()
	if log[2] != "PUT /v1/runners/Sam%27s%20MacBook%20%3F%20%231%20100%25" {
		t.Fatalf("path %q", log[2])
	}
}

// ── who may ────────────────────────────────────────────────────────────────

func TestRunnerSlotsOperatorHasNoPowerOverAnotherPersonsComputer(t *testing.T) {
	me := alice()
	me.Operator = true
	stub := newRunnerStub(t, me, liveRunner("bob-laptop", "bob", 2, true), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	_, err := r.slots("bob-laptop", 4)
	wantRunnerCode(t, err, appwire.CodeForbidden)
	if stub.count("PUT ") != 0 {
		t.Fatalf("an operator's write on another's computer was sent: %v", stub.requests())
	}
	// A computer nobody lists is simply not found, with no PUT either.
	_, err = r.slots("ghost", 4)
	wantRunnerCode(t, err, appwire.CodeNotFound)
	if stub.count("PUT ") != 0 {
		t.Fatal("a PUT left for an unlisted computer")
	}
}

func TestRunnerSlotsOwnerIsJudgedFreshEveryCall(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	if _, err := r.slots("mbp", 3); err != nil {
		t.Fatal(err)
	}
	// The same credential is now someone else, per /v1/me: the next call must
	// not reuse the earlier answer.
	stub.mu.Lock()
	stub.me.Handle = "carol"
	stub.mu.Unlock()
	_, err := r.slots("mbp", 4)
	wantRunnerCode(t, err, appwire.CodeForbidden)
	if stub.count("PUT ") != 1 {
		t.Fatalf("puts = %d", stub.count("PUT "))
	}
	// A sign-in with no handle owns nothing.
	stub.mu.Lock()
	stub.me.Handle = ""
	stub.mu.Unlock()
	_, err = r.slots("mbp", 4)
	wantRunnerCode(t, err, appwire.CodeForbidden)
	if stub.count("PUT ") != 1 {
		t.Fatalf("puts = %d", stub.count("PUT "))
	}
}

func TestRunnerSlotsNeedsAHeldCredentialAndSendsNothingWithout(t *testing.T) {
	t.Setenv("CODEAF_FACTORY_TOKEN", "tok_ambient_host_key")
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, "")
	_, err := r.slots("mbp", 3)
	wantRunnerCode(t, err, appwire.CodeSignedOut)
	if got := stub.requests(); len(got) != 0 {
		t.Fatalf("a request left with no credential: %v", got)
	}
}

// ── mixed versions and live capacity ───────────────────────────────────────

func TestRunnerSlotsAnOlderFactoryWithoutPolicyIsUnsupportedBeforeAnyPut(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	stub.dir.SlotsPolicy = nil
	r := newRunnerWire(t, stub, runnerToken)
	_, err := r.slots("mbp", 3)
	wantRunnerCode(t, err, appwire.CodeConflict)
	if stub.count("PUT ") != 0 {
		t.Fatalf("a PUT was sent to a factory with no policy: %v", stub.requests())
	}
}

func TestRunnerSlotsAnExistingEightSlotFactoryStillAcceptsSupportedChanges(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	stub.dir.SlotsPolicy = &api.RunnerSlotsPolicy{Min: 1, Max: 8, RequiresLive: true}
	r := newRunnerWire(t, stub, runnerToken)
	if _, err := r.slots("mbp", 8); err != nil {
		t.Fatalf("compatible older factory refused: %v", err)
	}
	if stub.count("PUT ") != 1 {
		t.Fatal("the supported change was not sent once")
	}
	_, err := r.slots("mbp", 15)
	wantRunnerCode(t, err, appwire.CodeConflict)
	if stub.count("PUT ") != 1 {
		t.Fatal("an above-server-limit change was sent")
	}
}

func TestRunnerSlotsASpoofedOrMismatchedPolicyIsRefusedBeforeAnyPut(t *testing.T) {
	bad := map[string]*api.RunnerSlotsPolicy{
		"wider":         {Min: 1, Max: api.MaxRunnerSlots + 4, RequiresLive: true},
		"zero ceiling":  {Min: 1, Max: 0, RequiresLive: true},
		"zero floor":    {Min: 0, Max: api.MaxRunnerSlots, RequiresLive: true},
		"raised floor":  {Min: 2, Max: api.MaxRunnerSlots, RequiresLive: true},
		"no live guard": {Min: 1, Max: api.MaxRunnerSlots, RequiresLive: false},
		"empty":         {},
	}
	for name, policy := range bad {
		t.Run(name, func(t *testing.T) {
			stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
			stub.dir.SlotsPolicy = policy
			r := newRunnerWire(t, stub, runnerToken)
			_, err := r.slots("mbp", 3)
			wantRunnerCode(t, err, appwire.CodeConflict)
			if stub.count("PUT ") != 0 {
				t.Fatalf("a PUT left against policy %+v", policy)
			}
		})
	}
}

func TestRunnerSlotsAWorkerThatCannotFollowIsRefusedBeforeAnyPut(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, false))
	r := newRunnerWire(t, stub, runnerToken)
	err := func() error { _, err := r.slots("mbp", 3); return err }()
	w := wantRunnerCode(t, err, appwire.CodeConflict)
	if !strings.Contains(w.Message, "update") || strings.Contains(w.Message, "mbp") {
		t.Fatalf("words %q", w.Message)
	}
	if stub.count("PUT ") != 0 {
		t.Fatal("a PUT left for a worker that is not live")
	}
}

func TestRunnerSlotsDirectoryFailuresSendNoWriteAndLeakNothing(t *testing.T) {
	for name, set := range map[string]func(*runnerStub){
		"403":        func(s *runnerStub) { s.dirStatus = http.StatusForbidden },
		"404":        func(s *runnerStub) { s.dirStatus = http.StatusNotFound },
		"500":        func(s *runnerStub) { s.dirStatus = http.StatusInternalServerError },
		"401":        func(s *runnerStub) { s.dirStatus = http.StatusUnauthorized },
		"malformed":  func(s *runnerStub) { s.dirBody = `{"runners": "nope"` },
		"wrong type": func(s *runnerStub) { s.dirBody = `{"runners": 5}` },
		"duplicate own id": func(s *runnerStub) {
			s.dir.Runners = append(s.dir.Runners, liveRunner("mbp", "alice", 3, true))
		},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
			set(stub)
			r := newRunnerWire(t, stub, runnerToken)
			_, err := r.slots("mbp", 3)
			if err == nil {
				t.Fatal("no error")
			}
			r.leakless(t, err)
			if stub.count("PUT ") != 0 {
				t.Fatal("a PUT left after a failed directory read")
			}
		})
	}
}

func TestRunnerSlotsUnreachableFactoryIsUnreachableAndNothingWasSent(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	stub.srv.Close()
	_, err := r.slots("mbp", 3)
	if got := wireCodeOf(t, err); got != appwire.CodeUnreachable {
		t.Fatalf("code %q (%v)", got, err)
	}
}

// ── the write's outcomes ───────────────────────────────────────────────────

func TestRunnerSlotsDefiniteRefusalsNeedTheMarkerAndUseFixedWords(t *testing.T) {
	for status, code := range map[int]string{
		http.StatusForbidden: appwire.CodeForbidden, http.StatusNotFound: appwire.CodeNotFound, http.StatusConflict: appwire.CodeConflict,
		http.StatusUnprocessableEntity: appwire.CodeBadParams, http.StatusUnauthorized: appwire.CodeSignedOut,
		http.StatusNotImplemented: appwire.CodeConflict,
	} {
		stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
		stub.put = runnerMarked(status)
		r := newRunnerWire(t, stub, runnerToken)
		_, err := r.slots("mbp", 3)
		wantRunnerCode(t, err, code)
		r.leakless(t, err)
		if stub.count("PUT ") != 1 {
			t.Fatalf("%d: puts = %d", status, stub.count("PUT "))
		}
	}
}

// A 4XX WITH NO MARKER MAY FOLLOW A STORED WRITE: the factory saves the number
// and then records the event, and either can fail with a refusal-looking status.
func TestRunnerSlotsUnmarkedOrUnexpectedAnswersAreUncertainAndSentOnce(t *testing.T) {
	for name, put := range map[string]func(http.ResponseWriter, *http.Request){
		"403 bare": runnerBare(403), "404 bare": runnerBare(404), "409 bare": runnerBare(409), "422 bare": runnerBare(422),
		"500 bare": runnerBare(500), "502 bare": runnerBare(502), "500 marked": runnerMarked(500), "200 marked": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
			w.WriteHeader(200)
		},
		"202": runnerBare(202), "201": runnerBare(201), "206": runnerBare(206),
		"redirect": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) },
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
			stub.put = put
			r := newRunnerWire(t, stub, runnerToken)
			_, err := r.slots("mbp", 3)
			if name == "200 marked" {
				// A completed answer is a completed answer, marker or not.
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantRunnerCode(t, err, appwire.CodeUncertain)
			r.leakless(t, err)
			if stub.count("PUT ") != 1 {
				t.Fatalf("puts = %d: a write must never be repeated", stub.count("PUT "))
			}
		})
	}
}

func TestRunnerSlotsStoredThenEventFailedIsUncertainAndNeverRetried(t *testing.T) {
	// What the real handler does when the event cannot be recorded after the
	// number was stored: the marker is gone, the status is a 5xx or a 4xx.
	for _, status := range []int{500, 403, 409} {
		stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
		stub.put = runnerBare(status)
		r := newRunnerWire(t, stub, runnerToken)
		_, err := r.slots("mbp", 6)
		wantRunnerCode(t, err, appwire.CodeUncertain)
		if stub.count("PUT ") != 1 {
			t.Fatalf("%d: puts = %d", status, stub.count("PUT "))
		}
	}
}

func TestRunnerSlotsAWriteThatMayHaveArrivedAndGotNoAnswerIsUncertain(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	stub.put = func(w http.ResponseWriter, _ *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}
	r := newRunnerWire(t, stub, runnerToken)
	_, err := r.slots("mbp", 3)
	wantRunnerCode(t, err, appwire.CodeUncertain)
	if stub.count("PUT ") != 1 {
		t.Fatalf("puts = %d", stub.count("PUT "))
	}
}

// ── identity changes ───────────────────────────────────────────────────────

func TestRunnerSlotsCallerSwappedBeforeTheWriteSendsNoWrite(t *testing.T) {
	for name, hook := range map[string]func(*runnerStub, *runnerWire){
		"during me": func(s *runnerStub, r *runnerWire) {
			s.onMe = func() { r.hub.hold(r.id, "tok_other_caller_credential", 0) }
		},
		"during directory": func(s *runnerStub, r *runnerWire) {
			s.onDir = func() { r.hub.hold(r.id, "tok_other_caller_credential", 0) }
		},
		"signed out during directory": func(s *runnerStub, r *runnerWire) {
			s.onDir = func() { r.hub.dropSession(r.id, runnerToken) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
			r := newRunnerWire(t, stub, runnerToken)
			hook(stub, r)
			_, err := r.slots("mbp", 3)
			wantRunnerCode(t, err, appwire.CodeConflict)
			r.leakless(t, err)
			if stub.count("PUT ") != 0 {
				t.Fatalf("a write left after the caller changed: %v", stub.requests())
			}
		})
	}
}

func TestRunnerSlotsCallerSwappedDuringTheWriteGetsNoScopedAnswer(t *testing.T) {
	for name, put := range map[string]func(http.ResponseWriter, *http.Request){
		"completed": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) },
		"refused":   runnerMarked(409),
		"failed":    runnerBare(500),
	} {
		t.Run(name, func(t *testing.T) {
			stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
			stub.put = put
			r := newRunnerWire(t, stub, runnerToken)
			stub.onPut = func() { r.hub.hold(r.id, "tok_other_caller_credential", 0) }
			got, err := r.slots("mbp", 3)
			w := wantRunnerCode(t, err, appwire.CodeUncertain)
			if got != (appwire.FactoryRunnerSlotsDone{}) {
				t.Fatalf("a receipt reached a replaced caller: %+v", got)
			}
			for _, bad := range []string{"mbp", "3", r.id} {
				if strings.Contains(w.Message, bad) {
					t.Fatalf("words %q name %q", w.Message, bad)
				}
			}
		})
	}
}

func TestRunnerSlotsASecondPressWhileOneIsInFlightIsBusyAndSendsNothing(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true))
	r := newRunnerWire(t, stub, runnerToken)
	entered, release := make(chan struct{}), make(chan struct{})
	stub.put = func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(200)
	}
	done := make(chan error, 1)
	go func() { _, err := r.slots("mbp", 3); done <- err }()
	<-entered
	_, err := r.slots("mbp", 4)
	wantRunnerCode(t, err, appwire.CodeBusy)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stub.count("PUT ") != 1 {
		t.Fatalf("puts = %d", stub.count("PUT "))
	}
}

// ── the surface ────────────────────────────────────────────────────────────

func TestRunnerSlotsIsAnnouncedAsItsOwnFeature(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	caps, err := b.FactoryCapabilities(context.Background())
	if err != nil || !caps.RunnerManage {
		t.Fatalf("capabilities %+v %v", caps, err)
	}
	var _ appwire.FactoryRunnerAdmin = b
}

// The directory passes the server's policy through untouched, so a typed UI can
// read the range and never hard-code it.
func TestRunnerDirectoryPassesThePolicyAndLiveFlagThrough(t *testing.T) {
	stub := newRunnerStub(t, alice(), liveRunner("mbp", "alice", 2, true), liveRunner("old", "alice", 2, false))
	r := newRunnerWire(t, stub, runnerToken)
	var dir api.Runners
	r.must("factory.runners", map[string]any{"factory": r.id}, &dir)
	if dir.SlotsPolicy == nil || *dir.SlotsPolicy != *api.CurrentRunnerSlotsPolicy() || !dir.Runners[0].LiveCapacity || dir.Runners[1].LiveCapacity {
		t.Fatalf("directory %+v", dir)
	}
}

// ── against the real factory handler ───────────────────────────────────────
//
// The same engine code, a real team factory (httptest, a fake GitHub, no model
// and no worker process; a worker is only a row the attach route stores).

func realAttach(t *testing.T, f *adminFactory, token, machine string, live bool) {
	t.Helper()
	raw, _ := json.Marshal(api.RunnerAttach{Runner: machine, Name: machine, Owner: "alice", Kind: api.RunnerMac, LiveCapacity: live,
		Checkouts: []api.Checkout{{Repo: "octo/hello", Path: "/home/x/hello"}}})
	req, _ := http.NewRequest(http.MethodPost, f.URL+"/v1/runners/attach", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("attach = %d %s", resp.StatusCode, body)
	}
}

func TestRunnerSlotsAgainstTheRealFactory(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	realAttach(t, f, alice.held(), "box", true)
	realAttach(t, f, alice.held(), "old-box", false)
	f.since()

	var done appwire.FactoryRunnerSlotsDone
	alice.w.must("factory.runner_slots", map[string]any{"factory": alice.conn.ID, "runner": "box", "slots": 5}, &done)
	if done != (appwire.FactoryRunnerSlotsDone{Factory: alice.conn.ID, Runner: "box", Slots: 5}) {
		t.Fatalf("receipt %+v", done)
	}
	if got := f.since(); strings.Join(got, "|") != "GET /v1/me|GET /v1/runners|PUT /v1/runners/box" {
		t.Fatalf("requests %v", got)
	}
	var dir api.Runners
	alice.w.must("factory.runners", map[string]any{"factory": alice.conn.ID}, &dir)
	for _, r := range dir.Runners {
		if r.ID == "box" && (r.Slots != 5 || len(r.Checkouts) != 1) {
			t.Fatalf("after the write: %+v", r)
		}
	}
	if dir.SlotsPolicy == nil || *dir.SlotsPolicy != *api.CurrentRunnerSlotsPolicy() {
		t.Fatalf("policy %+v", dir.SlotsPolicy)
	}

	// A worker that cannot follow is refused here, with no PUT.
	f.since()
	alice.w.mustFail("factory.runner_slots", map[string]any{"factory": alice.conn.ID, "runner": "old-box", "slots": 4}, appwire.CodeConflict)
	for _, r := range f.since() {
		if strings.HasPrefix(r, "PUT ") {
			t.Fatalf("a PUT left for a worker that is not live: %s", r)
		}
	}
}

// THE GUARD IS THE FACTORY'S: when the worker went back to an older binary after
// this engine read the directory, the real handler refuses with the marker and
// the engine reports the definite, unchanged outcome.
func TestRunnerSlotsAgainstTheRealFactoryRefusesAWorkerThatChangedUnderneath(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	realAttach(t, f, alice.held(), "box", true)
	f.clock.advance(time.Minute)
	f.gate = func(path string) {
		if path == "/v1/runners/box" {
			// The directory was read as live; the worker restarts onto an
			// older binary just before the write reaches the handler.
			f.gate = nil
			f.clock.advance(time.Minute)
			realAttach(t, f, alice.held(), "box", false)
		}
	}
	msg := alice.w.mustFail("factory.runner_slots", map[string]any{"factory": alice.conn.ID, "runner": "box", "slots": 4}, appwire.CodeConflict)
	if strings.Contains(msg, "box") {
		t.Fatalf("message %q", msg)
	}
	// It was the factory's own check that refused: the write did leave once.
	puts := 0
	for _, r := range f.since() {
		if r == "PUT /v1/runners/box" {
			puts++
		}
	}
	if puts != 1 {
		t.Fatalf("puts = %d", puts)
	}
	var dir api.Runners
	alice.w.must("factory.runners", map[string]any{"factory": alice.conn.ID}, &dir)
	if dir.Runners[0].Slots != api.DefaultRunnerSlots || dir.Runners[0].LiveCapacity {
		t.Fatalf("the refused write changed or mislabelled the runner: %+v", dir.Runners[0])
	}
}
