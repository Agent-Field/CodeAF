package appwire

import (
	"reflect"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

type AppAgentAsk struct {
	App    string         `json:"app"`
	Screen string         `json:"screen"`
	Record *DataRecordRef `json:"record,omitempty"`
	Text   string         `json:"text"`
}

type AppAgentGrant struct {
	App     string `json:"app"`
	Agent   string `json:"agent"`
	Group   string `json:"group"`
	ID      string `json:"id"`
	Allowed bool   `json:"allowed"`
}

type AppAgentQuery struct {
	App    string         `json:"app"`
	Kind   string         `json:"kind"`
	Record *DataRecordRef `json:"record,omitempty"`
	Screen string         `json:"screen,omitempty"`
	Queue  string         `json:"queue,omitempty"`
	Type   string         `json:"type,omitempty"`
}

type AppAgentDuty struct {
	AppAgentAsk
	Cron string `json:"cron"`
}

type AppAgentDutyRef struct {
	App string `json:"app"`
	ID  string `json:"id"`
}

type AppAgentChanged struct {
	App  string `json:"app"`
	Kind string `json:"kind"`
}

const EventAppAgentChanged = "agents.changed"

type appAgentWork interface {
	AskAppAgent(AppAgentAsk) (runs.Run, error)
	GrantAppAgent(AppAgentGrant) error
	QueryAppAgent(AppAgentQuery) (any, error)
	AddAppAgentDuty(AppAgentDuty) (string, error)
	RemoveAppAgentDuty(AppAgentDutyRef) error
}

func init() {
	eventShapes[EventAppAgentChanged] = reflect.TypeFor[AppAgentChanged]()
	methods["agents.ask"] = typed(func(e Engine, p AppAgentAsk) (*RunStarted, error) {
		a, ok := e.(appAgentWork)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "App agents are unavailable.")
		}
		run, err := a.AskAppAgent(p)
		return &RunStarted{ID: run.ID}, err
	})
	methods["agents.grant"] = typed(func(e Engine, p AppAgentGrant) (*Empty, error) {
		a, ok := e.(appAgentWork)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "App agents are unavailable.")
		}
		return done(a.GrantAppAgent(p))
	})
	methods["agents.query"] = typed(func(e Engine, p AppAgentQuery) (*any, error) {
		a, ok := e.(appAgentWork)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "App agents are unavailable.")
		}
		result, err := a.QueryAppAgent(p)
		return &result, err
	})
	methods["agents.duty_add"] = typed(func(e Engine, p AppAgentDuty) (*RunStarted, error) {
		a, ok := e.(appAgentWork)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "App agents are unavailable.")
		}
		id, err := a.AddAppAgentDuty(p)
		return &RunStarted{ID: id}, err
	})
	methods["agents.duty_remove"] = typed(func(e Engine, p AppAgentDutyRef) (*Empty, error) {
		a, ok := e.(appAgentWork)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "App agents are unavailable.")
		}
		return done(a.RemoveAppAgentDuty(p))
	})
}
