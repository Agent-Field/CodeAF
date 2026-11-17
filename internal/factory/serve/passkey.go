package serve

// ── PASSKEYS (WebAuthn) ON AN ORGANIZATION SERVER ───────────────────────────
//
// A passkey is the first way into an organization wherever the server can
// offer one; a password is the second and GitHub the third. The doors:
//
//   POST /v1/auth/passkey/start   the engine starts a flow: purpose sign_in,
//                                 join (an invitation, operator claim or
//                                 reset link) or add (signed in), with the
//                                 SHA-256 of a verifier only it holds
//   GET  /passkey/<flow>          the ceremony page, opened in the person's
//                                 own browser
//   POST /passkey/<flow>/options  the page asks for a fresh challenge
//   POST /passkey/<flow>/finish   the page hands back what the authenticator
//                                 signed
//   POST /v1/auth/passkey/poll    the engine, with its verifier, until done;
//                                 the session is answered exactly once
//   POST /v1/auth/passkey/cancel  the engine ends its own flow
//   GET  /v1/me/passkeys          the signed-in person's passkeys
//   POST /v1/me/passkeys/remove   remove one (never the last way in)
//
// WHY A BROWSER. A WebAuthn ceremony is bound to the origin of the page that
// runs it, and the relying party id must be that origin's host. The desk is a
// web view loaded from the app itself; macOS lets an app run WebAuthn for a
// domain only when the domain is in its signed webcredentials entitlement,
// fixed at build time, which an organization's own server can never be. So
// the ceremony runs on a page this server serves, in the person's browser,
// and the result is handed back to the engine the way GitHub's device flow
// hands one back: by polling.
//
// WHY A VERIFIER. The flow's url travels from the engine to the desk to the
// browser. Whoever holds it can perform the ceremony, but the session is
// answered only to a poll that shows the verifier whose SHA-256 started the
// flow, and the verifier never leaves the engine. A stolen url alone cannot
// mint a session. A poll with a wrong verifier ends the flow.
//
// WHERE PASSKEYS ARE OFFERED. Only on a team server that keeps accounts and
// whose address is https with a DNS name: WebAuthn refuses a bare IP address
// and a page without a secure context. A server that cannot offer passkeys
// does not list them in /v1/about sign_in and answers 404 on these routes:
// absent, not broken.
//
// THE RULES THE LIBRARY ENFORCES. github.com/go-webauthn/webauthn parses and
// verifies every CBOR, COSE and signature byte; this file never does. It is
// configured with the relying party id = the server's host, the one allowed
// origin = the server's own scheme://host[:port], attestation "none", a
// discoverable credential required and user verification REQUIRED (which is
// what makes Touch ID, Windows Hello or the phone's unlock mandatory).
// authenticatorAttachment is left unset so a platform authenticator, a phone
// over hybrid (QR), a security key and a password manager are all offered.
//
// EVERY CHALLENGE IS SINGLE USE: it is taken out of the flow before it is
// verified, so a replayed response meets no challenge. A challenge lives five
// minutes, a flow ten.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

const (
	signInPasskey = "passkey"

	// passkeyFlowLife is how long a started flow may be finished and polled.
	passkeyFlowLife = 10 * time.Minute
	// passkeyChallengeLife is how long one challenge may be answered.
	passkeyChallengeLife = 5 * time.Minute
	// passkeyFlowsMost bounds the flows in flight, so starts cannot exhaust
	// the server's memory. Sign-ins, which anyone may start, hold at most
	// passkeySignInFlowsMost of them: the rest is kept for joins (which need
	// a live link) and for signed-in adds and confirms, so a flood of
	// sign-ins never shuts a person with an invitation out.
	passkeyFlowsMost       = 512
	passkeySignInFlowsMost = 384
	// passkeyFlowsPerAddr is how many flows one address (an IPv6 /64) may
	// have waiting at once.
	passkeyFlowsPerAddr = 8
	// passkeyOpenedPerPrefix is how many opened sign-ins one /24 (IPv4) or
	// /48 (IPv6) keeps; one more drops the oldest of them.
	passkeyOpenedPerPrefix = 16
	// passkeyFairShare is how many opened sign-ins a network (/24, or /32
	// for IPv6) keeps against everyone else: a full table never takes one
	// of these from it to make room for another network.
	passkeyFairShare = 4
	// passkeyCodeLife is how long a finished flow's hand-over and code work.
	passkeyCodeLife = 60 * time.Second
	// passkeyGrantsPerSession is how many confirmations one session holds;
	// a fourth replaces its oldest.
	passkeyGrantsPerSession = 3
	// passkeyGrantsPerAccount is how many one account holds across all its
	// sessions, so many sessions of one member cannot fill the table.
	passkeyGrantsPerAccount = 3
	// passkeyGrantsMost bounds the confirmations held in memory across all
	// sessions; past it the oldest anyone holds is dropped, never the new one.
	passkeyGrantsMost = 4096
	// passkeyUnopenedLife is how long a sign-in flow whose page was never
	// fetched lives; such flows are the first to go when sign-ins are full.
	passkeyUnopenedLife = 2 * time.Minute
	// passkeyWrongMost is how many polls or cancels with a wrong verifier or
	// code one flow survives: a guess is counted, not let end the flow.
	passkeyWrongMost = 5
	// passkeyStepUpLife is how recent a passkey or password proof must be
	// for adding a passkey, setting a first password or removing a passkey.
	passkeyStepUpLife = 5 * time.Minute
	// passkeyTriesMost is how many failed ceremonies one flow survives.
	passkeyTriesMost = 5
	// passkeyChallengesMost is how many challenges one flow may ask for.
	passkeyChallengesMost = 20
	// passkeyFinishMost bounds a ceremony response. With attestation "none"
	// a registration is well under 4 KiB; 32 KiB leaves room for any
	// authenticator's extensions without letting a body grow unbounded.
	passkeyFinishMost = 32 << 10
	// passkeyStarts is how many flows one address may start per throttle
	// window (15 minutes), spent on entry whether or not they finish. An
	// IPv6 address counts by its /48, so one subscriber's many /64s share it.
	passkeyStarts = 30
	// passkeyNameMost bounds a passkey's name.
	passkeyNameMost = 60
	// passkeyTryName prefixes the guard name a ceremony try counts under.
	// It holds a NUL, which no username can, so it never meets a person's.
	passkeyTryName = "\x00passkey\x00"
)

// Words a person reads.
const (
	passkeyOffWords      = "this organization does not take passkeys"
	passkeyEndedWords    = "This sign-in has ended. Start again in codeaf."
	passkeyRefusedWords  = "That passkey could not be used here. Try again."
	passkeyUnknownWords  = "That passkey is not one this organization knows."
	passkeyTakenWords    = "This passkey is already registered here."
	passkeyLastWords     = "This is your only way to sign in. Add a password or another passkey first."
	passkeyNotHereWords  = "This sign-in was not started here."
	passkeyNoAccount     = "this sign-in has no passkeys"
	passkeyWrongPageWord = "this page belongs to another server"
	passkeyStepUpWords   = "Confirm it's you first: use one of your passkeys, or enter your current password."
	passkeyFullWords     = "Too many passkey sign-ins are waiting. Try again in a minute."
	passkeyHandOverWords = "Returning to codeaf on the computer that started this…"
	// passkeyElsewhereWords is the page of a finished ceremony that has not
	// reached codeaf: on another device it never will.
	passkeyElsewhereWords = "Nothing was signed in on this device. Open the address on the computer where you started, or start again there."
)

// passkeyFlow is one ceremony in flight. Everything in it is server-side;
// the browser holds only the flow id (in its url) and the engine only the
// flow id and its verifier.
type passkeyFlow struct {
	id        string
	purpose   string
	challenge [sha256.Size]byte // SHA-256 of the engine's verifier
	created   time.Time
	expires   time.Time

	// What the flow is for. A join names a link row (linkID) or a reset
	// link (resetSecret); an add and a reset name the person (handle).
	linkID      string
	operator    bool
	resetSecret string
	handle      string

	// A join's chosen username, display name and fresh WebAuthn user handle.
	username string
	name     string
	userID   string

	// The open challenge, taken out before it is verified.
	session    *webauthn.SessionData
	sessionEnd time.Time
	challenges int
	tries      int

	// addr is the /64 the flow was started from and prefix its /24 (IPv4)
	// or /48 (IPv6); opened is whether a browser ever asked the page for a
	// ceremony (a GET alone does not count).
	addr   string
	prefix string
	// network is the flow's /24 (IPv4) or /32 (IPv6), the unit a full
	// table shares its room by; a /32 is what one party is commonly given.
	network string
	opened  bool
	// browserSum is the SHA-256 of the nonce in the cookie of the one
	// browser the flow is bound to, set on its first options call; zero is
	// unbound. Every later options, finish and hand-over needs that cookie.
	browserSum [sha256.Size]byte
	// returnTo is the engine's loopback url (no code yet) and returnKey the
	// engine's key for its code; codeSum is the SHA-256 of the one-time code
	// the hand-over sent the bound browser there with, made once (handed).
	// wrong counts polls and cancels with a wrong verifier or code.
	returnTo  string
	returnKey []byte
	codeSum   [sha256.Size]byte
	handed    bool
	wrong     int
	// sessionID is the signed-in session a confirm proves; action is the one
	// thing it confirms and requestedBy the computer that asked, as its page
	// says it (see requestedBy). doneAt is
	// when the ceremony finished: the code works passkeyCodeLife after it.
	sessionID   string
	action      api.StepUpAction
	requestedBy string
	requestedAt string
	passkeyName string
	doneAt      time.Time
	// made is a verified new passkey, kept until the poll that shows the
	// code: only then is an account made or changed, so a ceremony run on a
	// stolen url never spends a link or adds anyone's key.
	made *tokens.Passkey

	state   string
	message string
	// profile is whom a done sign-in, join or reset signs in, and gen the
	// account generation the ceremony saw: the poll mints a session only
	// while the account is still at it, so a passkey removed or a password
	// changed between the ceremony and the poll signs nobody in.
	profile *api.Profile
	gen     int64
}

// passkeyState is the handler's flows in flight and its start throttle.
type passkeyState struct {
	once   sync.Once
	mu     sync.Mutex
	flows  map[string]*passkeyFlow
	starts *throttle
	// grants are confirmations by session id, newest last, at most
	// passkeyGrantsPerSession each: one person can only ever hold a few, and
	// the table as a whole is bounded by passkeyGrantsMost, oldest out.
	grants map[string][]passkeyGrant
	// plainLocalhost lets tests run a ceremony against http://localhost,
	// which browsers treat as a secure context. It is never set by a config.
	plainLocalhost bool
}

func (h *Handler) pk() *passkeyState {
	h.passkeys.once.Do(func() {
		h.passkeys.flows = map[string]*passkeyFlow{}
		h.passkeys.starts = newThrottle("passkey start address", passkeyStarts, false)
	})
	return &h.passkeys
}

// passkeyOpenPath is a passkey route that answers without a bearer: the page
// and its JSON (the flow id is their proof), start (an add checks the
// session itself) and the engine's poll and cancel (the verifier is theirs).
func passkeyOpenPath(p string) bool {
	switch p {
	case "/v1/auth/passkey/start", "/v1/auth/passkey/poll", "/v1/auth/passkey/cancel":
		return true
	}
	return strings.HasPrefix(p, "/passkey/")
}

func (h *Handler) passkeyRoutes() {
	h.route("/v1/auth/passkey/start", map[string]http.HandlerFunc{http.MethodPost: h.passkeyStart})
	h.route("/v1/auth/passkey/poll", map[string]http.HandlerFunc{http.MethodPost: h.passkeyPoll})
	h.route("/v1/auth/passkey/cancel", map[string]http.HandlerFunc{http.MethodPost: h.passkeyCancel})
	h.route("/v1/me/passkeys", map[string]http.HandlerFunc{http.MethodGet: h.passkeyList})
	h.route("/v1/me/passkeys/remove", map[string]http.HandlerFunc{http.MethodPost: h.passkeyRemove})
	h.route("/v1/me/password/confirm", map[string]http.HandlerFunc{http.MethodPost: h.passwordConfirm})
	h.route("/passkey/", map[string]http.HandlerFunc{http.MethodGet: h.passkeyPage, http.MethodPost: h.passkeyPagePost})
}

// relyingParty is this server's WebAuthn identity, read from its public
// address: the relying party id is the host, the one origin is the address's
// own scheme://host[:port]. ok is false where WebAuthn cannot work: plain
// http (except localhost under test), an IP address, no address at all.
func (h *Handler) relyingParty() (rpID, origin string, ok bool) {
	u, err := url.Parse(strings.TrimRight(h.factory.Address, "/"))
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || net.ParseIP(host) != nil {
		return "", "", false
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && host == "localhost" && h.pk().plainLocalhost:
	default:
		return "", "", false
	}
	// A browser's origin never spells its scheme's default port, so neither
	// does this one: https://org:443 is https://org.
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return host, u.Scheme + "://" + net.JoinHostPort(host, port), true
	}
	return host, u.Scheme + "://" + host, true
}

// hsts is this server's HSTS header: includeSubDomains only when the
// operator asked for it and the host is a name they can own.
func (h *Handler) hsts() string {
	if !h.config.HSTSSubdomains {
		return hstsValue
	}
	u, err := url.Parse(strings.TrimRight(h.factory.Address, "/"))
	if err != nil {
		return hstsValue
	}
	return hstsFor(u.Hostname())
}

// passkeysOn is whether this server offers passkeys at all.
func (h *Handler) passkeysOn() bool {
	_, _, ok := h.relyingParty()
	return ok && h.passwordsOn()
}

// relying is the configured WebAuthn relying party for this server.
func (h *Handler) relying() (*webauthn.WebAuthn, string, error) {
	rpID, origin, ok := h.relyingParty()
	if !ok {
		return nil, "", errors.New(passkeyOffWords)
	}
	name := strings.TrimSpace(h.factory.Name)
	if name == "" {
		name = "codeaf"
	}
	required := true
	w, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: name, RPOrigins: []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		// authenticatorAttachment is UNSET on purpose: every kind of
		// authenticator is offered (see the file comment).
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementRequired, RequireResidentKey: &required,
			UserVerification: protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyChallengeLife, TimeoutUVD: passkeyChallengeLife},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyChallengeLife, TimeoutUVD: passkeyChallengeLife},
		},
	})
	return w, origin, err
}

// passkeyUser is an account as the WebAuthn library reads one.
type passkeyUser struct {
	id          []byte
	name, shown string
	creds       []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u passkeyUser) WebAuthnName() string                       { return u.name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.shown }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func accountUser(acct tokens.Account) (passkeyUser, error) {
	id, err := base64.RawURLEncoding.DecodeString(acct.UserID)
	if err != nil || len(id) == 0 {
		return passkeyUser{}, errors.New("this account has no passkey identity")
	}
	shown := acct.Name
	if shown == "" {
		shown = acct.Handle
	}
	u := passkeyUser{id: id, name: acct.Handle, shown: shown}
	for _, pk := range acct.Passkeys {
		c, err := credentialOf(pk)
		if err != nil {
			continue
		}
		u.creds = append(u.creds, c)
	}
	return u, nil
}

// credentialOf is a stored passkey as the library verifies one: with its
// backup-eligible flag, which the library checks never changes.
func credentialOf(pk tokens.Passkey) (webauthn.Credential, error) {
	id, err := base64.RawURLEncoding.DecodeString(pk.ID)
	if err != nil {
		return webauthn.Credential{}, err
	}
	key, err := base64.RawURLEncoding.DecodeString(pk.PublicKey)
	if err != nil {
		return webauthn.Credential{}, err
	}
	c := webauthn.Credential{ID: id, PublicKey: key, AttestationType: pk.Attestation,
		Flags:         webauthn.CredentialFlags{UserPresent: true, UserVerified: true, BackupEligible: pk.BackupEligible, BackupState: pk.BackupState},
		Authenticator: webauthn.Authenticator{SignCount: pk.SignCount}}
	for _, t := range pk.Transports {
		c.Transport = append(c.Transport, protocol.AuthenticatorTransport(t))
	}
	return c, nil
}

// storedPasskey is a newly registered credential as accounts.json keeps it.
func storedPasskey(c *webauthn.Credential, name string, now time.Time) tokens.Passkey {
	pk := tokens.Passkey{ID: base64.RawURLEncoding.EncodeToString(c.ID), PublicKey: base64.RawURLEncoding.EncodeToString(c.PublicKey),
		SignCount: c.Authenticator.SignCount, BackupEligible: c.Flags.BackupEligible, BackupState: c.Flags.BackupState,
		Attestation: c.AttestationType, Name: name, Created: now}
	if len(c.Authenticator.AAGUID) == 16 {
		pk.AAGUID = hexString(c.Authenticator.AAGUID)
	}
	for _, t := range c.Transport {
		if s := string(t); s != "" && len(pk.Transports) < 8 && len(s) <= 32 {
			pk.Transports = append(pk.Transports, s)
		}
	}
	return pk
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

// passkeyName is the name a new passkey is kept under: the person's own, or
// one read from the browser that made it ("Mac passkey").
func passkeyName(given string, r *http.Request) string {
	if name, ok := cleanDisplayName(given); ok && name != "" {
		if len(name) > passkeyNameMost {
			name = strings.TrimSpace(name[:passkeyNameMost])
		}
		return name
	}
	ua := r.UserAgent()
	for _, p := range []struct{ needle, word string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"CrOS", "Chromebook"},
		{"Macintosh", "Mac"}, {"Windows", "Windows"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, p.needle) {
			return p.word + " passkey"
		}
	}
	return "Passkey"
}

// ── the engine's doors ──────────────────────────────────────────────────────

func decodeB64URL32(s string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return b, err == nil && len(b) == 32
}

// passkeyStartKey is the key a start counts under: an IPv4 address, or an
// IPv6 address by its /48.
func passkeyStartKey(r *http.Request) string {
	addr := clientOf(r).addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(48); err == nil {
			return p.String()
		}
	}
	return throttleAddr(addr)
}

// passkeyPrefixKey is the network an opened sign-in counts under: an IPv4
// /24 or an IPv6 /48.
func passkeyPrefixKey(r *http.Request) string {
	addr := clientOf(r).addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 48
	}
	if p, err := addr.Prefix(bits); err == nil {
		return p.String()
	}
	return throttleAddr(addr)
}

// passkeyNetworkKey is the request's /24 (IPv4) or /32 (IPv6): the unit
// eviction is fair between. An IPv6 /48 is too cheap to hold many of.
func passkeyNetworkKey(r *http.Request) string {
	addr := clientOf(r).addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 32
	}
	if p, err := addr.Prefix(bits); err == nil {
		return p.String()
	}
	return throttleAddr(addr)
}

// passkeyReturn is the engine's loopback url a start names, or "".
func passkeyReturn(port int, path string) string {
	if port < 1024 || port > 65535 {
		return ""
	}
	if _, ok := decodeB64URL32(path); !ok {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/" + path
}

// passkeyStart is POST /v1/auth/passkey/start.
func (h *Handler) passkeyStart(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !h.passkeysOn() {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasskeyStart
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	_, addrKey := throttleKeys(r, "")
	st := h.pk()
	now := h.now()
	// SPENT ON ENTRY: every start costs one, finished or not, so starting
	// flows cannot be used to probe or to fill the table.
	st.mu.Lock()
	admitted := st.starts.take(passkeyStartKey(r), now)
	st.mu.Unlock()
	if !admitted {
		h.tooManyTries(w)
		return
	}
	challenge, ok := decodeB64URL32(body.CodeChallenge)
	if !ok {
		h.fail(w, http.StatusUnprocessableEntity, "code_challenge is the base64url SHA-256 of a 32-byte verifier")
		return
	}
	returnTo := passkeyReturn(body.ReturnPort, body.ReturnPath)
	returnKey, keyOK := decodeB64URL32(body.ReturnKey)
	if returnTo == "" || !keyOK {
		h.fail(w, http.StatusUnprocessableEntity, "return_port, return_path and return_key name the engine's loopback listener")
		return
	}
	flow := &passkeyFlow{purpose: body.Purpose, created: now, expires: now.Add(passkeyFlowLife), state: api.PasskeyPending,
		addr: addrKey, prefix: passkeyPrefixKey(r), network: passkeyNetworkKey(r), returnTo: returnTo, returnKey: returnKey}
	copy(flow.challenge[:], challenge)
	if (body.Action != nil) != (body.Purpose == api.PasskeyConfirm) {
		h.fail(w, http.StatusUnprocessableEntity, "a confirm names one action, and nothing else does")
		return
	}
	switch body.Purpose {
	case api.PasskeySignIn:
		if body.Link != "" || body.Username != "" || body.Name != "" {
			h.fail(w, http.StatusUnprocessableEntity, "a passkey sign-in takes no link or username")
			return
		}
	case api.PasskeyAdd, api.PasskeyConfirm:
		session, acct, ok := h.passkeyStartAccount(w, r, body)
		if !ok {
			return
		}
		flow.handle, flow.sessionID = acct.Handle, session.id
		switch {
		case body.Purpose == api.PasskeyConfirm:
			if len(acct.Passkeys) == 0 {
				h.fail(w, http.StatusConflict, "this account has no passkey to confirm with")
				return
			}
			if _, ok := stepUpKey(*body.Action); !ok {
				h.fail(w, http.StatusUnprocessableEntity, "action names one of add_passkey, remove_passkey (with passkey) or set_password")
				return
			}
			if body.Action.Kind == api.StepUpRemovePasskey {
				name, found := passkeyNamed(acct, body.Action.Passkey)
				if !found {
					h.fail(w, http.StatusNotFound, "no such passkey")
					return
				}
				flow.passkeyName = name
			}
			flow.action = *body.Action
			flow.requestedBy, flow.requestedAt = requestedBy(body.Device, session, clientOf(r).addr.Unmap().String())
		case len(acct.Passkeys) >= tokens.PasskeyMost:
			h.fail(w, http.StatusConflict, "this account already keeps as many passkeys as it can")
			return
		case !h.granted(session, api.StepUpAction{Kind: api.StepUpAddPasskey}, true):
			// A STEP-UP IS ALWAYS ITS OWN FLOW, made before and for this:
			// a confirm (whose code goes to the starter's loopback) or the
			// current password. An add never asks for one along the way.
			h.stepUp(w, api.StepUpAction{Kind: api.StepUpAddPasskey})
			return
		}
	case api.PasskeyJoin:
		if !h.passkeyJoinFlow(w, body, flow, now) {
			return
		}
	default:
		h.fail(w, http.StatusUnprocessableEntity, "choose sign_in, join, add or confirm")
		return
	}
	id, err := secret()
	if h.failed(w, err) {
		return
	}
	flow.id = id
	if !st.admitFlow(flow, now) {
		w.Header().Set("Retry-After", "60")
		h.refuse(w, http.StatusTooManyRequests, passkeyFullWords, "too_many_tries", "", nil, "")
		return
	}
	_, origin, _ := h.relyingParty()
	h.write(w, http.StatusCreated, api.PasskeyStarted{Flow: id, URL: origin + "/passkey/" + id, ExpiresAt: flow.expires.UTC()})
}

// passkeyStartAccount is the signed-in password-account person an add or a
// confirm is for. A refusal is answered here.
func (h *Handler) passkeyStartAccount(w http.ResponseWriter, r *http.Request, body api.PasskeyStart) (accessSession, tokens.Account, bool) {
	if body.Link != "" || body.Username != "" || body.Name != "" {
		// An add or confirm names nobody but the signed-in person; a field
		// for someone else is a bad request, not a missing account.
		h.fail(w, http.StatusUnprocessableEntity, "an add or confirm takes no link, username or name")
		return accessSession{}, tokens.Account{}, false
	}
	session, ok := h.credential(r)
	if !ok || session.profile == nil || session.profile.Kind != tokens.KindPassword {
		h.fail(w, http.StatusNotFound, passkeyNoAccount)
		return accessSession{}, tokens.Account{}, false
	}
	acct, found, err := h.freshAccount(session.profile.Handle)
	if h.failed(w, err) {
		return accessSession{}, tokens.Account{}, false
	}
	if !found {
		h.fail(w, http.StatusNotFound, passkeyNoAccount)
		return accessSession{}, tokens.Account{}, false
	}
	return session, acct, true
}

// admitFlow puts f in the table, or answers false: its address already has
// as many waiting as it may, or there is no room for its kind. A full table
// of sign-ins makes room by dropping the oldest sign-in whose page nobody
// ever opened.
func (st *passkeyState) admitFlow(f *passkeyFlow, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pruneLocked(now)
	mine, signIns := 0, 0
	for _, g := range st.flows {
		if g.addr == f.addr {
			mine++
		}
		if g.purpose == api.PasskeySignIn {
			signIns++
		}
	}
	if mine >= passkeyFlowsPerAddr {
		return false
	}
	if f.purpose == api.PasskeySignIn && signIns >= passkeySignInFlowsMost && !st.evictUnopenedLocked() && !st.evictFairLocked(f.network) {
		return false
	}
	if len(st.flows) >= passkeyFlowsMost && !st.evictUnopenedLocked() && !st.evictFairLocked(f.network) {
		return false
	}
	st.flows[f.id] = f
	return true
}

// evictFairLocked makes room for a newcomer from network, when every
// sign-in has been opened, by dropping one opened sign-in from the network
// holding the most, and only when that network holds clearly more than its
// share: more than passkeyFairShare and more than the newcomer's own network
// holds plus one. Otherwise it answers false and the newcomer is refused, so
// a flood spread one sign-in per network never drains a busy office of the
// few it holds, while a flood from a few networks still yields room. Among
// networks that hold equally many, the one with the oldest flow gives way.
func (st *passkeyState) evictFairLocked(network string) bool {
	held := map[string]int{}
	for _, g := range st.flows {
		if g.purpose == api.PasskeySignIn && g.opened {
			held[g.network]++
		}
	}
	most := 0
	for _, n := range held {
		most = max(most, n)
	}
	if most <= passkeyFairShare || most <= held[network]+1 {
		return false
	}
	var oldest *passkeyFlow
	for _, g := range st.flows {
		if g.purpose == api.PasskeySignIn && g.opened && held[g.network] == most && (oldest == nil || g.created.Before(oldest.created)) {
			oldest = g
		}
	}
	// An IPv6 /32 is many parties' (one provider's customers), so within it
	// the flow goes from its busiest /48: an attacker on the same provider
	// pays for their own flood, not the person they share it with.
	if network := oldest.network; strings.Contains(network, ":") {
		within := map[string]int{}
		for _, g := range st.flows {
			if g.purpose == api.PasskeySignIn && g.opened && g.network == network {
				within[g.prefix]++
			}
		}
		busiest := 0
		for _, n := range within {
			busiest = max(busiest, n)
		}
		oldest = nil
		for _, g := range st.flows {
			if g.purpose == api.PasskeySignIn && g.opened && g.network == network && within[g.prefix] == busiest && (oldest == nil || g.created.Before(oldest.created)) {
				oldest = g
			}
		}
	}
	delete(st.flows, oldest.id)
	return true
}

// evictUnopenedLocked drops the oldest sign-in flow whose page was never
// fetched, or answers false.
func (st *passkeyState) evictUnopenedLocked() bool {
	var oldest *passkeyFlow
	for _, g := range st.flows {
		if g.purpose == api.PasskeySignIn && !g.opened && (oldest == nil || g.created.Before(oldest.created)) {
			oldest = g
		}
	}
	if oldest == nil {
		return false
	}
	delete(st.flows, oldest.id)
	return true
}

// passkeyJoinFlow judges a join's link exactly as the access card does and
// records what it is for. A refusal is answered here.
func (h *Handler) passkeyJoinFlow(w http.ResponseWriter, body api.PasskeyStart, flow *passkeyFlow, now time.Time) bool {
	secretText, err := accessSecret(body.Link, h.factory.Address)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	rows, err := tokens.Load(h.config.TokensPath)
	if h.failed(w, err) {
		return false
	}
	var tok *tokens.Token
	for i := range rows {
		if rows[i].Matches(secretText) && (rows[i].AccessLink || rows[i].Operator) {
			tok = &rows[i]
			break
		}
	}
	if tok == nil {
		accounts, err := tokens.LoadAccounts(h.config.TokensPath)
		if h.failed(w, err) {
			return false
		}
		reset, ok := accounts.FindReset(secretText)
		if !ok || !reset.Open(now) {
			h.refuse(w, http.StatusConflict, "This access link is not valid.", "link_invalid", "", nil, "")
			return false
		}
		if _, held := accounts.Find(reset.Handle); !held {
			h.refuse(w, http.StatusConflict, "This access link is not valid.", "link_invalid", "", nil, "")
			return false
		}
		if body.Username != "" || body.Name != "" {
			h.fail(w, http.StatusUnprocessableEntity, "a reset keeps its person's username")
			return false
		}
		flow.resetSecret, flow.handle = secretText, reset.Handle
		return true
	}
	switch {
	case tok.Revoked:
		h.refuse(w, http.StatusConflict, "This access link was revoked.", "revoked", "", nil, "")
		return false
	case !tok.Expires.IsZero() && !now.Before(tok.Expires):
		h.refuse(w, http.StatusConflict, "This access link has expired.", "expired", "", nil, "")
		return false
	case tok.Human() && tok.Remaining() == 0, !tok.Human() && !tok.Joined.IsZero():
		h.refuse(w, http.StatusConflict, "This access link has been used.", "used_up", "", nil, "")
		return false
	}
	// A link made for one GitHub login is that person's: they sign in with
	// GitHub, never with a passkey made by whoever holds the link (E9's rule).
	if tok.InvitedLogin != "" {
		h.refuse(w, http.StatusConflict, refusalWords[reasonGitHubLink], reasonGitHubLink, "", nil, "")
		return false
	}
	flow.linkID, flow.operator = tok.ID, tok.Operator
	if body.Username != "" {
		flow.username = cleanUsername(body.Username)
		if flow.username == "" {
			h.refuse(w, http.StatusUnprocessableEntity, refusalWords[reasonUsernameInvalid], reasonUsernameInvalid, "", nil, "")
			return false
		}
	}
	name, ok := cleanDisplayName(body.Name)
	if !ok {
		h.refuse(w, http.StatusUnprocessableEntity, refusalWords[reasonNameInvalid], reasonNameInvalid, "", nil, "")
		return false
	}
	flow.name = name
	return true
}

// pruneLocked drops ended flows, and sign-ins whose page nobody opened in
// time. Called with mu held.
func (st *passkeyState) pruneLocked(now time.Time) {
	for id, f := range st.flows {
		if f.ended(now) {
			delete(st.flows, id)
		}
	}
}

// ended is whether f has outlived its life: ten minutes, or two for a
// sign-in whose page nobody opened.
func (f *passkeyFlow) ended(now time.Time) bool {
	return !now.Before(f.expires) || (f.purpose == api.PasskeySignIn && !f.opened && now.Sub(f.created) >= passkeyUnopenedLife) ||
		(f.state == api.PasskeyDone && now.Sub(f.doneAt) >= passkeyCodeLife)
}

// verifiedLocked finds the flow id answers to with verifier. A wrong verifier
// is counted against the flow, which ends at passkeyWrongMost: whoever holds
// the id is not whoever started it, but one guess must not end the real
// engine's sign-in. Called with mu held; the flow answered is still in the
// table.
func (st *passkeyState) verifiedLocked(id, verifier string, now time.Time) (*passkeyFlow, string) {
	f := st.flows[id]
	if f == nil {
		return nil, api.PasskeyExpired
	}
	if f.ended(now) {
		delete(st.flows, id)
		return nil, api.PasskeyExpired
	}
	v, ok := decodeB64URL32(verifier)
	sum := sha256.Sum256(v)
	if !ok || subtle.ConstantTimeCompare(sum[:], f.challenge[:]) != 1 {
		st.wrongLocked(f)
		return nil, api.PasskeyRefused
	}
	return f, ""
}

// wrongLocked counts one wrong verifier or code against f.
func (st *passkeyState) wrongLocked(f *passkeyFlow) {
	if f.wrong++; f.wrong >= passkeyWrongMost {
		delete(st.flows, f.id)
	}
}

// codeLocked is whether code is the one the ceremony sent the browser to the
// engine's loopback with. A wrong one is counted.
func (st *passkeyState) codeLocked(f *passkeyFlow, code string) bool {
	c, ok := decodeB64URL32(code)
	ok = ok && f.handed
	sum := sha256.Sum256(c)
	if ok && subtle.ConstantTimeCompare(sum[:], f.codeSum[:]) == 1 {
		return true
	}
	st.wrongLocked(f)
	return false
}

// passkeyPoll is POST /v1/auth/passkey/poll. A done flow answers only to a
// poll with both the verifier and the loopback code, once, and ends.
func (h *Handler) passkeyPoll(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !h.passkeysOn() {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasskeyPoll
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	st := h.pk()
	now := h.now()
	st.mu.Lock()
	f, ended := st.verifiedLocked(body.Flow, body.Verifier, now)
	if f == nil {
		st.mu.Unlock()
		out := api.PasskeyPolled{State: ended}
		if ended == api.PasskeyRefused {
			out.Message = passkeyNotHereWords
		}
		h.write(w, http.StatusOK, out)
		return
	}
	if f.state == api.PasskeyPending {
		st.mu.Unlock()
		h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyPending, Purpose: f.purpose})
		return
	}
	if f.state == api.PasskeyDone {
		// NO CODE, NO SESSION: the ceremony finished, but in a browser that
		// has not reached this engine's loopback (or not yet).
		if body.Code == "" {
			st.mu.Unlock()
			h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyElsewhere, Purpose: f.purpose})
			return
		}
		if !st.codeLocked(f, body.Code) {
			st.mu.Unlock()
			h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyRefused, Message: passkeyNotHereWords, Purpose: f.purpose})
			return
		}
	}
	// ANSWERED EXACTLY ONCE: the flow leaves the table before anything is
	// written or a session is made, so a second poll, even a concurrent one,
	// finds nothing.
	delete(st.flows, f.id)
	snapshot := *f
	st.mu.Unlock()
	if snapshot.state != api.PasskeyDone {
		h.write(w, http.StatusOK, api.PasskeyPolled{State: snapshot.state, Message: snapshot.message, Purpose: snapshot.purpose})
		return
	}
	refused := func(words string) {
		h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyRefused, Message: words, Purpose: snapshot.purpose})
	}
	profile, gen := snapshot.profile, snapshot.gen
	switch {
	case snapshot.purpose == api.PasskeyConfirm:
		h.grant(snapshot.sessionID, snapshot.handle, snapshot.action, h.now())
		h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyDone, Purpose: snapshot.purpose})
		return
	case snapshot.made != nil:
		var words string
		var err error
		profile, gen, words, err = h.passkeyRegistered(r, snapshot, *snapshot.made, h.now())
		if h.failed(w, err) {
			return
		}
		if words != "" {
			refused(words)
			return
		}
	}
	if profile == nil {
		h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyDone, Purpose: snapshot.purpose})
		return
	}
	// The account may have been removed, or a way in changed, between the
	// ceremony and this poll; either signs nobody in.
	acct, ok, err := h.freshAccount(profile.Handle)
	if h.failed(w, err) {
		return
	}
	if !ok || acct.Gen != gen {
		refused(passkeyUnknownWords)
		return
	}
	if snapshot.purpose == api.PasskeySignIn {
		if h.failed(w, h.accountEvent(r, profile.Handle, "", "signed in", "signed in with a passkey", false)) {
			return
		}
	}
	credential, session, err := h.mintSessionVia(w, r, *profile, gen, signInPasskey)
	if h.failed(w, err) {
		return
	}
	me := h.meBody(*profile, session.id)
	me.Operator = profile.Handle == h.operatorLogin(r)
	// A passkey person signed in with a passkey, not with GitHub.
	me.GitHubSignedIn, me.SignInKind = false, signInPasskey
	me.Teams = h.Membership().TeamsOf(profile.Handle)
	expires := session.expires.UTC()
	fac := h.factory
	h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyDone, Purpose: snapshot.purpose, Factory: &fac,
		AccessToken: credential, TokenType: "bearer", ExpiresIn: int(accessSessionTTL.Seconds()), ExpiresAt: &expires,
		Person: &api.PasswordPerson{Handle: profile.Handle, Name: profile.Name}, Me: &me})
}

// passkeyCancel is POST /v1/auth/passkey/cancel: the engine ends its flow.
func (h *Handler) passkeyCancel(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !h.passkeysOn() {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasskeyPoll
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	st := h.pk()
	st.mu.Lock()
	if f, _ := st.verifiedLocked(body.Flow, body.Verifier, h.now()); f != nil {
		delete(st.flows, f.id)
	}
	st.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// ── the signed-in person's passkeys ─────────────────────────────────────────

func (h *Handler) passkeyAccount(w http.ResponseWriter, r *http.Request) (accessSession, tokens.Account, bool) {
	session, ok := h.credential(r)
	if !ok || session.profile == nil || session.profile.Kind != tokens.KindPassword || !h.passwordsOn() {
		h.fail(w, http.StatusNotFound, passkeyNoAccount)
		return accessSession{}, tokens.Account{}, false
	}
	acct, found, err := h.passwordAccount(session.profile.Handle)
	if h.failed(w, err) {
		return accessSession{}, tokens.Account{}, false
	}
	if !found {
		h.fail(w, http.StatusNotFound, passkeyNoAccount)
		return accessSession{}, tokens.Account{}, false
	}
	return session, acct, true
}

// passkeyList is GET /v1/me/passkeys.
func (h *Handler) passkeyList(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	_, acct, ok := h.passkeyAccount(w, r)
	if !ok {
		return
	}
	out := api.PasskeyList{Passkeys: []api.PasskeyRow{}, Password: acct.Hash != ""}
	for _, pk := range acct.Passkeys {
		row := api.PasskeyRow{ID: pk.ID, Name: pk.Name, CreatedAt: pk.Created.UTC(), Synced: pk.BackupEligible}
		if !pk.LastUsed.IsZero() {
			used := pk.LastUsed.UTC()
			row.LastUsedAt = &used
		}
		out.Passkeys = append(out.Passkeys, row)
	}
	h.write(w, http.StatusOK, out)
}

// passkeyRemove is POST /v1/me/passkeys/remove. Every other session of the
// person ends, as a password change ends them: whoever signed in with the
// removed passkey is out.
func (h *Handler) passkeyRemove(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	session, acct, ok := h.passkeyAccount(w, r)
	if !ok {
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasskeyRemove
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	// STEP-UP: a session alone, perhaps a stolen one, may not take a way in
	// away; only a confirmation made for removing this passkey may, once. It
	// is checked after what would refuse anyway, so a refusal spends none.
	action := api.StepUpAction{Kind: api.StepUpRemovePasskey, Passkey: body.ID}
	known := false
	for _, pk := range acct.Passkeys {
		known = known || pk.ID == body.ID
	}
	switch {
	case !known:
		h.fail(w, http.StatusNotFound, "no such passkey")
		return
	case len(acct.Passkeys) == 1 && acct.Hash == "":
		h.refuse(w, http.StatusConflict, passkeyLastWords, "last_way_in", "", nil, "")
		return
	case !h.granted(session, action, true):
		h.stepUp(w, action)
		return
	}
	gen, err := tokens.RemovePasskey(h.config.TokensPath, acct.Handle, body.ID, h.now())
	h.forgetAccounts()
	switch {
	case errors.Is(err, tokens.ErrNoPasskey):
		h.fail(w, http.StatusNotFound, "no such passkey")
		return
	case errors.Is(err, tokens.ErrLastWayIn):
		h.refuse(w, http.StatusConflict, passkeyLastWords, "last_way_in", "", nil, "")
		return
	case h.failed(w, err):
		return
	}
	if h.failed(w, h.endSessionsOf(acct.Handle, session.id, gen)) {
		return
	}
	if h.failed(w, h.accountEvent(r, acct.Handle, "", "removed passkey", "removed a passkey; other sign-ins ended", false)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── the ceremony page ───────────────────────────────────────────────────────

//go:embed passkey_page.js
var passkeyScript []byte

//go:embed passkey_page.css
var passkeyStyle []byte

//go:embed passkey_page.html
var passkeyPageHTML string

var passkeyPageTemplate = template.Must(template.New("passkey").Parse(passkeyPageHTML))

// passkeyPageCSP: nothing but this server's own script and stylesheet, no
// inline script, no frames around it, no form posts anywhere.
const passkeyPageCSP = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func passkeyHeaders(w http.ResponseWriter) {
	noStore(w)
	w.Header().Set("Content-Security-Policy", passkeyPageCSP)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "publickey-credentials-create=(self), publickey-credentials-get=(self)")
}

type passkeyPageData struct {
	Flow, Purpose, Org, Host, Handle, Username, Heading, Lede, Button string
	AskUsername, AskName, AskLabel, Ended, Handover                   bool
	// LedeParts, when set, is the lede in pieces: a Name piece is a name
	// someone chose, set in its own <bdi> so it cannot reorder the words
	// around it.
	LedeParts []ledePart
}

type ledePart struct {
	Text string
	Name bool
}

// passkeyPage is GET /passkey/<flow>, /passkey/page.js and /passkey/page.css.
func (h *Handler) passkeyPage(w http.ResponseWriter, r *http.Request) {
	passkeyHeaders(w)
	rest := strings.TrimPrefix(r.URL.Path, "/passkey/")
	switch rest {
	case "page.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(passkeyScript)
		return
	case "page.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(passkeyStyle)
		return
	}
	if id, step, ok := strings.Cut(rest, "/"); ok && step == "go" && h.passkeysOn() {
		h.passkeyHandOver(w, r, id)
		return
	}
	if !h.passkeysOn() || strings.Contains(rest, "/") {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
		return
	}
	rpID, _, _ := h.relyingParty()
	org := strings.TrimSpace(h.factory.Name)
	if org == "" {
		org = rpID
	}
	data := passkeyPageData{Flow: rest, Org: org, Host: rpID}
	st := h.pk()
	now := h.now()
	st.mu.Lock()
	f := st.flows[rest]
	if f != nil && f.ended(now) {
		f = nil
	}
	if f != nil {
		data.Purpose, data.Handle, data.Username = f.purpose, f.handle, f.username
		if f.state != api.PasskeyPending {
			data.Ended = true
			data.Handover = f.state == api.PasskeyDone
		}
	}
	st.mu.Unlock()
	switch {
	case f == nil:
		data.Ended, data.Heading, data.Lede = true, "This page has ended", passkeyEndedWords
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = passkeyPageTemplate.Execute(w, data)
		return
	case data.Handover:
		// A finished ceremony whose browser has not reached codeaf: reopened,
		// or come back to from a browser on another device.
		data.Heading, data.Lede = "Finish on the computer that started this", passkeyElsewhereWords
	case data.Ended:
		data.Heading, data.Lede = "Done", "Go back to codeaf."
	case f.purpose == api.PasskeySignIn:
		data.Heading, data.Lede, data.Button = "Sign in to "+org, "Use the passkey you keep for "+rpID+".", "Sign in with a passkey"
	case f.purpose == api.PasskeyConfirm:
		data.Heading, data.Button = "Confirm it's you", "Confirm with a passkey"
		data.LedeParts = append(passkeyActionParts(f), ledePart{Text: " — requested by a computer"})
		if f.requestedBy != "" {
			data.LedeParts = append(data.LedeParts, ledePart{Text: " named “"}, ledePart{Text: f.requestedBy, Name: true}, ledePart{Text: "”"})
		}
		data.LedeParts = append(data.LedeParts, ledePart{Text: f.requestedAt + ". Use one of your passkeys for @" + f.handle + " on " + org + "."})
		for _, p := range data.LedeParts {
			data.Lede += p.Text
		}
	case f.purpose == api.PasskeyAdd:
		data.Heading, data.Lede, data.Button, data.AskLabel = "Add a passkey", "For @"+f.handle+" on "+org+".", "Create a passkey", true
	case f.resetSecret != "":
		data.Heading, data.Lede, data.Button = "Choose a new passkey", "For @"+f.handle+" on "+org+". It replaces your password and passkeys here.", "Create a passkey"
	default:
		data.Heading, data.Lede, data.Button = "Join "+org, "Choose how people here will see you, then create a passkey.", "Create a passkey"
		data.AskUsername, data.AskName = f.username == "", true
		if f.username != "" {
			data.Lede = "You join as @" + f.username + "."
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = passkeyPageTemplate.Execute(w, data)
}

// pageOrigin refuses a page post that did not come from this server's own
// page. The ceremony is origin-bound anyway; this keeps another site from
// even asking for a challenge.
func (h *Handler) pageOrigin(w http.ResponseWriter, r *http.Request) bool {
	_, origin, ok := h.relyingParty()
	if !ok {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
		return false
	}
	if got := strings.ToLower(r.Header.Get("Origin")); got != origin {
		h.fail(w, http.StatusForbidden, passkeyWrongPageWord)
		return false
	}
	if sf := r.Header.Get("Sec-Fetch-Site"); sf != "" && sf != "same-origin" {
		h.fail(w, http.StatusForbidden, passkeyWrongPageWord)
		return false
	}
	return true
}

// readPageBody reads a page post: JSON, bounded, strict.
func (h *Handler) readPageBody(w http.ResponseWriter, r *http.Request, most int64, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		h.fail(w, http.StatusUnsupportedMediaType, "send this as application/json")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, most))
	if err != nil {
		h.fail(w, http.StatusRequestEntityTooLarge, "the request is too large")
		return false
	}
	return h.strictJSON(w, data, out)
}

// passkeyPagePost is POST /passkey/<flow>/options and /passkey/<flow>/finish.
func (h *Handler) passkeyPagePost(w http.ResponseWriter, r *http.Request) {
	passkeyHeaders(w)
	if !h.passkeysOn() {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
		return
	}
	if !h.passwordTransport(w, r) || !h.pageOrigin(w, r) {
		return
	}
	id, step, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/passkey/"), "/")
	switch step {
	case "options":
		h.passkeyOptions(w, r, id)
	case "finish":
		h.passkeyFinish(w, r, id)
	default:
		h.fail(w, http.StatusNotFound, api.EndpointNotFound)
	}
}

type passkeyOptionsBody struct {
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
}

type passkeyFinishBody struct {
	Credential json.RawMessage `json:"credential"`
	Name       string          `json:"name,omitempty"`
}

// openFlowLocked is the pending flow id names, or an answer saying it has ended.
func (h *Handler) openFlowLocked(w http.ResponseWriter, id string, now time.Time) *passkeyFlow {
	f := h.pk().flows[id]
	if f == nil || f.ended(now) || f.state != api.PasskeyPending {
		h.refuse(w, http.StatusGone, passkeyEndedWords, "flow_ended", "", nil, "")
		return nil
	}
	return f
}

// passkeyOptionsAnswer is what the page is asked to do next: kind "get" (an
// assertion: a sign-in, a confirm, or an add's step-up) or "create".
type passkeyOptionsAnswer struct {
	Kind      string `json:"kind"`
	PublicKey any    `json:"publicKey"`
}

// passkeyActionParts names what a confirm is for, as its page says it; a
// passkey's name is isolated like any name a person chose.
func passkeyActionParts(f *passkeyFlow) []ledePart {
	switch f.action.Kind {
	case api.StepUpRemovePasskey:
		return []ledePart{{Text: "Remove passkey \u201c"}, {Text: plainText(f.passkeyName), Name: true}, {Text: "\u201d"}}
	case api.StepUpSetPassword:
		return []ledePart{{Text: "Set a first password"}}
	}
	return []ledePart{{Text: "Add a passkey"}}
}

// passkeyCookie is this browser's id for passkey pages. THE __Host- PREFIX IS
// THE POINT: a browser keeps such a cookie only when this very host set it
// over HTTPS, with Path=/ and no Domain, so neither a sibling host (another
// name under a shared wildcard DNS suffix) nor a plain-HTTP attacker can
// plant one. Each flow keeps the SHA-256 of the id that bound it.
const passkeyCookie = "__Host-codeaf_pkb"

// passkeyCookieLife is how long the browser id lives; it is renewed by use.
const passkeyCookieLife = 24 * time.Hour

// passkeyOtherBrowserWords answer a page post from a browser the flow is not
// bound to.
const passkeyOtherBrowserWords = "This sign-in is open in another browser. Start again in codeaf."

// boundLocked is whether r comes from the browser f is bound to.
func (f *passkeyFlow) boundLocked(r *http.Request) bool {
	var zero [sha256.Size]byte
	if f.browserSum == zero {
		return false
	}
	c, err := r.Cookie(passkeyCookie)
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(c.Value))
	return subtle.ConstantTimeCompare(sum[:], f.browserSum[:]) == 1
}

// bindLocked binds an unbound flow to the browser asking now, answering the
// cookie to set; a bound flow answers true only to its own browser.
func (f *passkeyFlow) bindLocked(r *http.Request) (*http.Cookie, bool) {
	var zero [sha256.Size]byte
	if f.browserSum != zero {
		return nil, f.boundLocked(r)
	}
	id := ""
	if c, err := r.Cookie(passkeyCookie); err == nil && len(c.Value) == 64 && strings.Trim(c.Value, "0123456789abcdef") == "" {
		id = c.Value
	} else if id, err = secret(); err != nil {
		return nil, false
	}
	f.browserSum = sha256.Sum256([]byte(id))
	return &http.Cookie{Name: passkeyCookie, Value: id, Path: "/", MaxAge: int(passkeyCookieLife / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}, true
}

// openedLocked marks f opened by a browser. A sign-in counts against its
// network: past passkeyOpenedPerPrefix the oldest other opened sign-in from
// the same /24 or /48 is dropped, so pages a flood opens cannot hold the
// table.
func (st *passkeyState) openedLocked(f *passkeyFlow) {
	if f.opened {
		return
	}
	f.opened = true
	if f.purpose != api.PasskeySignIn {
		return
	}
	n := 0
	var oldest *passkeyFlow
	for _, g := range st.flows {
		if g == f || g.purpose != api.PasskeySignIn || !g.opened || g.prefix != f.prefix {
			continue
		}
		n++
		if oldest == nil || g.created.Before(oldest.created) {
			oldest = g
		}
	}
	if n >= passkeyOpenedPerPrefix && oldest != nil {
		delete(st.flows, oldest.id)
	}
}

// passkeyOptions answers a fresh challenge for the flow's next ceremony. A
// join checks its username first, so nobody makes a passkey for a name that
// is taken.
func (h *Handler) passkeyOptions(w http.ResponseWriter, r *http.Request, id string) {
	var body passkeyOptionsBody
	if !h.readPageBody(w, r, passwordBodyMost, &body) {
		return
	}
	rp, _, err := h.relying()
	if h.failed(w, err) {
		return
	}
	st := h.pk()
	now := h.now()
	st.mu.Lock()
	f := h.openFlowLocked(w, id, now)
	if f == nil {
		st.mu.Unlock()
		return
	}
	if f.challenges >= passkeyChallengesMost {
		st.mu.Unlock()
		h.tooManyTries(w)
		return
	}
	cookie, bound := f.bindLocked(r)
	if !bound {
		st.mu.Unlock()
		h.refuse(w, http.StatusForbidden, passkeyOtherBrowserWords, "other_browser", "", nil, "")
		return
	}
	f.challenges++
	st.openedLocked(f)
	purpose, reset, handle, username, name := f.purpose, f.resetSecret, f.handle, f.username, f.name
	st.mu.Unlock()
	if cookie != nil {
		http.SetCookie(w, cookie)
	}

	var answer passkeyOptionsAnswer
	var session *webauthn.SessionData
	userID := ""
	switch {
	case purpose == api.PasskeySignIn:
		assertion, s, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
		if h.failed(w, err) {
			return
		}
		answer, session = passkeyOptionsAnswer{"get", assertion.Response}, s
	case purpose == api.PasskeyConfirm:
		acct, ok, err := h.freshAccount(handle)
		if h.failed(w, err) {
			return
		}
		if !ok {
			h.refuse(w, http.StatusGone, passkeyEndedWords, "flow_ended", "", nil, "")
			return
		}
		user, err := accountUser(acct)
		if h.failed(w, err) {
			return
		}
		assertion, s, err := rp.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
		if h.failed(w, err) {
			return
		}
		answer, session = passkeyOptionsAnswer{"get", assertion.Response}, s
	case purpose == api.PasskeyAdd || reset != "":
		_, err := tokens.EnsureUserID(h.config.TokensPath, handle)
		h.forgetAccounts()
		if h.failed(w, err) {
			return
		}
		acct, ok, err := h.passwordAccount(handle)
		if h.failed(w, err) {
			return
		}
		if !ok {
			h.refuse(w, http.StatusGone, passkeyEndedWords, "flow_ended", "", nil, "")
			return
		}
		user, err := accountUser(acct)
		if h.failed(w, err) {
			return
		}
		var opts []webauthn.RegistrationOption
		if reset == "" {
			// The person's own passkeys are excluded so the authenticator
			// says "already registered" rather than making a second one.
			opts = append(opts, webauthn.WithExclusions(webauthn.Credentials(user.creds).CredentialDescriptors()))
		}
		creation, s, err := rp.BeginRegistration(user, opts...)
		if h.failed(w, err) {
			return
		}
		answer, session = passkeyOptionsAnswer{"create", creation.Response}, s
	default:
		if username == "" {
			username = cleanUsername(body.Username)
			if username == "" {
				h.refuse(w, http.StatusUnprocessableEntity, refusalWords[reasonUsernameInvalid], reasonUsernameInvalid, "", nil, "")
				return
			}
		}
		if name == "" {
			n, ok := cleanDisplayName(body.Name)
			if !ok {
				h.refuse(w, http.StatusUnprocessableEntity, refusalWords[reasonNameInvalid], reasonNameInvalid, "", nil, "")
				return
			}
			name = n
		}
		if name == "" {
			name = username
		}
		taken, err := h.passkeyNameTaken(username)
		if h.failed(w, err) {
			return
		}
		if taken {
			h.refuse(w, http.StatusConflict, refusalWords[reasonUsernameTaken], reasonUsernameTaken, "", nil, "")
			return
		}
		if userID, err = tokens.NewUserID(); h.failed(w, err) {
			return
		}
		raw, _ := base64.RawURLEncoding.DecodeString(userID)
		creation, s, err := rp.BeginRegistration(passkeyUser{id: raw, name: username, shown: name})
		if h.failed(w, err) {
			return
		}
		answer, session = passkeyOptionsAnswer{"create", creation.Response}, s
	}
	st.mu.Lock()
	f = h.openFlowLocked(w, id, h.now())
	if f == nil {
		st.mu.Unlock()
		return
	}
	// A new challenge replaces the last: only the newest can be answered.
	f.session, f.sessionEnd = session, h.now().Add(passkeyChallengeLife)
	if userID != "" {
		f.username, f.name, f.userID = username, name, userID
	}
	st.mu.Unlock()
	h.write(w, http.StatusOK, answer)
}

// passkeyFinish verifies what the authenticator signed. A sign-in learns
// whom it signs in; a confirm or an add's step-up learns the person is
// there; a new passkey is kept in the flow until the code-bearing poll. The
// challenge is taken out first, so it can be answered only once.
func (h *Handler) passkeyFinish(w http.ResponseWriter, r *http.Request, id string) {
	var body passkeyFinishBody
	if !h.readPageBody(w, r, passkeyFinishMost, &body) {
		return
	}
	// A ceremony try is counted as a password try is, by E9's guard, under
	// a name of its own per address: a passkey names nobody before it is
	// verified, and one shared name would let one address use up everyone's.
	_, addrKey := throttleKeys(r, "")
	now := h.now()
	try, admitted := h.guard().admit(passkeyTryName+addrKey, addrKey, "", now, false)
	if !admitted {
		h.tooManyTries(w)
		return
	}
	defer try.abandon(h.now())
	rp, _, err := h.relying()
	if h.failed(w, err) {
		return
	}
	st := h.pk()
	st.mu.Lock()
	f := h.openFlowLocked(w, id, now)
	if f == nil {
		st.mu.Unlock()
		return
	}
	if !f.boundLocked(r) {
		st.mu.Unlock()
		h.refuse(w, http.StatusForbidden, passkeyOtherBrowserWords, "other_browser", "", nil, "")
		return
	}
	session, live := f.session, f.session != nil && now.Before(f.sessionEnd)
	f.session = nil
	snapshot := *f
	st.mu.Unlock()
	if !live {
		h.refuse(w, http.StatusConflict, "That try has ended. Try again.", "challenge_ended", "", nil, "")
		return
	}
	refused := func(words, reason string) {
		try.failed(h.now())
		st.mu.Lock()
		if f := st.flows[id]; f != nil {
			f.tries++
			if f.tries >= passkeyTriesMost {
				f.state, f.message = api.PasskeyRefused, passkeyRefusedWords
			}
		}
		st.mu.Unlock()
		h.refuse(w, http.StatusBadRequest, words, reason, "", nil, "")
	}
	var profile *api.Profile
	var gen int64
	var made *tokens.Passkey
	switch {
	case snapshot.purpose == api.PasskeySignIn:
		parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
		if err != nil {
			refused(passkeyRefusedWords, "passkey_refused")
			return
		}
		var acct tokens.Account
		user, cred, err := rp.ValidatePasskeyLogin(func(_, userHandle []byte) (webauthn.User, error) {
			accounts, err := tokens.LoadAccounts(h.config.TokensPath)
			if err != nil {
				return nil, err
			}
			found, ok := accounts.FindUserID(base64.RawURLEncoding.EncodeToString(userHandle))
			if !ok {
				return nil, errors.New("unknown passkey")
			}
			acct = found
			return accountUser(found)
		}, *session, parsed)
		if err != nil || user == nil || !h.passkeyUsed(r, acct, cred, now) {
			refused(passkeyUnknownWords, "passkey_refused")
			return
		}
		gen = acct.Gen
		name := acct.Name
		if name == "" {
			name = acct.Handle
		}
		profile = &api.Profile{Handle: acct.Handle, Name: name, Scopes: []string{}, Kind: tokens.KindPassword}
	case snapshot.purpose == api.PasskeyConfirm:
		parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
		if err != nil {
			refused(passkeyRefusedWords, "passkey_refused")
			return
		}
		acct, ok, err := h.freshAccount(snapshot.handle)
		if h.failed(w, err) {
			return
		}
		if !ok {
			refused(passkeyUnknownWords, "passkey_refused")
			return
		}
		user, err := accountUser(acct)
		if err != nil {
			refused(passkeyUnknownWords, "passkey_refused")
			return
		}
		cred, err := rp.ValidateLogin(user, *session, parsed)
		if err != nil || !h.passkeyUsed(r, acct, cred, now) {
			refused(passkeyUnknownWords, "passkey_refused")
			return
		}
	default:
		parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
		if err != nil {
			refused(passkeyRefusedWords, "passkey_refused")
			return
		}
		user := passkeyUser{}
		if raw, e := base64.RawURLEncoding.DecodeString(snapshot.userID); e == nil && snapshot.userID != "" {
			user = passkeyUser{id: raw, name: snapshot.username, shown: snapshot.name}
		} else if acct, ok, e := h.passwordAccount(snapshot.handle); e == nil && ok {
			if user, err = accountUser(acct); err != nil {
				refused(passkeyRefusedWords, "passkey_refused")
				return
			}
		}
		cred, err := rp.CreateCredential(user, *session, parsed)
		if err != nil {
			refused(passkeyRefusedWords, "passkey_refused")
			return
		}
		pk := storedPasskey(cred, passkeyName(body.Name, r), now)
		// Said here, where the page can still take the passkey back out of
		// the keychain; the poll asks again, and that answer counts.
		accounts, err := tokens.LoadAccounts(h.config.TokensPath)
		if h.failed(w, err) {
			return
		}
		if _, _, taken := accounts.FindPasskey(pk.ID); taken {
			h.refuse(w, http.StatusConflict, passkeyTakenWords, "passkey_taken", "", nil, "")
			return
		}
		made = &pk
	}
	try.succeeded(h.now())
	st.mu.Lock()
	f = st.flows[id]
	if f == nil || f.state != api.PasskeyPending {
		st.mu.Unlock()
		h.refuse(w, http.StatusGone, passkeyEndedWords, "flow_ended", "", nil, "")
		return
	}
	f.state, f.profile, f.gen, f.made, f.doneAt = api.PasskeyDone, profile, gen, made, h.now()
	st.mu.Unlock()
	// NO CODE IN THIS ANSWER: the bound browser fetches the hand-over, which
	// answers the loopback address with the code as a 303 (passkeyHandOver).
	h.write(w, http.StatusOK, map[string]string{"state": api.PasskeyDone, "message": passkeyHandOverWords, "next": "/passkey/" + id + "/go"})
}

// passkeyHandOver is GET /passkey/<flow>/go, the bound browser's navigation
// after a finished ceremony: once, a 303 to the engine's loopback with a
// fresh one-time code. Any other request (no cookie, another browser, a
// second visit) is sent to the page, which says where the ceremony finishes.
func (h *Handler) passkeyHandOver(w http.ResponseWriter, r *http.Request, id string) {
	passkeyHeaders(w)
	st := h.pk()
	now := h.now()
	st.mu.Lock()
	defer st.mu.Unlock()
	f := st.flows[id]
	if f == nil || f.ended(now) || f.state != api.PasskeyDone || f.handed || !f.boundLocked(r) || now.Sub(f.doneAt) >= passkeyCodeLife {
		http.Redirect(w, r, "/passkey/"+id, http.StatusSeeOther)
		return
	}
	code, err := passkeyLoopbackCode(f.returnKey)
	if err != nil {
		http.Redirect(w, r, "/passkey/"+id, http.StatusSeeOther)
		return
	}
	f.codeSum, f.handed = sha256.Sum256(code), true
	http.Redirect(w, r, f.returnTo+"?code="+base64.RawURLEncoding.EncodeToString(code), http.StatusSeeOther)
}

// passkeyLoopbackCode is 16 random bytes and the first 16 bytes of their
// HMAC-SHA256 under the engine's key, so the engine's listener can tell its
// own flow's code from any other request before it takes one.
func passkeyLoopbackCode(key []byte) ([]byte, error) {
	code := make([]byte, 32)
	if _, err := rand.Read(code[:16]); err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(code[:16])
	copy(code[16:], mac.Sum(nil))
	return code, nil
}

// passkeyUsed records a verified assertion on the account: its counter, its
// backup state, and a row for the operator when the counter went backwards
// (a copied authenticator; a synced passkey never counts, so this is not a
// refusal, as the library advises). false is a write that failed.
func (h *Handler) passkeyUsed(r *http.Request, acct tokens.Account, cred *webauthn.Credential, now time.Time) bool {
	if cred == nil {
		return false
	}
	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	err := tokens.UsedPasskey(h.config.TokensPath, acct.Handle, credID, cred.Authenticator.SignCount, cred.Flags.BackupState, now)
	h.forgetAccounts()
	if err != nil {
		return false
	}
	if cred.Authenticator.CloneWarning {
		_ = h.accountEvent(r, acct.Handle, "", "passkey counter went backwards", "a passkey's signature counter went backwards; it may have been copied", false)
	}
	return true
}

// passkeyRegistered does what a verified registration is for, at the poll
// that showed the code: a join makes the account and spends the link in
// SignUp's one critical section; a reset replaces every way in; an add puts
// the passkey on the account. It answers whom the flow signs in (nil for an
// add) and at what generation, or the words of a refusal; err is a fault.
func (h *Handler) passkeyRegistered(r *http.Request, f passkeyFlow, pk tokens.Passkey, now time.Time) (*api.Profile, int64, string, error) {
	switch {
	case f.purpose == api.PasskeyAdd:
		err := tokens.AddPasskey(h.config.TokensPath, f.handle, pk)
		h.forgetAccounts()
		switch {
		case errors.Is(err, tokens.ErrPasskeyTaken):
			return nil, 0, passkeyTakenWords, nil
		case errors.Is(err, tokens.ErrNoAccount):
			return nil, 0, passkeyEndedWords, nil
		case err != nil:
			return nil, 0, "", err
		}
		return nil, 0, "", h.accountEvent(r, f.handle, "", "added passkey", "added a passkey", false)
	case f.resetSecret != "":
		handle, gen, err := tokens.UseResetPasskey(h.config.TokensPath, f.resetSecret, pk, now)
		h.forgetAccounts()
		switch {
		case errors.Is(err, tokens.ErrPasskeyTaken):
			return nil, 0, passkeyTakenWords, nil
		case errors.Is(err, tokens.ErrResetInvalid):
			return nil, 0, "This reset link has been used or has ended.", nil
		case err != nil:
			return nil, 0, "", err
		}
		if err := h.endSessionsOf(handle, "", gen); err != nil {
			return nil, 0, "", err
		}
		if err := h.accountEvent(r, handle, "", "reset password", "chose a new passkey through the operator's reset link", false); err != nil {
			return nil, 0, "", err
		}
		acct, _, _ := h.passwordAccount(handle)
		name := acct.Name
		if name == "" {
			name = handle
		}
		return &api.Profile{Handle: handle, Name: name, Scopes: []string{}, Kind: tokens.KindPassword}, gen, "", nil
	}
	profile := api.Profile{Handle: f.username, Name: f.name, Scopes: []string{}, Kind: tokens.KindPassword}
	h.joinMu.Lock()
	// As E9's signUp: what else names people is read before the row lock.
	referenced, err := h.handleReferenced(f.username)
	if err != nil {
		h.joinMu.Unlock()
		return nil, 0, "", err
	}
	held := func(handle string) bool { return referenced || h.knownElsewhere(handle) }
	_, err = tokens.SignUp(h.config.TokensPath, f.linkID, tokens.Account{Name: f.name, UserID: f.userID, Passkeys: []tokens.Passkey{pk}},
		profile, now, held, func() error {
			if h.config.Record == nil {
				return nil
			}
			_, e := h.config.Record.Append(r.Context(), record.Event{Kind: record.OperatorClaimed, Handle: f.username, Object: "factory",
				Actor: record.Actor{Kind: "person", Name: "@" + f.username, Operator: true}, Detail: "claimed factory as operator"})
			return e
		})
	h.joinMu.Unlock()
	h.forgetAccounts()
	switch {
	case err == nil:
	case errors.Is(err, tokens.ErrHandleTaken):
		return nil, 0, refusalWords[reasonUsernameTaken], nil
	case errors.Is(err, tokens.ErrPasskeyTaken):
		return nil, 0, passkeyTakenWords, nil
	case errors.Is(err, tokens.ErrGitHubLink):
		return nil, 0, refusalWords[reasonGitHubLink], nil
	case errors.Is(err, tokens.ErrRevoked), errors.Is(err, tokens.ErrExpired), errors.Is(err, tokens.ErrUsedUp), errors.Is(err, os.ErrPermission):
		return nil, 0, "This access link has been used or has ended.", nil
	default:
		return nil, 0, "", err
	}
	if err := h.accountEvent(r, f.username, "", "signed up", "signed up with a passkey", f.operator); err != nil {
		return nil, 0, "", err
	}
	// An invite's team, workstreams and tasks are theirs from the first screen.
	h.settleInvites(r)
	// A new account starts at generation 1, as SignUp writes it.
	return &profile, 1, "", nil
}

// passkeyNameTaken is the join's early answer to whether username is free,
// asked before the browser makes a passkey for it, so nobody is left with a
// passkey in their keychain for an account that was never made. SignUp asks
// again in its own critical section; that is the answer that counts.
func (h *Handler) passkeyNameTaken(username string) (bool, error) {
	referenced, err := h.handleReferenced(username)
	if err != nil || referenced {
		return referenced, err
	}
	return tokens.HandleTaken(h.config.TokensPath, username, h.knownElsewhere)
}
