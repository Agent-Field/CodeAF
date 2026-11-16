package serve

// ── PASSWORD ACCOUNTS AS THE SERVER READS THEM ──────────────────────────────
//
// Every request a password person makes asks whether their account is still
// there and still at the generation their session was made under. That is
// one read of accounts.json per request, so the file is kept parsed and read
// again only when its modification time or size moves; every write this
// server makes forgets the copy at once, and another process's write moves
// the modification time.
//
// A FILE THAT CANNOT BE READ IS NOT AN EMPTY ONE. A read error answers
// "unknown": the request is refused, and the session is kept for when the
// file reads again. Only an account that is really absent, or really moved on
// to a newer password, ends a session.

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// accountsParses counts reads of accounts.json that parsed it, so a test can
// prove the copy is used.
var accountsParses atomic.Int64

type accountsCache struct {
	mu   sync.Mutex
	ok   bool
	mod  time.Time
	size int64
	doc  tokens.Accounts
}

// loadAccounts is accounts.json, from the copy while the file has not moved.
func (h *Handler) loadAccounts() (tokens.Accounts, error) {
	if h.config.TokensPath == "" {
		return tokens.Accounts{}, nil
	}
	c := &h.accounts
	c.mu.Lock()
	defer c.mu.Unlock()
	info, err := os.Stat(tokens.AccountsPath(h.config.TokensPath))
	if errors.Is(err, os.ErrNotExist) {
		c.ok, c.doc = false, tokens.Accounts{}
		return tokens.Accounts{}, nil
	}
	if err != nil {
		return tokens.Accounts{}, err
	}
	if c.ok && info.ModTime().Equal(c.mod) && info.Size() == c.size {
		return c.doc, nil
	}
	accountsParses.Add(1)
	doc, err := tokens.LoadAccounts(h.config.TokensPath)
	if err != nil {
		c.ok = false
		return tokens.Accounts{}, err
	}
	c.ok, c.mod, c.size, c.doc = true, info.ModTime(), info.Size(), doc
	return doc, nil
}

// forgetAccounts drops the copy after this server wrote the file.
func (h *Handler) forgetAccounts() {
	h.accounts.mu.Lock()
	h.accounts.ok = false
	h.accounts.mu.Unlock()
}

// passwordAccount is handle's account, if it has one. err is a file that
// could not be read: whether the account exists is then unknown.
func (h *Handler) passwordAccount(handle string) (tokens.Account, bool, error) {
	if h.config.TokensPath == "" || handle == "" {
		return tokens.Account{}, false, nil
	}
	doc, err := h.loadAccounts()
	if err != nil {
		return tokens.Account{}, false, err
	}
	acct, ok := doc.Find(handle)
	return acct, ok, nil
}

// freshAccount is passwordAccount read from the file itself, for the checks
// that decide who owns a name or whether a session may be made.
func (h *Handler) freshAccount(handle string) (tokens.Account, bool, error) {
	h.forgetAccounts()
	return h.passwordAccount(handle)
}

// passwordSessionAlive is whether a password person's session may be used:
// the account exists and is still at the generation the session was made
// under. err means the account file could not be read; the session is then
// neither alive nor ended.
func (h *Handler) passwordSessionAlive(session accessSession) (bool, error) {
	if session.profile == nil || session.profile.Kind != tokens.KindPassword {
		return true, nil
	}
	acct, ok, err := h.passwordAccount(session.profile.Handle)
	if err != nil {
		return false, err
	}
	return ok && acct.Gen == session.credGen, nil
}

// handleReferenced is whether anything this organization keeps already
// names handle as a person: a team's members, a workstream's or a canvas's
// owner, shares, maker or editors, a driver's hand, a job's person, a person
// the server has seen, or a row in the record. A username is a person's name
// here, and a name somebody was already given (a share made to @bob before
// bob first signed in) must not pass to whoever signs up as bob first. A
// store that cannot be read is an error, never a free name.
func (h *Handler) handleReferenced(handle string) (bool, error) {
	is := func(s string) bool { return factory.NormalizeHandle(strings.TrimPrefix(s, "@")) == handle }
	person := func(kind, id string) bool { return kind == factory.ShareToPerson && is(id) }
	shared := func(shares []factory.Share) bool {
		for _, s := range shares {
			if person(s.Kind, s.ID) {
				return true
			}
		}
		return false
	}
	if h.viewer != nil {
		h.viewer.mu.Lock()
		_, seen := h.viewer.people[handle]
		h.viewer.mu.Unlock()
		if seen {
			return true, nil
		}
	}
	h.drivers.mu.Lock()
	for _, hand := range h.drivers.held {
		if is(hand.handle) {
			h.drivers.mu.Unlock()
			return true, nil
		}
	}
	h.drivers.mu.Unlock()
	if st := h.config.Store; st != nil {
		teams, err := st.Teams()
		if err != nil {
			return false, err
		}
		for _, t := range teams {
			if slices.ContainsFunc(t.Members, is) {
				return true, nil
			}
		}
		works, err := st.Workstreams()
		if err != nil {
			return false, err
		}
		for _, ws := range works {
			if person(ws.Owner.Kind, ws.Owner.ID) || shared(ws.Shares) || is(ws.CreatedBy) || is(ws.ImportedBy) {
				return true, nil
			}
			for _, p := range ws.Pages {
				if is(p.UpdatedBy) {
					return true, nil
				}
			}
			for _, d := range ws.Decisions {
				if is(d.By) {
					return true, nil
				}
			}
		}
		canvases, err := st.Canvases()
		if err != nil {
			return false, err
		}
		for _, c := range canvases {
			if person(c.Owner.Kind, c.Owner.ID) || shared(c.Shares) || is(c.CreatedBy) || is(c.UpdatedBy) {
				return true, nil
			}
		}
	}
	if h.seam.Load != nil {
		snap, err := h.seam.Load()
		if err != nil {
			return false, err
		}
		for _, it := range snap.Items {
			if is(it.Person()) {
				return true, nil
			}
		}
	}
	if h.config.Record != nil {
		events, err := h.config.Record.Since(context.Background(), time.Time{})
		if err != nil {
			return false, err
		}
		for _, e := range events {
			if is(e.Handle) || (e.Actor.Kind == "person" && is(e.Actor.Name)) || (strings.HasPrefix(e.Object, "person:") && is(strings.TrimPrefix(e.Object, "person:"))) {
				return true, nil
			}
		}
	}
	return false, nil
}
