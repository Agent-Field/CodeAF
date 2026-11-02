package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// ── KEEPING A FACTORY SIGN-IN ACROSS A RESTART ─────────────────────────────
//
// The engine holds a team factory's sign-in in memory only, so the desktop app
// keeps it for the next launch the way it keeps provider keys: in the OS
// keychain, one item per factory, handed back before any window's call runs.
//
// THE TOKEN CROSSES ONCE, BY PULL, AND NEVER ON AN EVENT. When a sign-in this
// engine holds begins or ends, it emits [EventFactorySessionsChanged], which
// carries nothing secret. The app's own core then calls factory.sessions_take,
// which drains what changed: a sign-in to keep, or one to forget. Each change
// is answered once and a change nobody takes within [FactorySessionsTakeLife]
// is dropped, so no later caller can read a sign-in the engine holds. This
// wire is served only on the engine's standard input, whose one client is the
// app's core; no window reaches it, because a window speaks only the core's
// typed commands. factory.sessions_take is the one factory method whose result
// carries a credential, and TestFactoryResultsHaveNoSecretField names it.
//
// factory.session_restore is the other half: the core hands a kept sign-in
// back at launch. The engine takes it only for a saved connection at the same
// address that is not the machine's own factory; otherwise Kept is false and
// the core removes its item. A restored sign-in the factory later refuses is
// forgotten once, which reads as signed out and queues its removal.
//
// ALL OF THIS IS OPTIONAL. An engine that is not a [FactorySessionKeeper]
// answers both methods as unknown_method and does not announce the feature.

// FeatureFactorySessions is announced in hello when the engine serves
// factory.sessions_take and factory.session_restore.
const FeatureFactorySessions = "factory.sessions"

// EventFactorySessionsChanged says a kept sign-in changed; its data is [Empty].
const EventFactorySessionsChanged = "factory.sessions_changed"

// FactorySessionsMost bounds the changes waiting to be taken.
const FactorySessionsMost = 64

// FactorySessionsTakeLife is how long a change waits to be taken.
const FactorySessionsTakeLife = 60 * time.Second

// FactorySessionChange is one sign-in to keep, or, with Removed, one to forget.
// Expires is absent for a sign-in that does not expire.
type FactorySessionChange struct {
	Factory string     `json:"factory"`
	URL     string     `json:"url,omitempty"`
	Token   string     `json:"token,omitempty"`
	Expires *time.Time `json:"expires,omitempty"`
	Removed bool       `json:"removed,omitempty"`
}

// FactorySessionsTaken is every change not yet taken, oldest first.
type FactorySessionsTaken struct {
	Changes []FactorySessionChange `json:"changes"`
}

// FactorySessionRestoreParams is a kept sign-in handed back.
type FactorySessionRestoreParams struct {
	Factory string     `json:"factory"`
	URL     string     `json:"url"`
	Token   string     `json:"token"`
	Expires *time.Time `json:"expires,omitempty"`
}

// FactorySessionRestored says whether the engine now holds the sign-in.
type FactorySessionRestored struct {
	Kept bool `json:"kept"`
}

// FactorySessionKeeper is the engine's half of the two methods.
type FactorySessionKeeper interface {
	FactorySessionsTake(ctx context.Context) (FactorySessionsTaken, error)
	FactorySessionRestore(ctx context.Context, p FactorySessionRestoreParams) (FactorySessionRestored, error)
}

func withFactorySessionsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactorySessionKeeper); ok {
		hello = withFeature(hello, FeatureFactorySessions)
	}
	return hello
}

func factoryKeeperOf(e Engine, name string) (FactorySessionKeeper, error) {
	k, ok := e.(FactorySessionKeeper)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return k, nil
}

func init() {
	eventShapes[EventFactorySessionsChanged] = reflect.TypeFor[Empty]()
	for name, m := range map[string]method{
		"factory.sessions_take": shaped[Empty, FactorySessionsTaken](func(e Engine, raw json.RawMessage) (*FactorySessionsTaken, error) {
			k, err := factoryKeeperOf(e, "factory.sessions_take")
			if err != nil {
				return nil, err
			}
			var p Empty
			if len(raw) > 0 && string(raw) != "null" {
				if err := decodeStrict("factory.sessions_take", raw, &p); err != nil {
					return nil, err
				}
			}
			ctx, cancel := factoryCall()
			defer cancel()
			out, err := k.FactorySessionsTake(ctx)
			if err != nil {
				return nil, err
			}
			if out.Changes == nil {
				out.Changes = []FactorySessionChange{}
			}
			return &out, nil
		}),
		"factory.session_restore": shaped[FactorySessionRestoreParams, FactorySessionRestored](func(e Engine, raw json.RawMessage) (*FactorySessionRestored, error) {
			k, err := factoryKeeperOf(e, "factory.session_restore")
			if err != nil {
				return nil, err
			}
			var p FactorySessionRestoreParams
			if err := decodeStrict("factory.session_restore", raw, &p); err != nil {
				return nil, err
			}
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return nil, err
			}
			if p.Token = strings.TrimSpace(p.Token); p.Token == "" || len(p.Token) > 4096 {
				return nil, Errorf(CodeBadParams, "factory.session_restore: token is required")
			}
			ctx, cancel := factoryCall()
			defer cancel()
			out, err := k.FactorySessionRestore(ctx, p)
			if err != nil {
				return nil, err
			}
			return &out, nil
		}),
	} {
		methods[name] = m
		factoryMethods[name] = m
	}
}
