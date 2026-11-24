package agentfield

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── THE RUN-OWNER PROOF: THIS NODE'S CONSENT, MINTED OVER ITS OWN LINK ───────
//
// X-AgentField-Run-Owner names the node that is to receive a run's tree, its
// transcript and its questions, and it used to be a free string: any caller of
// the control plane could name this factory. A control plane that has
// agentfield's generic app link (docs/app-link-contract.md) will also check an
// X-AgentField-Run-Owner-Proof, a signed, scoped, expiring token that only the
// node's own live link can mint, by a `delegate` frame. This file asks for that
// token and the Client sends it on the calls the contract names: the root
// execute (own), the transcript read (read), and the transcript ack, the steer
// and the approval answer (act).
//
// THE PROOF IS AN ADDITION AND NEVER A CONDITION. A control plane that predates
// it drops the delegate frame without answering; one without relay storage
// answers 501; either way the link is marked as giving none and every call goes
// out exactly as it did before, with the owner header and no proof. Nothing
// waits on it for longer than delegateWait, once per link, and nothing asks
// again on a link that has said no. A node with no live link (an HTTP-only node,
// or a link that is down) sends none and waits for nothing.
//
// THE TOKEN IS A BEARER SECRET. It is registered with the redactor the moment it
// is minted, so no log line, error or status carries it.

// RunOwnerProofHeader carries the proof beside X-AgentField-Run-Owner.
const RunOwnerProofHeader = "X-AgentField-Run-Owner-Proof"

// The scopes a proof is minted for: the three things a run's owner does.
const (
	proofScopeOwn  = "own"
	proofScopeRead = "read"
	proofScopeAct  = "act"
)

// delegateWait bounds the wait for a control plane's answer to a delegate frame.
// A control plane that predates the proof never answers, so this is the most any
// link ever waits for it, once. A variable so a test need not sit through it.
var delegateWait = 4 * time.Second

// proofRenewBefore is how long before a proof lapses it is replaced; the control
// plane accepts it for sixty seconds past its expiry, so five minutes is ample.
const proofRenewBefore = 5 * time.Minute

// proofRetryAfter keeps a failed renewal from being asked again on every call.
const proofRetryAfter = 30 * time.Second

// ProofSource gives a Client a proof of its node's consent. A nil source sends
// none, which is how a control plane that predates the proof is served.
type ProofSource interface {
	// RunOwnerProof is a live proof for owner, or "" when there is none to
	// send: no live link, a control plane that gives none, or an owner that
	// is not this node.
	RunOwnerProof(ctx context.Context, owner string) string
	// RunOwnerProofRefused says the control plane refused token, so the next
	// call mints a fresh one rather than sending the same again.
	RunOwnerProofRefused(token string)
}

// delegateBody is the delegate frame's body (the control plane's DelegateUp).
// No run restriction is asked for: the proof is for every run this node owns.
type delegateBody struct {
	Scopes []string `json:"scopes"`
}

// linkProof is one link's proof state, guarded by its own mutex so a mint is one
// call however many callers ask at once.
type linkProof struct {
	mu    sync.Mutex
	token string
	until time.Time
	// given is true once a mint has worked on this link, so a later failure is
	// a bad moment and not a control plane that gives none.
	given bool
	// none is true once this link has learned the control plane gives no proof.
	none bool
	// retry is the earliest a failed renewal is asked again.
	retry time.Time
}

// RunOwnerProof implements ProofSource for this node's live link.
func (n *Node) RunOwnerProof(ctx context.Context, owner string) string {
	n.mu.Lock()
	var ls *linkSession
	if r := n.running; r != nil && r.link != nil && owner != "" && r.settings.NodeID == owner {
		ls = r.link
	}
	n.mu.Unlock()
	if ls == nil {
		return ""
	}
	return ls.proofToken(ctx, n.now)
}

// RunOwnerProofRefused implements ProofSource.
func (n *Node) RunOwnerProofRefused(token string) {
	if token == "" {
		return
	}
	if ls := n.liveLink(); ls != nil {
		ls.proof.mu.Lock()
		if ls.proof.token == token {
			ls.proof.token, ls.proof.until = "", time.Time{}
		}
		ls.proof.mu.Unlock()
	}
}

// proofToken is the link's live proof, minted when there is none or it is near
// its end, and "" when the control plane gives none.
func (ls *linkSession) proofToken(ctx context.Context, now func() time.Time) string {
	p := &ls.proof
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.none {
		return ""
	}
	if p.token != "" && now().Before(p.until.Add(-proofRenewBefore)) {
		return p.token
	}
	if !p.retry.IsZero() && now().Before(p.retry) {
		return p.live(now)
	}
	ok, err := ls.callWithin(ctx, delegateWait, frame{T: tDelegate}, delegateBody{Scopes: []string{proofScopeOwn, proofScopeRead, proofScopeAct}})
	switch {
	case errors.Is(err, errLinkDropped) || ctx.Err() != nil:
		return p.live(now)
	case err == nil && ok.Code == http.StatusOK && ok.Token != "":
		redact.Register(ok.Token)
		p.token, p.given, p.retry = ok.Token, true, time.Time{}
		p.until = proofExpiry(ok, now)
		return p.token
	}
	// An answer that is not a proof, or none at all.
	if !p.given {
		p.none = true
		if err != nil {
			log.Printf("agentfield: this control plane gave no run-owner proof (%s); calls go without one, as before", redact.Secrets(err.Error()))
		} else {
			log.Printf("agentfield: this control plane gave no run-owner proof (%d); calls go without one, as before", ok.Code)
		}
		return ""
	}
	p.retry = now().Add(proofRetryAfter)
	return p.live(now)
}

// live is the held proof while it has not lapsed, "" otherwise.
func (p *linkProof) live(now func() time.Time) string {
	if p.token != "" && now().Before(p.until) {
		return p.token
	}
	return ""
}

// proofExpiry is when the answer's proof lapses: expires_in seconds from now
// (a length of time, so a laptop clock that disagrees with the control plane's
// cannot make a live proof look spent), else its expires_at, else the control
// plane's default lifetime of an hour.
func proofExpiry(ok okBody, now func() time.Time) time.Time {
	if ok.ExpiresIn > 0 {
		return now().Add(time.Duration(ok.ExpiresIn) * time.Second)
	}
	if t, err := time.Parse(time.RFC3339, ok.ExpiresAt); err == nil {
		return t
	}
	return now().Add(time.Hour)
}

// How a control plane that checks the proof refuses it, as a 403 whose sentence
// starts with one of these codes. A refusal is NOT a bad API key: the key was
// accepted, and what is missing or stale is this node's proof.
const (
	proofRequired = "required"
	proofInvalid  = "invalid"
	proofExpired  = "expired"
)

// proofRefusalOf is which proof refusal the control plane's sentence is, or ""
// when it is not one. A signing key the control plane no longer knows reads as
// an expired proof (it asks for a new one just the same), and a code this build
// does not know reads as an invalid one, so a newer control plane's refusal is
// still not mistaken for a bad key.
func proofRefusalOf(message string) string {
	rest, ok := strings.CutPrefix(message, "run_owner_proof_")
	if !ok {
		return ""
	}
	code, _, _ := strings.Cut(rest, ":")
	switch strings.TrimSpace(code) {
	case "required":
		return proofRequired
	case "expired", "key_unknown":
		return proofExpired
	}
	return proofInvalid
}

// proofRefusalWords is what a person is told. It names no code and no token,
// and it says what happens next: a refused or lapsed proof is replaced on the
// next try, and one that was never sent needs the link to be up.
func proofRefusalWords(reason string) string {
	switch reason {
	case proofRequired:
		return "AgentField needs this factory to show that the run is its own, which only the factory's link to AgentField can do, and that link is not up. Reconnect AgentField and try again"
	case proofExpired:
		return "AgentField says this factory's proof that the run is its own had run out. The next try asks the link for a new one"
	}
	return "AgentField did not accept this factory's proof that the run is its own. The next try asks the link for a new one"
}

// Client is a control plane client over the kept connection that asks this
// node's link for a run-owner proof, or false when nothing is connected. Every
// client a running factory makes comes from here, so no call that wants a proof
// is made without one.
func (n *Node) Client() (*Client, Settings, bool) {
	c, s, ok := n.Conn.Client()
	if ok && c != nil {
		c.Proofs = n
	}
	return c, s, ok
}
