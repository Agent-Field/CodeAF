//go:build !windows

// Question lifecycle service
package question

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
)

// Event contains the three question bus definitions.
var Event = struct {
	Asked    bus.Definition
	Replied  bus.Definition
	Rejected bus.Definition
}{
	Asked:    bus.Define("question.asked", "QuestionRequest"),
	Replied:  bus.Define("question.replied", "QuestionReplied"),
	Rejected: bus.Define("question.rejected", "QuestionRejected"),
}

// RejectedError is returned when a question is dismissed or the service is
// finalized.
type RejectedError struct{}

// Error is the model-visible dismissal message.
func (*RejectedError) Error() string { return "The user dismissed this question" }

// Publisher is the narrow bus surface used by Service.
type Publisher interface {
	Publish(bus.Definition, any, ...bus.PublishOptions)
}

// IDGenerator creates a new ascending question identifier.
type IDGenerator func() (QuestionID, error)

// AskInput is the input accepted by Service.Ask.
type AskInput struct {
	SessionID string
	Questions []Info
	Tool      *Tool
}

// ReplyInput is the input accepted by Service.Reply.
type ReplyInput struct {
	RequestID QuestionID
	Answers   []Answer
}

type pendingResult struct {
	answers []Answer
	err     error
}

type pendingEntry struct {
	info     Request
	deferred chan pendingResult
}

// Service owns the insertion-ordered pending-question map.
type Service struct {
	mu        sync.Mutex
	pending   map[QuestionID]*pendingEntry
	order     []QuestionID
	publisher Publisher
	createID  IDGenerator
	closed    bool
}

// NewService constructs a question service. Nil dependencies use the
// package-level bus and QuestionID generator.
func NewService(publisher Publisher, createID IDGenerator) *Service {
	if publisher == nil {
		publisher = bus.Default
	}
	if createID == nil {
		createID = func() (QuestionID, error) { return AscendingQuestionID() }
	}
	return &Service{
		pending:   make(map[QuestionID]*pendingEntry),
		publisher: publisher,
		createID:  createID,
	}
}

// Ask registers and publishes a request, then waits for a reply, rejection, or
// context cancellation. The pending entry is removed on every exit path.
func (s *Service) Ask(ctx context.Context, input AskInput) ([]Answer, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, &RejectedError{}
	}
	requestID, err := s.createID()
	if err != nil {
		return nil, err
	}
	request := Request{
		ID:        requestID,
		SessionID: input.SessionID,
		Questions: cloneInfos(input.Questions),
		Tool:      cloneTool(input.Tool),
	}
	entry := &pendingEntry{info: request, deferred: make(chan pendingResult, 1)}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, &RejectedError{}
	}
	if _, exists := s.pending[requestID]; !exists {
		s.order = append(s.order, requestID)
	}
	s.pending[requestID] = entry
	s.mu.Unlock()

	defer s.delete(requestID)
	s.publisher.Publish(Event.Asked, request)

	select {
	case result := <-entry.deferred:
		return result.answers, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Reply publishes the reply and resolves its pending Ask. Unknown request IDs
// are ignored.
func (s *Service) Reply(input ReplyInput) {
	entry := s.take(input.RequestID)
	if entry == nil {
		return
	}
	s.publisher.Publish(Event.Replied, Replied{
		SessionID: entry.info.SessionID,
		RequestID: entry.info.ID,
		Answers:   cloneAnswers(input.Answers),
	})
	entry.deferred <- pendingResult{answers: input.Answers}
}

// Reject publishes the rejection and fails its pending Ask. Unknown request
// IDs are ignored.
func (s *Service) Reject(requestID QuestionID) {
	entry := s.take(requestID)
	if entry == nil {
		return
	}
	s.publisher.Publish(Event.Rejected, Rejected{
		SessionID: entry.info.SessionID,
		RequestID: entry.info.ID,
	})
	entry.deferred <- pendingResult{err: &RejectedError{}}
}

// List returns pending requests in insertion order.
func (s *Service) List() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Request, 0, len(s.order))
	for _, requestID := range s.order {
		if entry := s.pending[requestID]; entry != nil {
			result = append(result, entry.info)
		}
	}
	return result
}

// Close rejects every waiter, clears the pending map, and makes subsequent
// asks fail as dismissed.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	entries := make([]*pendingEntry, 0, len(s.order))
	for _, requestID := range s.order {
		if entry := s.pending[requestID]; entry != nil {
			entries = append(entries, entry)
		}
	}
	s.pending = make(map[QuestionID]*pendingEntry)
	s.order = nil
	s.mu.Unlock()

	for _, entry := range entries {
		entry.deferred <- pendingResult{err: &RejectedError{}}
	}
}

func (s *Service) take(requestID QuestionID) *pendingEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.pending[requestID]
	if entry == nil {
		return nil
	}
	delete(s.pending, requestID)
	s.removeOrder(requestID)
	return entry
}

func (s *Service) delete(requestID QuestionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pending[requestID]; !exists {
		return
	}
	delete(s.pending, requestID)
	s.removeOrder(requestID)
}

func (s *Service) removeOrder(requestID QuestionID) {
	for index, item := range s.order {
		if item == requestID {
			s.order = append(s.order[:index], s.order[index+1:]...)
			return
		}
	}
}

func cloneTool(tool *Tool) *Tool {
	if tool == nil {
		return nil
	}
	result := *tool
	return &result
}

func cloneInfos(questions []Info) []Info {
	result := make([]Info, len(questions))
	for index, question := range questions {
		result[index] = question
		result[index].Options = append([]Option{}, question.Options...)
	}
	return result
}

func cloneAnswers(answers []Answer) []Answer {
	result := make([]Answer, len(answers))
	for index, answer := range answers {
		result[index] = append(Answer{}, answer...)
	}
	return result
}

// Default is the package-level service backed by bus.Default.
var Default = NewService(nil, nil)
