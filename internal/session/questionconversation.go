package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// QuestionDiscussion carries a clarification beside the conversation's blocked
// turn. Its events never finish or replace that turn.
type QuestionDiscussion struct {
	ID    string
	Seq   uint64
	Words string
	Event *Event
	Error string
}

type questionDiscussion struct {
	child  *Agent
	cancel context.CancelFunc
	done   chan struct{}
	depth  int
}

// ReplaceQuestion stops the pending turn before submitting the revised request.
// No answer is recorded and no permission is granted by this operation.
func (a *Agent) ReplaceQuestion(ctx context.Context, answer Answer) (<-chan Event, error) {
	owner, original := a, answer
	if child, given, nested := a.discussionAnswer(answer); nested {
		owner, original = child, given
	}
	q, ok := owner.questionSaid(original.Kind, answerToken(original))
	if !ok {
		return nil, errors.New("that question is no longer open")
	}
	words := strings.TrimSpace(answer.Change)
	if words == "" {
		return nil, errors.New("write the updated request first")
	}
	// Taking a different course is not a yes or a no. The withdrawal reaches
	// every window before cancellation releases the tool waiting on this turn.
	owner.WithdrawQuestion(q.Kind, q.Token(), "you gave an updated request")
	a.mu.Lock()
	done := a.done
	running := a.running
	a.mu.Unlock()
	a.Interrupt()
	a.closeDiscussions()
	if running && done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if q.Kind == QuestionAsk {
		owner.mu.Lock()
		owner.asked.letGoLocked(q.ID)
		owner.mu.Unlock()
	}
	switch q.Kind {
	case QuestionTask:
		owner.ResolveTask(q.ID, TaskAnswer{})
	case QuestionStanding:
		owner.ResolveStanding(q.ID, StandingAnswer{})
	case QuestionHarness:
		owner.ResolveHarness(q.ID, false, "")
	case QuestionSubharness:
		owner.ResolveSubharness(q.ID, false, nil)
	case QuestionConnect:
		owner.ResolveConnect(q.Ref, false)
	}
	return a.Submit(ctx, words)
}

// clarifyQuestion starts an independent conversation with a snapshot
// of the context. The original wait retains ownership of its pending action.
func (a *Agent) clarifyQuestion(answer Answer) error {
	q, ok := a.questionSaid(answer.Kind, answerToken(answer))
	if !ok {
		return errors.New("that question is no longer open")
	}
	words := askedBackWords(answer)
	if strings.TrimSpace(words) == "" {
		return errors.New("write your clarification first")
	}
	a.mu.Lock()
	cfg := a.config
	cfg.Model = a.model
	cfg.SessionFile, cfg.Place = "", Place{}
	cfg.rootSession = a.id
	cfg.Errand, cfg.Interactive = true, true
	cfg.InTask, cfg.Divide, cfg.HarnessCards = false, false, false
	cfg.Standing = nil
	cfg.tasker, cfg.taskID = nil, 0
	cfg.ApprovalPolicy = a.approvalPolicy
	if cfg.ApprovalPolicy == nil {
		cfg.ApprovalPolicy = a.config.ApprovalPolicy
	}
	cfg.System = a.system
	if a.guardianOverride != nil {
		cfg.Guardian = *a.guardianOverride
	}
	cfg.ApprovalPosture = a.approvalPosture
	a.mu.Unlock()
	child, err := a.newChildAgent(cfg)
	if err != nil {
		return err
	}
	// A pending tool batch cannot be copied onto another provider request. The
	// plain conversation snapshot preserves its meaning without dangling calls.
	contextText := new(strings.Builder)
	for _, e := range displayEntries(a.snapshot()) {
		fmt.Fprintf(contextText, "%s: %s %s %s\n", e.Role, e.Tool, e.Hint, e.Text)
	}
	fmt.Fprintf(contextText, "\nPending question: %s\nReason: %s\n", q.Head, q.Reason)
	for _, option := range q.Options {
		fmt.Fprintf(contextText, "%s: %s\n", option.Key, option.Label)
	}
	prompt := "Clarify the person's question using the conversation below. The pending decision remains open and belongs to them. Do not carry out the pending action, answer the decision, or repeat it. Use tools only as needed for this clarification, under the current approvals mode.\n\n" + contextText.String() + "\nTheir clarification: " + words
	discussionCtx, cancelDiscussion := context.WithCancel(context.Background())
	d := &questionDiscussion{child: child, cancel: cancelDiscussion, done: make(chan struct{}), depth: q.ClarificationDepth + 1}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		cancelDiscussion()
		child.Close()
		return errors.New("session: agent is closed")
	}
	if a.discussions == nil {
		a.discussions = make(map[string]*questionDiscussion)
	}
	a.discussionSeq++
	id := fmt.Sprintf("clarify-%d", a.discussionSeq)
	child.approvalParent = a
	child.questionParent = func(ev Event) { a.forwardDiscussionQuestion(id, d, ev) }
	a.discussions[id] = d
	a.mu.Unlock()
	a.HoldQuestion(q.Kind, q.Token())
	events, err := child.Submit(discussionCtx, prompt)
	if err != nil {
		child.Close()
		close(d.done)
		return err
	}
	a.emitDiscussion(QuestionDiscussion{ID: id, Words: words})
	go func() {
		defer close(d.done)
		defer cancelDiscussion()
		defer child.Close()
		var reply strings.Builder
		var ending *Event
		streamed := false
		for events != nil {
			select {
			case <-discussionCtx.Done():
				events = nil
			case ev, ok := <-events:
				if !ok {
					events = nil
					break
				}
				if ev.Kind == EventTurnDone {
					saved := ev
					ending = &saved
					continue
				}

				if ev.Kind == EventTextDelta {
					streamed = true
				}
				update := QuestionDiscussion{ID: id, Event: &ev}
				if ev.Err != nil {
					update.Error = ev.Err.Error()
					ev.Err = nil
				}
				a.emitDiscussion(update)
			}
		}
		// Questions emitted just before the final stream event still owe their
		// removal. Closing the child retires any unanswered nonblocking questions.
		pending := child.OpenQuestions()
		child.Close()
		used := child.Usage()
		cost := used.CostUSD
		a.addFoldedUsage(&ai.Response{Usage: &ai.Usage{PromptTokens: used.Input, CompletionTokens: used.Output, CacheReadInputTokens: used.CacheRead, CacheCreationInputTokens: used.CacheWrite, Cost: &cost}}, child.Model(), used.Calls)
		for _, open := range pending {
			open.Withdrawn = &Withdrawal{Reason: "clarification finished"}
			a.forwardDiscussionQuestion(id, d, Event{Kind: EventQuestionWithdrawn, Question: &open})
		}
		// The journal's complete response also covers nonstreaming providers.
		reply.Reset()
		for _, entry := range child.Transcript() {
			if entry.Role == "assistant" {
				if reply.Len() > 0 {
					reply.WriteString("\n\n")
				}
				reply.WriteString(entry.Text)
			}
		}
		if !streamed && reply.Len() > 0 {
			a.emitDiscussion(QuestionDiscussion{ID: id, Event: &Event{Kind: EventTextDelta, Text: reply.String()}})
		}
		a.mu.Lock()
		if a.discussionRecorded == nil {
			a.discussionRecorded = make(map[string]bool)
		}
		a.discussionRecorded[id] = true
		history := []ai.Message{textMessage("user", "clarify: "+words), textMessage("assistant", reply.String())}
		if a.running {
			a.discussionRecorded[id] = false
			a.discussionHistory = append(a.discussionHistory, history...)
			a.discussionPending = append(a.discussionPending, id)
		} else {
			for _, message := range history {
				a.recordLocked(message)
			}
			a.forgetRecordedDiscussionsLocked()
		}
		a.mu.Unlock()
		if ending == nil {
			ending = &Event{Kind: EventTurnDone}
		}
		a.emitDiscussion(QuestionDiscussion{ID: id, Event: ending})
	}()
	return nil
}

func (a *Agent) emitDiscussion(update QuestionDiscussion) {
	ev := Event{Kind: EventQuestionDiscussion, Discussion: &update}
	if a.questionParent != nil {
		a.questionParent(ev)
	}
	a.mu.Lock()
	a.discussionEventSeq++
	update.Seq = a.discussionEventSeq
	if !a.discussionRecorded[update.ID] {
		a.discussionEvents = append(a.discussionEvents, ev)
	}
	watchers := append([]*eventStream(nil), a.questionWatchers...)
	a.mu.Unlock()
	for _, watcher := range watchers {
		watcher.send(ev)
	}
}

func (a *Agent) forwardDiscussionQuestion(id string, d *questionDiscussion, ev Event) {
	if ev.Discussion != nil {
		update := *ev.Discussion
		update.ID = id + "/" + update.ID
		a.emitDiscussion(update)
		return
	}
	if ev.Question == nil {
		return
	}
	q := *ev.Question
	q.Ref = id + "/" + q.Token()
	q.Batch = id + "/" + q.Batch
	if q.Subject.CallID != "" {
		q.Subject.CallID = id + "/" + q.Subject.CallID
	}
	q.ClarificationDepth += d.depth
	if ev.Answer != nil {
		answer := *ev.Answer
		answer.Ref = q.Ref
		ev.Answer = &answer
	}
	a.emitQuestion(ev.Kind, q, ev.Answer)
}

func (a *Agent) discussionAnswer(answer Answer) (*Agent, Answer, bool) {
	id, ref, ok := strings.Cut(answer.Ref, "/")
	if !ok {
		return nil, answer, false
	}
	a.mu.Lock()
	d := a.discussions[id]
	a.mu.Unlock()
	if d == nil {
		return nil, answer, false
	}
	for _, q := range d.child.OpenQuestions() {
		if q.Kind == answer.Kind && q.Token() == ref {
			answer.Ref, answer.ID = q.Ref, q.ID
			return d.child, answer, true
		}
	}
	answer.Ref = ref
	return d.child, answer, true
}

func (a *Agent) discussionQuestions() []Question {
	a.mu.Lock()
	ds := make(map[string]*questionDiscussion, len(a.discussions))
	for id, d := range a.discussions {
		ds[id] = d
	}
	a.mu.Unlock()
	var open []Question
	for id, d := range ds {
		for _, q := range d.child.OpenQuestions() {
			q.Ref = id + "/" + q.Token()
			q.Batch = id + "/" + q.Batch
			if q.Subject.CallID != "" {
				q.Subject.CallID = id + "/" + q.Subject.CallID
			}
			q.ClarificationDepth += d.depth
			open = append(open, q)
		}
	}
	return open
}

func (a *Agent) closeDiscussions() {
	a.mu.Lock()
	ds := make([]*questionDiscussion, 0, len(a.discussions))
	for _, d := range a.discussions {
		ds = append(ds, d)
	}
	a.mu.Unlock()
	for _, d := range ds {
		d.cancel()
		d.child.Close()
	}
	for _, d := range ds {
		<-d.done
	}
}

// Once the exchange is in the transcript, a returning window reads it there.
func (a *Agent) forgetRecordedDiscussionsLocked() {
	kept := a.discussionEvents[:0]
	for _, event := range a.discussionEvents {
		if event.Discussion != nil && !a.discussionRecorded[event.Discussion.ID] {
			kept = append(kept, event)
		}
	}
	a.discussionEvents = kept
}

func (a *Agent) interruptDiscussions() {
	a.mu.Lock()
	ds := make([]*questionDiscussion, 0, len(a.discussions))
	for _, d := range a.discussions {
		ds = append(ds, d)
	}
	a.mu.Unlock()
	for _, d := range ds {
		d.cancel()
		d.child.Interrupt()
	}
}
