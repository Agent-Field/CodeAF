package agentfield

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// THE VALIDATION CONTRACT of the run-owner proof (org-land lane, written before
// the code and checkable without reading it):
//
//	N1  Over a control plane that mints, the run's start, its transcript read, its
//	    transcript ack, a steer and an approval answer carry the proof beside the
//	    owner, and no other call carries it.
//	N2  Over a control plane that never answers the delegate frame (one that
//	    predates the proof), every call goes out exactly as before, no call waits
//	    more than the bound, and the node asks once on that link and no more.
//	N3  Over one that answers no (501, or ok with no token) the same holds.
//	N4  A proof is minted once and reused, replaced shortly before it lapses, and
//	    asked for again on a new link; one mint however many callers ask at once.
//	N5  A 403 on a call that carried a proof makes the proof forgotten and the call
//	    goes once more with a fresh one; a 403 on a call that carried none, or one
//	    that a fresh proof does not change, is the answer and is not repeated.
//	N6  The token is never in a log line or an error.
//	N7  A node with no link sends no proof and waits for nothing.
//	N8  The proof is sent only for this node's own id.
//	N9  A refusal of the proof is told in plain words and is never a bad API key;
//	    a 403 that is not one of them still is.

// proofPlane records, per call, the owner and proof headers the control plane saw.
type proofPlane struct {
	mu    sync.Mutex
	calls []proofCall
	// refuse is the proof the plane answers 403, "" for none.
	refuse string
}

type proofCall struct{ route, owner, proof string }

func (p *proofPlane) routes(mux *http.ServeMux) {
	record := func(route string, status func(proofCall) int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			c := proofCall{route: route, owner: r.Header.Get("X-AgentField-Run-Owner"), proof: r.Header.Get(RunOwnerProofHeader)}
			p.mu.Lock()
			p.calls = append(p.calls, c)
			p.mu.Unlock()
			code := http.StatusOK
			if status != nil {
				code = status(c)
			}
			if code != http.StatusOK {
				body = `{"error":"run_owner_proof_invalid: invalid proof of owner consent"}`
			}
			writeJSON(w, code, json.RawMessage(body))
		}
	}
	gate := func(c proofCall) int {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.refuse != "" && c.proof == p.refuse {
			return http.StatusForbidden
		}
		return http.StatusOK
	}
	mux.HandleFunc("POST /api/v1/execute/async/{target}", record("dispatch", gate, `{"execution_id":"e1","run_id":"r1","status":"running"}`))
	mux.HandleFunc("GET /api/v1/executions/{id}/relay", record("relay", gate, `{"entries":[],"next":0}`))
	mux.HandleFunc("POST /api/v1/executions/{id}/relay/ack", record("relay ack", gate, `{}`))
	mux.HandleFunc("POST /api/v1/executions/{id}/steer", record("steer", gate, `{"ok":true}`))
	mux.HandleFunc("POST /api/v1/executions/{id}/approval-response", record("approval", gate, `{"status":"approved"}`))
	mux.HandleFunc("GET /api/ui/v1/workflows/{run}/dag", record("tree", nil, `{"workflow_status":"running","dag":{"execution_id":"e1"}}`))
	mux.HandleFunc("POST /api/ui/v1/workflows/{run}/cancel-tree", record("cancel", nil, `{}`))
	mux.HandleFunc("GET /api/v1/executions/{id}/approval-status", record("approval status", nil, `{"status":"waiting"}`))
	mux.HandleFunc("GET /api/v1/executions/{id}/notes", record("notes", nil, `{"notes":[]}`))
	mux.HandleFunc("GET /api/v1/executions/{id}", record("result", nil, `{"result":{}}`))
}

func (p *proofPlane) seen(route string) []proofCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []proofCall
	for _, c := range p.calls {
		if c.route == route {
			out = append(out, c)
		}
	}
	return out
}

// mintingHub answers every delegate frame with a new proof "afo1.proof-N" valid
// for an hour, and counts the frames.
func mintingHub(h *fakeHub, minted *atomic.Int64) {
	h.delegate = func() (okBody, bool) {
		n := minted.Add(1)
		return okBody{Code: http.StatusOK, Token: fmt.Sprintf("afo1.proof-%d", n), ExpiresIn: 3600, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Scopes: []string{"act", "own", "read"}}, true
	}
}

// shortWait makes the bound on a silent control plane short for one test.
func shortWait(t *testing.T) {
	t.Helper()
	was := delegateWait
	delegateWait = 150 * time.Millisecond
	t.Cleanup(func() { delegateWait = was })
}

// proofWorld is a linked node over a plane that serves the run calls.
func proofWorld(t *testing.T, hub func(*fakeHub), tune ...func(*Node)) (*linkWorld, *proofPlane) {
	t.Helper()
	p := &proofPlane{}
	return newLinkWorldWith(t, hub, p.routes, tune...), p
}

// delegates is how many delegate frames the hub has heard.
func delegates(lw *linkWorld) int { return len(lw.hub.upFrames(tDelegate)) }

// runCalls makes the five calls that carry the proof and the five that do not.
func runCalls(t *testing.T, c *Client, owner string) {
	t.Helper()
	ctx := context.Background()
	if _, err := c.Dispatch(ctx, "support.triage", map[string]any{"title": "t"}, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Relay(ctx, "e1", 0); err != nil {
		t.Fatal(err)
	}
	if err := c.RelayAck(ctx, "e1", 3); err != nil {
		t.Fatal(err)
	}
	if err := c.Steer(ctx, "e1", "look at the edge"); err != nil {
		t.Fatal(err)
	}
	if err := c.ResolveApproval(ctx, "e1", DecisionApproved, "ok"); err != nil {
		t.Fatal(err)
	}
	_, _, _ = c.Tree(ctx, "r1")
	_ = c.CancelTree(ctx, "r1")
	_, _ = c.ApprovalStatus(ctx, "e1")
	_, _ = c.Result(ctx, "e1")
	_, _ = c.Notes(ctx, "e1")
}

var proofCarriers = []string{"dispatch", "relay", "relay ack", "steer", "approval"}
var proofBystanders = []string{"tree", "cancel", "approval status", "result", "notes"}

func TestContractN1ProofRidesTheFiveNamedCallsOnly(t *testing.T) {
	var minted atomic.Int64
	lw, plane := proofWorld(t, func(h *fakeHub) { mintingHub(h, &minted) })
	eventually(t, "the proof asked for on the link", func() bool { return delegates(lw) == 1 })
	c, s, ok := lw.node.Client()
	if !ok {
		t.Fatal("no client")
	}
	runCalls(t, c, s.NodeID)
	for _, route := range proofCarriers {
		calls := plane.seen(route)
		if len(calls) != 1 || calls[0].proof != "afo1.proof-1" || calls[0].owner != s.NodeID {
			t.Errorf("%s carried %+v, want owner %q and proof afo1.proof-1", route, calls, s.NodeID)
		}
	}
	for _, route := range proofBystanders {
		calls := plane.seen(route)
		if len(calls) != 1 || calls[0].proof != "" || calls[0].owner != s.NodeID {
			t.Errorf("%s carried %+v, want the owner and no proof", route, calls)
		}
	}
	var body delegateBody
	if err := json.Unmarshal(lw.hub.upFrames(tDelegate)[0].Body, &body); err != nil || strings.Join(body.Scopes, ",") != "own,read,act" {
		t.Errorf("the delegate frame asked for %+v (%v), want own, read and act", body, err)
	}
	if minted.Load() != 1 {
		t.Errorf("%d proofs minted for ten calls, want one", minted.Load())
	}
}

func TestContractN2ASilentControlPlaneIsServedAsBefore(t *testing.T) {
	shortWait(t)
	lw, plane := proofWorld(t, func(h *fakeHub) { h.delegate = func() (okBody, bool) { return okBody{}, false } })
	eventually(t, "the proof asked for once", func() bool { return delegates(lw) == 1 })
	c, s, _ := lw.node.Client()
	began := time.Now()
	runCalls(t, c, s.NodeID)
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("ten calls took %v against a control plane that never answers the proof", took)
	}
	for _, route := range append(append([]string{}, proofCarriers...), proofBystanders...) {
		calls := plane.seen(route)
		if len(calls) != 1 || calls[0].proof != "" || calls[0].owner != s.NodeID {
			t.Errorf("%s carried %+v, want the owner and no proof", route, calls)
		}
	}
	if n := delegates(lw); n != 1 {
		t.Errorf("the node asked %d times on a link that never answered, want once", n)
	}
	if lw.node.Health().Link != "up" {
		t.Error("a silent answer to the proof took the link down")
	}
}

func TestContractN3AControlPlaneThatSaysNoIsServedAsBefore(t *testing.T) {
	for name, answer := range map[string]okBody{
		"not implemented":  {Code: http.StatusNotImplemented, Error: "this control plane does not take delegate frames"},
		"ok with no token": {Code: http.StatusOK},
		"refused":          {Code: http.StatusBadRequest, Error: "delegate: invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			lw, plane := proofWorld(t, func(h *fakeHub) { h.delegate = func() (okBody, bool) { return answer, true } })
			eventually(t, "the proof asked for", func() bool { return delegates(lw) == 1 })
			c, s, _ := lw.node.Client()
			runCalls(t, c, s.NodeID)
			for _, route := range proofCarriers {
				if calls := plane.seen(route); len(calls) != 1 || calls[0].proof != "" || calls[0].owner != s.NodeID {
					t.Errorf("%s carried %+v, want the owner and no proof", route, calls)
				}
			}
			if n := delegates(lw); n != 1 {
				t.Errorf("asked %d times on a link that said no, want once", n)
			}
		})
	}
}

func TestContractN4AProofIsMintedOnceReusedRenewedAndAskedAgainOnANewLink(t *testing.T) {
	var minted atomic.Int64
	var clock atomic.Int64
	base := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	lw, plane := proofWorld(t, func(h *fakeHub) { mintingHub(h, &minted) }, func(n *Node) {
		n.Clock = func() time.Time { return base.Add(time.Duration(clock.Load())) }
	})
	eventually(t, "the first proof", func() bool { return minted.Load() == 1 })
	c, s, _ := lw.node.Client()
	for i := 0; i < 3; i++ {
		if _, err := c.Relay(context.Background(), "e1", 0); err != nil {
			t.Fatal(err)
		}
	}
	if minted.Load() != 1 {
		t.Fatalf("%d minted for three calls, want one", minted.Load())
	}
	// Past the point five minutes before an hour, the proof is replaced.
	clock.Store(int64(56 * time.Minute))
	if _, err := c.Relay(context.Background(), "e1", 0); err != nil {
		t.Fatal(err)
	}
	if minted.Load() != 2 {
		t.Fatalf("%d minted after the first neared its end, want two", minted.Load())
	}
	if got := plane.seen("relay"); got[len(got)-1].proof != "afo1.proof-2" {
		t.Fatalf("the replaced proof was not the one sent: %+v", got)
	}
	// A new link is a new control plane's word: it is asked again, once.
	lw.hub.drop()
	eventually(t, "the link back and a proof asked on it", func() bool { return minted.Load() == 3 && lw.node.Health().Link == "up" })
	c, _, _ = lw.node.Client()
	if _, err := c.Relay(context.Background(), "e1", 0); err != nil {
		t.Fatal(err)
	}
	if got := plane.seen("relay"); got[len(got)-1].proof != "afo1.proof-3" || got[len(got)-1].owner != s.NodeID {
		t.Fatalf("after the link came back: %+v", got)
	}
	if minted.Load() != 3 {
		t.Fatalf("%d minted, want three (one per link, one renewal)", minted.Load())
	}
}

func TestContractN4OneMintHoweverManyAskAtOnce(t *testing.T) {
	var minted atomic.Int64
	lw, _ := proofWorld(t, func(h *fakeHub) {
		h.delegate = func() (okBody, bool) {
			time.Sleep(60 * time.Millisecond) // a slow answer, so the callers pile up behind it
			n := minted.Add(1)
			return okBody{Code: http.StatusOK, Token: fmt.Sprintf("afo1.proof-%d", n), ExpiresIn: 3600}, true
		}
	})
	_, s, _ := lw.node.Client()
	var wg sync.WaitGroup
	got := make([]string, 20)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = lw.node.RunOwnerProof(context.Background(), s.NodeID)
		}()
	}
	wg.Wait()
	for i, tok := range got {
		if tok != "afo1.proof-1" {
			t.Errorf("caller %d got %q", i, tok)
		}
	}
	if minted.Load() != 1 || delegates(lw) != 1 {
		t.Errorf("%d minted over %d frames for twenty callers, want one", minted.Load(), delegates(lw))
	}
}

func TestContractN5ARefusedProofIsReplacedOnceAndAnythingElseIsTheAnswer(t *testing.T) {
	var minted atomic.Int64
	lw, plane := proofWorld(t, func(h *fakeHub) { mintingHub(h, &minted) })
	eventually(t, "the first proof", func() bool { return minted.Load() == 1 })
	c, s, _ := lw.node.Client()

	// The control plane refuses proof 1 (its key moved) and takes proof 2.
	plane.mu.Lock()
	plane.refuse = "afo1.proof-1"
	plane.mu.Unlock()
	if _, err := c.Dispatch(context.Background(), "support.triage", nil, s.NodeID); err != nil {
		t.Fatalf("a refused proof was not replaced: %v", err)
	}
	calls := plane.seen("dispatch")
	if len(calls) != 2 || calls[0].proof != "afo1.proof-1" || calls[1].proof != "afo1.proof-2" {
		t.Fatalf("the run's start went %+v, want proof 1 refused and then proof 2", calls)
	}

	// A control plane that refuses every proof it is given is heard once, not forever.
	plane.mu.Lock()
	plane.refuse = "afo1.proof-2"
	plane.calls = nil
	plane.mu.Unlock()
	lw.hub.delegate = func() (okBody, bool) {
		return okBody{Code: http.StatusOK, Token: "afo1.proof-2", ExpiresIn: 3600}, true // the same token again
	}
	_, err := c.Dispatch(context.Background(), "support.triage", nil, s.NodeID)
	if !Refused(err) {
		t.Fatalf("a proof that stays refused answered %v, want the control plane's refusal", err)
	}
	if n := len(plane.seen("dispatch")); n != 1 {
		t.Fatalf("a call refused with the same proof again went %d times, want once", n)
	}
}

func TestContractN5ARefusalOfACallWithNoProofIsNotRepeated(t *testing.T) {
	shortWait(t)
	var calls atomic.Int64
	lw := newLinkWorldWith(t, func(h *fakeHub) { h.delegate = func() (okBody, bool) { return okBody{}, false } }, func(mux *http.ServeMux) {
		mux.HandleFunc("POST /api/v1/execute/async/{target}", func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "not yours"})
		})
	})
	eventually(t, "the silence", func() bool { return delegates(lw) == 1 })
	c, s, _ := lw.node.Client()
	if _, err := c.Dispatch(context.Background(), "support.triage", nil, s.NodeID); !Refused(err) {
		t.Fatalf("answered %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("a refused call with no proof went %d times, want once", calls.Load())
	}
}

func TestContractN6TheTokenIsNeverInALogLineOrAnError(t *testing.T) {
	const token = "afo1.n6-bearer-secret-token"
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	lw, plane := proofWorld(t, func(h *fakeHub) {
		h.delegate = func() (okBody, bool) { return okBody{Code: http.StatusOK, Token: token, ExpiresIn: 3600}, true }
	})
	plane.refuse = token
	eventually(t, "the proof", func() bool { return delegates(lw) == 1 })
	c, s, _ := lw.node.Client()
	_, err := c.Dispatch(context.Background(), "support.triage", nil, s.NodeID)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the refusal carries the token: %v", err)
	}
	if strings.Contains(logged.String(), token) {
		t.Errorf("a log line carries the token: %s", logged.String())
	}
	if got := redact.Secrets("leak " + token); strings.Contains(got, token) {
		t.Errorf("the redactor lets the token through: %q", got)
	}
}

func TestContractN7ANodeWithNoLinkSendsNoProofAndWaitsForNothing(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get(RunOwnerProofHeader))
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"execution_id": "e1", "run_id": "r1", "status": "running"})
	}))
	defer server.Close()
	// The HTTP-only node of the other tests: connected, never linked.
	w := newWorld(t, factory.TierOwner)
	c, s, ok := w.node.Client()
	if !ok {
		t.Fatal("no client")
	}
	c.URL = server.URL
	began := time.Now()
	if _, err := c.Dispatch(context.Background(), "support.triage", nil, s.NodeID); err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > time.Second {
		t.Errorf("a call on a node with no link took %v", time.Since(began))
	}
	heard := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
	if got := heard(); len(got) != 1 || got[0] != "" {
		t.Errorf("a node with no link sent proof %q", got)
	}
	// And a client with no proof source at all is the client it always was.
	plain := &Client{URL: server.URL, Owner: s.NodeID}
	if _, err := plain.Dispatch(context.Background(), "support.triage", nil, s.NodeID); err != nil {
		t.Fatal(err)
	}
	if got := heard(); len(got) != 2 || got[1] != "" {
		t.Errorf("a client with no source sent proof %q", got)
	}
}

func TestContractN8TheProofIsOnlyForThisNodesOwnId(t *testing.T) {
	var minted atomic.Int64
	lw, plane := proofWorld(t, func(h *fakeHub) { mintingHub(h, &minted) })
	eventually(t, "the proof", func() bool { return minted.Load() == 1 })
	c, _, _ := lw.node.Client()
	if _, err := c.Dispatch(context.Background(), "support.triage", nil, "some-other-node"); err != nil {
		t.Fatal(err)
	}
	calls := plane.seen("dispatch")
	if len(calls) != 1 || calls[0].owner != "some-other-node" || calls[0].proof != "" {
		t.Fatalf("a call naming another owner carried %+v, want that owner and no proof", calls)
	}
}

func TestContractN9AProofRefusalIsNeverToldAsABadKey(t *testing.T) {
	for _, tc := range []struct {
		name, answer, want, reason string
	}{
		{"required", "run_owner_proof_required: supply a proof of owner consent", "link is not up", proofRequired},
		{"invalid", "run_owner_proof_invalid: invalid proof of owner consent", "did not accept this factory's proof", proofInvalid},
		{"expired", "run_owner_proof_expired: ask your node link for a new proof", "had run out", proofExpired},
		{"a signing key it no longer knows", "run_owner_proof_key_unknown: ask your node link for a new proof", "had run out", proofExpired},
		{"a refusal a newer control plane adds", "run_owner_proof_something_new: words", "did not accept this factory's proof", proofInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": tc.answer})
			}))
			defer server.Close()
			c := &Client{URL: server.URL, Key: "the-key", Owner: "codeaf-factory"}
			_, err := c.Dispatch(context.Background(), "support.triage", nil, "codeaf-factory")
			var e *Error
			if !errors.As(err, &e) || e.Refusal != RefusedProof || e.Proof != tc.reason || e.Code != http.StatusForbidden {
				t.Fatalf("answered %+v (%v), want a proof refusal (%s) on a 403", e, err, tc.reason)
			}
			words := err.Error()
			if !strings.Contains(words, tc.want) {
				t.Fatalf("a person is told %q, want it to say %q", words, tc.want)
			}
			for _, banned := range []string{"API key", "run_owner_proof", "token", "scope", "delegate", "the-key"} {
				if strings.Contains(words, banned) {
					t.Fatalf("a person is told %q, which says %q", words, banned)
				}
			}
			if !Refused(err) {
				t.Fatalf("a refused proof must still count as the control plane's answer: %v", err)
			}
		})
	}

	// A 403 that is not a proof refusal, and a 401, are still a key the control plane does not accept.
	for _, tc := range []struct {
		code   int
		answer map[string]any
	}{
		{http.StatusForbidden, map[string]any{"error": "forbidden"}},
		{http.StatusForbidden, map[string]any{"message": "not allowed"}},
		{http.StatusUnauthorized, map[string]any{"error": "unauthorized", "message": "bad key"}},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tc.code, tc.answer) }))
		c := &Client{URL: server.URL, Key: "the-key", Owner: "codeaf-factory"}
		_, err := c.Dispatch(context.Background(), "support.triage", nil, "codeaf-factory")
		server.Close()
		if !RefusedAs(err, RefusedKey) || !strings.Contains(err.Error(), "did not accept that API key") {
			t.Fatalf("a %d answered %v, want it told as a bad API key", tc.code, err)
		}
	}
}
