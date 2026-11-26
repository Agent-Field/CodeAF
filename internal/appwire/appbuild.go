package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

const EventAppsBuild = "apps.build"

// AppDraftInboxPrefix identifies an Inbox destination backed by an editing job.
const AppDraftInboxPrefix = "app-draft:"

// AppBuilds is the engine door shared by the desktop and app assistant.
type AppBuilds interface {
	AppBuilds() (*builder.Jobs, error)
}

type appNewParams struct {
	Name string `json:"name,omitempty"`
}

type draftParams struct {
	Draft string `json:"draft"`
}

type appTargetInput struct {
	builder.Target
	NodeID string `json:"node_id,omitempty"`
}

func (in *appTargetInput) target() *builder.Target {
	if in == nil {
		return nil
	}
	target := in.Target
	// Normalize the Desk request without changing older requests or the saved mark format.
	if in.NodeID != "" {
		target.Node = in.NodeID
	}
	return &target
}

type appSayParams struct {
	Draft  string          `json:"draft"`
	Text   string          `json:"text"`
	Target *appTargetInput `json:"target,omitempty"`
}

type appChangeParams struct {
	Target  *builder.Target `json:"target,omitempty"`
	App     string          `json:"app"`
	Request string          `json:"request"`
}

type appRestoreParams struct {
	Draft string `json:"draft"`
	ID    string `json:"id"`
}

type appCheckpointsResult struct {
	Checkpoints []builder.Checkpoint `json:"checkpoints"`
}

func init() {
	eventShapes[EventAppsBuild] = reflect.TypeFor[builder.Event]()
	methods["apps.new"] = buildCall(func(b *builder.Jobs, p appNewParams) (*draftParams, error) {
		draft, err := b.NewDraft(p.Name)
		return &draftParams{Draft: draft}, err
	})
	methods["apps.mark"] = buildCall(func(b *builder.Jobs, p appSayParams) (*Empty, error) {
		if p.Target == nil {
			return nil, Errorf(CodeBadParams, "Choose the element to mark.")
		}
		return done(b.Mark(p.Draft, p.Text, *p.Target.target()))
	})
	methods["apps.presence"] = buildCall(func(b *builder.Jobs, p draftParams) (*builder.Presence, error) {
		state, err := b.State(p.Draft)
		return state.Presence, err
	})
	methods["apps.check"] = buildCall(func(b *builder.Jobs, p draftParams) (*builder.State, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 295*time.Second)
		defer cancel()
		state, err := b.Check(ctx, p.Draft)
		return &state, err
	})
	methods["apps.reference"] = typed(func(_ Engine, _ Empty) (*string, error) {
		ref := builder.Reference()
		return &ref, nil
	})
	methods["apps.parts"] = typed(func(_ Engine, p struct {
		For string `json:"for,omitempty"`
	}) (*json.RawMessage, error) {
		ref, err := builder.Parts(p.For)
		if err != nil {
			return nil, Errorf(CodeBadParams, "%s", err)
		}
		return &ref, nil
	})
	methods["apps.plan"] = buildCall(func(b *builder.Jobs, p builder.PlanRequest) (*builder.PlanResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 295*time.Second)
		defer cancel()
		res, err := b.Plan(ctx, p)
		return &res, err
	})
	methods["apps.build"] = buildCall(func(b *builder.Jobs, p draftParams) (*draftParams, error) {
		return &p, b.Build(p.Draft)
	})
	methods["apps.say"] = buildCall(func(b *builder.Jobs, p appSayParams) (*Empty, error) {
		return done(b.Say(p.Draft, p.Text, p.Target.target()))
	})
	methods["apps.build_state"] = buildCall(func(b *builder.Jobs, p draftParams) (*builder.State, error) {
		res, err := b.State(p.Draft)
		return &res, err
	})
	methods["apps.checkpoints"] = buildCall(func(b *builder.Jobs, p draftParams) (*appCheckpointsResult, error) {
		res, err := b.State(p.Draft)
		return &appCheckpointsResult{Checkpoints: res.Checkpoints}, err
	})
	methods["apps.restore_checkpoint"] = buildCall(func(b *builder.Jobs, p appRestoreParams) (*Empty, error) {
		return done(b.Restore(p.Draft, p.ID))
	})
	methods["apps.preview"] = buildCall(func(b *builder.Jobs, p builder.PreviewRequest) (*builder.Preview, error) {
		res, err := b.Preview(p)
		return &res, err
	})
	methods["apps.apply"] = buildCall(func(b *builder.Jobs, p draftParams) (*appMetaResult, error) {
		res, err := b.Apply(p.Draft)
		return &appMetaResult{Meta: res}, err
	})
	methods["apps.keep"] = methods["apps.apply"]
	methods["apps.discard"] = buildCall(func(b *builder.Jobs, p draftParams) (*Empty, error) {
		return done(b.Discard(p.Draft))
	})
	methods["apps.change"] = buildCall(func(b *builder.Jobs, p appChangeParams) (*draftParams, error) {
		res, err := b.Propose(p.App, p.Request, p.Target)
		return &draftParams{Draft: res}, err
	})
}

func openBuilds(e Engine) (*builder.Jobs, error) {
	door, ok := e.(AppBuilds)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "This engine does not make apps.")
	}
	return door.AppBuilds()
}

func buildCall[P, R any](call func(*builder.Jobs, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		b, err := openBuilds(e)
		if err != nil {
			return nil, err
		}
		r, err := call(b, p)
		return r, buildError(err)
	})
}

func buildError(err error) error {
	var wire *Error
	if errors.As(err, &wire) {
		return wire
	}
	var plan *builder.PlanFailure
	if errors.As(err, &plan) {
		return Errorf(CodeEngine, "%s", plan.Message)
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrBadInput):
		return Errorf(CodeBadParams, "Check the app, request or preview selection.")
	case errors.Is(err, store.ErrNotFound):
		return Errorf(CodeNotFound, "That app, screen or step isn't available.")
	case errors.Is(err, store.ErrConflict):
		return Errorf(CodeConflict, "The app has changed or is still being worked on.")
	case errors.Is(err, context.DeadlineExceeded):
		return Errorf(CodeEngine, "The plan is taking longer. Reopen the app to see its progress.")
	default:
		return Errorf(CodeEngine, "That app step couldn't finish. Open the app to see its progress.")
	}
}
