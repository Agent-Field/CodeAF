package serve

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

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
// provider key or GitHub sign-in they stored is not left behind. Then their
// chats are deleted on the chats' host, which frees their Linux user and
// folder, and forgotten in the registry only once the host says so. Last,
// whatever chat the host did not confirm is closed here, so nobody can wake
// it. The host being down never stops a removal.
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
	reachable := true
	var ids []string
	status, _, body, err := hs.call(ctx, http.MethodGet, "/v1/sessions", person, nil)
	if err != nil || status != http.StatusOK {
		log.Print("hosted chats: a removed person's chats could not be listed on the chats' host")
		reachable = false
	} else {
		ids = hostedChatIDs(body)
	}
	for _, id := range ids {
		hs.registry.Lock()
		status, _, _, err := hs.call(ctx, http.MethodDelete, "/v1/sessions/"+id, person, nil)
		hs.models.forget(id, person)
		if err == nil && (status/100 == 2 || status == http.StatusNotFound) {
			hs.noteAsker(id, "")
			if st := h.config.Store; st != nil {
				if err := st.DeleteResource(store.SessionKind, id); err != nil {
					log.Print("hosted chats: a removed person's chat could not be forgotten by the registry")
				}
			}
		} else if err != nil {
			// The host stopped answering: asking it about every other chat would only wait.
			reachable = false
		}
		hs.registry.Unlock()
		if !reachable {
			break
		}
	}
	h.closeRemovedChats(person)
}

// closeRemovedChats shuts every chat the registry still holds for a removed
// person: their grants go and the chat becomes private, so it answers "There
// is no such chat." to everyone, the owner included (whose sign-ins are gone).
// Without this a guest holding steer, run or full on a chat the host could not
// delete would wake it, and waking a chat mints a fresh model bearer for its
// owner, which is the access the removal just ended. The row stays, so the
// chat is still tracked: removing the person again, once the host answers,
// deletes it for good.
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
		log.Printf("hosted chats: %d chats of a removed person are still on the chats' host and are closed to everyone; removing the person again once the host answers deletes them", left)
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
		if hostedIDForm.MatchString(strings.TrimSpace(row.ID)) {
			ids = append(ids, row.ID)
		}
	}
	return ids
}
