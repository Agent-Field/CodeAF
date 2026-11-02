package appwire

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"strings"
)

// FeatureFactoryJobRunAsNew starts an ended job's request as a separate job.
const FeatureFactoryJobRunAsNew = "factory.job_run_as_new"

// JobActRunAsNew names the action whose receipt names the new job.
const JobActRunAsNew = "run_as_new"

// FactoryJobRunAsNewParams names the source and, optionally, the new assignee.
// Expect guards the source read, like every other job action.
type FactoryJobRunAsNewParams struct {
	Factory  string      `json:"factory"`
	Job      int         `json:"job"`
	Assignee *string     `json:"assignee,omitempty"`
	Expect   *api.Expect `json:"expect,omitempty"`
}

// FactoryJobNewRunner is the optional engine door for running a copy.
type FactoryJobNewRunner interface {
	FactoryJobRunAsNew(context.Context, FactoryJobRunAsNewParams) (FactoryJobActed, error)
}

func prepRunAsNew(p *FactoryJobRunAsNewParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	if p.Assignee != nil {
		login := strings.TrimPrefix(strings.TrimSpace(*p.Assignee), "@")
		if !factoryLogin.MatchString(login) {
			return Errorf(CodeBadParams, "assignee must be a GitHub login")
		}
		p.Assignee = &login
	}
	return nil
}

func init() {
	methods[FeatureFactoryJobRunAsNew] = factoryAction(FeatureFactoryJobRunAsNew, prepRunAsNew,
		func(a FactoryActor, ctx context.Context, p FactoryJobRunAsNewParams) (FactoryJobActed, error) {
			runner, ok := a.(FactoryJobNewRunner)
			if !ok {
				return FactoryJobActed{}, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobRunAsNew)
			}
			return runner.FactoryJobRunAsNew(ctx, p)
		})
}
