package serve

import (
	"net/http"
	"slices"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── A PERSON WHO LEAVES LEAVES NOTHING ORPHANED ────────────────────────────
//
// When the operator removes a person from the organization, every workstream
// that person owned is given to an heir: the person named by ?heir=, or the
// operator. Without one, a workstream would keep an owner who can never sign
// in again, whose budget and go-aheads nobody could carry. The person is also
// taken off every share. Each workstream that changes publishes its event, so
// every desk that shows it redraws with its new owner.
//
// A PRIVATE WORKSTREAM STAYS PRIVATE, now its heir's: it is still one
// person's, and the heir is the one person who may decide what happens to it.

// workstreamHeir is who a leaving person's workstreams go to: ?heir= when the
// operator names someone, else the operator. ok is false, with the refusal
// written, when nobody can take them and the person owns any.
func (h *Handler) workstreamHeir(w http.ResponseWriter, r *http.Request, leaving string) (string, bool) {
	heir := factory.NormalizeHandle(r.URL.Query().Get("heir"))
	if heir != "" && !personHandle.MatchString(heir) {
		h.fail(w, http.StatusUnprocessableEntity, "heir must be a GitHub handle")
		return "", false
	}
	if heir == "" {
		heir = h.operatorLogin(r)
	}
	if heir == leaving {
		heir = ""
	}
	if heir != "" || h.config.Store == nil {
		return heir, true
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return "", false
	}
	for _, ws := range all {
		if ws.Owner.Kind == api.OwnerPerson && ws.Owner.ID == leaving {
			h.fail(w, http.StatusUnprocessableEntity, "this person owns workstreams; name who takes them with ?heir=<handle>")
			return "", false
		}
	}
	return "", true
}

// handOnWorkstreams gives every workstream leaving owns to heir and takes
// leaving off every share, in one save, and answers how many it gave.
func (h *Handler) handOnWorkstreams(leaving, heir, by string) (int, error) {
	if h.config.Store == nil || leaving == "" {
		return 0, nil
	}
	now := h.clock().UTC()
	// THE LEAVING PERSON'S AGENTS GO WITH THEIR SPACE: to the heir, still
	// private to whoever holds them, or with no heir they are deleted. The
	// workstreams they shared one into keep it, now shared by the heir.
	theirs := map[string]bool{}
	if _, err := h.config.Store.EditOrgAgents(func(agents factory.OrgAgents) error {
		for id, a := range agents {
			if factory.NormalizeHandle(a.Owner) != leaving {
				continue
			}
			theirs[id] = true
			if heir == "" {
				delete(agents, id)
				continue
			}
			a.Owner, a.UpdatedAt = heir, now
			if a.Roster == factory.AgentRosterAsked {
				a.Roster = factory.AgentRosterMine
			}
			agents[id] = a
		}
		return nil
	}); err != nil {
		return 0, err
	}
	befores := map[string]factory.OrgWorkstream{}
	given := 0
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		var changed []string
		for id, ws := range all {
			before := ws
			links := slices.Clone(ws.Agents)
			for i := 0; i < len(links); i++ {
				if !theirs[links[i].Agent] || factory.NormalizeHandle(links[i].By) != leaving {
					continue
				}
				if heir == "" {
					links = slices.Delete(links, i, i+1)
					i--
					continue
				}
				links[i].By = heir
			}
			ws.Agents = links
			owned := ws.Owner.Kind == api.OwnerPerson && ws.Owner.ID == leaving
			if owned && heir != "" {
				ws.Owner = api.WorkstreamOwner{Kind: api.OwnerPerson, ID: heir}
				given++
				// A LEAVING PERSON'S SPACE GOES TO THE HEIR WHOLE, as one
				// private workstream named for them: the heir keeps their own
				// space, and this is the only door into someone else's
				// (org-space DESIGN §2.1).
				if ws.Space {
					ws.Space, ws.Name = false, leaving+"'s space"
				}
			}
			ws.Shares = withoutOwnerShare(slices.DeleteFunc(slices.Clone(ws.Shares), func(s factory.Share) bool {
				return s.Kind == factory.ShareToPerson && s.ID == leaving
			}), ws.Owner)
			if ws.Owner != before.Owner || !slices.Equal(ws.Shares, before.Shares) || !slices.Equal(ws.Agents, before.Agents) {
				ws.UpdatedAt = now
				all[id] = ws
				befores[id] = before
				changed = append(changed, id)
			}
		}
		return changed, nil
	})
	if err != nil {
		return 0, err
	}
	for id, before := range befores {
		h.publishWorkstreams(api.EventWorkstreamChanged, by, &before, saved[id])
	}
	return given, nil
}
