package serve

import (
	"context"
	"encoding/json"
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// This file is what removing a person from the organization does to the hosted
// chats they left running. Their sign-ins and bot credentials already end in
// revokePerson; a chat's model and git access are a separate credential (a
// minted bearer that lasts seven days and is held by the chat itself), so
// without this a removed person's chat would go on spending the organization's
// model key, and reaching git as them, until that bearer ran out.

// revokePerson ends every live bearer held for one person's chats and answers
// how many it ended. It matches on the person the bearer was minted for, so a
// chat a guest was merely let into is not touched: that bearer is the owner's.
func (t *bearerTable) revokePerson(person string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	ended := 0
	for hash, row := range t.rows {
		if row.revoked || factory.NormalizeHandle(row.person) != person {
			continue
		}
		row.revoked = true
		if t.current[row.session] == hash {
			delete(t.current, row.session)
		}
		ended++
	}
	if ended > 0 {
		t.saveLocked()
	}
	return ended
}

// endHostedLifeOf ends what a removed person's hosted chats could still do.
// The order is the security order: the bearers go first, in this process, so
// the model and git doors refuse the person's chats before anything that can
// fail or wait is tried. Then the person's sealed keys are forgotten, so a
// provider key or GitHub sign-in they stored is not left behind. The removal
// is recorded before asking the chats' host to free the Linux users and
// folders, so it can finish after a restart. Any chat the host did not
// confirm is closed here so nobody can wake it while it waits to be deleted.
func (h *Handler) endHostedLifeOf(ctx context.Context, handle string) {
	cfg := h.config.Hosted
	if cfg == nil || cfg.state == nil {
		return
	}
	hs := cfg.state
	person := factory.NormalizeHandle(handle)
	if person == "" {
		return
	}
	hs.bearers.revokePerson(person)
	if hs.keys != nil && !hs.keys.Locked() {
		for _, key := range hs.keys.List(person) {
			if _, err := hs.keys.Delete(person, key.Provider); err != nil {
				log.Print("hosted chats: a removed person's stored key could not be forgotten")
			}
		}
	}
	since := h.clock()
	st := h.config.Store
	if st != nil {
		if err := st.MarkHostedRemoval(person, since); err != nil {
			log.Print("hosted chats: the removal could not be saved for another try")
		}
	}
	done, listed := h.cleanRemovedChats(ctx, person, since)
	if !listed {
		log.Print("hosted chats: a removed person's chats could not be listed on the chats' host")
	}
	h.closeRemovedChats(person)
	if st == nil {
		return
	}
	if done {
		if err := st.ClearHostedRemoval(person, since); err == nil {
			return
		}
		log.Print("hosted chats: the finished removal could not be cleared")
	}
	h.scheduleHostedRemovals()
}

// cleanRemoved is one try at deleting a removed person's chats on the chats'
// host, and says whether the host confirmed every one of them gone. It works
// from the registry first, so a chat the host cannot list is still tried, and
// from the host's list for chats from before the registry existed. It only
// touches what existed at since: a chat registered later belongs to a person
// who joined again under the same handle.
func (h *Handler) cleanRemoved(ctx context.Context, person string, since time.Time) (done bool) {
	done, _ = h.cleanRemovedChats(ctx, person, since)
	return done
}

// cleanRemovedChats returns the list outcome as well, so the request can
// report a failed list while background retries can report totals instead.
func (h *Handler) cleanRemovedChats(ctx context.Context, person string, since time.Time) (done, listed bool) {
	hs := h.config.Hosted.state
	registered := map[string]struct{}{}
	ids := map[string]struct{}{}
	registryRead := true
	if st := h.config.Store; st != nil {
		rows, err := st.Resources(store.SessionKind)
		registryRead = err == nil
		for _, row := range rows {
			registered[row.ID] = struct{}{}
			if removedHostedChat(row, person, since) {
				ids[row.ID] = struct{}{}
			}
		}
	}
	status, _, body, err := hs.call(ctx, http.MethodGet, "/v1/sessions", person, nil)
	listed = err == nil && status == http.StatusOK
	if listed {
		for _, id := range hostedChatIDs(body) {
			if _, found := registered[id]; !found {
				ids[id] = struct{}{}
			}
		}
	}
	done = listed && registryRead
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		confirmed, reachable := h.deleteRemovedChat(ctx, id, person, since)
		if !reachable {
			// When the host stops answering, asking about each remaining chat would only wait.
			return false, listed
		}
		done = done && confirmed
	}
	return done, listed
}

// removedHostedChat says a registry row is a chat the removal at since owns.
func removedHostedChat(row store.Resource, person string, since time.Time) bool {
	return hostedRegistration(row) && factory.NormalizeHandle(row.Owner.ID) == person && !row.CreatedAt.After(since)
}

// deleteRemovedChat deletes one chat on the host and forgets its registry row
// once the host confirms (or says it has no such chat). It answers whether the
// chat is gone and whether the host answered at all.
func (h *Handler) deleteRemovedChat(ctx context.Context, id, person string, since time.Time) (confirmed, reachable bool) {
	hs := h.config.Hosted.state
	hs.registry.Lock()
	defer hs.registry.Unlock()
	st := h.config.Store
	if st != nil {
		// A chat can be registered between the snapshot and this delete.
		row, found, err := st.Resource(store.SessionKind, id)
		if err != nil {
			return false, true
		}
		if found && !removedHostedChat(row, person, since) {
			return true, true
		}
	}
	status, _, _, err := hs.call(ctx, http.MethodDelete, "/v1/sessions/"+id, person, nil)
	hs.models.forget(id, person)
	if err != nil {
		return false, false
	}
	if status/100 != 2 && status != http.StatusNotFound {
		return false, true
	}
	hs.noteAsker(id, "")
	return st == nil || st.DeleteResource(store.SessionKind, id) == nil, true
}

// closeRemovedChats shuts every chat the registry still holds for a removed
// person: their grants go and the chat becomes private, so it answers "There
// is no such chat." to everyone, the owner included (whose sign-ins are gone).
// Without this a guest holding steer, run or full on a chat the host could not
// delete would wake it, and waking a chat mints a fresh model bearer for its
// owner, which is the access the removal just ended. The row stays so the
// server can finish the removal when the chats' host answers again.
func (h *Handler) closeRemovedChats(person string) {
	st := h.config.Store
	if st == nil {
		return
	}
	hs := h.config.Hosted.state
	hs.registry.Lock()
	defer hs.registry.Unlock()
	rows, err := st.Resources(store.SessionKind)
	if err != nil {
		log.Print("hosted chats: a removed person's chats could not be read from the registry, so they were not closed")
		return
	}
	left := 0
	for _, row := range rows {
		if !hostedRegistration(row) || factory.NormalizeHandle(row.Owner.ID) != person {
			continue
		}
		left++
		grants, err := st.GrantsOn(store.SessionKind, row.ID)
		if err != nil {
			log.Printf("hosted chats: the shares of a removed person's chat %s could not be read, so it was not closed", row.ID)
			continue
		}
		for _, g := range grants {
			if err := st.RemoveGrant(g.ID); err != nil {
				log.Printf("hosted chats: a share of a removed person's chat %s could not be ended", row.ID)
			}
		}
		row.Visibility = acl.Private
		if err := st.PutResource(row); err != nil {
			log.Printf("hosted chats: a removed person's chat %s could not be made private", row.ID)
		}
	}
	if left > 0 {
		log.Printf("hosted chats: %d chats of a removed person are still on the chats' host and are closed to everyone; the server deletes them when the host answers again", left)
	}
}

// hostedChatIDs reads the ids out of the chats' host's list, keeping only
// well-formed ones: an id goes into an address, so nothing else may.
func hostedChatIDs(body []byte) []string {
	var list struct {
		Sessions []hostedStatusFields `json:"sessions"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil
	}
	var ids []string
	for _, row := range list.Sessions {
		if id := strings.TrimSpace(row.ID); hostedIDForm.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids
}
