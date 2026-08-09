package status

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
)

type recordingPublisher struct {
	service *Service
	events  []bus.Payload
	seen    []Info
}

func (publisher *recordingPublisher) Publish(
	def bus.Definition, properties any, _ ...bus.PublishOptions,
) {
	publisher.events = append(publisher.events, bus.Payload{
		Type: def.Type, Properties: properties,
	})
	if publisher.service != nil {
		sessionID := properties.(interface{ session() string }).session()
		publisher.seen = append(publisher.seen, publisher.service.Get(sessionID))
	}
}

func (value StatusProperties) session() string { return value.SessionID }
func (value IdleProperties) session() string   { return value.SessionID }

func TestServicePublishesBeforeMutatingAndDeletesIdle(t *testing.T) {
	publisher := &recordingPublisher{}
	service := New(publisher)
	publisher.service = service
	service.Set("s1", Info{Type: "busy"})
	service.Set("s1", Info{Type: "idle"})
	if got := service.Get("s1"); got.Type != "idle" {
		t.Fatalf("status=%#v", got)
	}
	if got := eventTypes(publisher.events); !reflect.DeepEqual(
		got, []string{"session.status", "session.status", "session.idle"},
	) {
		t.Fatalf("events=%v", got)
	}
	if got := publisher.seen; len(got) != 3 || got[0].Type != "idle" ||
		got[1].Type != "busy" || got[2].Type != "busy" {
		t.Fatalf("status observed by subscribers=%#v", got)
	}
}

func TestListIsCopyAndValidation(t *testing.T) {
	service := New(nil)
	service.Set("s1", Info{Type: "retry", Attempt: 0, Next: 1})
	list := service.List()
	delete(list, "s1")
	if service.Get("s1").Type != "retry" {
		t.Fatal("List exposed the state map")
	}
	for _, value := range []Info{
		{Type: "idle"}, {Type: "busy"}, {Type: "retry", Attempt: 0, Next: 0},
	} {
		if !Validate(value) {
			t.Fatalf("valid status rejected: %#v", value)
		}
	}
	if Validate(Info{Type: "retry", Attempt: -1}) || Validate(Info{Type: "other"}) {
		t.Fatal("invalid status accepted")
	}
}

func eventTypes(events []bus.Payload) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.Type
	}
	return out
}
