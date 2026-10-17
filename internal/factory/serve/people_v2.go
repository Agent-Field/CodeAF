package serve

import (
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// mode follows the actual serving door unless the host explicitly selects a mode.
func (h *Handler) mode() string {
	if h.config.Mode == api.FactorySolo || h.config.Mode == api.FactoryTeam {
		return h.config.Mode
	}
	if h.config.ServedBy == "tui" || h.config.ServedBy == "app" {
		return api.FactorySolo
	}
	if h.config.AccessToken != "" || h.config.TokensPath != "" || h.config.GitHubClientID != "" || h.config.ServedBy == "factory" {
		return api.FactoryTeam
	}
	return api.FactorySolo
}

func (h *Handler) operatorLogin(r *http.Request) string {
	if h.mode() == api.FactoryTeam {
		claim, err := tokens.LoadOperator(h.config.TokensPath)
		if err != nil {
			return ""
		}
		return claim.Login
	}
	if handle := factory.NormalizeHandle(h.config.OperatorLogin); handle != "" {
		return handle
	}
	handle, _ := h.seam.PersonHandle(r.Context())
	return factory.NormalizeHandle(handle)
}

// isOperator accepts script credentials or the persisted operator's signed-in identity.
func (h *Handler) isOperator(r *http.Request) bool {
	if h.floorToken(bearer(r)) {
		return true
	}
	if session, ok := h.credential(r); ok {
		return session.profile != nil && session.profile.Handle != "" && session.profile.Handle == h.operatorLogin(r)
	}
	_, cookieErr := r.Cookie(accessCookieName)
	return bearer(r) == "" && cookieErr != nil && h.loopbackOwner(r)
}

func (h *Handler) requireOperator(w http.ResponseWriter, r *http.Request) bool {
	if h.isOperator(r) {
		return true
	}
	h.fail(w, http.StatusForbidden, "only the operator can do this")
	return false
}

func (h *Handler) operatorEvent(r *http.Request, kind, object, detail string) error {
	if h.config.Record == nil {
		return nil
	}
	handle := h.operatorLogin(r)
	name := ""
	if handle != "" {
		name = "@" + handle
	}
	_, err := h.config.Record.Append(r.Context(), record.Event{Kind: kind, Handle: handle, Object: object,
		Actor: record.Actor{Kind: "person", Name: name, Operator: true}, Detail: strings.TrimSpace(detail + " as operator")})
	return err
}

func personStatus(last time.Time, now time.Time) string {
	if last.IsZero() {
		return api.PersonInvited
	}
	if !now.Before(last.Add(api.PersonIdleAfter)) {
		return api.PersonAway
	}
	return api.PersonActive
}

func (h *Handler) peopleDirectory(w http.ResponseWriter, r *http.Request) {
	out := api.People{Factory: h.factory, Mode: h.mode(), People: []api.Person{}}
	if out.Mode == api.FactorySolo {
		h.write(w, http.StatusOK, out)
		return
	}
	caller, _ := h.callerHandle(r)
	operator := h.operatorLogin(r)
	members := h.memberList()
	found := false
	for _, member := range members {
		if member.Handle == operator {
			found = true
		}
	}
	if !found && operator != "" {
		members = append(members, api.Member{Handle: operator, LastSeen: h.now()})
	}
	for _, member := range members {
		p := api.Person{Factory: h.factory, Handle: member.Handle, Login: member.Handle, Role: "member", You: member.Handle == caller,
			Operator: member.Handle == operator, Status: personStatus(member.LastSeen, h.now()), Owns: []api.Ownership{}, Machines: []api.Machine{}}
		p.Name, p.AvatarURL = member.Name, member.AvatarURL
		last := member.LastSeen
		if !last.IsZero() {
			p.LastSeen = &last
		}
		var err error
		p.Runners, err = h.runnerList(r, member.Handle)
		if h.failed(w, err) {
			return
		}
		if h.isOperator(r) || p.You {
			p.Sessions = h.sessionsFor(member.Handle)
		}
		if h.seam.Load != nil {
			snap, err := h.seam.Load()
			if h.failed(w, err) {
				return
			}
			snap, _, _ = h.forViewer(r, snap)
			for _, it := range snap.Items {
				if it.Person() != member.Handle || it.State == factory.StateShipped || it.State == factory.StateDismissed {
					continue
				}
				p.AssignedNow++
				if factory.NeedsKind(it) != "" || it.QKind == "runner_offline" {
					p.WaitingOn++
				}
			}
		}
		out.People = append(out.People, p)
	}
	// Invited links without a bound profile name people before their first sign-in.
	known := map[string]bool{}
	for _, p := range out.People {
		known[p.Login] = true
	}
	for _, tok := range h.tokenRows() {
		if tok.Revoked || tok.Profile != nil || tok.InvitedLogin == "" || (!tok.Expires.IsZero() && !h.now().Before(tok.Expires)) || known[tok.InvitedLogin] {
			continue
		}
		known[tok.InvitedLogin] = true
		p := api.Person{Factory: h.factory, Handle: tok.InvitedLogin, Login: tok.InvitedLogin, Status: api.PersonInvited, Runners: []api.Runner{}, Owns: []api.Ownership{}, Machines: []api.Machine{}}
		p.Name = tok.InvitedName
		out.People = append(out.People, p)
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) sessionsFor(handle string) []api.Session {
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	out := []api.Session{}
	for _, s := range h.sessions.ids {
		if profileHandle(s) == handle && h.sessionUsable(s) {
			out = append(out, api.Session{ID: s.id, Device: s.device, Created: s.created, LastSeen: s.lastSeen})
		}
	}
	return out
}

func (h *Handler) operatorRevokePerson(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) {
		return
	}
	handle := factory.NormalizeHandle(r.PathValue("handle"))
	if !personHandle.MatchString(handle) {
		h.fail(w, http.StatusUnprocessableEntity, "person handle must be a GitHub handle")
		return
	}
	if h.failed(w, h.revokePerson(r, handle)) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.SessionsRevoked, "person:"+handle, "jobs moved to Unassigned"))
}

// revokePerson ends sign-ins and runner credentials before ownership is made available again.
func (h *Handler) revokePerson(r *http.Request, handle string) error {
	h.sessions.mu.Lock()
	for key, s := range h.sessions.ids {
		if profileHandle(s) == handle {
			delete(h.sessions.ids, key)
		}
	}
	h.releaseToken(handle)
	err := h.saveSessions()
	h.sessions.mu.Unlock()
	if err != nil {
		return err
	}
	if h.workers != nil {
		h.workers.mu.Lock()
		for key, row := range h.workers.credentials {
			if row.Worker.Handle == handle {
				delete(h.workers.credentials, key)
			}
		}
		err = h.workers.saveRegistry()
		h.workers.mu.Unlock()
		if err != nil {
			return err
		}
		if h.workers.desk != nil {
			if err := h.workers.desk.UnassignPerson(handle); err != nil {
				return err
			}
		}
	}
	if h.viewer != nil {
		h.viewer.mu.Lock()
		delete(h.viewer.people, handle)
		delete(h.viewer.github, handle)
		h.viewer.mu.Unlock()
	}
	return nil
}

// soloJob removes the team assignment field from a solo response.
func (h *Handler) soloJob(job *api.Job) {
	it := api.ToItem(*job)
	it.Assignee, it.Taker, it.Starter, it.SourceAssignees = "", "", "", nil
	job.Can = api.CanFor(it, "", h.clock())
	job.AssignedTo = ""
	job.Assignee, job.WaitingFor = "", ""
	job.Can.AssignToMe = false
	if job.Desk != nil {
		job.Desk.Assignee, job.Desk.Starter, job.Desk.Taker, job.Desk.Assignees = "", "", "", nil
	}
}
