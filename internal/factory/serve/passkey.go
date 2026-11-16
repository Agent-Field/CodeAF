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
	// the server's memory.
	passkeyFlowsMost = 512
	// passkeyTriesMost is how many failed ceremonies one flow survives.
	passkeyTriesMost = 5
	// passkeyChallengesMost is how many challenges one flow may ask for.
	passkeyChallengesMost = 20
	// passkeyFinishMost bounds a ceremony response. With attestation "none"
	// a registration is well under 4 KiB; 32 KiB leaves room for any
	// authenticator's extensions without letting a body grow unbounded.
	passkeyFinishMost = 32 << 10
	// passkeyStarts is how many flows one address may start per throttle
	// window (15 minutes), spent on entry whether or not they finish.
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
	return host, u.Scheme + "://" + strings.ToLower(u.Host), true
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
	admitted := st.starts.take(addrKey, now)
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
	flow := &passkeyFlow{purpose: body.Purpose, created: now, expires: now.Add(passkeyFlowLife), state: api.PasskeyPending}
	copy(flow.challenge[:], challenge)
	switch body.Purpose {
	case api.PasskeySignIn:
		if body.Link != "" || body.Username != "" || body.Name != "" {
			h.fail(w, http.StatusUnprocessableEntity, "a passkey sign-in takes no link or username")
			return
		}
	case api.PasskeyAdd:
		session, ok := h.credential(r)
		if !ok || session.profile == nil || session.profile.Kind != tokens.KindPassword || body.Link != "" {
			h.fail(w, http.StatusNotFound, passkeyNoAccount)
			return
		}
		acct, found, err := h.freshAccount(session.profile.Handle)
		if h.failed(w, err) {
			return
		}
		if !found {
			h.fail(w, http.StatusNotFound, passkeyNoAccount)
			return
		}
		if len(acct.Passkeys) >= tokens.PasskeyMost {
			h.fail(w, http.StatusConflict, "this account already keeps as many passkeys as it can")
			return
		}
		flow.handle = acct.Handle
	case api.PasskeyJoin:
		if !h.passkeyJoinFlow(w, body, flow, now) {
			return
		}
	default:
		h.fail(w, http.StatusUnprocessableEntity, "choose sign_in, join or add")
		return
	}
	id, err := secret()
	if h.failed(w, err) {
		return
	}
	flow.id = id
	st.mu.Lock()
	st.pruneLocked(now)
	if len(st.flows) >= passkeyFlowsMost {
		st.mu.Unlock()
		h.tooManyTries(w)
		return
	}
	st.flows[id] = flow
	st.mu.Unlock()
	_, origin, _ := h.relyingParty()
	h.write(w, http.StatusCreated, api.PasskeyStarted{Flow: id, URL: origin + "/passkey/" + id, ExpiresAt: flow.expires.UTC()})
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

// pruneLocked drops ended flows. Called with mu held.
func (st *passkeyState) pruneLocked(now time.Time) {
	for id, f := range st.flows {
		if !now.Before(f.expires) {
			delete(st.flows, id)
		}
	}
}

// verifiedLocked finds the flow id answers to with verifier. A wrong verifier ends
// the flow: whoever holds the id is not whoever started it. Called with mu
// held; the flow answered is still in the table.
func (st *passkeyState) verifiedLocked(id, verifier string, now time.Time) (*passkeyFlow, string) {
	f := st.flows[id]
	if f == nil {
		return nil, api.PasskeyExpired
	}
	v, ok := decodeB64URL32(verifier)
	sum := sha256.Sum256(v)
	if !ok || subtle.ConstantTimeCompare(sum[:], f.challenge[:]) != 1 {
		delete(st.flows, id)
		return nil, api.PasskeyRefused
	}
	if !now.Before(f.expires) {
		delete(st.flows, id)
		return nil, api.PasskeyExpired
	}
	return f, ""
}

// passkeyPoll is POST /v1/auth/passkey/poll. Done answers the session once
// and ends the flow.
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
	// ANSWERED EXACTLY ONCE: the flow leaves the table before the session
	// is made, so a second poll, even a concurrent one, finds nothing.
	delete(st.flows, f.id)
	st.mu.Unlock()
	if f.state != api.PasskeyDone {
		h.write(w, http.StatusOK, api.PasskeyPolled{State: f.state, Message: f.message, Purpose: f.purpose})
		return
	}
	if f.profile == nil {
		h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyDone, Purpose: f.purpose})
		return
	}
	// The account may have been removed, or a way in changed, between the
	// ceremony and this poll; either signs nobody in.
	acct, ok, err := h.freshAccount(f.profile.Handle)
	if h.failed(w, err) {
		return
	}
	if !ok || acct.Gen != f.gen {
		h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyRefused, Message: passkeyUnknownWords, Purpose: f.purpose})
		return
	}
	credential, session, err := h.mintSessionVia(w, r, *f.profile, f.gen, signInPasskey)
	if h.failed(w, err) {
		return
	}
	me := h.meBody(*f.profile, session.id)
	me.Operator = f.profile.Handle == h.operatorLogin(r)
	// A passkey person signed in with a passkey, not with GitHub.
	me.GitHubSignedIn, me.SignInKind = false, signInPasskey
	me.Teams = h.Membership().TeamsOf(f.profile.Handle)
	expires := session.expires.UTC()
	fac := h.factory
	h.write(w, http.StatusOK, api.PasskeyPolled{State: api.PasskeyDone, Purpose: f.purpose, Factory: &fac,
		AccessToken: credential, TokenType: "bearer", ExpiresIn: int(accessSessionTTL.Seconds()), ExpiresAt: &expires,
		Person: &api.PasswordPerson{Handle: f.profile.Handle, Name: f.profile.Name}, Me: &me})
}

// passkeyCancel is POST /v1/auth/passkey/cancel: the engine ends its flow.
func (h *Handler) passkeyCancel(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !h.passkeysOn() {
		h.fail(w, http.StatusNotFound, passkeyOffWords)
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
	AskUsername, AskName, AskLabel, Ended                             bool
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
	if f != nil && !now.Before(f.expires) {
		f = nil
	}
	if f != nil {
		data.Purpose, data.Handle, data.Username = f.purpose, f.handle, f.username
		if f.state != api.PasskeyPending {
			data.Ended = true
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
	case data.Ended:
		data.Heading, data.Lede = "Done", "Go back to codeaf."
	case f.purpose == api.PasskeySignIn:
		data.Heading, data.Lede, data.Button = "Sign in to "+org, "Use the passkey you keep for "+rpID+".", "Sign in with a passkey"
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
	if f == nil || !now.Before(f.expires) || f.state != api.PasskeyPending {
		h.refuse(w, http.StatusGone, passkeyEndedWords, "flow_ended", "", nil, "")
		return nil
	}
	return f
}

// passkeyOptions answers a fresh challenge for the flow's ceremony. A join
// checks its username first, so nobody makes a passkey for a name that is
// taken.
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
	f.challenges++
	purpose, reset, handle, username, name := f.purpose, f.resetSecret, f.handle, f.username, f.name
	st.mu.Unlock()

	var answer any
	var session *webauthn.SessionData
	userID := ""
	switch {
	case purpose == api.PasskeySignIn:
		assertion, s, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
		if h.failed(w, err) {
			return
		}
		answer, session = assertion, s
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
		answer, session = creation, s
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
		answer, session = creation, s
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

// passkeyFinish verifies what the authenticator signed and does what the flow
// is for. The challenge is taken out first, so it can be answered only once.
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
	if snapshot.purpose == api.PasskeySignIn {
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
		if err != nil || user == nil {
			refused(passkeyUnknownWords, "passkey_refused")
			return
		}
		credID := base64.RawURLEncoding.EncodeToString(cred.ID)
		err = tokens.UsedPasskey(h.config.TokensPath, acct.Handle, credID, cred.Authenticator.SignCount, cred.Flags.BackupState, now)
		h.forgetAccounts()
		if err != nil {
			refused(passkeyUnknownWords, "passkey_refused")
			return
		}
		gen = acct.Gen
		if cred.Authenticator.CloneWarning {
			// A counter that went backwards can mean a copied authenticator;
			// a synced passkey never counts, so this is written down for the
			// operator, not refused (the library's guidance).
			_ = h.accountEvent(r, acct.Handle, "", "passkey counter went backwards", "a passkey's signature counter went backwards; it may have been copied", false)
		}
		name := acct.Name
		if name == "" {
			name = acct.Handle
		}
		profile = &api.Profile{Handle: acct.Handle, Name: name, Scopes: []string{}, Kind: tokens.KindPassword}
		if h.failed(w, h.accountEvent(r, acct.Handle, "", "signed in", "signed in with a passkey", false)) {
			return
		}
	} else {
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
		var ok bool
		if profile, gen, ok = h.passkeyRegistered(w, r, snapshot, pk, now); !ok {
			return
		}
	}
	try.succeeded(h.now())
	st.mu.Lock()
	if f := st.flows[id]; f != nil {
		f.state, f.profile, f.gen = api.PasskeyDone, profile, gen
	}
	st.mu.Unlock()
	h.write(w, http.StatusOK, map[string]string{"state": api.PasskeyDone, "message": "Done — go back to codeaf."})
}

// passkeyRegistered does what a verified registration is for: a join makes
// the account and spends the link in SignUp's one critical section; a reset
// replaces every way in; an add puts the passkey on the account. It answers
// whom the flow signs in (nil for an add) or has answered a refusal.
func (h *Handler) passkeyRegistered(w http.ResponseWriter, r *http.Request, f passkeyFlow, pk tokens.Passkey, now time.Time) (*api.Profile, int64, bool) {
	taken := func(words, reason string) {
		h.refuse(w, http.StatusConflict, words, reason, "", nil, "")
	}
	ended := func(words string) {
		st := h.pk()
		st.mu.Lock()
		if live := st.flows[f.id]; live != nil {
			live.state, live.message = api.PasskeyRefused, words
		}
		st.mu.Unlock()
		h.refuse(w, http.StatusGone, words, "flow_ended", "", nil, "")
	}
	switch {
	case f.purpose == api.PasskeyAdd:
		err := tokens.AddPasskey(h.config.TokensPath, f.handle, pk)
		h.forgetAccounts()
		switch {
		case errors.Is(err, tokens.ErrPasskeyTaken):
			taken(passkeyTakenWords, "passkey_taken")
			return nil, 0, false
		case errors.Is(err, tokens.ErrNoAccount):
			ended(passkeyEndedWords)
			return nil, 0, false
		case h.failed(w, err):
			return nil, 0, false
		}
		if h.failed(w, h.accountEvent(r, f.handle, "", "added passkey", "added a passkey", false)) {
			return nil, 0, false
		}
		return nil, 0, true
	case f.resetSecret != "":
		handle, gen, err := tokens.UseResetPasskey(h.config.TokensPath, f.resetSecret, pk, now)
		h.forgetAccounts()
		switch {
		case errors.Is(err, tokens.ErrPasskeyTaken):
			taken(passkeyTakenWords, "passkey_taken")
			return nil, 0, false
		case errors.Is(err, tokens.ErrResetInvalid):
			ended("This reset link has been used or has ended.")
			return nil, 0, false
		case h.failed(w, err):
			return nil, 0, false
		}
		if h.failed(w, h.endSessionsOf(handle, "", gen)) {
			return nil, 0, false
		}
		if h.failed(w, h.accountEvent(r, handle, "", "reset password", "chose a new passkey through the operator's reset link", false)) {
			return nil, 0, false
		}
		acct, _, _ := h.passwordAccount(handle)
		name := acct.Name
		if name == "" {
			name = handle
		}
		return &api.Profile{Handle: handle, Name: name, Scopes: []string{}, Kind: tokens.KindPassword}, gen, true
	}
	profile := api.Profile{Handle: f.username, Name: f.name, Scopes: []string{}, Kind: tokens.KindPassword}
	h.joinMu.Lock()
	// As E9's signUp: what else names people is read before the row lock.
	referenced, err := h.handleReferenced(f.username)
	if err != nil {
		h.joinMu.Unlock()
		h.failed(w, err)
		return nil, 0, false
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
		taken(refusalWords[reasonUsernameTaken], reasonUsernameTaken)
		return nil, 0, false
	case errors.Is(err, tokens.ErrPasskeyTaken):
		taken(passkeyTakenWords, "passkey_taken")
		return nil, 0, false
	case errors.Is(err, tokens.ErrGitHubLink):
		taken(refusalWords[reasonGitHubLink], reasonGitHubLink)
		return nil, 0, false
	case errors.Is(err, tokens.ErrRevoked), errors.Is(err, tokens.ErrExpired), errors.Is(err, tokens.ErrUsedUp), errors.Is(err, os.ErrPermission):
		ended("This access link has been used or has ended.")
		return nil, 0, false
	default:
		h.failed(w, err)
		return nil, 0, false
	}
	if h.failed(w, h.accountEvent(r, f.username, "", "signed up", "signed up with a passkey", f.operator)) {
		return nil, 0, false
	}
	// A new account starts at generation 1, as SignUp writes it.
	return &profile, 1, true
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
