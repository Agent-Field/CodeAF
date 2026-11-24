package serve

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const (
	botTokenPrefix   = "cbt_"
	botAgentPrefix   = "bot:"
	botIDPrefix      = "bt_"
	botTokenIDPrefix = "btk_"
	botTokenBytes    = 32
	botTokenHintSize = 8
	botNameMost      = 60
	botsPath         = "/v1/bots"
	botRouteRefused  = "a bot cannot use this yet"
	botRunRefused    = "a bot cannot start an agent run yet"
)

func (h *Handler) botRoutes() {
	h.route(botsPath, map[string]http.HandlerFunc{http.MethodGet: h.listBots, http.MethodPost: h.createBot})
	h.route(botsPath+"/{bot}/tokens", map[string]http.HandlerFunc{http.MethodPost: h.createBotToken})
	h.route(botsPath+"/{bot}/tokens/{token}", map[string]http.HandlerFunc{http.MethodDelete: h.revokeBotToken})
	h.route(botsPath+"/{bot}", map[string]http.HandlerFunc{http.MethodDelete: h.removeBot})
}

func isBotCall(r *http.Request) bool {
	call, _ := agentCallOf(r)
	return call.bot != ""
}

// The allowlist admits only routes whose item decisions go through the
// evaluator and organization metadata that does not rely on acting as a person.
var botRoutes = []struct {
	path     string
	under    bool
	readOnly bool
}{
	{path: "/v1/workstreams", under: true},
	{path: "/v1/canvases", under: true},
	{path: "/v1/resources", under: true},
	{path: "/v1/hosted/sessions", under: true},
	{path: "/v1/myspace", readOnly: true},
	{path: "/v1/org/policy", readOnly: true},
	{path: "/v1/members", readOnly: true},
	{path: "/v1/teams", readOnly: true},
	{path: "/v1/about", readOnly: true},
}

func botRouteRefusal(r *http.Request) string {
	path := r.URL.Path
	if rest, ok := strings.CutPrefix(path, "/v1/workstreams/"); ok {
		_, subpath, _ := strings.Cut(rest, "/")
		if slices.Contains(strings.Split(subpath, "/"), "agent") {
			return botRunRefused
		}
		// The request and driver doors can act as a person without the item
		// evaluator. Import can also replace an earlier import by that person.
		first, tail, _ := strings.Cut(subpath, "/")
		if rest == "requestable" || rest == "import" || first == "requests" || first == "driver" || first == "jobs" {
			return botRouteRefused
		}
		if r.Method != http.MethodGet && first == "tasks" {
			task, _, _ := strings.Cut(tail, "/")
			if _, job := factory.IsJobTask(task); job {
				return botRouteRefused
			}
		}
		if r.Method != http.MethodGet && (first == "orders" || strings.HasSuffix(subpath, "/work/messages")) {
			return botRunRefused
		}
	}
	// A new root workstream and a canvas import have no item permission check.
	if (path == "/v1/workstreams" && r.Method != http.MethodGet) || path == "/v1/canvases/import" {
		return botRouteRefused
	}
	for _, route := range botRoutes {
		if (path == route.path || route.under && strings.HasPrefix(path, route.path+"/")) &&
			(!route.readOnly || r.Method == http.MethodGet) {
			return ""
		}
	}
	return botRouteRefused
}

// withBot authenticates a bot before any person door. A refused bot credential
// ends the request because neither loopback nor a person credential can cover it.
func (h *Handler) withBot(w http.ResponseWriter, r *http.Request, secret string) (*http.Request, bool) {
	if _, err := r.Cookie(accessCookieName); err == nil {
		h.fail(w, http.StatusBadRequest, "send one credential")
		return r, false
	}
	if h.config.ServedBy == "app" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.fail(w, http.StatusUnauthorized, "the app bearer token is required")
		return r, false
	}
	now := h.now()
	state := store.BotTokenUnknown
	var bot store.Bot
	var token store.BotToken
	if h.config.Store != nil {
		var err error
		bot, token, state, err = h.config.Store.FindBotToken(sessionHash(secret), now)
		if h.failed(w, err) {
			return r, false
		}
	}
	if state != store.BotTokenLive {
		h.refuseBot(w, state)
		return r, false
	}
	if !h.teamAllows(bot.Creator) {
		h.fail(w, http.StatusForbidden, teamHandleRefusal)
		return r, false
	}
	if words := botRouteRefusal(r); words != "" {
		h.fail(w, http.StatusForbidden, words)
		return r, false
	}
	var only []string
	if bot.Scope != "" {
		policy, err := h.config.Store.OrgPolicy()
		if h.failed(w, err) {
			return r, false
		}
		only = botScopeVerbs(policy, bot.Scope)
	}
	lastUsed := time.Time{}
	if token.LastUsedAt != nil {
		lastUsed, _ = time.Parse(time.RFC3339Nano, *token.LastUsedAt)
	}
	if token.LastUsedAt == nil || now.Sub(lastUsed) >= time.Minute {
		if h.failed(w, h.config.Store.TouchBotToken(token.ID, now)) {
			return r, false
		}
	}
	agent := botAgentPrefix + bot.ID
	call := agentCall{handle: bot.Creator, agent: agent, bot: bot.ID, only: only,
		by: factory.TaskEventBy{Kind: factory.TaskByAgent, ID: agent, Name: bot.Name}}
	return r.WithContext(context.WithValue(r.Context(), agentCallKey{}, call)), true
}

// A named scope with no matching role must still restrict the bot's verbs.
func botScopeVerbs(policy acl.Policy, scope string) []string {
	if scope == "" {
		return nil
	}
	role, _ := policy.Role(scope)
	return append([]string{}, role.Verbs...)
}

func (h *Handler) refuseBot(w http.ResponseWriter, state store.BotTokenState) {
	words := "that bot token is not recognized"
	switch state {
	case store.BotTokenRevoked:
		words = "that bot token was revoked"
	case store.BotTokenExpired:
		words = "that bot token has expired"
	case store.BotTokenRemoved:
		words = "that bot was removed"
	case store.BotTokenDisabled:
		words = store.ErrBotDisabled.Error()
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
	h.refuse(w, http.StatusUnauthorized, words, string(state), "", nil, "")
}

func (h *Handler) botActor(w http.ResponseWriter, r *http.Request) (person string, operator, ok bool) {
	if !h.require(w, h.config.Store != nil) {
		return "", false, false
	}
	if _, agent := agentCallOf(r); agent {
		h.fail(w, http.StatusForbidden, botRouteRefused)
		return "", false, false
	}
	// A link with no signed-in profile cannot claim the host's identity.
	if !h.allowed(r) {
		h.fail(w, http.StatusUnauthorized, "sign in to manage bots")
		return "", false, false
	}
	operator = h.isOperator(r)
	if !h.profileless(r) {
		person = h.orgActor(r)
	}
	if person == "" && !operator {
		h.fail(w, http.StatusForbidden, "sign in as a person to manage bots")
		return "", false, false
	}
	return person, operator, true
}

func (h *Handler) listBots(w http.ResponseWriter, r *http.Request) {
	person, operator, ok := h.botActor(w, r)
	if !ok {
		return
	}
	if operator {
		person = ""
	}
	bots, err := h.config.Store.Bots(person)
	if !h.failed(w, err) {
		h.write(w, http.StatusOK, api.BotList{Bots: bots})
	}
}

func newBotToken(now time.Time, label string) (store.BotToken, string, error) {
	id, err := cell.NewID(now)
	if err != nil {
		return store.BotToken{}, "", err
	}
	var bytes [botTokenBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return store.BotToken{}, "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(bytes[:])
	return store.BotToken{ID: botTokenIDPrefix + id, Label: label, Hint: secret[:botTokenHintSize]}, botTokenPrefix + secret, nil
}

func (h *Handler) createBot(w http.ResponseWriter, r *http.Request) {
	person, _, ok := h.botActor(w, r)
	if !ok {
		return
	}
	if person == "" {
		h.fail(w, http.StatusForbidden, "sign in as a person to make a bot")
		return
	}
	var input api.BotNew
	if !h.decode(w, r, &input, false) {
		return
	}
	name, scope := strings.TrimSpace(input.Name), strings.TrimSpace(input.Scope)
	if err := plainWords("a bot name", name, 1, botNameMost); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if scope != "" {
		policy, err := h.config.Store.OrgPolicy()
		if h.failed(w, err) {
			return
		}
		if _, exists := policy.Role(scope); !exists {
			h.fail(w, http.StatusUnprocessableEntity, "that bot scope is not an organization role")
			return
		}
	}
	now := h.now()
	id, err := cell.NewID(now)
	if h.failed(w, err) {
		return
	}
	tok, secret, err := newBotToken(now, "Initial token")
	if h.failed(w, err) {
		return
	}
	// Check the sign-in again while serialized with removal so a request
	// waiting on a removal cannot create a bot with the old session.
	h.botCreateMu.Lock()
	if !h.allowed(r) {
		h.botCreateMu.Unlock()
		h.fail(w, http.StatusUnauthorized, "sign in to manage bots")
		return
	}
	bot, err := h.config.Store.CreateBot(store.Bot{ID: botIDPrefix + id, Name: name, Creator: person, Scope: scope}, tok, sessionHash(secret), now)
	h.botCreateMu.Unlock()
	if h.botFailed(w, err) {
		return
	}
	h.recordBot(r, bot, "created bot")
	h.write(w, http.StatusCreated, api.BotCreated{Bot: bot, Token: secret})
}

func (h *Handler) findManagedBot(w http.ResponseWriter, r *http.Request, mint bool) (store.Bot, bool) {
	person, operator, ok := h.botActor(w, r)
	if !ok {
		return store.Bot{}, false
	}
	bot, err := h.config.Store.Bot(r.PathValue("bot"))
	if h.botFailed(w, err) {
		return store.Bot{}, false
	}
	if person == bot.Creator {
		return bot, true
	}
	if !operator {
		h.fail(w, http.StatusNotFound, store.ErrBotGone.Error())
	} else if mint {
		h.fail(w, http.StatusForbidden, "only a bot's creator can mint its tokens")
	} else {
		return bot, true
	}
	return store.Bot{}, false
}

func (h *Handler) createBotToken(w http.ResponseWriter, r *http.Request) {
	bot, ok := h.findManagedBot(w, r, true)
	if !ok {
		return
	}
	var input api.BotTokenNew
	if !h.decode(w, r, &input, true) {
		return
	}
	label := strings.TrimSpace(input.Label)
	now := h.now()
	var expires *time.Time
	if input.ExpiresAt != "" {
		at, err := time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil || !at.After(now) {
			h.fail(w, http.StatusUnprocessableEntity, "expires_at must be a future time in RFC 3339")
			return
		}
		expires = &at
	}
	tok, secret, err := newBotToken(now, label)
	if h.failed(w, err) {
		return
	}
	tok, err = h.config.Store.AddBotToken(bot.ID, tok, sessionHash(secret), now, expires)
	if h.botFailed(w, err) {
		return
	}
	h.recordBot(r, bot, "minted bot token")
	h.write(w, http.StatusCreated, api.BotTokenCreated{ID: tok.ID, Token: secret, Label: tok.Label,
		Hint: tok.Hint, CreatedAt: tok.CreatedAt, ExpiresAt: tok.ExpiresAt})
}

func (h *Handler) revokeBotToken(w http.ResponseWriter, r *http.Request) {
	bot, ok := h.findManagedBot(w, r, false)
	if !ok || h.botFailed(w, h.config.Store.RevokeBotToken(bot.ID, r.PathValue("token"), h.now())) {
		return
	}
	h.recordBot(r, bot, "revoked bot token")
	h.finish(w, nil)
}

func (h *Handler) removeBot(w http.ResponseWriter, r *http.Request) {
	bot, ok := h.findManagedBot(w, r, false)
	if !ok || h.botFailed(w, h.config.Store.RemoveBot(bot.ID)) {
		return
	}
	h.recordBot(r, bot, "removed bot")
	h.finish(w, nil)
}

func (h *Handler) recordBot(r *http.Request, bot store.Bot, verb string) {
	h.recordOrg(r, record.SettingsChanged, "changed settings", botAgentPrefix+bot.ID, verb+" "+bot.Name)
}

func (h *Handler) botFailed(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrBotGone), errors.Is(err, store.ErrBotTokenGone):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrBotNameTaken), errors.Is(err, store.ErrBotLimit),
		errors.Is(err, store.ErrBotTokenLimit), errors.Is(err, store.ErrBotDisabled):
		h.fail(w, http.StatusConflict, err.Error())
	default:
		return h.failed(w, err)
	}
	return true
}
