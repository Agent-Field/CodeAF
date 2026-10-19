package appwire

import (
	"context"
	"encoding/json"
)

// ── factory.job_chats: a job's conversations, on the job's own page ───────
//
// A factory job has conversations: its agent (the item's own conversation,
// which the run reports into and which reads what the person types as the
// brief before a run and the steer during one) and one per step it ran. The
// app shows them on the job's page rather than as a team of their own, so a
// person talks to the job's agent where the job is, and may open any step's
// chat to message that step directly.
//
// IT IS THIS MACHINE'S FACTORY ONLY. The conversations live where the job
// runs, which for the factory this engine serves is this engine; any other
// factory is refused, because its chats are on the machine running its jobs.
//
// Reading makes the job's agent when the job has none yet (no model call, no
// job started), and holds every answered conversation open so the app can draw
// it at once. A step that has not run yet has no chat and is not listed.

// FeatureFactoryJobChats is announced in hello when the engine serves
// factory.job_chats.
const FeatureFactoryJobChats = "factory.job_chats"

// FactoryJobChatsParams names one job of one factory.
type FactoryJobChatsParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
}

// FactoryJobChats is a job's conversations: Agent is the job's agent, and
// Steps the latest chat of each step that ran, in the run's order. Each is an
// agent id the app opens as it opens any conversation.
type FactoryJobChats struct {
	Factory string               `json:"factory"`
	Job     int                  `json:"job"`
	Agent   string               `json:"agent"`
	Steps   []FactoryJobStepChat `json:"steps"`
}

// FactoryJobStepChat is one step's latest chat.
type FactoryJobStepChat struct {
	Step  string `json:"step"`
	Agent string `json:"agent"`
}

// FactoryJobChatter is the engine's half of factory.job_chats.
type FactoryJobChatter interface {
	FactoryJobChats(ctx context.Context, p FactoryJobChatsParams) (FactoryJobChats, error)
}

func withFactoryJobChatsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobChatter); ok {
		hello = withFeature(hello, FeatureFactoryJobChats)
	}
	return hello
}

func prepJobChats(p *FactoryJobChatsParams) error {
	id, err := factoryID(p.Factory)
	if err != nil {
		return err
	}
	p.Factory = id
	if p.Job <= 0 {
		return Errorf(CodeBadParams, "job must be a job's number")
	}
	return nil
}

func init() {
	methods["factory.job_chats"] = shaped[FactoryJobChatsParams, FactoryJobChats](jobChats)
}

func jobChats(e Engine, raw json.RawMessage) (*FactoryJobChats, error) {
	const name = "factory.job_chats"
	c, ok := e.(FactoryJobChatter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobChatsParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	if err := prepJobChats(&p); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := c.FactoryJobChats(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
