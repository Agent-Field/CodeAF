package appwire

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ── INVITATIONS: LISTED, MADE-AND-COPIED, RETRIED, REVOKED ──────────────────
//
// factory.links lists the invitations a team factory has made. factory.link_create
// makes ONE and puts it on the clipboard of the machine this engine runs on.
// factory.link_copy puts the invitation the engine is still holding on the
// clipboard again. factory.link_revoke ends one. Nothing else: there is no
// method that names a route, a verb or a body.
//
// THE INVITATION'S ADDRESS AND SECRET NEVER CROSS THIS WIRE. The factory shows
// them once, in the answer to creating the invitation; the engine takes them
// from that answer, writes them to the clipboard, and answers only a row of
// public facts and an outcome. No field here is named for, or holds, the
// address, the secret or anything that opens a sign-in. [FactoryLinkMade.Ref]
// is the engine's own opaque handle on a held copy; it is not the factory's id
// for the invitation and opens nothing on its own.
//
// ONLY WHAT WAS CONFIRMED IS EVER CALLED COPIED. Outcome is "copied" when the
// clipboard helper said it succeeded, "not_copied" when it said it failed, and
// "unconfirmed" when it did not answer in time. In the last two the invitation
// exists and is real: it is neither revoked nor made again. The engine holds
// the same invitation in memory for at most ten minutes, never past the
// invitation's own expiry, and link_copy retries the copy of that same one.
//
// A CREATE IS SENT ONCE, AND AN AMBIGUOUS ONE IS NEVER GUESSED AT. A dropped
// connection, a timeout, a redirect, a 5xx or an answer that cannot be read as
// an invitation is [CodeUncertain]: the invitation may exist, nothing is
// copied or revoked, and the engine refuses another explicit create on that
// factory until factory.links has been read successfully. After that read the
// next create is allowed, and while an earlier create could still be live the
// list and the next create say so ([FactoryLinkList.EarlierUnconfirmed]).
//
// ALL OF THIS IS OPTIONAL AND DISTINCT FROM factory.admin. An engine that is
// not a [FactoryLinkAdmin] answers all four methods as unknown_method and does
// not announce [FeatureFactoryLinks].

// FeatureFactoryLinks is announced in hello when the engine serves
// factory.links, factory.link_create, factory.link_copy and
// factory.link_revoke.
const FeatureFactoryLinks = "factory.links"

// The limits a create is read against. The factory enforces the same two
// numbers; they are repeated here so a request that cannot succeed is never
// sent (cmd/codeaf asserts they equal the factory's own).
const (
	// LinkMaxUses is the most people one invitation may admit.
	LinkMaxUses = 50
	// LinkNameMax is the longest note on an invitation, in characters.
	LinkNameMax = 80
	// LinkMaxRows is the most invitations one list answers.
	LinkMaxRows = 500
	// LinkHoldFor is the longest the engine holds a made invitation for a
	// retry of the copy.
	LinkHoldFor = 10 * time.Minute
	// LinkHoldMax is the most invitations held at once, over every factory.
	LinkHoldMax = 32
)

// The outcomes of a create or a retried copy. Only LinkCopied may be shown as
// copied.
const (
	LinkCopied      = "copied"
	LinkNotCopied   = "not_copied"
	LinkUnconfirmed = "unconfirmed"
)

var (
	linkIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	linkRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
	// linkLogin mirrors the factory's own handle rule.
	linkLogin = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}(\[bot\])?$`)
)

// FactoryLinkParams names the connection. It is the whole request of
// factory.links.
type FactoryLinkParams struct {
	Factory string `json:"factory"`
}

// FactoryLinkCreateParams is what a person chose. Every field but the
// connection is optional, and an omitted one is the factory's own default: one
// person, seven days, no note, any GitHub login. MaxUses, when given, is 1 to
// [LinkMaxUses]; Login admits that one GitHub login and so needs MaxUses of 1
// or none; ExpiresAt, when given, is in the future and within seven days.
type FactoryLinkCreateParams struct {
	Factory   string     `json:"factory"`
	Name      string     `json:"name,omitempty"`
	Login     string     `json:"login,omitempty"`
	MaxUses   *int       `json:"max_uses,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// FactoryLinkCopyParams names the held invitation to copy again.
type FactoryLinkCopyParams struct {
	Factory string `json:"factory"`
	Ref     string `json:"ref"`
}

// FactoryLinkRevokeParams names the invitation to end, by the id a list gave.
type FactoryLinkRevokeParams struct {
	Factory string `json:"factory"`
	ID      string `json:"id"`
}

// FactoryLink is one invitation, as public facts. Counted is nil when the
// factory did not say; true means revoking it leaves the people who joined
// signed in, false means revoking it ends their sessions as well.
type FactoryLink struct {
	ID        string     `json:"id"`
	Name      string     `json:"name,omitempty"`
	Login     string     `json:"login,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedBy string     `json:"created_by"`
	MaxUses   int        `json:"max_uses"`
	Used      int        `json:"used"`
	Remaining int        `json:"remaining"`
	Status    string     `json:"status" enum:"open,used_up,expired,revoked"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	UsedBy    []string   `json:"used_by"`
	Counted   *bool      `json:"counted,omitempty"`
}

// FactoryLinkList is the factory's invitations: people's invitations only,
// never the operator's own claim or a script's key. EarlierUnconfirmed is true
// while a create this engine could not confirm may still be live, so one of
// these rows (or none) may be it.
type FactoryLinkList struct {
	Factory            string        `json:"factory"`
	Items              []FactoryLink `json:"items"`
	EarlierUnconfirmed bool          `json:"earlier_unconfirmed"`
}

// FactoryLinkMade answers a create or a retried copy. Entry is the
// invitation's row. Ref and HoldsUntil are present exactly when the engine is
// holding the invitation for another try at the copy. EarlierUnconfirmed is
// true when a previous create on this factory was never confirmed and could
// still be live.
type FactoryLinkMade struct {
	Factory            string      `json:"factory"`
	Entry              FactoryLink `json:"entry"`
	Outcome            string      `json:"outcome" enum:"copied,not_copied,unconfirmed"`
	Ref                string      `json:"ref,omitempty"`
	HoldsUntil         *time.Time  `json:"holds_until,omitempty"`
	EarlierUnconfirmed bool        `json:"earlier_unconfirmed"`
}

// FactoryLinkRevoked is what the factory confirmed.
type FactoryLinkRevoked struct {
	Factory string `json:"factory"`
	ID      string `json:"id"`
	Revoked bool   `json:"revoked"`
}

// FactoryLinkAdmin is the engine's half of the four invitation methods. Every
// write is sent once; an outcome it cannot confirm is [CodeUncertain].
type FactoryLinkAdmin interface {
	FactoryLinkList(ctx context.Context, p FactoryLinkParams) (FactoryLinkList, error)
	FactoryLinkCreate(ctx context.Context, p FactoryLinkCreateParams) (FactoryLinkMade, error)
	FactoryLinkCopy(ctx context.Context, p FactoryLinkCopyParams) (FactoryLinkMade, error)
	FactoryLinkRevoke(ctx context.Context, p FactoryLinkRevokeParams) (FactoryLinkRevoked, error)
}

func withFactoryLinksFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryLinkAdmin); ok {
		hello = withFeature(hello, FeatureFactoryLinks)
	}
	return hello
}

func factoryLinksOf(e Engine, name string) (FactoryLinkAdmin, error) {
	a, ok := e.(FactoryLinkAdmin)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return a, nil
}

// CheckLinkNote bounds a note: one line of at most [LinkNameMax] characters,
// no control characters. It answers the trimmed note.
func CheckLinkNote(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > LinkNameMax {
		return "", Errorf(CodeBadParams, "name is at most %d characters", LinkNameMax)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", Errorf(CodeBadParams, "name is one line of plain text")
		}
	}
	return name, nil
}

// CheckLinkLogin answers login in the factory's own spelling, or why it is not
// a GitHub login.
func CheckLinkLogin(login string) (string, error) {
	login = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(login), "@"))
	if login != "" && !linkLogin.MatchString(login) {
		return "", Errorf(CodeBadParams, "login must be a GitHub login")
	}
	return login, nil
}

// CheckLinkID is true for an id of the shape the factory makes. It decides what
// may be placed in a request path; the engine escapes it as well.
func CheckLinkID(id string) bool { return linkIDPattern.MatchString(id) }

// CheckLinkRef is true for a ref of the shape the engine makes.
func CheckLinkRef(ref string) bool { return linkRefPattern.MatchString(ref) }

// CheckLinkCreate reads a create's choices without the clock: bounds that need
// no "now". The engine checks the expiry against its own clock.
func CheckLinkCreate(p FactoryLinkCreateParams) (FactoryLinkCreateParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	if p.Name, err = CheckLinkNote(p.Name); err != nil {
		return p, err
	}
	if p.Login, err = CheckLinkLogin(p.Login); err != nil {
		return p, err
	}
	if p.MaxUses != nil {
		if *p.MaxUses < 1 || *p.MaxUses > LinkMaxUses {
			return p, Errorf(CodeBadParams, "max_uses is a whole number from 1 to %d", LinkMaxUses)
		}
		if p.Login != "" && *p.MaxUses > 1 {
			return p, Errorf(CodeBadParams, "a link for one GitHub login admits one person")
		}
	}
	if p.ExpiresAt != nil && p.ExpiresAt.IsZero() {
		return p, Errorf(CodeBadParams, "expires_at is a time in the future")
	}
	return p, nil
}

// linkCall builds one invitation method. Params are read STRICTLY, so a url, a
// token, a path or a body a caller tries to send is bad_params and never
// believed to have been honoured. check runs before the engine is asked.
func linkCall[P, R any](name string, check func(P) (P, error), call func(FactoryLinkAdmin, context.Context, P) (R, error)) method {
	return shaped[P, R](func(e Engine, raw json.RawMessage) (*R, error) {
		a, err := factoryLinksOf(e, name)
		if err != nil {
			return nil, err
		}
		var p P
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		if p, err = check(p); err != nil {
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
		"factory.links": linkCall("factory.links", func(p FactoryLinkParams) (FactoryLinkParams, error) {
			var err error
			p.Factory, err = factoryID(strings.TrimSpace(p.Factory))
			return p, err
		}, FactoryLinkAdmin.FactoryLinkList),
		"factory.link_create": linkCall("factory.link_create", CheckLinkCreate, FactoryLinkAdmin.FactoryLinkCreate),
		"factory.link_copy": linkCall("factory.link_copy", func(p FactoryLinkCopyParams) (FactoryLinkCopyParams, error) {
			var err error
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return p, err
			}
			if p.Ref = strings.TrimSpace(p.Ref); !CheckLinkRef(p.Ref) {
				return p, Errorf(CodeBadParams, "ref is not one this engine made")
			}
			return p, nil
		}, FactoryLinkAdmin.FactoryLinkCopy),
		"factory.link_revoke": linkCall("factory.link_revoke", func(p FactoryLinkRevokeParams) (FactoryLinkRevokeParams, error) {
			var err error
			if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
				return p, err
			}
			if p.ID = strings.TrimSpace(p.ID); !CheckLinkID(p.ID) {
				return p, Errorf(CodeBadParams, "id is not one a factory makes")
			}
			return p, nil
		}, FactoryLinkAdmin.FactoryLinkRevoke),
	} {
		methods[name] = m
		factoryLinkMethods[name] = m
	}
}

// factoryLinkMethods is the table of the four invitation methods, for the
// tests that walk it.
var factoryLinkMethods = map[string]method{}
