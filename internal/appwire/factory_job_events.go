package appwire

// Job outcomes are finished, merged, failed and needs-you transitions kept by
// the Factory. The cursor is the Factory's. A factory.event push is a prompt
// copy of an event the app can always read again from its own cursor. A reset
// means read the Factory's current state once, then continue from this cursor.

import (
	"context"
	"reflect"
	"time"
)

// FeatureFactoryJobEvents announces the durable outcome read and push.
const FeatureFactoryJobEvents = "factory.job_events"

// EventFactoryEvent is one outcome from a followed Factory connection.
const EventFactoryEvent = "factory.event"

// DefaultFactoryEvents bounds a read when no limit is supplied.
const DefaultFactoryEvents = 100

// MaxFactoryEvents is the most outcomes one read may request.
const MaxFactoryEvents = 500

// FactoryEventsSinceParams identifies the Factory and its optional outcome cursor.
type FactoryEventsSinceParams struct {
	Factory string `json:"factory"`
	After   *int64 `json:"after,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Follow  bool   `json:"follow,omitempty"`
}

// FactoryEvent is a durable outcome with the app's Factory connection id.
type FactoryEvent struct {
	Factory  string    `json:"factory"`
	Seq      int64     `json:"seq"`
	Kind     string    `json:"kind" enum:"finished,merged,failed,needs_you"`
	Job      int       `json:"job"`
	At       time.Time `json:"at"`
	Title    string    `json:"title,omitempty"`
	Group    string    `json:"group,omitempty"`
	How      string    `json:"how,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Need     string    `json:"need,omitempty"`
	Code     string    `json:"code,omitempty"`
	Step     string    `json:"step,omitempty"`
	PRNumber int       `json:"pr_number,omitempty"`
	PRURL    string    `json:"pr_url,omitempty"`
}

// FactoryEvents carries visible outcomes and the Factory's next read cursor.
type FactoryEvents struct {
	Factory   string         `json:"factory"`
	Supported bool           `json:"supported"`
	Events    []FactoryEvent `json:"events"`
	Cursor    int64          `json:"cursor"`
	Reset     bool           `json:"reset"`
	More      bool           `json:"more"`
}

// FactoryJobEventsReader is the engine's half of factory.events_since.
type FactoryJobEventsReader interface {
	FactoryEventsSince(ctx context.Context, p FactoryEventsSinceParams) (FactoryEvents, error)
}

func withFactoryJobEventsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobEventsReader); ok {
		hello = withFeature(hello, FeatureFactoryJobEvents)
	}
	return hello
}

func init() {
	methods["factory.events_since"] = typed(factoryEventsSince)
	eventShapes[EventFactoryEvent] = reflect.TypeFor[FactoryEvent]()
}

func factoryEventsSince(e Engine, p FactoryEventsSinceParams) (*FactoryEvents, error) {
	s, ok := e.(FactoryJobEventsReader)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.events_since")
	}
	var err error
	if p.Factory, err = factoryID(p.Factory); err != nil {
		return nil, err
	}
	if p.After != nil && *p.After < 0 {
		return nil, Errorf(CodeBadParams, "after must be an integer from 0")
	}
	if p.Limit < 0 || p.Limit > MaxFactoryEvents {
		return nil, Errorf(CodeBadParams, "limit must be from 1 to %d, or 0 for the default", MaxFactoryEvents)
	}
	if p.Limit == 0 {
		p.Limit = DefaultFactoryEvents
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryEventsSince(ctx, p)
	if err != nil {
		return nil, err
	}
	if out.Events == nil {
		out.Events = []FactoryEvent{}
	}
	return &out, nil
}
