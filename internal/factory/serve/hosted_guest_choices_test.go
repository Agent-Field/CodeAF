package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/coder/websocket"
)

// These tests are the contract for the two settings a team can turn on in a hosted chat's policy, `chats.approvals_by` and
// `chats.wake_by`. Each one drives the real organization handler with a real store over a real WebSocket, and the only
// stand-in is the chats' host behind it, which records what reaches it.

// guestPayloadCall is one call frame that carries a payload.
func guestPayloadCall(id int, method, payload string) string {
	return fmt.Sprintf("{\"kind\":\"call\",\"id\":%d,\"method\":%q,\"payload\":%s}\n", id, method, payload)
}

// consentAnswer is the payload of a ResolveQuestion that answers a tool approval, with extra fields spliced in.
func consentAnswer(extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return `{"answer":{"kind":"consent","id":7` + extra + `}}`
}

// guestHostGot reads what reached the host and answers the method and payload of the one frame.
func guestHostGot(t *testing.T, host guestHostWire) (string, string) {
	t.Helper()
	frame := guestFrame(t, guestHostLine(t, host))
	return frame.Method, string(frame.Payload)
}

// guestExpectNothingMore fails when anything else reached the host.
func guestExpectNothingMore(t *testing.T, host guestHostWire) {
	t.Helper()
	select {
	case raw, ok := <-host.incoming:
		if ok {
			t.Fatalf("a refused call reached the chat: %q", raw)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// guestApprovalRig is santosh's chat shared with abir at `view` and mia at `edit`, with one guest wire open for each of
// them. The policy edit, when there is one, is saved before the wires open so nothing waits on a heartbeat.
type guestApprovalRig struct {
	rg    *hostedRig
	chat  string
	srv   *httptest.Server
	wires map[string]guestWires
}

type guestWires struct {
	client   *websocket.Conn
	host     guestHostWire
	refusals chan string
}

func newGuestApprovalRig(t *testing.T, edit func(*acl.Policy)) *guestApprovalRig {
	t.Helper()
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	hostedGrant(t, rg, "santosh", chat, "mia", "edit")
	if edit != nil {
		rg.setAccessPolicy(t, edit)
	}
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	a := &guestApprovalRig{rg: rg, chat: chat, srv: srv, wires: map[string]guestWires{}}
	for _, person := range []string{"abir", "mia"} {
		client, host := guestConnect(t, srv, opened, rg.tokens[person], chat)
		refusals := make(chan string, 64)
		go func() {
			for {
				_, raw, err := client.Read(t.Context())
				if err != nil {
					return
				}
				refusals <- string(raw)
			}
		}()
		a.wires[person] = guestWires{client, host, refusals}
	}
	return a
}

// ask sends a call as who and answers "" when it reached the chat, or the sentence the guest was refused with on its own
// wire. Every call is answered one way or the other before the next is sent, so a late frame cannot be taken for the
// next call's.
func (a *guestApprovalRig) ask(t *testing.T, who string, id int, method, payload string) string {
	t.Helper()
	w := a.wires[who]
	line := guestCall(id, method)
	if payload != "" {
		line = guestPayloadCall(id, method, payload)
	}
	guestWrite(t, w.client, line)
	select {
	case raw := <-w.host.incoming:
		if frame := guestFrame(t, raw); frame.Method != method || frame.ID != uint64(id) {
			t.Fatalf("the chat received %q, want call %d to %s", raw, id, method)
		}
		return ""
	case raw := <-w.refusals:
		frame := guestFrame(t, raw)
		if frame.Kind != "result" || frame.ID != uint64(id) || frame.Error == "" {
			t.Fatalf("the refusal of %s was %q", method, raw)
		}
		return frame.Error
	case <-time.After(5 * time.Second):
		t.Fatalf("%s as %s was neither passed nor refused", method, who)
		return ""
	}
}

// Contract A1: under the default policy, and under a policy that says `owner` out loud, a tool approval stays with the
// owner on BOTH of its doors, whatever role the guest holds. A person who holds `answer` (the `edit` role does) cannot
// approve or refuse a tool call by sending the answer to the chat's question; they can still answer every other question.
func TestApprovalsByOwnerKeepsAToolApprovalWithTheOwnerOnEveryDoor(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*acl.Policy)
	}{
		{"absent", nil},
		{"owner", func(p *acl.Policy) { p.Chats.ApprovalsBy = acl.ApprovalsByOwner }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGuestApprovalRig(t, tc.edit)
			id := 0
			next := func() int { id++; return id }
			// The two doors a tool approval has, and the three plain ways to say it on the second.
			for _, call := range []struct{ name, method, payload string }{
				{"the older frame, allow", "ResolveConsent", `{"id":7,"allow":true}`},
				{"the older frame, remember", "ResolveConsentRemember", `{"id":7,"allow":true,"scope":"tool"}`},
				{"the answer door, allow once", "ResolveQuestion", consentAnswer(`"key":"1"`)},
				{"the answer door, allow always", "ResolveQuestion", consentAnswer(`"key":"2"`)},
				{"the answer door, refuse", "ResolveQuestion", consentAnswer(`"key":"3"`)},
				{"the answer door, a key hidden in picked", "ResolveQuestion", consentAnswer(`"key":"3","picked":["1"]`)},
				{"the answer door, the kind in capitals", "ResolveQuestion", `{"answer":{"KIND":" Consent ","ID":7,"KEY":"1"}}`},
				{"the answer door, put away", "ResolveQuestion", consentAnswer(`"key":"3","dismissed":true`)},
			} {
				if got := a.ask(t, "mia", next(), call.method, call.payload); got != guestOwnerWords {
					t.Errorf("%s: an edit guest reads %q, want %q", call.name, got, guestOwnerWords)
				}
			}
			// A guest who cannot answer anything is told so in the words they always read, not the owner-only ones.
			if got := a.ask(t, "abir", next(), "ResolveQuestion", consentAnswer(`"key":"1"`)); got != guestAnswerWords {
				t.Errorf("a view guest answering a tool approval reads %q, want %q", got, guestAnswerWords)
			}
			// Everything else a guest with `answer` could always do still reaches the chat.
			for _, payload := range []string{
				`{"answer":{"kind":"ask","id":3,"key":"1"}}`,
				`{"answer":{"kind":"task","id":4,"key":"2"}}`,
				``,
			} {
				if got := a.ask(t, "mia", next(), "ResolveQuestion", payload); got != "" {
					t.Errorf("an answer to %s was refused with %q", payload, got)
				}
			}
			guestExpectNothingMore(t, a.wires["mia"].host)
		})
	}
}

// Contract A2: under `approvals_by: answer`, a person who holds `answer` can allow one tool call once, or refuse it, by
// either door. Allowing a tool for the rest of the chat, putting the approval away, and every other frame of the chat's
// settings stay with the owner, and a person who cannot answer still cannot.
func TestApprovalsByAnswerLetsAnAnswererAllowOnceOrRefuseAndNothingWider(t *testing.T) {
	a := newGuestApprovalRig(t, func(p *acl.Policy) { p.Chats.ApprovalsBy = acl.ApprovalsByAnswer })
	id := 0
	next := func() int { id++; return id }
	for _, call := range []struct{ name, method, payload string }{
		{"the older frame, allow", "ResolveConsent", `{"id":7,"allow":true}`},
		{"the older frame, refuse", "ResolveConsent", `{"id":7,"allow":false}`},
		{"the answer door, allow once", "ResolveQuestion", consentAnswer(`"key":"1"`)},
		{"the answer door, refuse", "ResolveQuestion", consentAnswer(`"key":"3"`)},
		{"the answer door, allow once picked", "ResolveQuestion", consentAnswer(`"picked":["1"]`)},
	} {
		if got := a.ask(t, "mia", next(), call.method, call.payload); got != "" {
			t.Errorf("%s: an edit guest was refused with %q", call.name, got)
		}
	}
	for _, call := range []struct{ name, method, payload string }{
		{"the older frame, remember", "ResolveConsentRemember", `{"id":7,"allow":true,"scope":"tool"}`},
		{"the older frame, remember a refusal", "ResolveConsentRemember", `{"id":7,"allow":false,"scope":"tool"}`},
		{"the answer door, allow always", "ResolveQuestion", consentAnswer(`"key":"2"`)},
		{"the answer door, a pick hidden behind a plain key", "ResolveQuestion", consentAnswer(`"key":"1","picked":["2"]`)},
		{"the answer door, put away", "ResolveQuestion", consentAnswer(`"key":"3","dismissed":true`)},
		{"the answer door, a revision", "ResolveQuestion", consentAnswer(`"key":"1","revises":true`)},
		{"the answer door, no key", "ResolveQuestion", consentAnswer(``)},
		{"the chat's own approval setting", remote.MethodSetApproval, `{}`},
	} {
		if got := a.ask(t, "mia", next(), call.method, call.payload); got != guestOwnerWords {
			t.Errorf("%s: an edit guest reads %q, want %q", call.name, got, guestOwnerWords)
		}
	}
	// Without the answer verb nothing changes: the guest is told they may not answer.
	for _, call := range []struct{ method, payload string }{
		{"ResolveConsent", `{"id":7,"allow":true}`},
		{"ResolveQuestion", consentAnswer(`"key":"1"`)},
	} {
		if got := a.ask(t, "abir", next(), call.method, call.payload); got != guestAnswerWords {
			t.Errorf("a view guest sending %s reads %q, want %q", call.method, got, guestAnswerWords)
		}
	}
	guestExpectNothingMore(t, a.wires["mia"].host)
}

// Contract A3: the setting reaches a window that is already open, within a heartbeat, in both directions, the way a verb
// taken away does.
func TestApprovalsByReachesAnOpenWindowOnTheNextHeartbeat(t *testing.T) {
	a := newGuestApprovalRig(t, nil)
	allow := func(id int) string {
		return a.ask(t, "mia", id, "ResolveQuestion", consentAnswer(`"key":"1"`))
	}
	if got := allow(1); got != guestOwnerWords {
		t.Fatalf("before the setting: %q", got)
	}
	a.rg.setAccessPolicy(t, func(p *acl.Policy) { p.Chats.ApprovalsBy = acl.ApprovalsByAnswer })
	// Two beats let a check that was already running when the policy was saved finish.
	time.Sleep(2 * guestTestHeartbeat)
	if got := allow(2); got != "" {
		t.Fatalf("after setting answer: %q", got)
	}
	a.rg.setAccessPolicy(t, func(p *acl.Policy) { p.Chats = acl.Chats{} })
	time.Sleep(2 * guestTestHeartbeat)
	if got := allow(3); got != guestOwnerWords {
		t.Fatalf("after taking the setting out again: %q", got)
	}
}

// Contract A4: an approval answered by a guest is attributed to that guest on the chat's spend, because it is what lets
// the next model call happen. The owner's own window is not changed by any of it: it still reaches the chat through the
// unchanged byte-copying proxy.
func TestApprovalsByAnswerNamesTheApproverAsTheAsker(t *testing.T) {
	a := newGuestApprovalRig(t, func(p *acl.Policy) { p.Chats.ApprovalsBy = acl.ApprovalsByAnswer })
	hs := a.rg.h.config.Hosted.state
	if got := hs.askerOf(a.chat); got == "mia" {
		t.Fatalf("mia was the asker before she did anything")
	}
	if got := a.ask(t, "mia", 1, "ResolveConsent", `{"id":7,"allow":true}`); got != "" {
		t.Fatalf("the approval was refused: %q", got)
	}
	if got := hs.askerOf(a.chat); got != "mia" {
		t.Fatalf("the asker after mia's approval is %q", got)
	}
}

// Contract A5 and W4: the two settings are the operator's to set, take only their words and say the allowed ones when
// refused, and the policy that GET returns shows them. An organization that set neither shows neither.
func TestChatsSettingsAreValidatedAndShownByThePolicy(t *testing.T) {
	o := newWSOrg(t)
	var p acl.Policy
	o.do(t, "mia", "GET", policyPath, -1, nil, http.StatusOK, &p)
	if p.Chats.ApprovalsBy != "" || p.Chats.WakeBy != "" {
		t.Fatalf("the default policy already holds %+v", p.Chats)
	}
	raw := o.raw(t, "mia", "GET", policyPath, nil).Body.String()
	if strings.Contains(raw, "approvals_by") || strings.Contains(raw, "wake_by") {
		t.Fatalf("the default policy's JSON names a setting nobody set: %s", raw)
	}

	p.Chats.ApprovalsBy = acl.ApprovalsByAnswer
	o.do(t, "mia", "PUT", policyPath, p.Rev, p, http.StatusForbidden, nil)

	for _, tc := range []struct {
		name  string
		edit  func(*acl.Policy)
		words []string
	}{
		{"approvals_by", func(p *acl.Policy) { p.Chats.ApprovalsBy = "anyone" }, []string{"owner", "answer"}},
		{"wake_by", func(p *acl.Policy) { p.Chats.WakeBy = "anyone" }, []string{"view", "run", "owner"}},
	} {
		bad := p
		bad.Chats = acl.Chats{}
		tc.edit(&bad)
		w := o.raw(t, "owner", "PUT", policyPath, bad)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s set to a word it does not take: %d %s", tc.name, w.Code, w.Body)
		}
		sentence := errorOf(t, w.Body.Bytes())
		for _, word := range tc.words {
			if !strings.Contains(sentence, word) {
				t.Errorf("the refusal of a bad %s, %q, does not name %q", tc.name, sentence, word)
			}
		}
	}

	saved := o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Chats.ApprovalsBy = acl.ApprovalsByAnswer
		p.Chats.WakeBy = acl.WakeByRun
	})
	var read acl.Policy
	o.do(t, "mia", "GET", policyPath, -1, nil, http.StatusOK, &read)
	if saved.Chats.ApprovalsBy != "answer" || read.Chats.ApprovalsBy != "answer" || read.Chats.WakeBy != "run" {
		t.Fatalf("the policy after the write: saved %+v, read %+v", saved.Chats, read.Chats)
	}
	raw = o.raw(t, "mia", "GET", policyPath, nil).Body.String()
	if !strings.Contains(raw, `"approvals_by":"answer"`) || !strings.Contains(raw, `"wake_by":"run"`) {
		t.Fatalf("GET of the policy does not show the settings: %s", raw)
	}
}
