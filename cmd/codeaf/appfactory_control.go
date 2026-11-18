package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func (b *appBackend) FactoryJobControl(ctx context.Context, p appwire.FactoryJobControlParams) (appwire.FactoryJobActed, error) {
	if p.Mode == factory.AutonomyAuto || p.Mode == factory.AutonomyManual {
		return b.jobWrite(ctx, p.Factory, "set_"+p.Mode, p.Job, "control", struct {
			Mode   string      `json:"mode"`
			Expect *api.Expect `json:"expect"`
		}{p.Mode, p.Expect})
	}
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryJobActed{}, err
	}
	it, err := host.floor.Get(p.Job)
	if err != nil {
		return appwire.FactoryJobActed{}, err
	}
	// One rule for the job page and for the Factory manager's press of the
	// same door (factory_foreman_press.go).
	if p.Mode == "resume_pipeline" && workingChatRunning(it) {
		return appwire.FactoryJobActed{}, appwire.Errorf(appwire.CodeConflict, "%s", workingChatBusy)
	}
	out, err := b.jobWrite(ctx, p.Factory, p.Mode, p.Job, "control", struct {
		Mode   string      `json:"mode"`
		Words  string      `json:"words,omitempty"`
		Expect *api.Expect `json:"expect"`
	}{p.Mode, p.Words, p.Expect})
	if err == nil && p.Mode == "take_over" {
		working := stageAgentsWhere(func(agent *session.Agent) bool {
			owner, ok := readFactoryOwner(agent.SessionPath())
			return ok && owner.Job == p.Job && owner.Root == chatKey(host.floor.Root())
		})
		for _, agent := range working {
			file := agent.SessionPath()
			owner, _ := readFactoryOwner(file)
			agent.Interrupt()
			if closeErr := agent.Close(); closeErr != nil {
				return out, closeErr
			}
			_ = host.floor.Update(p.Job, func(cur *factory.Item) error {
				if !cur.Manual {
					return nil
				}
				if cur.Stream != nil {
					for i := range cur.Stream.Phases {
						if cur.Stream.Phases[i].Name == owner.Step {
							cur.Stream.Phases[i].Chat = file
						}
					}
				}
				return nil
			})
			_ = b.CloseAgent(appAgentID(file))
		}
	}

	// The HTTP route accepts only its own payload, not the factory/job envelope.
	return out, err
}
