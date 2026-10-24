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
	if p.Mode == "resume_pipeline" && it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.Chat != "" {
				if a, ok := session.LiveAgentFor(ph.Chat); ok && a.TurnRunning() {
					return appwire.FactoryJobActed{}, appwire.Errorf(appwire.CodeConflict, "The working chat is still running. Stop it or wait for it to finish before resuming.")
				}
			}
		}
	}
	out, err := b.jobWrite(ctx, p.Factory, p.Mode, p.Job, "control", struct {
		Mode   string      `json:"mode"`
		Words  string      `json:"words,omitempty"`
		Expect *api.Expect `json:"expect"`
	}{p.Mode, p.Words, p.Expect})
	if err == nil && p.Mode == "take_over" {
		var working []*session.Agent
		stageLends.mu.Lock()
		for agent := range stageLends.agents {
			if owner, ok := readFactoryOwner(agent.SessionPath()); ok && owner.Job == p.Job && owner.Root == chatKey(host.floor.Root()) {
				working = append(working, agent)
			}
		}
		stageLends.mu.Unlock()
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
