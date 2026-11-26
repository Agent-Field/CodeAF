package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// parkConsentQuestion banks one permission question through the approval gate
// and returns the request event, the channel receiving the gate's verdict, and
// the question watcher stream after the raise event has arrived.
func parkConsentQuestion(t *testing.T, agent *Agent, call ai.ToolCall, rule string) (Event, chan consentAnswer, <-chan Event) {
	t.Helper()
	hub := newEventHub()
	events := hub.subscribe()
	asks, stopAsking := agent.WatchQuestions()
	t.Cleanup(stopAsking)

	answers := make(chan consentAnswer, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	guard.Go("test/parkConsentQuestion", func() {
		answer, err := agent.askAnswer(ctx, hub, call, approval.Decision{
			Action: approval.ActionPrompt,
			Rule:   rule,
		})
		if err == nil {
			answers <- answer
		}
	})

	request := <-events
	if request.Kind != EventConsentRequest {
		t.Fatalf("the gate emitted %v before EventConsentRequest", request.Kind)
	}
	waitForAsk(t, asks, EventQuestion)
	return request, answers, asks
}

// assertNoQuestionAnswered verifies that no EventQuestionAnswered has been
// emitted on the question stream while an answer was refused.
func assertNoQuestionAnswered(t *testing.T, asks <-chan Event) {
	t.Helper()
	for {
		select {
		case ev := <-asks:
			if ev.Kind == EventQuestionAnswered {
				t.Fatalf("a refused answer still emitted EventQuestionAnswered: %+v", ev)
			}
		default:
			return
		}
	}
}

// TestARemoteDriverCannotAnswerAnIrreversibleAskRememberAPermissionOrRaiseThePosture
// verifies V26 in the session engine: an answer stamped from another computer
// cannot settle an irreversible permission question, cannot widen a permission
// beyond a single call, and remains subject to the same gate when routed
// through the older consent door or a side discussion, while the host window
// can settle the waiting question afterwards.
func TestARemoteDriverCannotAnswerAnIrreversibleAskRememberAPermissionOrRaiseThePosture(t *testing.T) {
	t.Run("irreversible ask is refused to a remote viewer and stays open for the host window", func(t *testing.T) {
		agent, _ := questionSession(t, "remote-irrev-host", nil)
		call := ai.ToolCall{
			ID:       "c-irrev",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf /tmp/victim"}`},
		}
		request, answers, asks := parkConsentQuestion(t, agent, call, "critical command pattern \"rm -rf /*\"")

		open := agent.OpenQuestions()
		if len(open) != 1 || open[0].Stakes != StakesIrreversible {
			t.Fatalf("expected one open irreversible question, got %+v", open)
		}

		// Remote allow, remote deny, and remote dismiss are all refused on an
		// irreversible question.
		for _, attempt := range []Answer{
			{Remote: true, Kind: QuestionConsent, ID: request.ID, Key: "1"},
			{Remote: true, Kind: QuestionConsent, ID: request.ID, Key: "3"},
			{Remote: true, Kind: QuestionConsent, ID: request.ID, Key: "3", Dismissed: true},
		} {
			err := agent.ResolveQuestion(attempt)
			if !errors.Is(err, ErrRemoteIrreversible) {
				t.Fatalf("ResolveQuestion(%+v) = %v, want ErrRemoteIrreversible", attempt, err)
			}
			select {
			case got := <-answers:
				t.Fatalf("refused remote answer %+v released the parked call with %+v", attempt, got)
			default:
			}
			if records := agent.Decisions(); len(records) != 0 {
				t.Fatalf("refused remote answer recorded a decision: %+v", records)
			}
			assertNoQuestionAnswered(t, asks)
			if open := agent.OpenQuestions(); len(open) != 1 || open[0].ID != request.ID {
				t.Fatalf("refused remote answer took the question off OpenQuestions: %+v", open)
			}
		}

		// The host's own window (Remote: false) can answer the same question afterwards.
		if err := agent.ResolveQuestion(Answer{Remote: false, Kind: QuestionConsent, ID: request.ID, Key: "1"}); err != nil {
			t.Fatalf("local ResolveQuestion failed after remote refusals: %v", err)
		}
		select {
		case got := <-answers:
			if !got.allow || got.scope != ConsentOnce {
				t.Fatalf("local allow delivered %+v, want allow=true scope=once", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the parked call did not proceed after the host window answered")
		}
		waitForAsk(t, asks, EventQuestionAnswered)
		if records := agent.Decisions(); len(records) != 1 {
			t.Fatalf("expected 1 recorded decision after host allow, got %+v", records)
		}
		if open := agent.OpenQuestions(); len(open) != 0 {
			t.Fatalf("expected question to close after host allow, got %+v", open)
		}

		// A remote viewer also cannot revise a settled irreversible decision.
		revErr := agent.ResolveQuestion(Answer{
			Remote:  true,
			Kind:    QuestionConsent,
			ID:      request.ID,
			Key:     "3",
			Revises: true,
		})
		if !errors.Is(revErr, ErrRemoteIrreversible) {
			t.Fatalf("remote revision of irreversible decision returned %v, want ErrRemoteIrreversible", revErr)
		}
	})

	t.Run("remembering a permission or banking a rule is refused to a remote viewer while once allow and deny deliver", func(t *testing.T) {
		agent, _ := questionSession(t, "remote-remember-costly", func(cfg *Config) {
			cfg.ApprovalPolicy = promptAll()
		})
		costlyCall := ai.ToolCall{
			ID:       "c-costly-1",
			Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"notes.txt","content":"first"}`},
		}
		request, answers, asks := parkConsentQuestion(t, agent, costlyCall, "write requires approval")

		if open := agent.OpenQuestions(); len(open) != 1 || open[0].Stakes != StakesCostly {
			t.Fatalf("expected one costly question, got %+v", open)
		}

		// Key "2" (always / ConsentToolSession) is refused as widening.
		err := agent.ResolveQuestion(Answer{Remote: true, Kind: QuestionConsent, ID: request.ID, Key: "2"})
		if !errors.Is(err, ErrRemoteWidening) {
			t.Fatalf("remote always returned %v, want ErrRemoteWidening", err)
		}
		select {
		case got := <-answers:
			t.Fatalf("refused remote always delivered %+v", got)
		default:
		}
		if _, known := agent.rememberedConsent("write"); known {
			t.Fatal("refused remote always recorded a session memo for write")
		}
		if records := agent.Decisions(); len(records) != 0 {
			t.Fatalf("refused remote always recorded a decision: %+v", records)
		}
		assertNoQuestionAnswered(t, asks)

		// A rule-banking answer (Comments[AnswerBanked] set with key "2") is also
		// refused as widening.
		bankedErr := agent.ResolveQuestion(Answer{
			Remote:   true,
			Kind:     QuestionConsent,
			ID:       request.ID,
			Key:      "2",
			Comments: map[string]string{AnswerBanked: "write notes.txt"},
		})
		if !errors.Is(bankedErr, ErrRemoteWidening) {
			t.Fatalf("remote banked-rule answer returned %v, want ErrRemoteWidening", bankedErr)
		}
		select {
		case got := <-answers:
			t.Fatalf("refused remote banked-rule answer delivered %+v", got)
		default:
		}
		if _, known := agent.rememberedConsent("write"); known {
			t.Fatal("refused remote banked-rule answer recorded a session memo")
		}
		assertNoQuestionAnswered(t, asks)

		// Key "1" (once) from a remote viewer IS delivered and recorded, without
		// leaving a session memo behind.
		if err := agent.ResolveQuestion(Answer{Remote: true, Kind: QuestionConsent, ID: request.ID, Key: "1"}); err != nil {
			t.Fatalf("remote allow-once returned %v", err)
		}
		select {
		case got := <-answers:
			if !got.allow || got.scope != ConsentOnce {
				t.Fatalf("remote allow-once delivered %+v, want allow=true scope=once", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("remote allow-once never reached the waiting call")
		}
		waitForAsk(t, asks, EventQuestionAnswered)
		if records := agent.Decisions(); len(records) != 1 || records[0].Words() != "allow once" {
			t.Fatalf("remote allow-once left decisions %+v, want one allow-once record", records)
		}
		if _, known := agent.rememberedConsent("write"); known {
			t.Fatal("remote allow-once recorded a standing session memo")
		}

		// Because no session memo was recorded, the next identical call still
		// prompts rather than passing silently through approve().
		hub := newEventHub()
		events := hub.subscribe()
		secondDone := make(chan bool, 1)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		guard.Go("test/secondCostlyCall", func() {
			_, allowed := agent.approve(ctx, hub, costlyCall)
			secondDone <- allowed
		})
		var secondReq Event
		select {
		case ev := <-events:
			if ev.Kind != EventConsentRequest {
				t.Fatalf("second identical call emitted %v, want EventConsentRequest", ev.Kind)
			}
			secondReq = ev
		case allowed := <-secondDone:
			t.Fatalf("second identical call did not ask again (allowed=%v)", allowed)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for the second identical call to ask")
		}

		// Key "3" (deny) from a remote viewer on a costly question IS delivered.
		if err := agent.ResolveQuestion(Answer{Remote: true, Kind: QuestionConsent, ID: secondReq.ID, Key: "3"}); err != nil {
			t.Fatalf("remote deny on costly ask returned %v", err)
		}
		select {
		case allowed := <-secondDone:
			if allowed {
				t.Fatal("remote deny allowed the second call")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("remote deny never released the second call")
		}
	})

	t.Run("the older consent door refuses irreversible asks to a remote viewer and delivers once on costly asks", func(t *testing.T) {
		agent, _ := questionSession(t, "remote-older-door", nil)
		irrevCall := ai.ToolCall{
			ID:       "c-irrev-old",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf /tmp/victim"}`},
		}
		irrevReq, irrevAnswers, _ := parkConsentQuestion(t, agent, irrevCall, "critical command pattern \"rm -rf /*\"")

		if err := agent.ResolveConsentFrom(irrevReq.ID, true, true); !errors.Is(err, ErrRemoteIrreversible) {
			t.Fatalf("ResolveConsentFrom(irrev, true, remote=true) = %v, want ErrRemoteIrreversible", err)
		}
		if err := agent.ResolveConsentFrom(irrevReq.ID, false, true); !errors.Is(err, ErrRemoteIrreversible) {
			t.Fatalf("ResolveConsentFrom(irrev, false, remote=true) = %v, want ErrRemoteIrreversible", err)
		}
		select {
		case got := <-irrevAnswers:
			t.Fatalf("refused ResolveConsentFrom delivered %+v", got)
		default:
		}

		// Costly ask via ResolveConsentFrom(..., remote=true) delivers once.
		costlyCall := ai.ToolCall{
			ID:       "c-costly-old",
			Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"a.txt","content":"b"}`},
		}
		costlyReq, costlyAnswers, _ := parkConsentQuestion(t, agent, costlyCall, "write asks")
		if err := agent.ResolveConsentFrom(costlyReq.ID, true, true); err != nil {
			t.Fatalf("ResolveConsentFrom(costly, true, remote=true) = %v", err)
		}
		select {
		case got := <-costlyAnswers:
			if !got.allow || got.scope != ConsentOnce {
				t.Fatalf("ResolveConsentFrom on costly ask delivered %+v, want allow=true scope=once", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("ResolveConsentFrom on costly ask did not deliver")
		}

		// Local ResolveConsentFrom(..., remote=false) on the irreversible ask delivers.
		if err := agent.ResolveConsentFrom(irrevReq.ID, true, false); err != nil {
			t.Fatalf("ResolveConsentFrom(irrev, true, remote=false) = %v", err)
		}
		select {
		case got := <-irrevAnswers:
			if !got.allow || got.scope != ConsentOnce {
				t.Fatalf("local ResolveConsentFrom on irreversible ask delivered %+v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("local ResolveConsentFrom on irreversible ask did not deliver")
		}
	})

	t.Run("a remote clarification on an irreversible ask and a remote answer to a reversible ask proceed", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("That command overwrites the remote branch history."), nil
			},
		}}, func(c *Config) {
			c.AskConsent = true
			c.Interactive = true
			c.ApprovalPolicy = promptAll()
		})
		watch, stop := agent.WatchQuestions()
		t.Cleanup(stop)

		irrevCall := ai.ToolCall{
			ID:       "c-clarify-irrev",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf /tmp/victim"}`},
		}
		irrevReq, irrevAnswers, _ := parkConsentQuestion(t, agent, irrevCall, "critical command pattern \"rm -rf /*\"")

		// A Clarify answer only asks for words and resolves nothing, so a remote
		// viewer is not refused even when the underlying question is irreversible.
		err := agent.ResolveQuestion(Answer{
			Remote:    true,
			Kind:      QuestionConsent,
			ID:        irrevReq.ID,
			Clarify:   true,
			AskedBack: []Exchange{{Asked: "why does this need force push?"}},
		})
		if err != nil {
			t.Fatalf("remote Clarify on irreversible ask was refused: %v", err)
		}
		discussionWait(t, watch, func(e Event) bool {
			return e.Discussion != nil && e.Discussion.Event != nil && e.Discussion.Event.Kind == EventTurnDone
		})
		// The irreversible ask itself is still waiting and can be settled locally.
		select {
		case got := <-irrevAnswers:
			t.Fatalf("clarification settled the irreversible ask: %+v", got)
		default:
		}
		if err := agent.ResolveQuestion(Answer{Remote: false, Kind: QuestionConsent, ID: irrevReq.ID, Key: "3"}); err != nil {
			t.Fatalf("local deny after clarification failed: %v", err)
		}
		<-irrevAnswers

		// A remote answer to a reversible model ask question is delivered normally.
		askAgent := askTestAgent(t, true)
		if _, _, err := askAgent.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
			t.Fatalf("executeAsk: %v", err)
		}
		q := waitForOneQuestion(t, askAgent)
		if err := askAgent.ResolveQuestion(Answer{Remote: true, Kind: QuestionAsk, ID: q.ID, Key: "2"}); err != nil {
			t.Fatalf("remote answer to reversible ask question failed: %v", err)
		}
		if notes := askDelivered(askAgent); len(notes) != 1 || !strings.Contains(notes[0], `"key":"2"`) {
			t.Fatalf("reversible ask answer was not delivered: %v", notes)
		}
	})

	t.Run("a side discussion preserves the remote flag and refuses an irreversible child ask", func(t *testing.T) {
		parent, _ := questionSession(t, "remote-discuss-parent", nil)
		child, _ := questionSession(t, "remote-discuss-child", nil)
		done := make(chan struct{})
		close(done)
		parent.mu.Lock()
		parent.discussions = map[string]*questionDiscussion{
			"clarify-1": {child: child, cancel: func() {}, done: done, depth: 1},
		}
		parent.mu.Unlock()

		irrevCall := ai.ToolCall{
			ID:       "c-child-irrev",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf /tmp/victim"}`},
		}
		childReq, childAnswers, _ := parkConsentQuestion(t, child, irrevCall, "critical command pattern \"rm -rf /*\"")
		nestedRef := "clarify-1/" + strconv.FormatUint(childReq.ID, 10)

		// Calling discussionAnswer directly returns the Answer value with Remote unchanged.
		gotChild, forwarded, ok := parent.discussionAnswer(Answer{
			Remote: true,
			Kind:   QuestionConsent,
			Ref:    nestedRef,
			Key:    "1",
		})
		if !ok || gotChild != child || !forwarded.Remote || forwarded.ID != childReq.ID {
			t.Fatalf("discussionAnswer lost Remote or target: ok=%v child=%v forwarded=%+v", ok, gotChild == child, forwarded)
		}

		// And routing the remote answer through parent.ResolveQuestion refuses the
		// child's irreversible ask with ErrRemoteIrreversible.
		err := parent.ResolveQuestion(Answer{
			Remote: true,
			Kind:   QuestionConsent,
			Ref:    nestedRef,
			Key:    "1",
		})
		if !errors.Is(err, ErrRemoteIrreversible) {
			t.Fatalf("parent.ResolveQuestion for remote child irreversible ask = %v, want ErrRemoteIrreversible", err)
		}
		select {
		case got := <-childAnswers:
			t.Fatalf("refused child irreversible ask still delivered %+v", got)
		default:
		}

		// Local answer through the parent discussion door settles the child question.
		if err := parent.ResolveQuestion(Answer{
			Remote: false,
			Kind:   QuestionConsent,
			Ref:    nestedRef,
			Key:    "1",
		}); err != nil {
			t.Fatalf("local parent.ResolveQuestion for child ask failed: %v", err)
		}
		select {
		case got := <-childAnswers:
			if !got.allow {
				t.Fatalf("local child answer delivered %+v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("local child answer never reached the child wait")
		}
	})
}

// TestAnAnswerCarriesNoRemoteBytesIntoWhatIsKept verifies V26: a remote
// once-allow on a costly permission leaves a decision record on disk and in
// memory, and an answers-file line on disk, with no "remote" key in the
// marshalled bytes.
func TestAnAnswerCarriesNoRemoteBytesIntoWhatIsKept(t *testing.T) {
	agent, dir := questionSession(t, "remote-no-persisted-bytes", nil)
	costlyCall := ai.ToolCall{
		ID:       "c-costly-kept",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"kept.txt","content":"ok"}`},
	}
	request, answers, _ := parkConsentQuestion(t, agent, costlyCall, "write asks")

	if err := agent.ResolveQuestion(Answer{
		Remote: true,
		Kind:   QuestionConsent,
		ID:     request.ID,
		Key:    "1",
	}); err != nil {
		t.Fatalf("remote once-allow failed: %v", err)
	}
	<-answers

	records := agent.Decisions()
	if len(records) != 1 {
		t.Fatalf("expected 1 decision record, got %d", len(records))
	}
	recordBytes, err := json.Marshal(records[0])
	if err != nil {
		t.Fatalf("marshal decision record: %v", err)
	}
	if strings.Contains(string(recordBytes), `"remote"`) {
		t.Fatalf("marshalled DecisionRecord contains remote key: %s", recordBytes)
	}
	diskDecisions, err := os.ReadFile(DecisionsPath(dir))
	if err != nil {
		t.Fatalf("read decisions file: %v", err)
	}
	if strings.Contains(string(diskDecisions), `"remote"`) {
		t.Fatalf("decisions.jsonl contains remote key: %s", diskDecisions)
	}

	if err := WriteAnswer(dir, QuestionConsent, request.ID, "1"); err != nil {
		t.Fatalf("WriteAnswer: %v", err)
	}
	if err := deliverAnswer(dir, Answer{
		At:     time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC),
		Kind:   QuestionConsent,
		ID:     request.ID,
		Key:    "1",
		Remote: true,
	}); err != nil {
		t.Fatalf("deliverAnswer: %v", err)
	}
	diskAnswers, err := os.ReadFile(AnswersPath(dir))
	if err != nil {
		t.Fatalf("read answers file: %v", err)
	}
	if strings.Contains(string(diskAnswers), `"remote"`) {
		t.Fatalf("answers.jsonl contains remote key: %s", diskAnswers)
	}
}

// TestAnAnswerWithoutTheRemoteFlagMarshalsAsItAlwaysDid verifies V26: an Answer
// with Remote false marshals to the exact JSON bytes written from the field
// list, with no "remote" key present.
func TestAnAnswerWithoutTheRemoteFlagMarshalsAsItAlwaysDid(t *testing.T) {
	answer := Answer{
		At:   time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC),
		Kind: QuestionConsent,
		ID:   7,
		Key:  "1",
		From: "home",
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("marshal Answer: %v", err)
	}
	const want = `{"at":"2026-09-09T10:00:00Z","kind":"consent","id":7,"key":"1","from":"home"}`
	if string(raw) != want {
		t.Fatalf("json.Marshal(Answer) =\n got: %s\nwant: %s", raw, want)
	}
}
