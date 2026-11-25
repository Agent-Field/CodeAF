package serve

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
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
// provider key or GitHub sign-in they stored is not left behind. Last, their
// chats are deleted on the chats' host, which frees their Linux user and
// folder and means a guest of theirs cannot wake a chat that now belongs to
// nobody. The host being down never stops a removal: the bearers are already
// refused, and the chats are the only thing left to tidy.
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
	status, _, body, err := hs.call(ctx, http.MethodGet, "/v1/sessions", person, nil)
	if err != nil || status != http.StatusOK {
		log.Print("hosted chats: a removed person's chats could not be listed, so they were left in place")
		return
	}
	for _, id := range hostedChatIDs(body) {
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
		}
		hs.registry.Unlock()
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
