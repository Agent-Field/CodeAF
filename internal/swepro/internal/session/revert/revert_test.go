package revert

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/bus"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
)

type memorySessions struct {
	session  Session
	messages []msgmodel.WithParts
	set      []SetRevertInput
	clears   int
}

func (memory *memorySessions) Messages(
	context.Context, string,
) ([]msgmodel.WithParts, error) {
	return append([]msgmodel.WithParts(nil), memory.messages...), nil
}
func (memory *memorySessions) Get(context.Context, string) (Session, error) {
	return memory.session, nil
}
func (memory *memorySessions) SetRevert(
	_ context.Context, input SetRevertInput,
) error {
	memory.set = append(memory.set, input)
	memory.session.Revert = &input.Revert
	memory.session.Summary = input.Summary
	return nil
}
func (memory *memorySessions) ClearRevert(context.Context, string) error {
	memory.clears++
	memory.session.Revert = nil
	return nil
}

type idleState struct{ err error }

func (state idleState) AssertNotBusy(string) error { return state.err }

type summaryComputer struct {
	seen  []msgmodel.WithParts
	diffs []msgmodel.FileDiff
}

func (summary *summaryComputer) ComputeDiff(
	_ context.Context, messages []msgmodel.WithParts,
) ([]msgmodel.FileDiff, error) {
	summary.seen = append([]msgmodel.WithParts(nil), messages...)
	return summary.diffs, nil
}

type failingStore struct{ calls int }

func (store *failingStore) Write([]string, any) error {
	store.calls++
	return errors.New("ignored")
}

type recordingPublisher struct{ events []bus.Payload }

func (publisher *recordingPublisher) Publish(
	def bus.Definition, properties any, _ ...bus.PublishOptions,
) {
	publisher.events = append(publisher.events, bus.Payload{
		Type: def.Type, Properties: properties,
	})
}

type syncCall struct {
	event string
	value any
}
type recordingSync struct{ calls []syncCall }

func (syncer *recordingSync) Run(_ context.Context, event string, value any) error {
	syncer.calls = append(syncer.calls, syncCall{event, value})
	return nil
}

func user(id string, parts ...msgmodel.Part) msgmodel.WithParts {
	return msgmodel.WithParts{
		Info: msgmodel.User{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: "s"},
		},
		Parts: parts,
	}
}

func assistant(id string, parts ...msgmodel.Part) msgmodel.WithParts {
	return msgmodel.WithParts{
		Info: msgmodel.Assistant{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: "s"},
		},
		Parts: parts,
	}
}

func text(id string) msgmodel.TextPart {
	return msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: id}}
}

func TestRevertBoundaryDiffAndIgnoredStorageFailure(t *testing.T) {
	sessions := &memorySessions{
		session: Session{ID: "s"},
		messages: []msgmodel.WithParts{
			user("msg_10", text("p1")),
			assistant("msg_2", text("p2")),
			user("msg_3", text("p3")),
		},
	}
	summary := &summaryComputer{diffs: []msgmodel.FileDiff{
		{File: "a", Additions: 2, Deletions: 1},
		{File: "b", Additions: 3, Deletions: 4},
	}}
	store := &failingStore{}
	publisher := &recordingPublisher{}
	service := New(Dependencies{
		Sessions: sessions, State: idleState{}, Summary: summary,
		Storage: store, Bus: publisher,
	})
	result, err := service.Revert(context.Background(), Input{
		SessionID: "s", MessageID: "msg_2",
	})
	if err != nil || result.Revert == nil || result.Revert.MessageID != "msg_10" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got := messageIDs(summary.seen); !reflect.DeepEqual(
		got, []string{"msg_10", "msg_2", "msg_3"},
	) {
		t.Fatalf("lexical range=%v", got)
	}
	if result.Summary != (Summary{Additions: 5, Deletions: 5, Files: 2}) ||
		store.calls != 1 || len(publisher.events) != 1 {
		t.Fatalf("result=%#v store=%d events=%#v", result, store.calls, publisher.events)
	}
}

func TestRevertCannotTargetMessageWithoutParts(t *testing.T) {
	sessions := &memorySessions{
		session: Session{ID: "s"},
		messages: []msgmodel.WithParts{
			user("u1", text("p1")), assistant("empty"),
		},
	}
	service := New(Dependencies{
		Sessions: sessions, State: idleState{}, Summary: &summaryComputer{},
	})
	result, err := service.Revert(context.Background(), Input{
		SessionID: "s", MessageID: "empty",
	})
	if err != nil || result.Revert != nil || len(sessions.set) != 0 {
		t.Fatalf("result=%#v set=%#v err=%v", result, sessions.set, err)
	}
}

func TestCleanupRemovesForwardMessagesAndSelectedPart(t *testing.T) {
	partID := "b"
	sessions := &memorySessions{
		session: Session{ID: "s", Revert: &RevertInfo{
			MessageID: "m2", PartID: &partID,
		}},
		messages: []msgmodel.WithParts{
			user("m1", text("a")),
			assistant("m2", text("a"), text("b"), text("c")),
			assistant("m3", text("d")),
		},
	}
	syncer := &recordingSync{}
	service := New(Dependencies{Sessions: sessions, Sync: syncer})
	if err := service.Cleanup(context.Background(), sessions.session); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, call := range syncer.calls {
		switch value := call.value.(type) {
		case msgmodel.RemovedEvent:
			got = append(got, call.event+":"+value.MessageID)
		case msgmodel.PartRemovedEvent:
			got = append(got, call.event+":"+value.PartID)
		}
	}
	want := []string{
		"message.removed:m3",
		"message.part.removed:b",
		"message.part.removed:c",
	}
	if !reflect.DeepEqual(got, want) || sessions.clears != 1 {
		t.Fatalf("calls=%v clears=%d", got, sessions.clears)
	}
}

func TestBusyStopsBeforeReads(t *testing.T) {
	want := errors.New("busy")
	service := New(Dependencies{State: idleState{err: want}})
	_, err := service.Revert(context.Background(), Input{SessionID: "s"})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}

func messageIDs(messages []msgmodel.WithParts) []string {
	out := make([]string, len(messages))
	for i, message := range messages {
		out[i] = message.Info.MessageID()
	}
	return out
}
