package appwire

import (
	"context"
	"encoding/json"
	"strings"
)

// ── USERNAME + PASSWORD ON AN ORGANIZATION ──────────────────────────────────
//
// factory.access_sign_up makes an account through an operator claim or an
// invitation link. factory.password_sign_in signs a person back in to an
// attached organization with no link. factory.password_change changes it,
// signed in. factory.access_reset sets a new one through the operator's reset
// link. Each sign-in keeps its session in the engine exactly as
// factory.access_sign_in does: THE SESSION CREDENTIAL NEVER CROSSES THIS WIRE.
//
// THE PASSWORD CROSSES IT ONCE, INWARD. These four are methods of their own so
// a client's wire trace can drop the password, current and new params by
// method and field name (the desk's wire log does). The engine registers each
// one with the redact and trace scrubbers the moment it is read, puts it in no
// error and no answer, and holds it only for the one request to the server.
//
// ALL OF THIS IS OPTIONAL. An engine that is not a [FactoryPassword] answers
// the four as unknown_method and does not announce [FeatureFactoryPassword].
// A server that does not take passwords says so in factory.list's sign_in.

// FeatureFactoryPassword is announced in hello when the engine serves the
// four password methods.
const FeatureFactoryPassword = "factory_password"

// FactoryPasswordMost bounds a password param before the server's own rules
// (12 characters to 256 bytes) are applied there.
const FactoryPasswordMost = 1024

// FactorySignedInState is the one state password_sign_in answers.
const FactorySignedInState = "signed_in"

// FactoryAccessSignUpParams makes an account through link.
type FactoryAccessSignUpParams struct {
	Link     string `json:"link"`
	Caller   string `json:"caller"`
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name,omitempty"`
}

// FactoryAccessResetParams sets a new password through a reset link.
type FactoryAccessResetParams struct {
	Link     string `json:"link"`
	Caller   string `json:"caller"`
	Password string `json:"password"`
}

// FactoryPasswordSignInParams signs in to the attached factory again.
type FactoryPasswordSignInParams struct {
	Factory  string `json:"factory"`
	Caller   string `json:"caller"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// FactoryPasswordChangeParams changes the signed-in person's password.
type FactoryPasswordChangeParams struct {
	Factory string `json:"factory"`
	Current string `json:"current"`
	New     string `json:"new"`
}

// FactoryPasswordSignedIn answers password_sign_in: signed in, and who.
type FactoryPasswordSignedIn struct {
	State string    `json:"state" enum:"signed_in"`
	Me    FactoryMe `json:"me"`
}

// FactoryPassword is the engine's half of the four password methods.
type FactoryPassword interface {
	FactoryAccessSignUp(ctx context.Context, p FactoryAccessSignUpParams) (FactoryAccess, error)
	FactoryAccessReset(ctx context.Context, p FactoryAccessResetParams) (FactoryAccess, error)
	FactoryPasswordSignIn(ctx context.Context, p FactoryPasswordSignInParams) (FactoryPasswordSignedIn, error)
	FactoryPasswordChange(ctx context.Context, p FactoryPasswordChangeParams) error
}

func withFactoryPasswordFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPassword); ok {
		hello = withFeature(hello, FeatureFactoryPassword)
	}
	return hello
}

func factoryPasswordOf(e Engine, name string) (FactoryPassword, error) {
	p, ok := e.(FactoryPassword)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return p, nil
}

// factoryPassword checks a password param's presence and bound. Its refusal
// never quotes the value.
func factoryPassword(field, value string) error {
	if value == "" || len(value) > FactoryPasswordMost {
		return Errorf(CodeBadParams, "%s is required and at most %d bytes", field, FactoryPasswordMost)
	}
	return nil
}

// decodePasswordParams reads params strictly. Its refusal names no value: a
// JSON error can quote what it failed to read, and that may be a password.
func decodePasswordParams(name string, raw json.RawMessage, v any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return Errorf(CodeBadParams, "%s: the params are not the ones this method takes", name)
	}
	return nil
}

func passwordCall[P, R any](name string, check func(*P) error, call func(FactoryPassword, context.Context, P) (R, error)) method {
	return shaped[P, R](func(e Engine, raw json.RawMessage) (*R, error) {
		f, err := factoryPasswordOf(e, name)
		if err != nil {
			return nil, err
		}
		var p P
		if err := decodePasswordParams(name, raw, &p); err != nil {
			return nil, err
		}
		if err := check(&p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := call(f, ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func init() {
	for name, m := range map[string]method{
		"factory.access_sign_up": passwordCall("factory.access_sign_up", func(p *FactoryAccessSignUpParams) error {
			var err error
			if p.Caller, err = factoryCaller(p.Caller); err != nil {
				return err
			}
			if p.Link, err = factoryLink(p.Link); err != nil {
				return err
			}
			if p.Username = strings.TrimSpace(p.Username); p.Username == "" || len(p.Username) > 64 {
				return Errorf(CodeBadParams, "username is required")
			}
			if len(p.Name) > 256 {
				return Errorf(CodeBadParams, "name is at most 256 bytes")
			}
			return factoryPassword("password", p.Password)
		}, FactoryPassword.FactoryAccessSignUp),
		"factory.access_reset": passwordCall("factory.access_reset", func(p *FactoryAccessResetParams) error {
			var err error
			if p.Caller, err = factoryCaller(p.Caller); err != nil {
				return err
			}
			if p.Link, err = factoryLink(p.Link); err != nil {
				return err
			}
			return factoryPassword("password", p.Password)
		}, FactoryPassword.FactoryAccessReset),
		"factory.password_sign_in": passwordCall("factory.password_sign_in", func(p *FactoryPasswordSignInParams) error {
			var err error
			if p.Caller, err = factoryCaller(p.Caller); err != nil {
				return err
			}
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return err
			}
			if p.Username = strings.TrimSpace(p.Username); p.Username == "" || len(p.Username) > 64 {
				return Errorf(CodeBadParams, "username is required")
			}
			return factoryPassword("password", p.Password)
		}, FactoryPassword.FactoryPasswordSignIn),
		"factory.password_change": passwordCall("factory.password_change", func(p *FactoryPasswordChangeParams) error {
			var err error
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return err
			}
			if err := factoryPassword("current", p.Current); err != nil {
				return err
			}
			return factoryPassword("new", p.New)
		}, func(f FactoryPassword, ctx context.Context, p FactoryPasswordChangeParams) (Empty, error) {
			return Empty{}, f.FactoryPasswordChange(ctx, p)
		}),
	} {
		methods[name] = m
		factoryPasswordMethods[name] = m
	}
}

// factoryPasswordMethods is the table of the four, for the tests that walk it.
var factoryPasswordMethods = map[string]method{}
