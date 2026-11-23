package appwire

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── REJECTING AN APPROVAL, ON AN ATTACHED FACTORY ───────────────────────────
//
// factory.job_reject is the app's Reject on a job waiting for an approval: the
// person's no that ENDS the job, with their words. It is carried to the
// factory's own typed route, POST /v1/jobs/{id}/reject. It is not
// factory.job_reject_plan, which sends a plan back to be planned again, and
// not factory.job_stop, which leaves a job stopped with a way to carry on.
//
// THE FACTORY DECIDES, ON ITS LOCKED DOCUMENT, whether the job still waits on
// an approval and whether the caller may answer it; a job that moved since the
// version the person read is refused as [CodeStale]. A rejected job is done:
// closed here with its work and spending kept, its words on it, out of Needs
// you, and out of reach of every rule that starts jobs. Reopen is the way back.

// FeatureFactoryJobReject is announced in hello when the engine serves
// factory.job_reject.
const FeatureFactoryJobReject = "factory.job_reject"

// JobActReject is the action a [FactoryJobActed] names for a rejection.
const JobActReject = "reject"

// FactoryJobRejectParams rejects what one job waits on its person to approve.
// Words are optional and at most [api.MaxRejectPlanWords] characters;
// Expect.Version is required.
type FactoryJobRejectParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Words   string      `json:"words,omitempty"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryRejecter is the engine's half of factory.job_reject.
type FactoryRejecter interface {
	FactoryJobReject(ctx context.Context, p FactoryJobRejectParams) (FactoryJobActed, error)
}

func withFactoryJobRejectFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRejecter); ok {
		hello = withFeature(hello, FeatureFactoryJobReject)
	}
	return hello
}

func init() {
	methods["factory.job_reject"] = shaped[FactoryJobRejectParams, FactoryJobActed](rejectJob)
}

func rejectJob(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	const name = "factory.job_reject"
	r, ok := e.(FactoryRejecter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobRejectParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return nil, err
	}
	var err error
	if p.Words, err = words("words", p.Words, api.MaxRejectPlanWords, false); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryJobReject(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
