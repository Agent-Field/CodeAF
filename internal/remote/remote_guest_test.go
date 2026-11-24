package remote

import (
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

type consentFromCall struct {
	id     uint64
	allow  bool
	remote bool
}

// guestAnswerAgent extends fakeAgent with the unified question door, the
// remote-aware consent door, and the approval posture door so the wire test can
// observe what the server stamps and what it blocks before reaching the agent.
type guestAnswerAgent struct {
	*fakeAgent

	mu               sync.Mutex
	resolved         []session.Answer
	resolveErr       error
	consentsFrom     []consentFromCall
	consentFromErr   error
	consentRemembers int
	setApprovals     int
}

func (g *guestAnswerAgent) ResolveQuestion(answer session.Answer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resolved = append(g.resolved, answer)
	return g.resolveErr
}

func (g *guestAnswerAgent) ResolveConsentFrom(id uint64, allow bool, remote bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.consentsFrom = append(g.consentsFrom, consentFromCall{id: id, allow: allow, remote: remote})
	return g.consentFromErr
}

func (g *guestAnswerAgent) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	g.mu.Lock()
	g.consentRemembers++
	g.mu.Unlock()
	g.fakeAgent.ResolveConsentRemember(id, allow, scope)
}

func (g *guestAnswerAgent) ApprovalDial() bool { return true }

func (g *guestAnswerAgent) ResolvedApprovalPosture() string { return session.PostureAsk }

func (g *guestAnswerAgent) SetApprovalPosture(string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.setApprovals++
	return nil
}

// TestARemoteDriverCannotAnswerAnIrreversibleAskRememberAPermissionOrRaiseThePosture
// verifies V26 on the remote wire: the server overwrites Answer.Remote from the
// connection's guest stamp in both directions, translates ErrRemoteIrreversible
// and ErrRemoteWidening into the host-named chatlist refusal sentences without
// dropping the connection, refuses MethodSetApproval and MethodConsentRemember
// before reaching the agent, and routes a remote guest's MethodConsent through
// ResolveConsentFrom with remote=true while keeping a local connection on the
// plain ResolveConsent path.
func TestARemoteDriverCannotAnswerAnIrreversibleAskRememberAPermissionOrRaiseThePosture(t *testing.T) {
	base := &fakeAgent{}
	agent := &guestAnswerAgent{fakeAgent: base}
	engine := engineOn(base)
	engine.Agent = agent
	sess := NewSession(engine, true)

	local := dialSession(t, sess)
	localWelcome := local.hello(Hello{
		Version: Version,
		Surface: "macbook",
	})
	if localWelcome.Kind != "welcome" {
		t.Fatalf("local handshake answered %q: %s", localWelcome.Kind, localWelcome.Error)
	}

	guest := dialSession(t, sess)
	guestWelcome := guest.hello(Hello{
		Version: Version,
		Surface: "laptop",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-drive",
			Label:  "laptop",
			Watch:  true,
			Drive:  true,
			Road:   "relay",
		},
	})
	if guestWelcome.Kind != "welcome" {
		t.Fatalf("remote drive handshake answered %q: %s", guestWelcome.Kind, guestWelcome.Error)
	}

	host := HostName()

	// 1. A remote drive guest's MethodQuestionResolve reaches the agent with
	// Answer.Remote == true even when the wire arguments said remote:false.
	guest.ok(1, MethodQuestionResolve, QuestionArgs{
		Answer: session.Answer{Kind: session.QuestionConsent, ID: 10, Key: "1", Remote: false},
	})
	agent.mu.Lock()
	if len(agent.resolved) != 1 || !agent.resolved[0].Remote {
		t.Fatalf("remote guest MethodQuestionResolve delivered %+v, want Remote=true", agent.resolved)
	}
	agent.mu.Unlock()

	// 2. A local connection's MethodQuestionResolve reaches the agent with
	// Answer.Remote == false even when the wire arguments said remote:true.
	local.ok(1, MethodQuestionResolve, QuestionArgs{
		Answer: session.Answer{Kind: session.QuestionConsent, ID: 11, Key: "1", Remote: true},
	})
	agent.mu.Lock()
	if len(agent.resolved) != 2 || agent.resolved[1].Remote {
		t.Fatalf("local MethodQuestionResolve delivered %+v, want Remote=false", agent.resolved)
	}
	agent.mu.Unlock()

	// 3. When the agent returns session.ErrRemoteIrreversible, the remote
	// viewer receives chatlist.LiveNeedsItsOwnOK(host) and the connection stays up.
	agent.mu.Lock()
	agent.resolveErr = session.ErrRemoteIrreversible
	agent.mu.Unlock()
	irrevRes := guest.call(2, MethodQuestionResolve, QuestionArgs{
		Answer: session.Answer{Kind: session.QuestionConsent, ID: 12, Key: "1"},
	})
	if want := chatlist.LiveNeedsItsOwnOK(host); irrevRes.Error != want {
		t.Fatalf("ErrRemoteIrreversible mapped to %q, want %q", irrevRes.Error, want)
	}

	// 4. When the agent returns session.ErrRemoteWidening, the remote viewer
	// receives chatlist.LiveNotAllowedHere(host) and the connection stays up.
	agent.mu.Lock()
	agent.resolveErr = session.ErrRemoteWidening
	agent.mu.Unlock()
	widenRes := guest.call(3, MethodQuestionResolve, QuestionArgs{
		Answer: session.Answer{Kind: session.QuestionConsent, ID: 13, Key: "2"},
	})
	if want := chatlist.LiveNotAllowedHere(host); widenRes.Error != want {
		t.Fatalf("ErrRemoteWidening mapped to %q, want %q", widenRes.Error, want)
	}
	agent.mu.Lock()
	agent.resolveErr = nil
	agent.mu.Unlock()

	// Prove the guest connection stayed up after both refusals.
	guest.ok(4, MethodPing, nil)

	// 5. MethodSetApproval and MethodConsentRemember from a remote drive guest
	// are refused with chatlist.LiveNotAllowedHere(host) and the agent's
	// counters for both stay 0.
	setApprovalRes := guest.call(5, MethodSetApproval, session.PostureAllow)
	if want := chatlist.LiveNotAllowedHere(host); setApprovalRes.Error != want {
		t.Fatalf("remote drive guest MethodSetApproval answered %q, want %q", setApprovalRes.Error, want)
	}
	rememberRes := guest.call(6, MethodConsentRemember, ConsentArgs{
		ID:    14,
		Allow: true,
		Scope: session.ConsentToolSession,
	})
	if want := chatlist.LiveNotAllowedHere(host); rememberRes.Error != want {
		t.Fatalf("remote drive guest MethodConsentRemember answered %q, want %q", rememberRes.Error, want)
	}
	agent.mu.Lock()
	if agent.setApprovals != 0 {
		t.Fatalf("agent SetApprovalPosture counter = %d, want 0", agent.setApprovals)
	}
	if agent.consentRemembers != 0 {
		t.Fatalf("agent ResolveConsentRemember counter = %d, want 0", agent.consentRemembers)
	}
	agent.mu.Unlock()

	// 6. A remote guest's MethodConsent reaches ResolveConsentFrom(.., remote=true)
	// and NOT ResolveConsent, and maps ErrRemoteIrreversible to LiveNeedsItsOwnOK.
	guest.ok(7, MethodConsent, ConsentArgs{ID: 21, Allow: true})
	agent.mu.Lock()
	if len(agent.consentsFrom) != 1 || agent.consentsFrom[0] != (consentFromCall{id: 21, allow: true, remote: true}) {
		t.Fatalf("remote guest MethodConsent recorded %+v, want [{id:21 allow:true remote:true}]", agent.consentsFrom)
	}
	agent.mu.Unlock()
	base.mu.Lock()
	if len(base.consents) != 0 {
		t.Fatalf("remote guest MethodConsent reached plain ResolveConsent: %v", base.consents)
	}
	base.mu.Unlock()

	agent.mu.Lock()
	agent.consentFromErr = session.ErrRemoteIrreversible
	agent.mu.Unlock()
	consentIrrev := guest.call(8, MethodConsent, ConsentArgs{ID: 22, Allow: true})
	if want := chatlist.LiveNeedsItsOwnOK(host); consentIrrev.Error != want {
		t.Fatalf("remote guest MethodConsent irreversible error = %q, want %q", consentIrrev.Error, want)
	}
	agent.mu.Lock()
	agent.consentFromErr = nil
	agent.mu.Unlock()

	// 7. A local connection's MethodConsent reaches the plain ResolveConsent
	// exactly as before, without calling ResolveConsentFrom.
	local.ok(2, MethodConsent, ConsentArgs{ID: 31, Allow: false})
	agent.mu.Lock()
	if len(agent.consentsFrom) != 2 {
		t.Fatalf("local MethodConsent called ResolveConsentFrom: %+v", agent.consentsFrom)
	}
	agent.mu.Unlock()
	base.mu.Lock()
	if len(base.consents) != 1 || base.consents[0] != "consent:31:no" {
		t.Fatalf("local MethodConsent recorded %v, want [consent:31:no]", base.consents)
	}
	base.mu.Unlock()
}

// V26: an engine that cannot say whether an ask is one only the host may settle
// is an engine a remote guest may not answer for. A drive guest's consent
// against an engine without the remote-aware door is refused with the sentence
// the question door gives and nothing is delivered, where falling through to the plain
// door would settle even an irreversible ask.
func TestARemoteGuestIsRefusedConsentWhereTheEngineCannotTellWhoMaySettleIt(t *testing.T) {
	base := &fakeAgent{}
	sess := NewSession(engineOn(base), true)
	guest := dialSession(t, sess)
	welcome := guest.hello(Hello{
		Version: Version,
		Surface: "laptop",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote:  &RemoteViewer{Device: "dev-drive", Label: "laptop", Watch: true, Drive: true, Road: "relay"},
	})
	if welcome.Kind != "welcome" {
		t.Fatalf("handshake answered %q: %s", welcome.Kind, welcome.Error)
	}
	got := guest.call(1, MethodConsent, ConsentArgs{ID: 7, Allow: true})
	if want := "engine: this session cannot answer questions from here"; got.Error != want {
		t.Fatalf("consent without the remote-aware door answered %q, want %q", got.Error, want)
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	if len(base.consents) != 0 {
		t.Fatalf("a guest's consent reached the plain door: %v", base.consents)
	}
}
