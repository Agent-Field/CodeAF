package appwire

// ── PROVIDER BALANCE: what a provider itself says an account has left ───────
//
// providers.balance asks the engine for the balance a provider REPORTS for the
// account a provider's key reaches. It is a capability of some providers, not
// of the app: a provider that reports none answers supported:false and the
// engine makes no request for it. The engine never estimates, never reads a
// plan or a subscription as a balance, and never answers a zero it was not
// told.
//
// THE ENGINE, NOT THE SCREEN, DECIDES HOW OFTEN THE PROVIDER IS ASKED. At most
// one request is made for an account in any sixty seconds, whoever asks and
// however often, and concurrent asks share one request. An ask inside the
// minute is answered from memory with retry_after_ms. There is no force.
// A key that changes (or a provider that is signed out and in again) is a
// different account: its first ask is made at once, and nothing of the earlier
// account's figures is ever returned for it.
//
// Nothing is written to disk. Amounts, times and error kinds live in memory
// per account for the life of the engine process.
//
// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB: providers.balance
// answers unknown_method from an engine that is not a [ProviderBalancer].

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

// ProviderBalanceReplyMax bounds an encoded reply; a longer one is refused.
const ProviderBalanceReplyMax = 4 << 10

// ProviderBalanceStringMax bounds each string a reply carries, in bytes.
const ProviderBalanceStringMax = 256

// ProviderBalanceIDMax bounds the provider id a request may carry.
const ProviderBalanceIDMax = 64

// The statuses of a balance answer. Status describes the LATEST attempt (or,
// for a provider that reports nothing, the provider).
const (
	BalanceKnown        = "known"
	BalanceUnavailable  = "unavailable"
	BalanceUnauthorized = "unauthorized"
	BalanceUnreachable  = "unreachable"
	BalanceInvalid      = "invalid"
)

// ProviderBalanceParams is providers.balance's params. The provider is an id
// as providers.list names it: lowercase letters, digits, '.', '_' and '-'.
type ProviderBalanceParams struct {
	Provider string `json:"provider"`
}

// ProviderBalance is providers.balance's answer.
//
// remaining_usd is the effective balance: the lesser of account_remaining_usd
// and key_limit_remaining_usd when both are reported, otherwise the one that
// is. Each is absent when the provider did not report it; none is ever
// invented. A negative figure is the provider's own and is not clamped.
//
// When the latest attempt failed in a way that may pass (unreachable, invalid)
// and an earlier attempt for the same key succeeded, the earlier figures are
// kept and stale is true; last_success_at says when they were read. A key the
// provider rejected or reported expired, or an answer without figures, keeps
// no figures.
type ProviderBalance struct {
	Provider             string   `json:"provider"`
	Name                 string   `json:"name,omitempty"`
	Supported            bool     `json:"supported"`
	Status               string   `json:"status" enum:"known,unavailable,unauthorized,unreachable,invalid"`
	RemainingUSD         *float64 `json:"remaining_usd,omitempty"`
	AccountRemainingUSD  *float64 `json:"account_remaining_usd,omitempty"`
	KeyLimitRemainingUSD *float64 `json:"key_limit_remaining_usd,omitempty"`
	Stale                bool     `json:"stale,omitempty"`
	LastSuccessAt        string   `json:"last_success_at,omitempty"`
	LastErrorAt          string   `json:"last_error_at,omitempty"`
	// ErrorKind is stable: unsupported, not_connected, not_reported,
	// key_rejected, key_expired, timeout, unreachable, provider_error,
	// bad_response, unexpected_status, busy.
	ErrorKind string `json:"error_kind,omitempty"`
	// Message is present only when there is something to do about it.
	Message string `json:"message,omitempty"`
	// RetryAfterMS is how long until the engine will ask the provider again.
	RetryAfterMS int64 `json:"retry_after_ms,omitempty"`
	// Busy says this answer is what the engine already held because another
	// request was in flight or the engine was at capacity.
	Busy bool `json:"busy,omitempty"`
}

// ProviderBalanceAsk is providers.balance as the engine is handed it: a
// checked provider id. The wire carries no context, so the engine bounds the
// request itself: a deadline of its own and its own lifetime.
type ProviderBalanceAsk struct {
	Provider string
}

// ProviderBalancer is the engine's half. It is optional.
type ProviderBalancer interface {
	ProviderBalance(ask ProviderBalanceAsk) (ProviderBalance, error)
}

func init() {
	methods["providers.balance"] = providerBalanceMethod
}

var providerBalanceMethod = shaped[ProviderBalanceParams, ProviderBalance](func(e Engine, raw json.RawMessage) (*ProviderBalance, error) {
	b, ok := e.(ProviderBalancer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", "providers.balance")
	}
	var p ProviderBalanceParams
	if err := decodeStrict("providers.balance", raw, &p); err != nil {
		return nil, err
	}
	id, err := CheckProviderBalanceID(p.Provider)
	if err != nil {
		return nil, err
	}
	reply, err := b.ProviderBalance(ProviderBalanceAsk{Provider: id})
	if err != nil {
		return nil, err
	}
	if err := checkProviderBalance(reply); err != nil {
		return nil, err
	}
	return &reply, nil
})

// CheckProviderBalanceID trims and lowercases a provider id and refuses what
// cannot be one.
func CheckProviderBalanceID(id string) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" || len(id) > ProviderBalanceIDMax {
		return "", Errorf(CodeBadParams, "provider is an id of 1 to %d characters", ProviderBalanceIDMax)
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return "", Errorf(CodeBadParams, "provider is lowercase letters, digits, '.', '_' and '-'")
		}
	}
	return id, nil
}

// checkProviderBalance refuses an engine's answer that would break the wire:
// a figure that cannot be encoded, a string over its bound, a reply over 4 KiB.
// It never cuts one.
func checkProviderBalance(r ProviderBalance) error {
	for _, f := range []*float64{r.RemainingUSD, r.AccountRemainingUSD, r.KeyLimitRemainingUSD} {
		if f != nil && (math.IsNaN(*f) || math.IsInf(*f, 0)) {
			return Errorf(CodeEngine, "the engine answered a balance that is not a number")
		}
	}
	for _, s := range []string{r.Provider, r.Name, r.Status, r.LastSuccessAt, r.LastErrorAt, r.ErrorKind, r.Message} {
		if len(s) > ProviderBalanceStringMax || !utf8.ValidString(s) {
			return Errorf(CodeEngine, "the engine answered a balance with a string out of bounds")
		}
	}
	out, err := json.Marshal(r)
	if err != nil || len(out) > ProviderBalanceReplyMax {
		return Errorf(CodeEngine, "the engine answered a balance over %d bytes", ProviderBalanceReplyMax)
	}
	return nil
}
