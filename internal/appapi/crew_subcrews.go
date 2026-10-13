package appapi

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func (s *Server) proposeSubCrew(w http.ResponseWriter, r *http.Request) {
	var body CrewHelpersRequest
	if !decodeCrew(w, r, &body) {
		return
	}
	if body.MemberID == "" {
		crewFailure(w, crewBad("choose the member that will manage the sub-crew"))
		return
	}
	s.proposeHelpersFor(w, r, s.options.Agents, body.MemberID, r.PathValue("cid"), body.Expect)
}
func checkCrewTreeBudgets(f *teams.File, t teams.Team, m crewMeta) error {
	budget, err := crewCents(m.Budget)
	if err != nil {
		return err
	}
	if t.Parent != "" {
		parent, ok := f.Team(t.Parent)
		if !ok {
			return crewMissing()
		}
		pm, err := readCrewMeta(parent)
		if err != nil {
			return err
		}
		pool, err := crewCents(pm.Budget)
		if err != nil {
			return err
		}
		if budget > pool*int64(config.DefaultTeamsSubSharePct)/100 {
			return crewBad("sub-crew budget cannot exceed its 50% parent share")
		}
	}
	for _, child := range f.Children(t.ID) {
		if child.Closed() {
			continue
		}
		cm, err := readCrewMeta(child)
		if err != nil {
			return err
		}
		cap, err := crewCents(cm.Budget)
		if err != nil {
			return err
		}
		if cap > budget*int64(config.DefaultTeamsSubSharePct)/100 {
			return crewBad("the crew budget must cover every sub-crew's 50% share")
		}
	}
	return nil
}
