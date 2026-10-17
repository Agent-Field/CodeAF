package serve

import (
	"net/http"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// members is GET /v1/members. People come from live sign-ins and from
// access-token rows that have bound a GitHub profile, so a restart does not
// forget someone who signed in.
func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	h.write(w, http.StatusOK, api.Members{Factory: h.factory, Members: h.memberList()})
}

// memberList gives cards and the member route the same role and last-seen facts.
func (h *Handler) memberList() []api.Member {
	type row struct {
		profile  api.Profile
		joined   time.Time
		lastSeen time.Time
		sessions int
	}
	by := map[string]*row{}
	take := func(handle string) *row {
		if by[handle] == nil {
			by[handle] = &row{}
		}
		return by[handle]
	}
	for _, tok := range h.tokenRows() {
		for _, joiner := range tok.Members() {
			if joiner.Profile.Handle == "" {
				continue
			}
			m := take(joiner.Profile.Handle)
			m.profile = joiner.Profile
			if !joiner.Joined.IsZero() && (m.joined.IsZero() || joiner.Joined.Before(m.joined)) {
				m.joined = joiner.Joined
			}
		}
	}
	// Copy under each lock separately. Holding the viewer lock and then the
	// session lock would invert the lock order liveSession uses.
	if h.viewer != nil {
		h.viewer.mu.Lock()
		for handle, person := range h.viewer.people {
			m := take(handle)
			if m.profile.Handle == "" {
				m.profile = person.profile
			} else {
				if person.profile.Name != "" {
					m.profile.Name = person.profile.Name
				}
				if person.profile.AvatarURL != "" {
					m.profile.AvatarURL = person.profile.AvatarURL
				}
			}
			if !person.joined.IsZero() && (m.joined.IsZero() || person.joined.Before(m.joined)) {
				m.joined = person.joined
			}
			if person.lastSeen.After(m.lastSeen) {
				m.lastSeen = person.lastSeen
			}
		}
		h.viewer.mu.Unlock()
	}
	h.sessions.mu.Lock()
	for _, session := range h.sessions.ids {
		if session.profile == nil || !h.sessionUsable(session) {
			continue
		}
		m := take(session.profile.Handle)
		m.sessions++
		if session.lastSeen.After(m.lastSeen) {
			m.lastSeen = session.lastSeen
		}
		if m.profile.Handle == "" {
			m.profile = *session.profile
		}
		if !session.created.IsZero() && (m.joined.IsZero() || session.created.Before(m.joined)) {
			m.joined = session.created
		}
	}
	h.sessions.mu.Unlock()

	handles := make([]string, 0, len(by))
	for handle, m := range by {
		if m.lastSeen.IsZero() {
			m.lastSeen = m.joined
		}
		handles = append(handles, handle)
	}
	sort.Strings(handles)
	out := make([]api.Member, 0, len(handles))
	for _, handle := range handles {
		m := by[handle]
		out = append(out, api.Member{
			Handle: m.profile.Handle, Name: m.profile.Name, AvatarURL: m.profile.AvatarURL,
			JoinedAt: m.joined, LastSeen: m.lastSeen, Sessions: m.sessions,
		})
	}
	return out
}

// mySessions is GET /v1/me/sessions: this caller's sign-ins only.
func (h *Handler) mySessions(w http.ResponseWriter, r *http.Request) {
	caller, _ := h.credential(r)
	h.sessions.mu.Lock()
	list := make([]api.Session, 0)
	for _, session := range h.sessions.ids {
		if !h.sessionUsable(session) || !ownsSession(caller, session) {
			continue
		}
		list = append(list, api.Session{
			ID: session.id, Device: session.device, Created: session.created,
			LastSeen: session.lastSeen, Current: session.id != "" && session.id == caller.id,
		})
	}
	h.sessions.mu.Unlock()
	sort.Slice(list, func(i, j int) bool {
		if list[i].Created.Equal(list[j].Created) {
			return list[i].ID < list[j].ID
		}
		return list[i].Created.Before(list[j].Created)
	})
	h.write(w, http.StatusOK, api.Sessions{Factory: h.factory, Sessions: list})
}

// deleteSession is DELETE /v1/me/sessions/{id}. Another person's session
// answers as if it were absent.
func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	caller, _ := h.credential(r)
	h.sessions.mu.Lock()
	var key string
	var found accessSession
	for k, session := range h.sessions.ids {
		if session.id == id {
			key, found = k, session
			break
		}
	}
	if key == "" || !ownsSession(caller, found) {
		h.sessions.mu.Unlock()
		h.fail(w, http.StatusNotFound, "session not found")
		return
	}
	handle := ""
	if found.profile != nil {
		handle = found.profile.Handle
	}
	delete(h.sessions.ids, key)
	h.releaseToken(handle)
	err := h.saveSessions()
	h.sessions.mu.Unlock()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
	}{h.factory})
}

// signOutOthers is POST /v1/me/sessions/signout-others. The session that
// called it stays. An access-token row is not a device session and is not revoked.
func (h *Handler) signOutOthers(w http.ResponseWriter, r *http.Request) {
	caller, _ := h.credential(r)
	if caller.profile == nil || caller.profile.Handle == "" {
		h.write(w, http.StatusOK, api.SignedOut{Factory: h.factory})
		return
	}
	h.sessions.mu.Lock()
	n := 0
	for key, session := range h.sessions.ids {
		if session.id == caller.id || session.profile == nil || session.profile.Handle != caller.profile.Handle {
			continue
		}
		delete(h.sessions.ids, key)
		n++
	}
	h.releaseToken(caller.profile.Handle)
	err := h.saveSessions()
	h.sessions.mu.Unlock()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.SignedOut{Factory: h.factory, SignedOut: n})
}

// ownsSession is true for the caller's own GitHub handle, or for the one
// anonymous session whose id is on this request.
func ownsSession(caller, session accessSession) bool {
	if caller.profile != nil && caller.profile.Handle != "" {
		return session.profile != nil && session.profile.Handle == caller.profile.Handle
	}
	return caller.id != "" && caller.id == session.id
}
