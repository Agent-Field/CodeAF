package todo

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
)

type recordingPublisher struct {
	events []bus.Payload
}

func (publisher *recordingPublisher) Publish(
	def bus.Definition, properties any, _ ...bus.PublishOptions,
) {
	publisher.events = append(publisher.events, bus.Payload{
		Type: def.Type, Properties: properties,
	})
}

func TestUpdateReplacesInOrderAndPublishes(t *testing.T) {
	repository := NewMemoryRepository()
	publisher := &recordingPublisher{}
	service := New(repository, publisher)
	input := UpdateInput{
		SessionID: "s1",
		Todos: []Info{
			{Content: "second", Status: "anything", Priority: "urgent"},
			{Content: "first", Status: "", Priority: ""},
		},
	}
	if err := service.Update(input); err != nil {
		t.Fatal(err)
	}
	got, err := service.Get("s1")
	if err != nil || !reflect.DeepEqual(got, input.Todos) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "todo.updated" ||
		!reflect.DeepEqual(publisher.events[0].Properties, input) {
		t.Fatalf("events=%#v", publisher.events)
	}
	if err := service.Update(UpdateInput{SessionID: "s1", Todos: []Info{}}); err != nil {
		t.Fatal(err)
	}
	got, _ = service.Get("s1")
	if len(got) != 0 {
		t.Fatalf("empty replace retained todos: %#v", got)
	}
}

type failingRepository struct{}

func (failingRepository) Replace(string, []Info) error { return errors.New("database") }
func (failingRepository) Get(string) ([]Info, error)   { return nil, errors.New("database") }

func TestDatabaseFailurePreventsEvent(t *testing.T) {
	publisher := &recordingPublisher{}
	service := New(failingRepository{}, publisher)
	if err := service.Update(UpdateInput{SessionID: "s"}); err == nil {
		t.Fatal("expected repository error")
	}
	if len(publisher.events) != 0 {
		t.Fatalf("events=%#v", publisher.events)
	}
}
