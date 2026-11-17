package serve

// ── USERNAME + PASSWORD SIGN-IN ON AN ORGANIZATION SERVER ───────────────────
//
// The default way into an organization is a username and a password; GitHub's
// device flow stays as the second way. The doors:
//
//   POST /v1/factory-access  verb sign_up  through an operator claim or an
//                            invitation: make the account, spend the link,
//                            answer a session (state joined, access_token)
//   POST /v1/factory-access  verb reset    through an operator's reset link
//   POST /v1/auth/password   sign in again, with no link
//   POST /v1/me/password     change it, signed in
//   POST /v1/access/links    kind reset    the operator makes a reset link
//
// EVERY ONE OF THEM IS JSON ONLY: Content-Type application/json, a body of at
// most passwordBodyMost bytes, decoded strictly. No HTML form takes a
// password, so there is no form for another site to post. Every answer is
// Cache-Control: no-store. A password is only taken over https, or from a
// caller on this machine; the https may be a trusted proxy's, which the
// server's own resolver (proxy.go) decides.
//
// A REFUSED SIGN-IN SAYS ONE THING. An unknown username, a wrong password and
// a removed person all answer 401 with signInRefusedWords, after the same
// argon2 verify (an unknown name verifies against a dummy hash). Only sign-up
// can say a username is taken, and only to someone holding an open link.
//
// THE PASSWORD GOES NOWHERE. It is normalized, judged, hashed and dropped. No
// error, record row, answer or log line carries it; the leak test pushes a
// known password through every door and reads every sink for it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// passwordBodyMost bounds every body a password door reads.
const passwordBodyMost = 8 << 10

// accessSessionLife is a session's absolute end; accessSessionTTL (30 days)
// is how long it lives unused.
const accessSessionLife = 90 * 24 * time.Hour

// Sign-in methods, as /v1/about and the access answers spell them.
const (
	signInPassword = "password"
	signInGitHub   = "github"
)

func (h *Handler) guard() *passwordGuard {
	h.pwOnce.Do(func() { h.pwGuard = newPasswordGuard() })
	return h.pwGuard
}

// passwordsOn is whether this server keeps password accounts: it is a team
// server with a place for its links, which is where accounts are kept.
func (h *Handler) passwordsOn() bool {
	return h.config.TokensPath != "" && h.mode() == api.FactoryTeam
}

// signInMethods is how a person may sign in here, preferred first.
func (h *Handler) signInMethods() []string {
	out := []string{}
	// A passkey is the first choice wherever the server can offer one: an
	// https address with a DNS name (passkey.go relyingParty).
	if h.passkeysOn() {
		out = append(out, signInPasskey)
	}
	if h.passwordsOn() {
		out = append(out, signInPassword)
	}
	if h.config.GitHubClientID != "" {
		out = append(out, signInGitHub)
	}
	return out
}

func personKind(p api.Profile) string {
	if p.Kind == tokens.KindPassword {
		return signInPassword
	}
	return signInGitHub
}

// noStore marks an answer that may carry a credential as never to be cached.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

// passwordTransport refuses a password over plain HTTP from another machine.
func (h *Handler) passwordTransport(w http.ResponseWriter, r *http.Request) bool {
	if secureRequest(r) || clientOf(r).onThisMachine() {
		return true
	}
	h.refuse(w, http.StatusForbidden, passwordHTTPSWords, "https_required", "", nil, "")
	return false
}

// readPasswordBody reads a password door's JSON body strictly, or answers why not.
func (h *Handler) readPasswordBody(w http.ResponseWriter, r *http.Request, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		h.fail(w, http.StatusUnsupportedMediaType, "send this as application/json")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, passwordBodyMost))
	if err != nil {
		h.fail(w, http.StatusRequestEntityTooLarge, "the request is larger than 8 KiB")
		return false
	}
	return h.strictJSON(w, data, out)
}

// strictJSON decodes one JSON object with no unknown fields. Its refusal
// never echoes the body, which may hold a password.
func (h *Handler) strictJSON(w http.ResponseWriter, data []byte, out any) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil || d.Decode(new(any)) != io.EOF {
		h.fail(w, http.StatusUnprocessableEntity, "the request is not the JSON this route takes")
		return false
	}
	return true
}

// throttleKeys are the username and the address a try is counted under.
func throttleKeys(r *http.Request, username string) (user, addr string) {
	user = strings.ToLower(strings.TrimSpace(username))
	if len(user) > 64 {
		user = user[:64]
	}
	return user, throttleAddr(clientOf(r).addr)
}

// admitTry admits one password try, or answers 429 and false. trusted is
// what the try is trusted by (passwordGuard.admit), or "". alone is whether
// it must be the only try of the name (or trusted pair) being verified.
func (h *Handler) admitTry(w http.ResponseWriter, r *http.Request, username, trusted string, alone bool) (*attempt, bool) {
	user, addr := throttleKeys(r, username)
	a, ok := h.guard().admit(user, addr, trusted, h.now(), alone)
	if !ok {
		h.tooManyTries(w)
	}
	return a, ok
}

func (h *Handler) tooManyTries(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	h.refuse(w, http.StatusTooManyRequests, tooManyTriesWords, "too_many_tries", "", nil, "")
}

// accountEvent writes one password account's audit row. It never carries a
// secret: the detail is fixed words and handles.
func (h *Handler) accountEvent(r *http.Request, handle, actor, verb, detail string, operator bool) error {
	if h.config.Record == nil {
		return nil
	}
	if actor == "" {
		actor = handle
	}
	name := ""
	if actor != "" {
		name = "@" + actor
	}
	_, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.AccountChanged, Handle: actor, Object: "person:" + handle,
		Verb: verb, Detail: detail, Actor: record.Actor{Kind: "person", Name: name, Operator: operator}})
	return err
}

// knownElsewhere is a handle the token files do not record but the server
// knows: a signed-in session, the host's own login, the configured team. It
// is asked under the row lock, so it reads only memory.
func (h *Handler) knownElsewhere(handle string) bool {
	if handle == factory.NormalizeHandle(h.config.OperatorLogin) {
		return true
	}
	for _, member := range h.config.Team {
		if factory.NormalizeHandle(member) == handle {
			return true
		}
	}
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	for _, s := range h.sessions.ids {
		if profileHandle(s) == handle {
			return true
		}
	}
	return false
}

// errPasswordMoved is a sign-in whose password was replaced while it was
// being verified: no session is made with a password that no longer works.
var errPasswordMoved = errors.New("the password changed during the sign-in")

// mintSession stores a new person session made under the account's
// generation gen and answers its credential. It is the device flow's ending
// without GitHub: no link row holds it, so it lives by its own expiry and the
// account staying at gen (sessionUsable). The generation is read again under
// the session lock, the one a change or a reset takes to end sessions: a
// sign-in verified against a password replaced meanwhile makes nothing.
func (h *Handler) mintSession(w http.ResponseWriter, r *http.Request, profile api.Profile, gen int64) (string, accessSession, error) {
	return h.mintSessionVia(w, r, profile, gen, "")
}

// mintSessionVia is mintSession for a session made some way other than a
// password (via, as accessSession.via), which /v1/me reports as its kind.
func (h *Handler) mintSessionVia(w http.ResponseWriter, r *http.Request, profile api.Profile, gen int64, via string) (string, accessSession, error) {
	credential, err := secret()
	if err != nil {
		return "", accessSession{}, err
	}
	id, err := secret()
	if err != nil {
		return "", accessSession{}, err
	}
	now := h.now()
	session := accessSession{id: "session_" + id, expires: now.Add(accessSessionTTL), created: now, lastSeen: now,
		profile: &profile, device: deviceLabel(r), credGen: gen, via: via}
	h.sessions.mu.Lock()
	acct, held, err := h.freshAccount(profile.Handle)
	if err == nil && (!held || acct.Gen != gen) {
		err = errPasswordMoved
	}
	if err != nil {
		h.sessions.mu.Unlock()
		return "", accessSession{}, err
	}
	for key, old := range h.sessions.ids {
		if !now.Before(old.expires) {
			delete(h.sessions.ids, key)
		}
	}
	h.sessions.ids[sessionHash(credential)] = session
	err = h.saveSessions()
	if err != nil {
		delete(h.sessions.ids, sessionHash(credential))
	}
	h.sessions.mu.Unlock()
	if err != nil {
		return "", accessSession{}, err
	}
	if h.viewer != nil {
		h.viewer.see(profile, now, now, "")
	}
	h.setSessionCookie(w, r, credential, session)
	return credential, session, nil
}

// endSessionsOf ends every session of handle except the one whose public id
// is keep ("" ends them all), which moves to the account's new generation.
func (h *Handler) endSessionsOf(handle, keep string, gen int64) error {
	h.sessions.mu.Lock()
	defer h.sessions.mu.Unlock()
	for key, s := range h.sessions.ids {
		if profileHandle(s) != handle {
			continue
		}
		if keep != "" && s.id == keep {
			s.credGen, s.pendingGen = gen, 0
			h.sessions.ids[key] = s
			continue
		}
		delete(h.sessions.ids, key)
	}
	return h.saveSessions()
}

// passwordRefused answers a refused sign_up or reset on the access card.
func (h *Handler) passwordRefused(w http.ResponseWriter, out api.FactoryAccess, reason string) {
	out.Reason, out.Message = reason, refusalWords[reason]
	h.write(w, http.StatusOK, out)
}

// signUp makes a password account through the link tok and signs it in.
// out is the access card already judged "ready" for that link.
func (h *Handler) signUp(w http.ResponseWriter, r *http.Request, body api.FactoryAccessRequest, tok tokens.Token, out api.FactoryAccess) {
	if !h.passwordsOn() {
		h.fail(w, http.StatusNotFound, "this organization does not take passwords")
		return
	}
	// A link made for one GitHub login is that person's: whoever holds it
	// signs in with GitHub as them or not at all, never with a password.
	if tok.InvitedLogin != "" {
		h.passwordRefused(w, out, reasonGitHubLink)
		return
	}
	username := cleanUsername(body.Username)
	if username == "" {
		h.passwordRefused(w, out, reasonUsernameInvalid)
		return
	}
	name, ok := cleanDisplayName(body.Name)
	if !ok {
		h.passwordRefused(w, out, reasonNameInvalid)
		return
	}
	if name == "" {
		name = username
	}
	normal := normalPassword(body.Password)
	if reason := passwordRefusal(normal, username); reason != "" {
		h.passwordRefused(w, out, reason)
		return
	}
	hash, err := hashPassword(normal)
	if errors.Is(err, errHashBusy) {
		h.tooManyTries(w)
		return
	}
	if h.failed(w, err) {
		return
	}
	profile := api.Profile{Handle: username, Name: name, Scopes: []string{}, Kind: tokens.KindPassword}
	now := h.now()
	h.joinMu.Lock()
	// Everything else that names people is read before the row lock, which
	// none of it may take; a GitHub sign-in stores its session under joinMu,
	// so it cannot slip between this read and the account.
	referenced, err := h.handleReferenced(username)
	if err != nil {
		h.joinMu.Unlock()
		h.failed(w, err)
		return
	}
	taken := func(handle string) bool { return referenced || h.knownElsewhere(handle) }
	_, err = tokens.SignUp(h.config.TokensPath, tok.ID, tokens.Account{Name: name, Hash: hash}, profile, now, taken, func() error {
		if h.config.Record == nil {
			return nil
		}
		_, e := h.config.Record.Append(r.Context(), record.Event{Kind: record.OperatorClaimed, Handle: username, Object: "factory",
			Actor: record.Actor{Kind: "person", Name: "@" + username, Operator: true}, Detail: "claimed factory as operator"})
		return e
	})
	h.joinMu.Unlock()
	h.forgetAccounts()
	switch {
	case err == nil:
	case errors.Is(err, tokens.ErrHandleTaken):
		h.passwordRefused(w, out, reasonUsernameTaken)
		return
	case errors.Is(err, tokens.ErrGitHubLink):
		h.passwordRefused(w, out, reasonGitHubLink)
		return
	case errors.Is(err, tokens.ErrRevoked):
		out.State = "revoked"
		h.write(w, http.StatusOK, out)
		return
	case errors.Is(err, tokens.ErrExpired):
		out.State = "expired"
		h.write(w, http.StatusOK, out)
		return
	case errors.Is(err, os.ErrPermission):
		// Used up since the card was read, or an operator claim already made.
		out.State = "used_up"
		h.write(w, http.StatusOK, out)
		return
	default:
		h.failed(w, err)
		return
	}
	if h.failed(w, h.accountEvent(r, username, "", "signed up", "signed up with a password", tok.Operator)) {
		return
	}
	// An invite's team, workstreams and tasks are theirs from the first screen.
	h.settleInvites(r)
	credential, _, err := h.mintSession(w, r, profile, 1)
	if h.failed(w, err) {
		return
	}
	device, err := h.issueDevice(username, 1, body.WantDevice)
	if h.failed(w, err) {
		return
	}
	out.State, out.AccessToken, out.DeviceToken, out.Message, out.Reason = "joined", credential, device, "", ""
	h.passwordRepos(&out)
	h.write(w, http.StatusOK, out)
}

// passwordRepos is the repositories a password person sees: none needs
// GitHub to be listed, and none is ever a reason to turn them away, because
// a repository grant is a GitHub thing they simply do not have.
func (h *Handler) passwordRepos(out *api.FactoryAccess) {
	out.Factory.Repos = []string{}
}

// resetAccess is the access card for an operator's reset link, and its use.
func (h *Handler) resetAccess(w http.ResponseWriter, r *http.Request, body api.FactoryAccessRequest, reset tokens.Reset, out api.FactoryAccess) {
	now := h.now()
	out.Kind = "reset"
	out.Detail = "Choose a new password for @" + reset.Handle + " · expires " + reset.Expires.Local().Format("15:04")
	expires := reset.Expires
	out.ExpiresAt = &expires
	out.MaxUses, out.Used, out.Remaining = 1, 0, 1
	switch {
	case !reset.Used.IsZero():
		out.State, out.Used, out.Remaining = "used_up", 1, 0
	case !now.Before(reset.Expires):
		out.State = "expired"
	default:
		out.State = "ready"
	}
	if _, ok, err := h.passwordAccount(reset.Handle); err != nil {
		h.failed(w, err)
		return
	} else if !ok && out.State == "ready" {
		out.State = "revoked"
	}
	if body.Verb != "reset" || out.State != "ready" {
		h.write(w, http.StatusOK, out)
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	normal := normalPassword(body.Password)
	if reason := passwordRefusal(normal, reset.Handle); reason != "" {
		h.passwordRefused(w, out, reason)
		return
	}
	hash, err := hashPassword(normal)
	if errors.Is(err, errHashBusy) {
		h.tooManyTries(w)
		return
	}
	if h.failed(w, err) {
		return
	}
	_, link, _ := strings.Cut(body.Link, "#")
	fragment, _ := url.ParseQuery(link)
	handle, gen, err := tokens.UseReset(h.config.TokensPath, fragment.Get("token"), hash, now)
	h.forgetAccounts()
	if errors.Is(err, tokens.ErrResetInvalid) {
		out.State = "used_up"
		h.write(w, http.StatusOK, out)
		return
	}
	if h.failed(w, err) {
		return
	}
	// A reset is the recovery door: whoever held the old password, or a
	// session made with it, is out.
	if h.failed(w, h.endSessionsOf(handle, "", gen)) {
		return
	}
	if h.failed(w, h.accountEvent(r, handle, "", "reset password", "reset password through the operator's link", false)) {
		return
	}
	acct, _, _ := h.passwordAccount(handle)
	credential, _, err := h.mintSession(w, r, api.Profile{Handle: handle, Name: acct.Name, Scopes: []string{}, Kind: tokens.KindPassword}, gen)
	if h.failed(w, err) {
		return
	}
	device, err := h.issueDevice(handle, gen, body.WantDevice)
	if h.failed(w, err) {
		return
	}
	out.State, out.AccessToken, out.DeviceToken = "joined", credential, device
	h.passwordRepos(&out)
	h.write(w, http.StatusOK, out)
}

// passwordSignIn is POST /v1/auth/password: sign in again with no link.
func (h *Handler) passwordSignIn(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	if !h.passwordsOn() {
		h.fail(w, http.StatusNotFound, "this organization does not take passwords")
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasswordSignIn
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	username := cleanUsername(body.Username)
	// A trusted device is judged before the try is admitted, since it is
	// what decides which buckets the try counts against.
	device := h.trustedDevice(username, body.Device)
	trusted := ""
	if device != "" {
		trusted = "device:" + device
	}
	try, ok := h.admitTry(w, r, body.Username, trusted, true)
	if !ok {
		return
	}
	defer try.abandon(h.now())
	acct, found, err := h.passwordAccount(username)
	if h.failed(w, err) {
		return
	}
	normal := normalPassword(body.Password)
	if len(normal) > passwordMaxBytes {
		// Nothing that long was ever accepted, so nothing is hashed for it.
		normal, found = "", false
	}
	phc := dummyHash()
	if found && acct.Hash != "" {
		phc = acct.Hash
	} else {
		// A person who signs in only with a passkey has no password to
		// match, and pays the same verify as an unknown name.
		found = false
	}
	ok, weaker, err := verifyPassword(normal, phc)
	if errors.Is(err, errHashBusy) {
		h.tooManyTries(w)
		return
	}
	refused := func() {
		if try.failed(h.now()) && found {
			_ = h.accountEvent(r, username, "", "refused sign-ins", "sign-ins with a wrong password ran out; tries are slowed down", false)
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.refuse(w, http.StatusUnauthorized, signInRefusedWords, "sign_in_refused", "", nil, "")
	}
	if !ok || !found || err != nil {
		refused()
		return
	}
	now := h.now()
	if weaker {
		// The stored parameters are older and weaker; the password is in hand
		// once, so it is rehashed at today's. Only over the very hash it
		// verified: a change or a reset made meanwhile is never undone.
		if stronger, e := hashPassword(normal); e == nil {
			_ = tokens.Rehash(h.config.TokensPath, username, acct.Hash, stronger)
			h.forgetAccounts()
		}
	}
	profile := api.Profile{Handle: username, Name: acct.Name, Scopes: []string{}, Kind: tokens.KindPassword}
	if profile.Name == "" {
		profile.Name = username
	}
	credential, session, err := h.mintSession(w, r, profile, acct.Gen)
	if errors.Is(err, errPasswordMoved) {
		refused()
		return
	}
	if h.failed(w, err) {
		return
	}
	try.succeeded(now)
	newDevice := ""
	if device != "" {
		h.touchDevice(device)
	} else if newDevice, err = h.issueDevice(username, acct.Gen, body.WantDevice); h.failed(w, err) {
		return
	}
	if h.failed(w, h.accountEvent(r, username, "", "signed in", "signed in with a password", false)) {
		return
	}
	me := h.meBody(profile, session.id)
	me.Operator = username == h.operatorLogin(r)
	me.SignInKind = signInPassword
	me.Teams = h.Membership().TeamsOf(username)
	h.write(w, http.StatusOK, api.PasswordSignedIn{Factory: h.factory, AccessToken: credential, TokenType: "bearer",
		ExpiresIn: int(accessSessionTTL.Seconds()), ExpiresAt: session.expires.UTC(),
		Person: api.PasswordPerson{Handle: username, Name: profile.Name}, Me: me, DeviceToken: newDevice})
}

// passwordChange is POST /v1/me/password: current and new, signed in. Every
// other session of the person ends; this one stays.
func (h *Handler) passwordChange(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	session, ok := h.credential(r)
	if !ok || session.profile == nil || session.profile.Kind != tokens.KindPassword || !h.passwordsOn() {
		h.fail(w, http.StatusNotFound, "this sign-in has no password")
		return
	}
	if !h.passwordTransport(w, r) {
		return
	}
	var body api.PasswordChange
	if !h.readPasswordBody(w, r, &body) {
		return
	}
	handle := session.profile.Handle
	// The session proves who is changing; a change counts per session, not
	// against the username's ceiling a stranger may be draining.
	try, ok := h.admitTry(w, r, handle, "session:"+session.id, false)
	if !ok {
		return
	}
	defer try.abandon(h.now())
	acct, found, err := h.passwordAccount(handle)
	if h.failed(w, err) {
		return
	}
	if !found {
		h.fail(w, http.StatusNotFound, "this sign-in has no password")
		return
	}
	current := normalPassword(body.Current)
	if len(current) > passwordMaxBytes {
		current = ""
	}
	// A person who signs in only with a passkey sets a first password with
	// no current one; the signed-in session is the proof, as it is for
	// adding a passkey. Anyone with a password must give it.
	// STEP-UP (passkey_stepup.go): setting a first password with no current
	// one is adding a way in, which a session alone may not do; it needs a
	// confirmation made for it, spent below once the new password is good.
	firstPassword := api.StepUpAction{Kind: api.StepUpSetPassword}
	if acct.Hash == "" && !h.granted(session, firstPassword, false) {
		h.stepUp(w, firstPassword)
		return
	}
	match, err := acct.Hash == "" && current == "", error(nil)
	if acct.Hash != "" {
		match, _, err = verifyPassword(current, acct.Hash)
	}
	if errors.Is(err, errHashBusy) {
		h.tooManyTries(w)
		return
	}
	if !match || err != nil {
		try.failed(h.now())
		h.refuse(w, http.StatusForbidden, "Your current password is not right.", "current_wrong", "", nil, "")
		return
	}
	try.succeeded(h.now())
	next := normalPassword(body.New)
	if reason := passwordRefusal(next, handle); reason != "" {
		h.refuse(w, http.StatusUnprocessableEntity, refusalWords[reason], reason, "", nil, "")
		return
	}
	if acct.Hash == "" && !h.granted(session, firstPassword, true) {
		h.stepUp(w, firstPassword)
		return
	}
	hash, err := hashPassword(next)
	if errors.Is(err, errHashBusy) {
		h.tooManyTries(w)
		return
	}
	if h.failed(w, err) {
		return
	}
	// This session lives at the new generation too from before the write,
	// so a request it makes while the change lands is not turned away; the
	// session lock is not held across the write (row lock, then sessions).
	h.setPendingGen(session.id, acct.Gen+1)
	gen, err := tokens.SetPassword(h.config.TokensPath, handle, hash, acct.Gen, h.now())
	h.forgetAccounts()
	if err != nil {
		h.setPendingGen(session.id, 0)
	}
	if errors.Is(err, tokens.ErrStale) {
		h.refuse(w, http.StatusConflict, "Your password was changed meanwhile; sign in again.", "password_moved", "", nil, "")
		return
	}
	if h.failed(w, err) {
		return
	}
	if h.failed(w, h.endSessionsOf(handle, session.id, gen)) {
		return
	}
	if h.failed(w, h.accountEvent(r, handle, "", "changed password", "changed password; other sign-ins ended", false)) {
		return
	}
	// Every trusted device ended with the old password; this one is
	// trusted again at the new.
	device, err := h.issueDevice(handle, gen, body.WantDevice)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.PasswordChanged{DeviceToken: device})
}

// createResetLink is POST /v1/access/links with kind reset: the operator's
// one-time link that lets a password person choose a new password.
func (h *Handler) createResetLink(w http.ResponseWriter, r *http.Request, body api.CreateAccessLink) {
	noStore(w)
	person := factory.NormalizeHandle(body.Person)
	if _, ok, err := h.passwordAccount(person); err != nil {
		h.failed(w, err)
		return
	} else if !ok || !h.passwordsOn() {
		h.fail(w, http.StatusUnprocessableEntity, "only a person who signs in with a password can be sent a reset link")
		return
	}
	minted, err := tokens.Mint()
	if h.failed(w, err) {
		return
	}
	now := h.now()
	reset := tokens.Reset{ID: "rst_" + strings.TrimPrefix(minted.ID, "tok_"), Handle: person, CreatedBy: h.operatorLogin(r),
		Created: now, Expires: now.Add(tokens.ResetLinkTTL)}
	clearPhase(w)
	if h.failed(w, tokens.AddReset(h.config.TokensPath, reset, minted.Token)) {
		return
	}
	if h.failed(w, h.accountEvent(r, person, h.operatorLogin(r), "made password reset link", "made a one-time password reset link for @"+person, true)) {
		return
	}
	counted := true
	link := api.AccessLink{Kind: "reset", ID: reset.ID, CreatedAt: now, ExpiresAt: reset.Expires, CreatedBy: reset.CreatedBy,
		Login: person, MaxUses: 1, Remaining: 1, Status: "open", Counted: &counted,
		URL: "codeaf://factory/join?" + url.Values{"url": {h.factory.Address}, "reset": {"true"}}.Encode() + "#token=" + url.QueryEscape(minted.Token)}
	h.write(w, http.StatusCreated, struct {
		Factory api.Factory    `json:"factory"`
		Link    api.AccessLink `json:"link"`
	}{h.factory, link})
}

// githubNameHeld is the refusal for a GitHub sign-in whose login is a
// password person's username: the two are never merged silently.
const githubNameHeldWords = "That name belongs to someone who signs in here with a password."
