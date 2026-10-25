package clientrun

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A crew is held by the executor leasing its manager conversation. Crew ids
// are team ids, never directory cell ids or a client-supplied chat scope.
func (r *Runner) crewSnapshots(method string, held []string) ([]Envelope, error) {
	result, err := appwire.Call(r.cfg.Engine, "crews.list", nil)
	if err != nil {
		return nil, err
	}
	crews := slices.DeleteFunc(slices.Clone(result.(*appwire.CrewsResult).Crews), func(c appwire.Crew) bool { return !slices.Contains(held, c.Manager) })
	if method == "crews.list" {
		return []Envelope{{Result: appwire.CrewsResult{Crews: crews}}}, nil
	}
	out := make([]Envelope, 0, len(crews))
	for _, crew := range crews {
		params := mustJSON(appwire.IDParams{ID: crew.ID})
		if method == "crews.log" {
			params = mustJSON(map[string]any{"id": crew.ID, "limit": phoneEntryLimit})
		}
		result, err := appwire.Call(r.cfg.Engine, method, params)
		if err != nil {
			return nil, err
		}
		out = append(out, Envelope{Crew: crew.ID, Result: result})
	}
	return out, nil
}

func (r *Runner) crewOwner(id string) (string, error) {
	crews, ok := r.cfg.Engine.(appwire.Crewer)
	if !ok {
		return "", errors.New("engine does not serve crews")
	}
	crew, err := crews.CrewGet(id)
	if err != nil {
		return "", err
	}
	if crew.ID != id || crew.Manager == "" {
		return "", errors.New("crew has no manager")
	}
	return crew.Manager, nil
}

func (r *Runner) crewRoute(ctx context.Context, call Call) (string, error) {
	listing, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return "", err
	}
	held := r.held(listing)
	var agent string
	var ids []string
	switch call.Method {
	case "crews.message":
		var p appwire.CrewMessage
		if err = json.Unmarshal(call.Params, &p); err != nil {
			return "", err
		}
		agent, ids, err = r.crewMessageScope(p)
	case "crews.decide":
		var p struct {
			Packet string `json:"packet"`
		}
		if err = json.Unmarshal(call.Params, &p); err != nil {
			return "", err
		}
		return r.crewDecisionAgent(held, p.Packet)
	case "crews.create":
		var p appwire.CrewCreate
		if err = json.Unmarshal(call.Params, &p); err != nil {
			return "", err
		}
		ids, err = r.crewCreationScope(held, p)
	default:
		return "", errors.New("unsupported queued crew method")
	}
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if !slices.Contains(held, id) {
			return "", errors.New("queued chat is not leased by this executor")
		}
	}
	return agent, nil
}

func (r *Runner) crewMessageScope(p appwire.CrewMessage) (string, []string, error) {
	owner, err := r.crewOwner(p.Crew)
	if err != nil {
		return "", nil, err
	}
	if p.To == "" || p.To == "manager" {
		return owner, []string{owner}, nil
	}
	crew, err := r.cfg.Engine.(appwire.Crewer).CrewGet(p.Crew)
	if err != nil {
		return "", nil, err
	}
	for _, member := range crew.Members {
		if member.Agent == p.To {
			return p.To, []string{owner, p.To}, nil
		}
	}
	return "", nil, errors.New("queued crew message target is not a member")
}

func (r *Runner) crewDecisionAgent(held []string, packet string) (string, error) {
	for _, agent := range held {
		asks, err := r.cfg.Engine.ListAsks(agent)
		if err != nil {
			return "", err
		}
		for _, ask := range asks {
			if ask.Agent == agent && ask.ID == "crew:"+packet && ask.Subject != nil && ask.Subject.Kind == appwire.SubjectCrewDecision && ask.Subject.Ref == packet {
				return agent, nil
			}
		}
	}
	return "", errors.New("queued crew decision is no longer open on this executor")
}

func (r *Runner) crewCreationScope(held []string, p appwire.CrewCreate) ([]string, error) {
	ids := slices.Clone(p.Agents)
	if p.Manager != "" {
		ids = append(ids, p.Manager)
	}
	if p.Parent != "" {
		owner, err := r.crewOwner(p.Parent)
		if err != nil {
			return nil, err
		}
		ids = append(ids, owner)
	}
	if p.From != "proposal" {
		return ids, nil
	}
	proposal, err := r.heldProposal(held, p.Proposal)
	if err != nil {
		return nil, err
	}
	ids = append(ids, proposal.Agent)
	existing := proposal.Existing
	if p.Edits != nil && p.Edits.Existing != nil {
		existing = p.Edits.Existing
	}
	return append(ids, existing...), nil
}

func (r *Runner) heldProposal(held []string, id string) (*appwire.Proposal, error) {
	for _, agent := range held {
		asks, err := r.cfg.Engine.ListAsks(agent)
		if err != nil {
			return nil, err
		}
		for _, ask := range asks {
			if p := ask.Proposal; p != nil && p.ID == id && p.Agent == agent {
				return p, nil
			}
		}
	}
	return nil, errors.New("queued crew proposal is no longer open on this executor")
}

// Keep the crew engine's optional start capability while recording its new
// manager with the same crash and lease boundary as agents.create.
type crewCreationRecorder struct {
	creationRecorder
	appwire.Crewer
	starter appwire.Engine
}

func (e crewCreationRecorder) CrewStart(ask appwire.CrewCreate) (appwire.CrewStartResult, error) {
	starter, ok := e.starter.(appwire.CrewStarter)
	if !ok {
		return appwire.CrewStartResult{}, appwire.Errorf(appwire.CodeUnknownMethod, "engine does not start crew managers")
	}
	ask.BeforeMint = e.reserve
	ask.BeforeStart = e.record
	return starter.CrewStart(ask)
}
