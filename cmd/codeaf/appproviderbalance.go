package main

// ── codeaf app: PROVIDER BALANCE, ONLY WHERE THE PROVIDER REPORTS ONE ───────
//
// providers.balance (internal/appwire/provider_balance.go) is served from
// [credits.Book]: one entry per account in this process's memory, one request
// at a time, one a minute, dropped when the key changes. The capability is a
// fact about a provider in the catalog, never a guess from a label: today only
// the default service, OpenRouter, reports an account balance, and the request
// is made only for the service the sources resolve under that exact id with
// its own key. A custom connection, a subscription sign-in and every other
// provider answer unsupported without a request, and a plan is never read as
// money.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The app's backend serves providers.balance.
var _ appwire.ProviderBalancer = (*appBackend)(nil)

// appBalances is the backend's balance state. stop ends every request in
// flight and is the backend's Close.
type appBalances struct {
	book *credits.Book
	stop context.CancelFunc
	ctx  context.Context
	// client and address replace the network and the provider's address in a
	// test; nil is the real thing.
	client  *http.Client
	address func(provider string) string
}

// balanceState makes the backend's balance state on first use.
func (b *appBackend) balanceState() *appBalances {
	var state *appBalances
	b.locked(func() {
		if b.balances == nil {
			ctx, stop := context.WithCancel(context.Background())
			b.balances = &appBalances{book: credits.NewBook(ctx, b.now), stop: stop, ctx: ctx}
			if b.closing {
				stop()
			}
		}
		state = b.balances
	})
	return state
}

// ProviderBalance answers what the provider reports for the account its key
// reaches, from memory when it was asked inside the last minute.
func (b *appBackend) ProviderBalance(ask appwire.ProviderBalanceAsk) (appwire.ProviderBalance, error) {
	id := ask.Provider
	name := ""
	if p, ok := b.appProviderNamed(id); ok {
		name = p.name
	}
	if id != modelsource.DefaultID {
		return appwire.ProviderBalance{
			Provider: id, Name: name, Status: appwire.BalanceUnavailable, ErrorKind: "unsupported",
		}, nil
	}
	state := b.balanceState()
	profile := config.ProfileDir()
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	service, ok := sources.ByID(id)
	key := strings.TrimSpace(service.Key)
	if !ok || key == "" || service.Source.ID != modelsource.DefaultID || modelsource.IsCustomID(service.Source.ID) {
		state.book.Drop(id)
		return appwire.ProviderBalance{
			Provider: id, Name: name, Supported: true, Status: appwire.BalanceUnavailable, ErrorKind: "not_connected",
		}, nil
	}
	address := strings.TrimSpace(service.Address)
	if address == "" {
		address = config.DefaultBaseURL
	}
	if state.address != nil {
		address = state.address(id)
	}
	// A REDIRECT MUST NEVER TURN THIS CALL INTO A KEY SENT ELSEWHERE.
	local := http.Client{}
	if state.client != nil {
		local = *state.client
	}
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	fetch := func(ctx context.Context) (credits.Detail, error) {
		return credits.ReadDetail(ctx, &local, address, key)
	}
	snap, err := state.book.Get(state.ctx, credits.NewIdentity(id, address, key), fetch)
	if err != nil && errors.Is(err, context.Canceled) {
		return appwire.ProviderBalance{}, appwire.Errorf(appwire.CodeClosed, "codeaf is closing")
	}
	return appBalanceReply(id, name, snap), nil
}

// appBalanceMessageRejected and appBalanceMessageExpired are the only words the
// engine adds: the two failures a person can do something about.
const (
	appBalanceMessageRejected = "The provider rejected this key. Reconnect it."
	appBalanceMessageExpired  = "This key has expired. Replace it."
)

// appBalanceReply is a book's snapshot as the wire's answer. Figures come only
// from a reading, or from the last good one kept through a transient failure.
func appBalanceReply(id, name string, snap credits.Snapshot) appwire.ProviderBalance {
	r := appwire.ProviderBalance{Provider: id, Name: name, Supported: true, Busy: snap.Busy}
	if !snap.LastSuccess.IsZero() {
		r.LastSuccessAt = snap.LastSuccess.UTC().Format(time.RFC3339)
	}
	if !snap.LastError.IsZero() {
		r.LastErrorAt = snap.LastError.UTC().Format(time.RFC3339)
	}
	if snap.RetryAfter > 0 {
		r.RetryAfterMS = (snap.RetryAfter + time.Millisecond - 1).Milliseconds()
	}
	figures := func(d credits.Detail) {
		remaining, _ := d.Remaining()
		r.RemainingUSD = &remaining
		if d.AccountRemaining != nil {
			v := *d.AccountRemaining
			r.AccountRemainingUSD = &v
		}
		if d.KeyLimitRemaining != nil {
			v := *d.KeyLimitRemaining
			r.KeyLimitRemainingUSD = &v
		}
	}
	switch {
	case !snap.Attempted:
		r.Status, r.ErrorKind, r.Busy = appwire.BalanceUnavailable, "busy", true
	case snap.Err != nil:
		kind := credits.Classify(snap.Err)
		r.ErrorKind = string(kind)
		switch kind {
		case credits.KindKeyRejected:
			r.Status, r.Message = appwire.BalanceUnauthorized, appBalanceMessageRejected
		case credits.KindBadResponse, credits.KindUnexpected:
			r.Status = appwire.BalanceInvalid
		default:
			r.Status = appwire.BalanceUnreachable
		}
		if snap.HasGood {
			figures(snap.Good)
			r.Stale = true
		}
	case snap.Detail.Expired:
		r.Status, r.ErrorKind, r.Message = appwire.BalanceUnauthorized, string(credits.KindKeyExpired), appBalanceMessageExpired
	default:
		if _, ok := snap.Detail.Remaining(); ok {
			r.Status = appwire.BalanceKnown
			figures(snap.Detail)
		} else {
			r.Status, r.ErrorKind = appwire.BalanceUnavailable, "not_reported"
		}
	}
	return r
}
