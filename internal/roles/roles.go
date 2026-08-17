// Package roles is the model-role registry: one name for every auxiliary LLM
// call the surface makes, and one rule for which model answers it.
//
// An auxiliary call is any call the person did not type — the title a session
// names itself, the summary a compaction writes, the advisor's aside, a commit
// message. Each is a ROLE. Roles group under TIERS, and a tier is what the
// person actually configures: "low model = X, high model = Y", set once. New
// auxiliary calls register a role, inherit their tier's model, and need no
// settings of their own; the person never learns a new knob per feature.
//
// The package is resolution and registry only. It reads settings through a
// [Source] seam and imports nothing of the surface — no session, no config —
// so the rule can be tested without a profile directory on disk and the
// wiring wave can attach whichever store it likes.
package roles

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Role is one named auxiliary LLM call.
//
// The constants below are the two calls that exist today, but the type is a
// string and the registry is OPEN: a package that adds an auxiliary call
// declares its own role in its own file and calls [Register] from an init.
// A closed enum would mean every new advisor, namer or commit-message writer
// had to edit this file to exist, which puts an unrelated package's vocabulary
// in the registry's source and makes the registry a merge point for work that
// has nothing to do with routing.
type Role string

const (
	// RoleTitle names a session from its opening exchange.
	RoleTitle Role = "title"
	// RoleCompaction summarizes a transcript that outgrew its window.
	RoleCompaction Role = "compaction"
	// RoleGuardian answers "is this specific tool call safe to run without
	// asking the person?" — a cheap binary classification, so it sits low.
	RoleGuardian Role = "guardian"
	// RoleVision reads images for a chat model that cannot see them natively;
	// it wants to be genuinely good at seeing, not cheap.
	RoleVision Role = "vision"
	// RoleImageGen generates images — a different modality entirely; the chat
	// model never substitutes.
	RoleImageGen Role = "imagegen"
	// RoleSpeech turns words into sound, and RoleVideo turns them into film.
	// They complete the set of media pins the v3 use-time resolver reads as its
	// second rung (docs/MULTIMODAL.md Decision 5), beside RoleImageGen and
	// RoleVision, so an operator can pin every modality by the same mechanism
	// rather than three of four.
	//
	// THEY ARE PIN NAMES AND ARE DELIBERATELY NOT REGISTERED. Registration
	// binds a role to a TIER, and a tier is a class of text model: resolving
	// "speech" through the low tier would hand a text model to an endpoint that
	// returns audio, which is the wrong answer delivered confidently. A media
	// role's ladder is the resolver's — slot, pin, catalog, curated name — and
	// every rung of it is capability-checked, which the tier ladder cannot be.
	// [Pinned] reads a key and needs no registration, which is exactly the
	// slice of this package a media resolver wants.
	RoleSpeech Role = "speech"
	RoleVideo  Role = "video"
	// RoleAuditor is the VERIFIED FRONTIER: the read-only judge that decides
	// whether a piece of finished-looking work is actually finished, against
	// hard evidence it gathered itself. It sits HIGH and it is the one role
	// where the tier is not an economy question at all — a wrong verdict
	// either lands broken work as done or throws good work away, and both are
	// mistakes nobody downstream can see to correct. Registered from
	// internal/session/task_audit.go, which owns the call.
	RoleAuditor Role = "auditor"
)

// Tier is a class of model the person configures once. Roles are open; tiers
// are deliberately not. Two settings is a decision someone can hold in their
// head — a tier per feature is the per-feature knob this package exists to
// avoid.
type Tier string

const (
	// TierLow is the cheap, fast model.
	TierLow Tier = "low"
	// TierHigh is the capable, expensive one.
	TierHigh Tier = "high"
)

// Tiers lists every tier, low first, for a settings surface to render.
var Tiers = []Tier{TierLow, TierHigh}

// DefaultAssignment is the tier each built-in role starts on.
//
// Titles are disposable prose: a wrong one costs a glance and is rewritten by
// the next session, so it goes to the cheap model. A compaction summary is the
// session's memory — everything before the cut is gone and only the summary
// survives it — so it goes to the capable one. The asymmetry is about what a
// bad answer destroys, not about how hard the task reads.
var DefaultAssignment = map[Role]Tier{
	RoleTitle:      TierLow,
	RoleCompaction: TierHigh,
}

// ErrUnknownRole is returned by [Resolve] for a role that was never
// registered. It is a programming error rather than a misconfiguration: a
// caller asking for a role it did not declare has a typo or a missing init.
var ErrUnknownRole = errors.New("roles: unknown role")

// ErrNoModel is returned by [Resolve] when the ladder runs out — no pin, no
// tier model, and an empty session default. There is no model to call and a
// caller must not invent one, so this is an error rather than a blank string.
var ErrNoModel = errors.New("roles: no model")

// Source reads one settings key. It reports false for a key that is unset,
// which is how the resolution ladder knows to fall through to the next rung.
//
// It is a function rather than an interface so the config registry can be
// mapped onto it in the wiring wave with a closure and no adapter type, and so
// a test can be a map literal. A nil Source is legal and reads as "nothing is
// set" — a fresh install before any settings file exists.
type Source func(key string) (string, bool)

// Key prefixes for the two settings a person can write. They live here, beside
// the reader, so the later settings writer cannot drift from the reader: both
// go through [PinKey] and [TierKey].
const (
	pinPrefix  = "roles."
	tierPrefix = "tiers."
)

// PinKey is the settings key that pins one role to one model outright.
func PinKey(role Role) string { return pinPrefix + string(role) }

// TierKey is the settings key holding a tier's model.
func TierKey(tier Tier) string { return tierPrefix + string(tier) }

var (
	registryMu sync.RWMutex
	registry   = map[Role]Tier{}
)

func init() {
	for role, tier := range DefaultAssignment {
		Register(role, tier)
	}
}

// Register declares a role and the tier it resolves under by default. It is
// meant to be called from a package's init, which is why it panics rather than
// returning an error: a role that failed to register would not fail loudly at
// registration but silently at the first auxiliary call, in whichever model
// answered it by accident.
//
// A repeat registration overwrites, last one wins. That is what lets a surface
// retune a built-in — moving titles to the high tier for a run, say — without
// editing this file, which is the same reason the role registry is open.
func Register(role Role, tier Tier) {
	if strings.TrimSpace(string(role)) == "" {
		panic("roles: register with empty role")
	}
	if tier != TierLow && tier != TierHigh {
		panic(fmt.Sprintf("roles: register %q with unknown tier %q", role, tier))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[role] = tier
}

// Registered lists every role, sorted. Sorted because the registry is a map
// filled from init functions whose order is not defined, and a settings screen
// that lists roles in a different order on every launch is a screen nobody
// trusts.
func Registered() []Role {
	registryMu.RLock()
	roles := make([]Role, 0, len(registry))
	for role := range registry {
		roles = append(roles, role)
	}
	registryMu.RUnlock()
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles
}

// TierOf reports the tier a role resolves under, and false if the role was
// never registered.
func TierOf(role Role) (Tier, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	tier, ok := registry[role]
	return tier, ok
}

// Resolve answers which model a role's call should use.
//
// The ladder is exact, most specific first:
//
//  1. the role's own pin, settings key "roles.<role>" — one call, one model,
//     set deliberately;
//  2. the model on the role's tier, key "tiers.<tier>" — the setting a person
//     actually maintains;
//  3. sessionDefault — the model the session is already talking to.
//
// The session model is the FLOOR, not a last resort that fails. A fresh
// install has no settings file, no tier models and no pins, and every
// auxiliary call still routes to the one model the person already has
// configured and already pays for. Tiers are an optimisation someone opts
// into once they care about the cost of titles, not a prerequisite for the
// surface to work.
//
// An unset key and a key set to blank are the same thing here: clearing a
// setting in a UI usually writes an empty string, and falling through to the
// next rung is what "cleared" plainly means.
func Resolve(src Source, role Role, sessionDefault string) (string, error) {
	tier, ok := TierOf(role)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownRole, role)
	}
	if model, ok := read(src, PinKey(role)); ok {
		return model, nil
	}
	if model, ok := read(src, TierKey(tier)); ok {
		return model, nil
	}
	if model := strings.TrimSpace(sessionDefault); model != "" {
		return model, nil
	}
	return "", fmt.Errorf("%w for role %q", ErrNoModel, role)
}

// Pinned reports a role's explicit pin — rung 1 of the ladder on its own, for
// a settings screen that wants to show whether a role is pinned rather than
// which model it ends up on.
func Pinned(src Source, role Role) (string, bool) {
	return read(src, PinKey(role))
}

// TierModel reports the model configured for a tier — rung 2 alone.
func TierModel(src Source, tier Tier) (string, bool) {
	return read(src, TierKey(tier))
}

// There are no writers here on purpose. Pinning a role and assigning a tier a
// model are settings WRITES, and this package holds a read seam; giving it a
// half-built writer would mean two places that know how a pin is stored. The
// settings writer wave owns them, and writes through [PinKey] and [TierKey] so
// the two halves cannot name a key differently.

// read is one rung of the ladder: a lookup that treats a nil source, a missing
// key and a blank value alike.
func read(src Source, key string) (string, bool) {
	if src == nil {
		return "", false
	}
	value, ok := src(key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
