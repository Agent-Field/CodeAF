package serve

import (
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE ORGANIZATION'S MODEL KEYS ON THE WIRE ──────────────────────────────
//
// The operator hands the server the keys its workstream agents call models
// with (store/org_keys.go). Only the operator reads the list or changes it,
// and the list never carries a key: the provider, its last four characters,
// and who set it when. A key goes in and is never answered back, so a desk
// never holds one.

// orgKeyMost is the longest key the server keeps, in bytes.
const orgKeyMost = 512

func (h *Handler) orgKeyRoutes() {
	h.route("/v1/org/keys", map[string]http.HandlerFunc{http.MethodGet: h.listOrgKeys})
	h.route("/v1/org/keys/{provider}", map[string]http.HandlerFunc{http.MethodPut: h.putOrgKey, http.MethodDelete: h.deleteOrgKey})
}

func (h *Handler) listOrgKeys(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	keys, err := h.config.Store.OrgKeys()
	if h.failed(w, err) {
		return
	}
	out := api.OrgKeyList{Keys: make([]api.OrgKey, 0, len(keys)), Providers: store.OrgKeyProviders}
	for _, k := range keys {
		out.Keys = append(out.Keys, api.OrgKey(k))
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) putOrgKey(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	provider := r.PathValue("provider")
	if !store.KnownOrgKeyProvider(provider) {
		h.fail(w, http.StatusNotFound, store.ErrOrgKeyProvider.Error())
		return
	}
	var body api.OrgKeyPut
	if !h.decode(w, r, &body, false) {
		return
	}
	key := strings.TrimSpace(body.Key)
	if len(key) < 12 || len(key) > orgKeyMost || strings.ContainsAny(key, " \t\r\n") {
		h.fail(w, http.StatusUnprocessableEntity, "that is not a model key: one word of 12 to 512 characters")
		return
	}
	saved, err := h.config.Store.SetOrgKey(provider, key, h.orgActor(r), h.clock().UTC().Format(time.RFC3339Nano))
	if h.failed(w, err) {
		return
	}
	// The record names the provider and never a character of the key.
	h.recordOrg(r, record.SettingsChanged, "set model key", orgObjectFactory, provider)
	h.write(w, http.StatusOK, api.OrgKey(saved))
}

func (h *Handler) deleteOrgKey(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	provider := r.PathValue("provider")
	if !store.KnownOrgKeyProvider(provider) {
		h.fail(w, http.StatusNotFound, store.ErrOrgKeyProvider.Error())
		return
	}
	had, err := h.config.Store.DeleteOrgKey(provider)
	if h.failed(w, err) {
		return
	}
	if had {
		h.recordOrg(r, record.SettingsChanged, "removed model key", orgObjectFactory, provider)
	}
	w.WriteHeader(http.StatusNoContent)
}

// orgModelKey is the key the server's runner calls models with, "" when the
// operator has set none.
func (h *Handler) orgModelKey() string {
	if h.config.Store == nil {
		return ""
	}
	key, _ := h.config.Store.OrgKey(modelsource.DefaultID)
	return key
}
