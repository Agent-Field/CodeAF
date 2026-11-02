package appwire

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── factory.job_brief_write: write a job's brief now ──────────────────────
//
// The brief's writer is a small model call on the job's agent (role brief, on
// the small-work tier the app's small-jobs model pins), never the manager's
// chat turn. The engine writes on its own after a step reports and when an
// ask opens, each job's burst of news taken as one after
// [factory.BriefDebounce]. This method is the page's Retry: one write now,
// whatever the cost guard says, unless one is already on its way.
//
// THE FACTORY DECIDES WHAT IS KEPT. The engine hands the factory the model's
// answer as it came; the factory checks it against the job (the palette's
// limits, evidence that exists, every number in its evidence) and keeps what
// passes. Drops are answered and counted on the version. The call's cost is
// booked to the job's spend.
//
// IT IS THIS MACHINE'S FACTORY ONLY, like factory.job_chats: the writer runs
// on the job's agent, which lives where the job runs.

// BriefWriteBudget is the whole write's wall: the writer's own deadline and
// the factory's begin and keep around it.
const BriefWriteBudget = BriefWriterDeadline + 2*factoryCallBudget

// BriefWriterDeadline is the writer's model call's own wall.
const BriefWriterDeadline = 45 * time.Second

// FeatureFactoryJobBriefWriter is announced in hello when the engine writes
// its factory's briefs and serves factory.job_brief_write.
const FeatureFactoryJobBriefWriter = "factory.job_brief_writer"

// FactoryJobBriefWriteParams names one job of this machine's factory.
type FactoryJobBriefWriteParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
}

// FactoryJobBriefWritten is how a write ended. Skip says why none began
// (another is on its way); otherwise Kept says a version was kept, Drops are
// the parts of the answer its check dropped, and Model and CostUSD are the
// call's. Current is the job's brief as it then reads.
type FactoryJobBriefWritten struct {
	Skip    string              `json:"skip,omitempty" enum:"unchanged,cost,writing"`
	Kept    bool                `json:"kept"`
	Drops   []factory.BriefDrop `json:"drops"`
	Model   string              `json:"model,omitempty"`
	CostUSD float64             `json:"cost_usd,omitempty"`
	Current FactoryJobBrief     `json:"current"`
}

// FactoryJobBriefWriter is the engine's half of factory.job_brief_write.
type FactoryJobBriefWriter interface {
	FactoryJobBriefWrite(ctx context.Context, p FactoryJobBriefWriteParams) (FactoryJobBriefWritten, error)
}

func withFactoryJobBriefWriterFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobBriefWriter); ok {
		hello = withFeature(hello, FeatureFactoryJobBriefWriter)
	}
	return hello
}

func init() {
	methods["factory.job_brief_write"] = shaped[FactoryJobBriefWriteParams, FactoryJobBriefWritten](jobBriefWrite)
}

func jobBriefWrite(e Engine, raw json.RawMessage) (*FactoryJobBriefWritten, error) {
	const name = "factory.job_brief_write"
	w, ok := e.(FactoryJobBriefWriter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobBriefWriteParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	p.Factory = id
	if p.Job <= 0 {
		return nil, Errorf(CodeBadParams, "job must be a job's number")
	}
	// The write carries a model call, so it has the errand's longest wall and
	// the factory's two calls around it, not one factory call's.
	ctx, cancel := context.WithTimeout(context.Background(), BriefWriteBudget)
	defer cancel()
	out, err := w.FactoryJobBriefWrite(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
