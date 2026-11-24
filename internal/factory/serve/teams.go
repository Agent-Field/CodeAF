package serve

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// teamNameLimit is the longest team name kept, in characters. A name is a
// label on a row, not a description.
const teamNameLimit = 80

// TeamMembership is how the rest of the organization server reads who is in
// which team: org workstreams decide visibility and write access with it.
// Handles are normalized ([factory.NormalizeHandle]); an unknown team or
// person answers nothing, and so does a team list that cannot be read, which
// grants no one anything.
type TeamMembership interface {
	// TeamsOf is the ids of every team handle is in.
	TeamsOf(handle string) []string
	// Members is the handles in team.
	Members(team string) []string
}

// TeamOwnership says whether a team owns a workstream. A team that owns one
// carries its budget and go-ahead approval, so it cannot be deleted while it
// does. The org workstreams lane fills it; with none set a team owns nothing.
// IT IS ASKED WHILE THE TEAMS LOCK IS HELD, so it may read teams but must never
// change them: a write from inside it would wait on itself.
type TeamOwnership interface {
	TeamOwnsWorkstream(team string) (bool, error)
}

// SetTeamOwnership names what answers whether a team owns a workstream.
func (h *Handler) SetTeamOwnership(o TeamOwnership) {
	h.teamOwnershipMu.Lock()
	h.teamOwnership = o
	h.teamOwnershipMu.Unlock()
}

// Membership is the organization's team membership, read from its store.
func (h *Handler) Membership() TeamMembership {
	return teamBook{st: h.config.Store}
}

// teamBook answers [TeamMembership] from the store on every call, so a change
// another process saved is seen at once.
type teamBook struct{ st *store.Store }

func (b teamBook) TeamsOf(handle string) []string {
	handle = factory.NormalizeHandle(handle)
	if b.st == nil || handle == "" {
		return []string{}
	}
	teams, err := b.st.Teams()
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, t := range teams {
		if slices.Contains(t.Members, handle) {
			out = append(out, t.ID)
		}
	}
	return out
}

func (b teamBook) Members(team string) []string {
	if b.st == nil {
		return []string{}
	}
	teams, err := b.st.Teams()
	if err != nil {
		return []string{}
	}
	for _, t := range teams {
		if t.ID == team {
			return slices.Clone(t.Members)
		}
	}
	return []string{}
}

// teamRoutes are the doors of the organization's teams. Every member reads
// them; only the operator changes them. All of them need the store, where the
// teams are kept, and answer that this server does not provide them without one.
func (h *Handler) teamRoutes() {
	h.route("/v1/teams", map[string]http.HandlerFunc{http.MethodGet: h.listTeams, http.MethodPost: h.createTeam})
	h.route("/v1/teams/{id}", map[string]http.HandlerFunc{http.MethodPatch: h.renameTeam, http.MethodDelete: h.deleteTeam})
	h.route("/v1/teams/{id}/members/{handle}", map[string]http.HandlerFunc{http.MethodPut: h.addTeamMember, http.MethodDelete: h.removeTeamMember})
}

var (
	errTeamNotFound  = errors.New("team not found")
	errTeamNameTaken = errors.New("a team with that name already exists")
	errTeamOwnsWork  = errors.New("this team owns a workstream; give the workstream another owner before deleting the team")
)

func (h *Handler) listTeams(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	teams, err := h.config.Store.Teams()
	if h.failed(w, err) {
		return
	}
	sortTeams(teams)
	h.write(w, http.StatusOK, api.Teams{Teams: teams})
}

func (h *Handler) createTeam(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	name, ok := h.teamName(w, r)
	if !ok {
		return
	}
	now := h.clock().UTC()
	made := store.Team{ID: newTeamID(now), Name: name, Members: []string{}, CreatedAt: now, UpdatedAt: now}
	err := h.config.Store.ChangeTeams(func(all *[]store.Team) error {
		if nameTaken(*all, name, "") {
			return errTeamNameTaken
		}
		*all = append(*all, made)
		return nil
	})
	if h.teamFailed(w, err) {
		return
	}
	h.recordTeam(r, made.ID, "created team", name)
	h.teamChanged(r, made.ID)
	h.write(w, http.StatusOK, made)
}

func (h *Handler) renameTeam(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	name, ok := h.teamName(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	saved, err := h.changeTeam(id, func(all []store.Team, t *store.Team) error {
		if nameTaken(all, name, id) {
			return errTeamNameTaken
		}
		t.Name = name
		return nil
	})
	if h.teamFailed(w, err) {
		return
	}
	h.recordTeam(r, id, "renamed team", name)
	h.teamChanged(r, id)
	h.write(w, http.StatusOK, saved)
}

func (h *Handler) deleteTeam(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	id := r.PathValue("id")
	err := h.config.Store.ChangeTeams(func(all *[]store.Team) error {
		at := slices.IndexFunc(*all, func(t store.Team) bool { return t.ID == id })
		if at < 0 {
			return errTeamNotFound
		}
		// Asked under the teams lock, so a team cannot be deleted between the
		// answer and the write. The answer comes from the workstreams' own store.
		h.teamOwnershipMu.Lock()
		owner := h.teamOwnership
		h.teamOwnershipMu.Unlock()
		if owner != nil {
			owns, err := owner.TeamOwnsWorkstream(id)
			if err != nil {
				return err
			}
			if owns {
				return errTeamOwnsWork
			}
		}
		*all = slices.Delete(*all, at, at+1)
		return nil
	})
	if h.teamFailed(w, err) {
		return
	}
	h.recordTeam(r, id, "deleted team", "")
	h.teamChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addTeamMember(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	handle := factory.NormalizeHandle(r.PathValue("handle"))
	if !personHandle.MatchString(handle) {
		h.fail(w, http.StatusUnprocessableEntity, "person handle must be a GitHub handle")
		return
	}
	// ONLY A MEMBER OF THE ORGANIZATION JOINS A TEAM. Membership is whoever
	// the members list names (members.go), so a team never vouches for a
	// handle the organization has never seen.
	if !h.isMember(handle) {
		h.fail(w, http.StatusUnprocessableEntity, "@"+handle+" is not a member of this organization")
		return
	}
	id := r.PathValue("id")
	saved, err := h.changeTeam(id, func(_ []store.Team, t *store.Team) error {
		if !slices.Contains(t.Members, handle) {
			t.Members = append(t.Members, handle)
			sort.Strings(t.Members)
		}
		return nil
	})
	if h.teamFailed(w, err) {
		return
	}
	h.recordTeam(r, id, "added to team", "@"+handle)
	h.teamChanged(r, id)
	h.write(w, http.StatusOK, saved)
}

func (h *Handler) removeTeamMember(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	handle := factory.NormalizeHandle(r.PathValue("handle"))
	id := r.PathValue("id")
	saved, err := h.changeTeam(id, func(_ []store.Team, t *store.Team) error {
		t.Members = slices.DeleteFunc(t.Members, func(m string) bool { return m == handle })
		return nil
	})
	if h.teamFailed(w, err) {
		return
	}
	h.recordTeam(r, id, "removed from team", "@"+handle)
	h.teamChanged(r, id)
	h.write(w, http.StatusOK, saved)
}

// leaveEveryTeam takes a person out of every team they are in. Removing a
// person from the organization calls it, so no team keeps naming someone who
// can no longer sign in, and a returning person starts in no team.
func (h *Handler) leaveEveryTeam(r *http.Request, handle string) error {
	handle = factory.NormalizeHandle(handle)
	if h.config.Store == nil || handle == "" {
		return nil
	}
	var left []string
	now := h.clock().UTC()
	err := h.config.Store.ChangeTeams(func(all *[]store.Team) error {
		left = nil
		for i := range *all {
			t := &(*all)[i]
			if slices.Contains(t.Members, handle) {
				t.Members = slices.DeleteFunc(t.Members, func(m string) bool { return m == handle })
				t.UpdatedAt = now
				left = append(left, t.ID)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range left {
		h.recordTeam(r, id, "removed from team", "@"+handle)
		h.teamChanged(r, id)
	}
	return nil
}

// changeTeam edits the team id under the store's lock and answers it as
// saved. updated_at moves only when the team actually changed, so adding a
// member who is already there is a quiet yes.
func (h *Handler) changeTeam(id string, change func(all []store.Team, t *store.Team) error) (store.Team, error) {
	var saved store.Team
	now := h.clock().UTC()
	err := h.config.Store.ChangeTeams(func(all *[]store.Team) error {
		at := slices.IndexFunc(*all, func(t store.Team) bool { return t.ID == id })
		if at < 0 {
			return errTeamNotFound
		}
		t := &(*all)[at]
		before := *t
		before.Members = slices.Clone(t.Members)
		if err := change(*all, t); err != nil {
			return err
		}
		if t.Name != before.Name || !slices.Equal(t.Members, before.Members) {
			t.UpdatedAt = now
		}
		saved = *t
		saved.Members = slices.Clone(t.Members)
		return nil
	})
	return saved, err
}

// teamName reads and checks a {"name"} body: trimmed, not empty, not longer
// than [teamNameLimit] characters.
func (h *Handler) teamName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body api.TeamName
	if !h.decode(w, r, &body, false) {
		return "", false
	}
	name := strings.Join(strings.Fields(body.Name), " ")
	switch {
	case name == "":
		h.fail(w, http.StatusUnprocessableEntity, "a team needs a name")
		return "", false
	case len([]rune(name)) > teamNameLimit:
		h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("a team name is at most %d characters", teamNameLimit))
		return "", false
	}
	return name, true
}

// teamFailed answers a team change's refusal with its own status.
func (h *Handler) teamFailed(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errTeamNotFound):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errTeamNameTaken), errors.Is(err, errTeamOwnsWork):
		h.fail(w, http.StatusConflict, err.Error())
	default:
		return h.failed(w, err)
	}
	return true
}

// isMember is whether the organization's members list names handle.
func (h *Handler) isMember(handle string) bool {
	for _, m := range h.memberList() {
		if factory.NormalizeHandle(m.Handle) == handle {
			return true
		}
	}
	return false
}

// teamChanged publishes {"kind":"team.changed","id"} on the event stream. The
// change is already saved; a live record that cannot be written costs the
// desks a prompt refresh, never the change, so it is not an error here.
func (h *Handler) teamChanged(r *http.Request, id string) {
	root := h.eventRoot()
	if root == "" {
		return
	}
	by := ""
	if r != nil {
		by = h.orgActor(r)
	}
	_ = live.Append(root, live.New(api.TeamChangedKind, 0, api.OrgChanged{Kind: api.TeamChangedKind, ID: id, By: by}))
}

// nameTaken is whether another team than except already has name, ignoring case.
func nameTaken(all []store.Team, name, except string) bool {
	for _, t := range all {
		if t.ID != except && strings.EqualFold(t.Name, name) {
			return true
		}
	}
	return false
}

// sortTeams orders teams by name, ignoring case, then by id.
func sortTeams(teams []store.Team) {
	sort.SliceStable(teams, func(i, j int) bool {
		a, b := strings.ToLower(teams[i].Name), strings.ToLower(teams[j].Name)
		if a != b {
			return a < b
		}
		return teams[i].ID < teams[j].ID
	})
}

// crockford is the ULID alphabet: no I, L, O or U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newTeamID is "tm_" and a ULID: 48 bits of milliseconds then 80 random bits,
// in Crockford base32, so ids sort by when the team was made.
func newTeamID(now time.Time) string {
	var b [16]byte
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	_, _ = rand.Read(b[6:])
	out := make([]byte, 26)
	// 128 bits in 26 five-bit groups: the first group holds the top 3 bits.
	var acc uint64
	bits := 0
	pos := 25
	for i := 15; i >= 0; i-- {
		acc |= uint64(b[i]) << bits
		bits += 8
		for bits >= 5 && pos >= 0 {
			out[pos] = crockford[acc&31]
			acc >>= 5
			bits -= 5
			pos--
		}
	}
	if pos >= 0 {
		out[pos] = crockford[acc&31]
	}
	return "tm_" + string(out)
}
