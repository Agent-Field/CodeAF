package question

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
)

type askResult struct {
	answers []Answer
	err     error
}

func isolatedService(ids ...QuestionID) (*Service, *bus.Bus, <-chan bus.Payload) {
	instanceBus := bus.New(
		bus.Context{},
		bus.WithGlobalEmitter(bus.NewGlobalEmitter(func() string { return "evt_global" })),
		bus.WithIDGenerator(func() string { return "evt_local" }),
	)
	events := make(chan bus.Payload, 32)
	instanceBus.SubscribeAllCallback(func(payload bus.Payload) { events <- payload })
	index := 0
	var idMutex sync.Mutex
	service := NewService(instanceBus, func() (QuestionID, error) {
		idMutex.Lock()
		defer idMutex.Unlock()
		value := ids[index]
		index++
		return value, nil
	})
	return service, instanceBus, events
}

func askAsync(service *Service, input AskInput) <-chan askResult {
	result := make(chan askResult, 1)
	go func() {
		answers, err := service.Ask(context.Background(), input)
		result <- askResult{answers: answers, err: err}
	}()
	return result
}

func receiveEvent(t *testing.T, events <-chan bus.Payload) bus.Payload {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return bus.Payload{}
	}
}

func receiveAsk(t *testing.T, result <-chan askResult) askResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Ask")
		return askResult{}
	}
}

func TestAskReplyLifecycleAndEvents(t *testing.T) {
	service, _, events := isolatedService("que_1")
	question := Info{
		Question: "Continue?",
		Header:   "Confirm",
		Options:  []Option{{Label: "Yes", Description: "Continue"}},
	}
	result := askAsync(service, AskInput{
		SessionID: "ses_1",
		Questions: []Info{question},
		Tool:      &Tool{MessageID: "msg_1", CallID: "call-1"},
	})

	asked := receiveEvent(t, events)
	if asked.Type != Event.Asked.Type {
		t.Fatalf("event type = %q", asked.Type)
	}
	request, ok := asked.Properties.(Request)
	if !ok || request.ID != "que_1" || request.SessionID != "ses_1" ||
		request.Tool == nil || request.Tool.CallID != "call-1" {
		t.Fatalf("asked payload = %#v", asked.Properties)
	}
	listed := service.List()
	if len(listed) != 1 || listed[0].ID != "que_1" {
		t.Fatalf("List = %#v", listed)
	}

	answers := []Answer{{"Yes"}, {"custom"}}
	service.Reply(ReplyInput{RequestID: "que_1", Answers: answers})
	replied := receiveEvent(t, events)
	if replied.Type != Event.Replied.Type {
		t.Fatalf("event type = %q", replied.Type)
	}
	properties, ok := replied.Properties.(Replied)
	if !ok || properties.SessionID != "ses_1" || properties.RequestID != "que_1" ||
		!reflect.DeepEqual(properties.Answers, answers) {
		t.Fatalf("replied payload = %#v", replied.Properties)
	}
	answers[0][0] = "mutated"
	if properties.Answers[0][0] != "Yes" {
		t.Fatal("published answers alias Reply input")
	}

	got := receiveAsk(t, result)
	if got.err != nil || got.answers[0][0] != "mutated" {
		t.Fatalf("Ask = %#v", got)
	}
	if len(service.List()) != 0 {
		t.Fatalf("pending after reply = %#v", service.List())
	}
}

func TestRejectLifecycleAndMessage(t *testing.T) {
	service, _, events := isolatedService("que_2")
	result := askAsync(service, AskInput{SessionID: "ses_2", Questions: []Info{}})
	_ = receiveEvent(t, events)

	service.Reject("que_2")
	rejected := receiveEvent(t, events)
	properties, ok := rejected.Properties.(Rejected)
	if rejected.Type != Event.Rejected.Type || !ok ||
		properties.SessionID != "ses_2" || properties.RequestID != "que_2" {
		t.Fatalf("rejected event = %#v", rejected)
	}
	got := receiveAsk(t, result)
	var rejectedError *RejectedError
	if !errors.As(got.err, &rejectedError) {
		t.Fatalf("Ask error = %v", got.err)
	}
	if got.err.Error() != "The user dismissed this question" {
		t.Fatalf("message = %q", got.err)
	}
}

func TestUnknownReplyAndRejectAreNoOps(t *testing.T) {
	service, _, events := isolatedService("que_unused")
	service.Reply(ReplyInput{RequestID: "que_unknown", Answers: []Answer{{"x"}}})
	service.Reject("que_unknown")
	select {
	case event := <-events:
		t.Fatalf("unexpected event %#v", event)
	default:
	}
}

func TestListPreservesInsertionOrder(t *testing.T) {
	service, _, events := isolatedService("que_1", "que_2", "que_3")
	var results []<-chan askResult
	for _, sessionID := range []string{"ses_1", "ses_2", "ses_3"} {
		results = append(results, askAsync(service, AskInput{
			SessionID: sessionID, Questions: []Info{},
		}))
		_ = receiveEvent(t, events)
	}
	listed := service.List()
	var ids []QuestionID
	for _, request := range listed {
		ids = append(ids, request.ID)
	}
	if want := []QuestionID{"que_1", "que_2", "que_3"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
	for index, requestID := range ids {
		service.Reply(ReplyInput{RequestID: requestID, Answers: []Answer{}})
		_ = receiveEvent(t, events)
		if got := receiveAsk(t, results[index]); got.err != nil {
			t.Fatal(got.err)
		}
	}
}

func TestSynchronousAskedSubscriberCanReply(t *testing.T) {
	instanceBus := bus.New(
		bus.Context{},
		bus.WithGlobalEmitter(bus.NewGlobalEmitter(func() string { return "evt_global" })),
		bus.WithIDGenerator(func() string { return "evt_local" }),
	)
	var service *Service
	instanceBus.SubscribeCallback(Event.Asked, func(payload bus.Payload) {
		request := payload.Properties.(Request)
		service.Reply(ReplyInput{RequestID: request.ID, Answers: []Answer{{"Immediately"}}})
	})
	service = NewService(instanceBus, func() (QuestionID, error) { return "que_sync", nil })
	answers, err := service.Ask(context.Background(), AskInput{
		SessionID: "ses_1", Questions: []Info{},
	})
	if err != nil || !reflect.DeepEqual(answers, []Answer{{"Immediately"}}) {
		t.Fatalf("Ask = %#v, %v", answers, err)
	}
}

func TestContextCancellationRemovesPending(t *testing.T) {
	service, _, events := isolatedService("que_cancel")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan askResult, 1)
	go func() {
		answers, err := service.Ask(ctx, AskInput{SessionID: "ses_1", Questions: []Info{}})
		result <- askResult{answers: answers, err: err}
	}()
	_ = receiveEvent(t, events)
	cancel()
	got := receiveAsk(t, result)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("Ask error = %v", got.err)
	}
	if len(service.List()) != 0 {
		t.Fatalf("pending after cancel = %#v", service.List())
	}
}

func TestCloseRejectsAllWithoutPublishingRejectedEvents(t *testing.T) {
	service, _, events := isolatedService("que_1", "que_2")
	first := askAsync(service, AskInput{SessionID: "ses_1", Questions: []Info{}})
	_ = receiveEvent(t, events)
	second := askAsync(service, AskInput{SessionID: "ses_2", Questions: []Info{}})
	_ = receiveEvent(t, events)

	service.Close()
	for _, result := range []<-chan askResult{first, second} {
		got := receiveAsk(t, result)
		var rejected *RejectedError
		if !errors.As(got.err, &rejected) {
			t.Fatalf("Ask error = %v", got.err)
		}
	}
	if len(service.List()) != 0 {
		t.Fatalf("pending after Close = %#v", service.List())
	}
	select {
	case event := <-events:
		t.Fatalf("Close published event %#v", event)
	default:
	}
	_, err := service.Ask(context.Background(), AskInput{})
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("Ask after Close error = %v", err)
	}
}

func TestConcurrentUnknownOperations(t *testing.T) {
	service, _, _ := isolatedService("que_unused")
	var wait sync.WaitGroup
	for index := range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			requestID := QuestionID("que_" + string(rune(index)))
			service.Reply(ReplyInput{RequestID: requestID})
			service.Reject(requestID)
			_ = service.List()
		}()
	}
	wait.Wait()
}
