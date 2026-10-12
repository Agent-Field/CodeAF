package serve

import (
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var personHandle = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}(\[bot\])?$`)

func itemFiles(it factory.Item) []string {
	files := append([]string(nil), it.PlannedFiles...)
	for _, file := range it.Files {
		files = append(files, file.Path)
	}
	if it.Change != nil {
		for _, file := range it.Change.Files {
			files = append(files, file.Path)
		}
	}
	return files
}

// withPeople enriches only the already-filtered snapshot, never the source copy.
func (h *Handler) withPeople(r *http.Request, s factory.Snapshot) (factory.Snapshot, map[string][]factory.CodeOwner) {
	s.Items = append([]factory.Item(nil), s.Items...)
	people := map[string]factory.PersonSummary{}
	rules := map[string][]factory.CodeOwner{}
	keys := map[string]bool{}
	if h.config.Store != nil {
		if watched, err := h.config.Store.Repos(); err == nil {
			for _, repo := range watched {
				keys[repo] = true
			}
		}
	}
	for _, source := range s.Sources {
		for _, repo := range source.Repos {
			keys[repo] = true
		}
	}
	for _, repo := range s.Repos {
		if strings.Count(repo.Name, "/") == 1 {
			keys[repo.Name] = true
		}
	}
	for _, it := range s.Items {
		if repo := itemRepo(it); repo != "" {
			keys[repo] = true
		}
	}
	if h.config.CodeOwners != nil {
		for repo := range keys {
			if !h.repoVisible(r, repo) {
				continue
			}
			if owners, err := h.config.CodeOwners(r.Context(), repo); err == nil {
				rules[repo] = owners
			}
		}
	}
	for i, it := range s.Items {
		if h.config.CodeOwners != nil {
			s.Items[i].Owners = factory.OwnersFor(rules[itemRepo(it)], itemFiles(it))
		}
		for _, handle := range factory.MentionedHandles(s.Items[i]) {
			handle = factory.NormalizeHandle(handle)
			if handle != "" {
				people[handle] = s.People[handle]
			}
		}
	}
	for handle := range people {
		h.addSummary(r, people, handle)
	}
	s.People = people
	return s, rules
}

// addSummary never lets a profile outage prevent a queue from being read.
func (h *Handler) addSummary(r *http.Request, people map[string]factory.PersonSummary, handle string) {
	handle = factory.NormalizeHandle(handle)
	if handle == "" {
		return
	}
	if _, ok := people[handle]; !ok {
		people[handle] = factory.PersonSummary{}
	}
	if h.config.PersonProfile != nil {
		profile, err := h.config.PersonProfile(r.Context(), handle)
		if err == nil {
			people[handle] = profile.PersonSummary
			return
		}
	}
	for _, member := range h.memberList() {
		if factory.NormalizeHandle(member.Handle) == handle {
			people[handle] = factory.PersonSummary{Name: member.Name, AvatarURL: member.AvatarURL}
			return
		}
	}
}

func (h *Handler) person(w http.ResponseWriter, r *http.Request) {
	handle := factory.NormalizeHandle(r.PathValue("handle"))
	if !personHandle.MatchString(handle) {
		h.fail(w, http.StatusUnprocessableEntity, "person handle must be a GitHub handle")
		return
	}
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	s, _, whole := h.forViewer(r, s)
	// Watched repositories can have no jobs yet. Their access still bounds
	// the machine roster, which otherwise describes the entire factory.
	if whole && h.config.Store != nil {
		if watched, err := h.config.Store.Repos(); err == nil {
			if allow, filter := h.repoAccess(r, watched); filter {
				for _, repo := range watched {
					if !allow[strings.ToLower(repo)] {
						whole = false
						break
					}
				}
			}
		} else {
			whole = false
		}
	}
	s, rules := h.withPeople(r, s)
	if h.authFailed(w, r) {
		return
	}
	p := api.Person{Factory: h.factory, Handle: handle, Role: "outside", Owns: []api.Ownership{}, Machines: []api.Machine{}}
	summary, known := s.People[handle]
	p.PersonSummary = summary
	if h.config.PersonProfile != nil {
		profile, err := h.config.PersonProfile(r.Context(), handle)
		if err == nil {
			p.GitHubPerson = profile
			known = true
		}
	}
	for _, member := range h.memberList() {
		if factory.NormalizeHandle(member.Handle) != handle {
			continue
		}
		known, p.Role = true, "member"
		if p.Name == "" {
			p.Name = member.Name
		}
		if p.AvatarURL == "" {
			p.AvatarURL = member.AvatarURL
		}
		if !member.LastSeen.IsZero() {
			at := member.LastSeen
			p.LastSeen = &at
		}
	}
	// The connected host is a member even when it has no device-flow session.
	if h.seam.GitHub != nil {
		if link, err := h.link(r); err == nil && factory.NormalizeHandle(link.Login) == handle {
			known, p.Role = true, "member"
		}
	}
	for repo, owners := range rules {
		for _, owner := range owners {
			for _, person := range owner.Handles {
				if person == handle {
					known = true
					p.Owns = append(p.Owns, api.Ownership{Repo: repo, Path: owner.Pattern})
					break
				}
			}
		}
	}
	visible := map[int]bool{}
	for _, it := range s.Items {
		visible[it.ID] = true
		if it.State == factory.StateShipped || it.State == factory.StateDismissed {
			continue
		}
		if it.Person() != handle {
			continue
		}
		p.AssignedNow++
		if kind := factory.NeedsKind(it); kind != "" {
			p.WaitingOn++
			if kind == factory.NeedsQuestion {
				p.Questions++
			}
		}
	}
	if !known {
		h.fail(w, http.StatusNotFound, "person not found")
		return
	}
	machines, err := h.viewerMachines(handle)
	if h.failed(w, err) {
		return
	}
	for _, machine := range machines {
		// A partial view reveals only machines with a visible hold; an idle
		// roster would describe work outside repositories this caller can read.
		if whole || visible[machine.Job] {
			p.Machines = append(p.Machines, machine)
		}
	}
	sort.Slice(p.Owns, func(i, j int) bool {
		if p.Owns[i].Repo == p.Owns[j].Repo {
			return p.Owns[i].Path < p.Owns[j].Path
		}
		return p.Owns[i].Repo < p.Owns[j].Repo
	})
	p.Login = handle
	p.You = false
	if caller, err := h.callerHandle(r); err == nil {
		p.You = caller == handle
	}
	p.Operator = handle == h.operatorLogin(r)
	p.Status = api.PersonInvited
	if p.LastSeen != nil {
		p.Status = personStatus(*p.LastSeen, h.now())
	}
	p.Runners, err = h.runnerList(r, handle)
	if h.failed(w, err) {
		return
	}
	if h.isOperator(r) || p.You {
		p.Sessions = h.sessionsFor(handle)
	}
	h.write(w, http.StatusOK, p)
}
