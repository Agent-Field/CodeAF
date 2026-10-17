package appwire

import (
	"context"
	"encoding/json"
	"strings"
)

// ── ENDING A SIGN-IN, AND ENDING EVERYONE'S ─────────────────────────────────
//
// factory.sign_out ends the sign-in THIS app holds on one attached team
// factory. factory.revoke_sessions asks the factory, as the operator this
// caller already is, to end every sign-in it has. Each is ONE method with one
// parameter, the connection; there is no method that names a session, a route
// or a body, so the wire cannot be used as a proxy or to end someone else's
// sign-in.
//
// NO SESSION ID CROSSES THIS WIRE, IN EITHER DIRECTION. The engine finds its
// own sign-in's id by asking the factory who it is, uses it once, and keeps it
// to itself. Params carry none and answers carry none; the schema test that
// walks every factory result for a credential-looking field covers these too.
//
// A WRITE IS SENT ONCE. Neither method is retried, replayed or tried by a second
// route when the first said no. An outcome the engine cannot confirm (a
// dropped connection, a timeout, a 5xx) is [CodeUncertain]: the person may
// still be signed in, or may not, and the answer never claims either. Only a
// confirmed yes ends the sign-in this app holds.
//
// THE MACHINE'S OWN FACTORY IS NOT SIGNED OUT OF. The solo factory this app
// serves has no sign-in to end, only the key the app made for it, so both
// methods refuse it as [CodeConflict] instead of detaching or stopping it. A
// connection that holds a key rather than a sign-in is refused by sign_out for
// the same reason.
//
// ALL OF THIS IS OPTIONAL. An engine that is not a [FactoryAdmin] answers both
// methods as unknown_method and does not announce [FeatureFactoryAdmin].

// FeatureFactoryAdmin is announced in hello when the engine serves
// factory.sign_out and factory.revoke_sessions.
const FeatureFactoryAdmin = "factory.admin"

// The actions a [FactoryAdminDone] names.
const (
	AdminActSignOut        = "sign_out"
	AdminActRevokeSessions = "revoke_sessions"
)

// FactoryAdminParams names the connection. It is the whole request of both
// methods.
type FactoryAdminParams struct {
	Factory string `json:"factory"`
}

// FactoryAdminDone is what the factory confirmed. SignedOut is true when the
// credential used for this request was a sign-in and that sign-in ended; false
// means the request used a key that revoking sessions does not end. A newer
// sign-in may have completed meanwhile, so read the current connection rather
// than treating this receipt as its current state. Only answered after success.
type FactoryAdminDone struct {
	Factory   string `json:"factory"`
	Action    string `json:"action" enum:"sign_out,revoke_sessions"`
	SignedOut bool   `json:"signed_out"`
}

// FactoryAdmin is the engine's half of factory.sign_out and
// factory.revoke_sessions. Every method makes at most one write to the factory
// and never sends it twice; an outcome it cannot confirm is [CodeUncertain].
type FactoryAdmin interface {
	FactorySignOut(ctx context.Context, p FactoryAdminParams) (FactoryAdminDone, error)
	FactoryRevokeSessions(ctx context.Context, p FactoryAdminParams) (FactoryAdminDone, error)
}

func withFactoryAdminFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryAdmin); ok {
		hello = withFeature(hello, FeatureFactoryAdmin)
	}
	return hello
}

func factoryAdminOf(e Engine, name string) (FactoryAdmin, error) {
	a, ok := e.(FactoryAdmin)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return a, nil
}

// factoryAdminCall builds one admin method. Params are read STRICTLY, so a
// session id, a path or a body a caller tries to send is bad_params and never
// believed to have been honoured.
func factoryAdminCall(name string, call func(FactoryAdmin, context.Context, FactoryAdminParams) (FactoryAdminDone, error)) method {
	return shaped[FactoryAdminParams, FactoryAdminDone](func(e Engine, raw json.RawMessage) (*FactoryAdminDone, error) {
		a, err := factoryAdminOf(e, name)
		if err != nil {
			return nil, err
		}
		var p FactoryAdminParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := call(a, ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func init() {
	for name, m := range map[string]method{
		"factory.sign_out":        factoryAdminCall("factory.sign_out", FactoryAdmin.FactorySignOut),
		"factory.revoke_sessions": factoryAdminCall("factory.revoke_sessions", FactoryAdmin.FactoryRevokeSessions),
	} {
		methods[name] = m
		factoryAdminMethods[name] = m
	}
}

// factoryAdminMethods is the table of factory.sign_out and
// factory.revoke_sessions, for the tests that walk it.
var factoryAdminMethods = map[string]method{}
