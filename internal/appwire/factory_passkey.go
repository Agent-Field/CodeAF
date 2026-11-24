package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ── PASSKEYS ON AN ORGANIZATION ─────────────────────────────────────────────
//
// A passkey ceremony runs in the person's own browser, on a page the
// organization's server serves: only a page on the server's own origin may
// use a passkey for it, and the desk's web view is not one. So:
//
//   factory.passkey_start   the engine starts a flow on the server and
//                           answers the url the client opens in the browser
//   factory.passkey_poll    the client asks, every couple of seconds, until
//                           the browser has finished
//   factory.passkey_cancel  the client gave up
//   factory.passkey_list    the signed-in person's passkeys
//   factory.passkey_remove  remove one (the server refuses the last way in)
//   factory.password_confirm  the current password, as a step-up
//
// THE CEREMONY ENDS ON THIS COMPUTER. The engine keeps a one-shot listener on
// 127.0.0.1 for each flow; the server hands the browser the page is bound to
// there with a one-time code only that listener accepts, and only a poll with
// that code gets the session. A ceremony
// finished in a browser on another device polls as "elsewhere" and never
// completes; the client keeps polling (it may still be opened here) or
// cancels.
//
// STEP-UP: ONE CONFIRMATION FOR ONE ACTION. Adding a passkey, setting a first
// password (password_change with an empty current) and removing a passkey
// each need a confirmation made for that action in the last five minutes;
// it lets the action happen once. Without one they fail with [CodeStepUp],
// whose Detail ([FactoryStepUpDetail]) names the action. A sign-in is never
// one. The client confirms that action, with passkey_start purpose "confirm"
// and the same action (one of the person's passkeys; the page names the
// action and this computer), or password_confirm with it, and retries once.
//
// THE ENGINE HOLDS THE VERIFIER. The flow is started with the SHA-256 of a
// 32-byte secret the engine makes and keeps; the server answers the session
// only to a poll that shows that secret, so the url the client opens cannot
// mint a session for whoever else sees it. Neither the verifier nor the
// session ever crosses this wire: a done poll keeps the session in the
// engine exactly as factory.access_sign_in does.
//
// ALL OF THIS IS OPTIONAL. An engine that is not a [FactoryPasskey] answers
// the five as unknown_method and does not announce [FeatureFactoryPasskey];
// a server that cannot offer passkeys leaves "passkey" out of sign_in.

// FeatureFactoryPasskey is announced in hello when the engine serves the
// passkey methods.
const FeatureFactoryPasskey = "factory_passkey"

// CodeStepUp is an action that needs a recent proof (see the file comment).
const CodeStepUp = "step_up"

// Passkey flow purposes and poll states, as the server spells them.
const (
	FactoryPasskeySignIn = "sign_in"
	FactoryPasskeyJoin   = "join"
	FactoryPasskeyAdd    = "add"
	// FactoryPasskeyConfirm proves the signed-in person is there, with one
	// of their passkeys: a step-up, signing nobody new in.
	FactoryPasskeyConfirm = "confirm"

	FactoryPasskeyPending = "pending"
	FactoryPasskeyDone    = "done"
	FactoryPasskeyExpired = "expired"
	FactoryPasskeyRefused = "refused"
	// FactoryPasskeyElsewhere is a ceremony finished in a browser that has
	// not reached this computer. Not final: keep polling, or cancel.
	FactoryPasskeyElsewhere = "elsewhere"
)

// FactoryPasskeyStartParams starts a ceremony. Exactly one of Factory (an
// attached organization: sign_in, add) and Link (an invitation, operator
// claim or reset link: join) names where.
type FactoryPasskeyStartParams struct {
	Factory  string `json:"factory,omitempty"`
	Link     string `json:"link,omitempty"`
	Caller   string `json:"caller"`
	Purpose  string `json:"purpose" enum:"sign_in,join,add,confirm"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	// Action (with Passkey for remove_passkey) is what a confirm is for;
	// required with purpose confirm and refused otherwise.
	Action  string `json:"action,omitempty" enum:"add_passkey,remove_passkey,set_password"`
	Passkey string `json:"passkey,omitempty"`
}

// The actions a confirmation is for (see the file comment).
const (
	FactoryStepUpAddPasskey    = "add_passkey"
	FactoryStepUpRemovePasskey = "remove_passkey"
	FactoryStepUpSetPassword   = "set_password"
)

// FactoryStepUpDetail is the [Error.Detail] of a [CodeStepUp]: the one
// action to confirm, and for remove_passkey which passkey.
type FactoryStepUpDetail struct {
	Action  string `json:"action" enum:"add_passkey,remove_passkey,set_password"`
	Passkey string `json:"passkey,omitempty"`
}

// stepUpAction checks an action and its passkey, as a confirm names them.
func stepUpAction(action, passkey string) error {
	switch action {
	case FactoryStepUpAddPasskey, FactoryStepUpSetPassword:
		if passkey != "" {
			return Errorf(CodeBadParams, "only remove_passkey names a passkey")
		}
	case FactoryStepUpRemovePasskey:
		if passkey == "" || len(passkey) > 512 {
			return Errorf(CodeBadParams, "remove_passkey names the passkey's id")
		}
	default:
		return Errorf(CodeBadParams, "action is add_passkey, remove_passkey or set_password")
	}
	return nil
}

// FactoryPasskeyStarted answers a start: open URL in the default browser.
// URL is always the organization server's own https origin (or a test
// server's localhost).
type FactoryPasskeyStarted struct {
	Flow      string    `json:"flow"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// FactoryPasskeyPollParams asks after, or cancels, a flow this caller started.
type FactoryPasskeyPollParams struct {
	Flow   string `json:"flow"`
	Caller string `json:"caller"`
}

// FactoryPasskeyPolled answers a poll. Factory is the connection the flow
// signed in to, once done (a join attaches a new one).
type FactoryPasskeyPolled struct {
	State   string `json:"state" enum:"pending,done,expired,refused,elsewhere"`
	Message string `json:"message,omitempty"`
	Factory string `json:"factory,omitempty"`
}

// FactoryPasskeyListParams names the attached organization.
type FactoryPasskeyListParams struct {
	Factory string `json:"factory"`
}

// FactoryPasskey is one of the signed-in person's passkeys.
type FactoryPasskey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Synced     bool       `json:"synced"`
}

// FactoryPasskeyList answers passkey_list. Password says whether the person
// also has a password, which decides whether their last passkey may go.
type FactoryPasskeyList struct {
	Passkeys []FactoryPasskey `json:"passkeys"`
	Password bool             `json:"password"`
}

// FactoryPasskeyRemoveParams removes passkey ID.
type FactoryPasskeyRemoveParams struct {
	Factory string `json:"factory"`
	ID      string `json:"id"`
}

// FactoryPasswordConfirmParams shows the signed-in person's current password.
type FactoryPasswordConfirmParams struct {
	Factory  string `json:"factory"`
	Password string `json:"password"`
	Action   string `json:"action" enum:"add_passkey,remove_passkey,set_password"`
	Passkey  string `json:"passkey,omitempty"`
}

// FactoryPasskeys is the engine's half of the passkey methods.
type FactoryPasskeys interface {
	FactoryPasskeyStart(ctx context.Context, p FactoryPasskeyStartParams) (FactoryPasskeyStarted, error)
	FactoryPasskeyPoll(ctx context.Context, p FactoryPasskeyPollParams) (FactoryPasskeyPolled, error)
	FactoryPasskeyCancel(ctx context.Context, p FactoryPasskeyPollParams) error
	FactoryPasskeyList(ctx context.Context, p FactoryPasskeyListParams) (FactoryPasskeyList, error)
	FactoryPasskeyRemove(ctx context.Context, p FactoryPasskeyRemoveParams) error
	FactoryPasswordConfirm(ctx context.Context, p FactoryPasswordConfirmParams) error
}

func withFactoryPasskeyFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPasskeys); ok {
		hello = withFeature(hello, FeatureFactoryPasskey)
	}
	return hello
}

func passkeyCall[P, R any](name string, check func(*P) error, call func(FactoryPasskeys, context.Context, P) (R, error)) method {
	return shaped[P, R](func(e Engine, raw json.RawMessage) (*R, error) {
		f, ok := e.(FactoryPasskeys)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p P
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.DisallowUnknownFields()
		if err := d.Decode(&p); err != nil {
			return nil, Errorf(CodeBadParams, "%s: the params are not the ones this method takes", name)
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

func passkeyFlowID(flow string) (string, error) {
	flow = strings.TrimSpace(flow)
	if flow == "" || len(flow) > 128 {
		return "", Errorf(CodeBadParams, "flow is required")
	}
	return flow, nil
}

func init() {
	pollCheck := func(p *FactoryPasskeyPollParams) error {
		var err error
		if p.Caller, err = factoryCaller(p.Caller); err != nil {
			return err
		}
		p.Flow, err = passkeyFlowID(p.Flow)
		return err
	}
	for name, m := range map[string]method{
		"factory.passkey_start": passkeyCall("factory.passkey_start", func(p *FactoryPasskeyStartParams) error {
			var err error
			if p.Caller, err = factoryCaller(p.Caller); err != nil {
				return err
			}
			p.Factory, p.Link = strings.TrimSpace(p.Factory), strings.TrimSpace(p.Link)
			switch p.Purpose {
			case FactoryPasskeySignIn, FactoryPasskeyAdd, FactoryPasskeyConfirm:
				if p.Link != "" || p.Factory == "" {
					return Errorf(CodeBadParams, "%s names the factory, not a link", p.Purpose)
				}
			case FactoryPasskeyJoin:
				if p.Factory != "" {
					return Errorf(CodeBadParams, "join names the link, not a factory")
				}
				if p.Link, err = factoryLink(p.Link); err != nil {
					return err
				}
			default:
				return Errorf(CodeBadParams, "purpose is sign_in, join, add or confirm")
			}
			if p.Purpose != FactoryPasskeyJoin && (p.Username != "" || p.Name != "") {
				return Errorf(CodeBadParams, "only join takes a username")
			}
			if p.Purpose == FactoryPasskeyConfirm {
				if err := stepUpAction(p.Action, p.Passkey); err != nil {
					return err
				}
			} else if p.Action != "" || p.Passkey != "" {
				return Errorf(CodeBadParams, "only confirm names an action")
			}
			if p.Username = strings.TrimSpace(p.Username); len(p.Username) > 64 {
				return Errorf(CodeBadParams, "username is at most 64 characters")
			}
			if len(p.Name) > 256 {
				return Errorf(CodeBadParams, "name is at most 256 bytes")
			}
			return nil
		}, FactoryPasskeys.FactoryPasskeyStart),
		"factory.passkey_poll": passkeyCall("factory.passkey_poll", pollCheck, FactoryPasskeys.FactoryPasskeyPoll),
		"factory.passkey_cancel": passkeyCall("factory.passkey_cancel", pollCheck, func(f FactoryPasskeys, ctx context.Context, p FactoryPasskeyPollParams) (Empty, error) {
			return Empty{}, f.FactoryPasskeyCancel(ctx, p)
		}),
		"factory.passkey_list": passkeyCall("factory.passkey_list", func(p *FactoryPasskeyListParams) error {
			var err error
			p.Factory, err = factoryID(strings.TrimSpace(p.Factory))
			return err
		}, FactoryPasskeys.FactoryPasskeyList),
		"factory.passkey_remove": passkeyCall("factory.passkey_remove", func(p *FactoryPasskeyRemoveParams) error {
			var err error
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return err
			}
			if p.ID = strings.TrimSpace(p.ID); p.ID == "" || len(p.ID) > 1024 {
				return Errorf(CodeBadParams, "id is required")
			}
			return nil
		}, func(f FactoryPasskeys, ctx context.Context, p FactoryPasskeyRemoveParams) (Empty, error) {
			return Empty{}, f.FactoryPasskeyRemove(ctx, p)
		}),
		"factory.password_confirm": passkeyCall("factory.password_confirm", func(p *FactoryPasswordConfirmParams) error {
			var err error
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return err
			}
			if err := stepUpAction(p.Action, p.Passkey); err != nil {
				return err
			}
			return factoryPassword("password", p.Password)
		}, func(f FactoryPasskeys, ctx context.Context, p FactoryPasswordConfirmParams) (Empty, error) {
			return Empty{}, f.FactoryPasswordConfirm(ctx, p)
		}),
	} {
		methods[name] = m
		factoryPasskeyMethods[name] = m
	}
}

// factoryPasskeyMethods is the table of the six, for the tests that walk it.
var factoryPasskeyMethods = map[string]method{}
