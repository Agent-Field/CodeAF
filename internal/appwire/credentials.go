package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"time"
)

// ── KEEPING A CREDENTIAL A DOOR GAVE THE ENGINE ACROSS A RESTART ─────────
//
// An app-run engine holds every key in memory only. A credential a door gave
// it — a pasted GitHub token (factory.setup_connect), a Jira token, an
// AgentField key and node secret (factory.agentfield_connect) — is handed to
// the app to keep in its secure store, and the app hands it back with
// keys.set at the next launch, as it does a provider key.
//
// THE KEY CROSSES ONCE, BY PULL, AND NEVER ON AN EVENT, exactly as a factory
// sign-in does (factory_sessions.go). When a door keeps or removes one, the
// engine emits [EventCredentialsChanged], which carries nothing secret. The
// app's own core then calls credentials.take, which drains what changed: a
// key to keep, or one to remove. Each change is answered once, the latest per
// provider wins, and a change nobody takes within [CredentialsTakeLife] is
// dropped. This wire is served only on the engine's standard input, whose one
// client is the app's core; no window reaches it.
//
// A key handed back with keys.set is never queued again, so a launch does not
// echo its own keys. ALL OF THIS IS OPTIONAL: an engine that is not a
// [CredentialKeeper] answers credentials.take as unknown_method and does not
// announce the feature.

// FeatureCredentials is announced in hello when the engine serves
// credentials.take.
const FeatureCredentials = "credentials.take"

// EventCredentialsChanged says a credential to keep changed; its data is [Empty].
const EventCredentialsChanged = "credentials.changed"

// CredentialsMost bounds the changes waiting to be taken.
const CredentialsMost = 16

// CredentialsTakeLife is how long a change waits to be taken.
const CredentialsTakeLife = 60 * time.Second

// CredentialChange is one key to keep under Provider, or, with Removed, the
// kept one to remove. Provider is the name keys.set takes it back under.
type CredentialChange struct {
	Provider string `json:"provider"`
	Key      string `json:"key,omitempty"`
	Removed  bool   `json:"removed,omitempty"`
}

// CredentialsTaken is every change not yet taken, oldest first.
type CredentialsTaken struct {
	Changes []CredentialChange `json:"changes"`
}

// CredentialKeeper is the engine's half of credentials.take.
type CredentialKeeper interface {
	CredentialsTake(ctx context.Context) (CredentialsTaken, error)
}

func withCredentialsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(CredentialKeeper); ok {
		hello = withFeature(hello, FeatureCredentials)
	}
	return hello
}

func init() {
	eventShapes[EventCredentialsChanged] = reflect.TypeFor[Empty]()
	methods["credentials.take"] = shaped[Empty, CredentialsTaken](func(e Engine, raw json.RawMessage) (*CredentialsTaken, error) {
		k, ok := e.(CredentialKeeper)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve credentials.take")
		}
		var p Empty
		if len(raw) > 0 && string(raw) != "null" {
			if err := decodeStrict("credentials.take", raw, &p); err != nil {
				return nil, err
			}
		}
		out, err := k.CredentialsTake(context.Background())
		if err != nil {
			return nil, err
		}
		if out.Changes == nil {
			out.Changes = []CredentialChange{}
		}
		return &out, nil
	})
}
