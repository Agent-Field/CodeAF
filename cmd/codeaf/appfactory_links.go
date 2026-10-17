package main

// ── codeaf app: INVITATIONS ─────────────────────────────────────────────────
//
// The engine's half of appwire.FactoryLinkAdmin. It lists a team factory's
// invitations, makes one and puts it on the clipboard, copies a held one again,
// and revokes one. Each method speaks to ONE route family of the factory's /v1
// API, spelled here as constants; the path and the verb are never taken from
// the app, and an id the app names is validated and escaped before it is part
// of a path.
//
// WHO MAY: the OPERATOR, with the credential this app holds for that factory
// right now (whoHolds), on a TEAM factory. A connection that is old, local,
// solo, unproven or signed out is refused before a request leaves. A
// GitHub-verified profile is NOT required: a legitimate operator may hold a key,
// and nothing here infers a profile from a role or an id.
//
// THE INVITATION'S ADDRESS AND SECRET EXIST IN ONE PLACE: the answer to
// creating it. This file takes it from that answer, registers it with the
// redactors, hands it to the clipboard on standard input, and is allowed to
// keep it in memory (heldLink) for at most ten minutes and never past the
// invitation's own expiry, only so that a failed or unconfirmed copy can be
// retried WITHOUT making a second live credential. It is never on the wire, in
// an argument, an environment variable, a file, a log or an error.
//
// A CREATE IS SENT ONCE. There is no idempotency key, so a second request is a
// second invitation. A confirmed invitation whose copy failed or could not be
// confirmed is NEVER deleted and NEVER made again: it is held, and link_copy
// retries the copy of that same one. A create whose outcome is ambiguous is
// appwire.CodeUncertain: nothing is copied, deleted or retried, and the next
// explicit create is refused until the list has been read once. After that it
// is allowed, and the list and that create say an earlier one could still be live.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clipboard"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

var _ appwire.FactoryLinkAdmin = (*appBackend)(nil)

const (
	appFactoryLinksRoute = "/v1/access/links"
	// appFactoryCopyBudget bounds one clipboard write, whatever the copier does.
	appFactoryCopyBudget = 5 * time.Second
	// appFactoryLinkSlack keeps an unconfirmed create "possibly live" a little
	// past the latest moment it could have expired, for clock differences.
	appFactoryLinkSlack = time.Minute
)

// ── the clipboard seam ─────────────────────────────────────────────────────

// copier is this backend's clipboard. PRODUCTION DEFAULT: UNAVAILABLE. A
// backend nobody has shown to be the person's own native session has no
// clipboard, so nothing is made that could not be handed over.
func (b *appBackend) copier() clipboard.Copier {
	if b.clipboard == nil {
		return clipboard.Unavailable{}
	}
	return b.clipboard
}

// useClipboard is THE ONE PLACE a real clipboard is installed. The launcher in
// app.go calls it once, before the first call is served, with
// clipboard.NewSystem(localGUI), where localGUI is true only when the engine was
// started as the local child of the person's own native app (the
// CODEAF_APP_LOCAL_GUI marker, app_localgui.go). Every other door installs
// nothing and so keeps clipboard.Unavailable. The operating system, DISPLAY and
// an installed helper are never evidence of that on their own.
func (b *appBackend) useClipboard(c clipboard.Copier) { b.clipboard = c }

// ── what is held ───────────────────────────────────────────────────────────

// heldLink is one made invitation kept for a retry of the copy. It prints as
// nothing under every verb, in JSON, and in a log record, so the secret cannot
// be formatted by accident.
type heldLink struct {
	ref    string
	linkID string
	entry  appwire.FactoryLink
	secret []byte
	until  time.Time
	// digest is sha256 of the bearer the invitation was made with: a different
	// caller, or the same person signed in again, does not own this copy.
	digest [32]byte
}

const heldWords = "[held invitation]"

func (heldLink) String() string               { return heldWords }
func (heldLink) GoString() string             { return heldWords }
func (heldLink) MarshalJSON() ([]byte, error) { return []byte(`"` + heldWords + `"`), nil }
func (heldLink) MarshalText() ([]byte, error) { return []byte(heldWords), nil }
func (heldLink) LogValue() slog.Value         { return slog.StringValue(heldWords) }
func (heldLink) Format(f fmt.State, _ rune)   { _, _ = io.WriteString(f, heldWords) }
func (l *heldLink) wipe() {
	for i := range l.secret {
		l.secret[i] = 0
	}
	l.secret = nil
}

func bearerDigest(token string) [32]byte { return sha256.Sum256([]byte(token)) }

// linkState is the hub's memory of invitations, under h.mu.
type linkState struct {
	held map[string]*heldLink
	// mustList: a create on this connection was ambiguous, so the list has to
	// be read before another is made.
	mustList map[string]bool
	// unknownUntil: an unconfirmed create could still be live until this time.
	unknownUntil map[string]time.Time
}

func (h *appFactoryHub) linkMem() *linkState {
	if h.linkSt == nil {
		h.linkSt = &linkState{held: map[string]*heldLink{}, mustList: map[string]bool{}, unknownUntil: map[string]time.Time{}}
	}
	return h.linkSt
}

// dropHeldLocked forgets and wipes the copy held for id. h.mu is held.
func (h *appFactoryHub) dropHeldLocked(id string) {
	if h.linkSt == nil {
		return
	}
	if l := h.linkSt.held[id]; l != nil {
		l.wipe()
		delete(h.linkSt.held, id)
	}
}

// dropHeld is dropHeldLocked from outside the lock.
func (h *appFactoryHub) dropHeld(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropHeldLocked(id)
}

// dropAllHeldLocked is for closing. h.mu is held.
func (h *appFactoryHub) dropAllHeldLocked() {
	if h.linkSt == nil {
		return
	}
	for id := range h.linkSt.held {
		h.dropHeldLocked(id)
	}
}

// sweepHeldLocked drops every held copy that has run out. h.mu is held.
func (h *appFactoryHub) sweepHeldLocked() {
	if h.linkSt == nil {
		return
	}
	now := h.now()
	for id, l := range h.linkSt.held {
		if !now.Before(l.until) {
			h.dropHeldLocked(id)
		}
	}
}

// keep holds l as the one copy for id, replacing the old one, for any caller.
// It is bounded: over the cap, the copy that runs out soonest goes.
func (h *appFactoryHub) keep(id string, l *heldLink) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.keepLocked(id, l)
}

// keepFor is keep for ONE caller, decided under the same lock that stores the
// copy: when id no longer holds token (a different sign-in, a sign-out, a
// detach, a close) nothing is stored, l is wiped, and the answer is false. A
// check made before keep would leave a window in which a replaced caller's
// secret was stored.
func (h *appFactoryHub) keepFor(id, token string, l *heldLink) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if token == "" || h.closed || h.tokenLocked(id) != token {
		l.wipe()
		return false
	}
	h.keepLocked(id, l)
	return true
}

func (h *appFactoryHub) keepLocked(id string, l *heldLink) {
	if h.closed {
		l.wipe()
		return
	}
	st := h.linkMem()
	h.sweepHeldLocked()
	h.dropHeldLocked(id)
	for len(st.held) >= appwire.LinkHoldMax {
		var oldest string
		for k, v := range st.held {
			if oldest == "" || v.until.Before(st.held[oldest].until) {
				oldest = k
			}
		}
		h.dropHeldLocked(oldest)
	}
	st.held[id] = l
}

// held answers the copy held for id when ref is its ref, the bearer is the one
// it was made with, AND that bearer is the one id holds right now; it drops the
// copy when it has run out or the bearer changed. A caller who was replaced
// while a request was in flight therefore gets nothing, however long ago it
// asked.
func (h *appFactoryHub) held(id, ref, token string) (secret string, entry appwire.FactoryLink, until time.Time, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if token == "" || h.closed || h.tokenLocked(id) != token {
		return "", appwire.FactoryLink{}, time.Time{}, false
	}
	h.sweepHeldLocked()
	if h.linkSt == nil {
		return "", appwire.FactoryLink{}, time.Time{}, false
	}
	l := h.linkSt.held[id]
	if l == nil {
		return "", appwire.FactoryLink{}, time.Time{}, false
	}
	digest := bearerDigest(token)
	if subtle.ConstantTimeCompare([]byte(l.ref), []byte(ref)) != 1 {
		return "", appwire.FactoryLink{}, time.Time{}, false
	}
	if subtle.ConstantTimeCompare(l.digest[:], digest[:]) != 1 {
		h.dropHeldLocked(id)
		return "", appwire.FactoryLink{}, time.Time{}, false
	}
	return string(l.secret), l.entry, l.until, true
}

func (h *appFactoryHub) forgetHeldLink(id, linkID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.linkSt != nil {
		if l := h.linkSt.held[id]; l != nil && l.linkID == linkID {
			h.dropHeldLocked(id)
		}
	}
}

func (h *appFactoryHub) setMustList(id string, v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.linkMem().mustList[id] = v
}

func (h *appFactoryHub) mustList(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.linkMem().mustList[id]
}

// markUnknown records that a create may still be live until until.
func (h *appFactoryHub) markUnknown(id string, until time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.linkMem()
	if until.After(st.unknownUntil[id]) {
		st.unknownUntil[id] = until
	}
}

// earlierUnknown is true while an unconfirmed create could still be live.
func (h *appFactoryHub) earlierUnknown(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.linkMem()
	until, ok := st.unknownUntil[id]
	if ok && !h.now().Before(until) {
		delete(st.unknownUntil, id)
		return false
	}
	return ok
}

// callerIs is true while id still holds exactly token. Every wait in this file
// ends with it, so what a request learned for one caller is never shown to,
// copied for or kept for another.
func (h *appFactoryHub) callerIs(id, token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return token != "" && !h.closed && h.tokenLocked(id) == token
}

// ── gates ──────────────────────────────────────────────────────────────────

// linkWho is the caller the factory says holds this connection, and only when
// that caller is the operator of a team factory.
func (b *appBackend) linkWho(ctx context.Context, id string) (adminWho, error) {
	who, err := b.whoHolds(ctx, id)
	if err != nil {
		return adminWho{}, err
	}
	// THE FACTORY ANSWERS A NON-OPERATOR WITH 401, which reads as "signed out",
	// so the refusal is made here, in words, before anything is sent.
	if !who.me.Operator {
		return adminWho{}, appwire.Errorf(appwire.CodeForbidden, "only the operator can manage invitations")
	}
	if who.me.Mode != api.FactoryTeam {
		return adminWho{}, appwire.Errorf(appwire.CodeConflict, "only a team factory has invitations")
	}
	return who, nil
}

// ── one request ────────────────────────────────────────────────────────────

var (
	errLinkNotSent = errors.New("not sent")
	errLinkNoReply = errors.New("no reply")
)

type linkReply struct {
	status int
	header http.Header
	body   []byte
	// bodyBad: the answer's body was cut, too large or unreadable.
	bodyBad bool
}

// linkDo makes ONE request with the credential whoHolds resolved. The error is
// for a request with no reply: errLinkNotSent when nothing left (the connection
// never opened), errLinkNoReply when it may have arrived. No Go error text is
// kept: it names the address.
func (h *appFactoryHub) linkDo(ctx context.Context, w adminWho, method, path string, in any, limit int64) (linkReply, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return linkReply{}, errLinkNotSent
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.row.URL+path, body)
	if err != nil {
		return linkReply{}, errLinkNotSent
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-app")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	resp, err := h.client.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return linkReply{}, errLinkNotSent
		}
		return linkReply{}, errLinkNoReply
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	out := linkReply{status: resp.StatusCode, header: resp.Header, body: data}
	if rerr != nil || int64(len(data)) > limit {
		out.body, out.bodyBad = nil, true
	}
	return out, nil
}

// provedNoWrite is true only for a refusal the factory marked as made BEFORE its
// first requested change (api.PhaseHeader). A bare status proves nothing: the
// factory stores a link or removes a row and THEN records the event, and that
// can fail with a 403, 404, 408 or 409. 2xx, 3xx and 5xx are never refusals, and
// a factory or proxy that does not send the marker is not believed.
func (r linkReply) provedNoWrite() bool {
	if r.header.Get(api.PhaseHeader) != api.PhasePreMutation {
		return false
	}
	return r.status >= 400 && r.status < 500 || r.status == http.StatusNotImplemented
}

// linkRefusal is the wire error for a refusal. Its words are fixed: the
// factory's own prose is never passed on, since it may echo an id or a path.
// Only the documented expiry reason is read from a 401.
func linkRefusal(r linkReply, whatNotFound string) error {
	switch {
	case r.status == http.StatusUnauthorized:
		var detail struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(r.body, &detail)
		if detail.Reason == api.ReasonGitHubExpired {
			return appwire.Errorf(appwire.CodeGitHubExpired, "GitHub access to this factory has expired; reconnect GitHub")
		}
		return appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
	case r.status == http.StatusForbidden:
		return appwire.Errorf(appwire.CodeForbidden, "the factory does not let you do that")
	case r.status == http.StatusNotFound:
		return appwire.Errorf(appwire.CodeNotFound, "%s", whatNotFound)
	case r.status == http.StatusTooManyRequests:
		return appwire.Errorf(appwire.CodeBusy, "the factory is busy; try again")
	case r.status == http.StatusNotImplemented:
		return appwire.Errorf(appwire.CodeConflict, "this factory does not provide that action")
	case r.status == http.StatusBadRequest || r.status == http.StatusUnprocessableEntity:
		return appwire.Errorf(appwire.CodeBadParams, "the factory refused those settings")
	case r.status >= 300 && r.status < 400:
		return appwire.Errorf(appwire.CodeUnreachable, "the factory redirected the request, and redirects are not followed")
	}
	return appwire.Errorf(appwire.CodeEngine, "the factory refused")
}

// ── rows ───────────────────────────────────────────────────────────────────

// cleanLinkText is text from the factory made safe to show: no control or
// format characters, one line, at most max characters.
func cleanLinkText(s string, max int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if n == max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func linkStatusKnown(s string) bool {
	switch s {
	case "open", "used_up", "expired", "revoked":
		return true
	}
	return false
}

// humanLinkRow is true for a row that is a person's invitation. Anything else
// the list returns (the operator's claim, a script's key) is dropped, so it can
// never be shown or revoked from here. A status the app cannot word drops the
// row too.
func humanLinkRow(l api.AccessLink) bool {
	return !l.Operator && linkStatusKnown(l.Status) && appwire.CheckLinkID(l.ID)
}

func linkEntry(l api.AccessLink) appwire.FactoryLink {
	out := appwire.FactoryLink{
		ID: l.ID, Name: cleanLinkText(l.Name, appwire.LinkNameMax), Login: cleanLinkText(l.Login, 39),
		CreatedAt: l.CreatedAt, CreatedBy: cleanLinkText(l.CreatedBy, 80),
		MaxUses: l.MaxUses, Used: l.Used, Remaining: l.Remaining, Status: l.Status,
		UsedBy: []string{}, Counted: l.Counted,
	}
	if !l.ExpiresAt.IsZero() {
		t := l.ExpiresAt
		out.ExpiresAt = &t
	}
	if l.EndedAt != nil {
		t := *l.EndedAt
		out.EndedAt = &t
	}
	for _, who := range l.UsedBy {
		if len(out.UsedBy) == appwire.LinkMaxUses {
			break
		}
		if who = cleanLinkText(who, 80); who != "" {
			out.UsedBy = append(out.UsedBy, who)
		}
	}
	return out
}

// readLinks asks for the list once and answers the rows that are people's
// invitations, uncapped (the revoke guard reads all of them).
func (h *appFactoryHub) readLinks(ctx context.Context, w adminWho) ([]api.AccessLink, error) {
	r, err := h.linkDo(ctx, w, http.MethodGet, appFactoryLinksRoute, nil, appFactoryBody)
	switch {
	case errors.Is(err, errLinkNotSent), errors.Is(err, errLinkNoReply):
		return nil, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	case err != nil:
		return nil, err
	}
	if r.status < 200 || r.status >= 300 {
		return nil, linkRefusal(r, "this factory does not list invitations")
	}
	var doc struct {
		Links []api.AccessLink `json:"links"`
	}
	if r.bodyBad || json.Unmarshal(r.body, &doc) != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	rows := doc.Links[:0:0]
	for _, l := range doc.Links {
		if humanLinkRow(l) {
			rows = append(rows, l)
		}
	}
	return rows, nil
}

// ── list ───────────────────────────────────────────────────────────────────

func (b *appBackend) FactoryLinkList(ctx context.Context, p appwire.FactoryLinkParams) (appwire.FactoryLinkList, error) {
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryLinkList{}, err
	}
	defer release()
	who, err := b.linkWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryLinkList{}, err
	}
	rows, err := h.readLinks(ctx, who)
	if err != nil {
		// A failed or partial list does not clear the refusal.
		return appwire.FactoryLinkList{}, err
	}
	// The rows are the caller's. If the caller changed while they loaded, they
	// are shown to nobody, and the list is not counted as read.
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryLinkList{}, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if oi, oj := rows[i].Status == "open", rows[j].Status == "open"; oi != oj {
			return oi
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	if len(rows) > appwire.LinkMaxRows {
		rows = rows[:appwire.LinkMaxRows]
	}
	out := appwire.FactoryLinkList{Factory: p.Factory, Items: make([]appwire.FactoryLink, len(rows))}
	for i, l := range rows {
		out.Items[i] = linkEntry(l)
	}
	h.setMustList(p.Factory, false)
	out.EarlierUnconfirmed = h.earlierUnknown(p.Factory)
	h.touch(p.Factory)
	return out, nil
}

// ── create ─────────────────────────────────────────────────────────────────

// linkCreateBody is the request. A field the person did not choose is absent,
// so the factory's own defaults (one person, seven days) apply.
type linkCreateBody struct {
	MaxUses   int        `json:"max_uses,omitempty"`
	Login     string     `json:"login,omitempty"`
	Name      string     `json:"name,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// madeLink is a confirmed invitation read from the answer to a create.
type madeLink struct {
	entry  appwire.FactoryLink
	url    string
	secret string
}

// readMade reads the 201 body. false means the invitation was made but cannot
// be used here, which is an ambiguous outcome and not a refusal.
func readMade(r linkReply) (madeLink, bool) {
	if r.bodyBad {
		return madeLink{}, false
	}
	var doc struct {
		Link api.AccessLink `json:"link"`
	}
	if json.Unmarshal(r.body, &doc) != nil {
		return madeLink{}, false
	}
	l := doc.Link
	if !humanLinkRow(l) || l.Status != "open" || l.URL == "" || len(l.URL) > clipboard.MaxText {
		return madeLink{}, false
	}
	if strings.ContainsFunc(l.URL, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return madeLink{}, false
	}
	// The address inside is the factory's own public one and may differ from
	// the one this app dialled (a reverse proxy). It is not "fixed": it only has
	// to be an http(s) address with a host, which parseFactoryJoinLink checks.
	parsed, err := parseFactoryJoinLink(l.URL)
	if err != nil || !validLinkSecret(parsed.secret) {
		return madeLink{}, false
	}
	return madeLink{entry: linkEntry(l), url: l.URL, secret: parsed.secret}, true
}

// validLinkSecret is true for what tokens.Mint makes: 32 random bytes as 64
// lowercase hex digits. Anything else is not an invitation this factory made.
func validLinkSecret(s string) bool {
	if len(s) != 2*32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// copyOutcome writes text to the clipboard within a bound the copier cannot
// exceed, and names the outcome. Only a copier that said yes is "copied".
func (b *appBackend) copyOutcome(ctx context.Context, text string) string {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), appFactoryCopyBudget)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- clipboard.ErrUnconfirmed
			}
		}()
		done <- b.copier().Copy(cctx, text)
	}()
	select {
	case err := <-done:
		switch {
		case err == nil:
			return appwire.LinkCopied
		case errors.Is(err, clipboard.ErrFailed), errors.Is(err, clipboard.ErrUnavailable):
			return appwire.LinkNotCopied
		}
	case <-cctx.Done():
	}
	return appwire.LinkUnconfirmed
}

func newLinkRef() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (b *appBackend) FactoryLinkCreate(ctx context.Context, p appwire.FactoryLinkCreateParams) (appwire.FactoryLinkMade, error) {
	p, err := appwire.CheckLinkCreate(p)
	if err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	defer release()
	who, err := b.linkWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	// THE FACTORY MUST HAVE SAID it can sign an invitee in. Silence from an
	// older factory is not a yes, and a role or a mode is not evidence.
	if !who.me.GitHubSignInAvailable || !who.me.InviteAvailable {
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeConflict, "this factory has not said it can sign invited people in with GitHub")
	}
	// No clipboard, no invitation: one that cannot be handed over is not made.
	if !b.copier().Available() {
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeConflict, "this machine cannot copy for you")
	}
	if h.mustList(p.Factory) {
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeConflict, "check the list of invitations before making another")
	}
	now := h.now()
	body := linkCreateBody{Login: p.Login, Name: p.Name}
	until := now.Add(tokens.HumanLinkTTL)
	if p.MaxUses != nil {
		body.MaxUses = *p.MaxUses
	}
	if p.ExpiresAt != nil {
		if !now.Before(*p.ExpiresAt) {
			return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeBadParams, "the expiry must be in the future")
		}
		if p.ExpiresAt.After(until) {
			return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeBadParams, "an invitation lasts at most 7 days")
		}
		t := p.ExpiresAt.UTC()
		body.ExpiresAt, until = &t, t
	}

	ambiguous := func() (appwire.FactoryLinkMade, error) {
		h.setMustList(p.Factory, true)
		h.markUnknown(p.Factory, until.Add(appFactoryLinkSlack))
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeUncertain,
			"the factory did not confirm the invitation; it may have been made, so read the list of invitations before making another")
	}

	// THE CALLER IS CHECKED AFTER EVERY WAIT. The invitation, if it was made, is
	// real and stays: a changed caller gets the same uncertain answer an
	// unconfirmed create gets (nothing deleted, nothing made again, the list
	// owed), and none of what this call learned.
	changed := func() (appwire.FactoryLinkMade, error) {
		h.setMustList(p.Factory, true)
		h.markUnknown(p.Factory, until.Add(appFactoryLinkSlack))
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeUncertain,
			"the sign-in changed while the invitation was being made; it may exist, so read the list of invitations before making another")
	}
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryLinkMade{}, err
	}

	r, err := h.linkDo(ctx, who, http.MethodPost, appFactoryLinksRoute, body, appFactorySmall)
	switch {
	case errors.Is(err, errLinkNotSent):
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; no invitation was made")
	case err != nil:
		return ambiguous()
	}
	switch {
	case r.status == http.StatusCreated:
	case r.provedNoWrite():
		// The factory said it refused BEFORE its first change.
		return appwire.FactoryLinkMade{}, linkRefusal(r, "this factory does not make invitations")
	default:
		// Anything else may follow a stored invitation: the factory stores it
		// and then records the event, and a failure of that second step comes
		// back as a 403, 404, 408, 409 or 5xx. A success that is not the
		// documented one, and a redirect, are not trusted either.
		return ambiguous()
	}
	made, ok := readMade(r)
	if !ok {
		return ambiguous()
	}

	// FROM HERE THE SECRET IS IN THIS PROCESS. Tell the redactors first, before
	// anything can build an error or a log line.
	for _, s := range []string{made.url, made.secret} {
		redact.Register(s)
		trace.Secret(s)
	}
	// It must be the invitation that was asked for. A link with other limits is
	// real but is not what the person chose, so it is neither copied nor held.
	if !madeMatches(made.entry, body, now, until) {
		return ambiguous()
	}
	if !h.callerIs(p.Factory, who.token) {
		return changed()
	}
	outcome := b.copyOutcome(ctx, made.url)
	out := appwire.FactoryLinkMade{Factory: p.Factory, Entry: made.entry, Outcome: outcome}
	if outcome == appwire.LinkCopied {
		// The person has it; an older held copy is no longer wanted.
		if !h.callerIs(p.Factory, who.token) {
			// It reached the clipboard under a caller who has since gone. That
			// cannot be taken back, so nothing of it is reported or kept.
			return changed()
		}
		h.dropHeld(p.Factory)
		out.EarlierUnconfirmed = h.earlierUnknown(p.Factory)
		h.touch(p.Factory)
		return out, nil
	}
	if !b.holdMade(h, p.Factory, who, made, &out) && !h.callerIs(p.Factory, who.token) {
		return changed()
	}
	out.EarlierUnconfirmed = h.earlierUnknown(p.Factory)
	h.touch(p.Factory)
	return out, nil
}

// madeMatches is true when the invitation the factory answered with has the
// limits the request asked for, on the defaults the factory applies to what the
// request left out (one person, seven days). The values are compared with the
// request this app sent, never with what the answer says it was asked.
func madeMatches(e appwire.FactoryLink, asked linkCreateBody, now, until time.Time) bool {
	wantUses := asked.MaxUses
	if wantUses == 0 {
		wantUses = 1
	}
	if e.MaxUses != wantUses || e.Used != 0 || e.Status != "open" {
		return false
	}
	if e.Login != factory.NormalizeHandle(asked.Login) {
		return false
	}
	if e.Name != cleanLinkText(asked.Name, appwire.LinkNameMax) {
		return false
	}
	if e.ExpiresAt == nil || e.CreatedAt.IsZero() || !e.ExpiresAt.After(e.CreatedAt) || e.ExpiresAt.After(e.CreatedAt.Add(tokens.HumanLinkTTL)) {
		return false
	}
	// The factory reads its own clock, so the default expiry is held to a window and not
	// to a value: never past the longest the request allows, never already over,
	// and for an explicit expiry, the one that was asked for.
	if !e.ExpiresAt.After(now) || e.ExpiresAt.After(until.Add(appFactoryLinkSlack)) {
		return false
	}
	if asked.ExpiresAt != nil {
		if !e.ExpiresAt.Equal(*asked.ExpiresAt) {
			return false
		}
	}
	return true
}

// holdMade keeps the made invitation for a retry, behind a ref, for at most
// appwire.LinkHoldFor and never past the invitation's own expiry. A caller that
// has been replaced since the request began holds nothing: the copy would not be
// theirs.
func (b *appBackend) holdMade(h *appFactoryHub, id string, who adminWho, made madeLink, out *appwire.FactoryLinkMade) bool {
	until := h.now().Add(appwire.LinkHoldFor)
	if made.entry.ExpiresAt != nil && made.entry.ExpiresAt.Before(until) {
		until = *made.entry.ExpiresAt
	}
	if !until.After(h.now()) {
		return false
	}
	ref, err := newLinkRef()
	if err != nil {
		return false
	}
	// keepFor decides "still the same caller" under the lock that stores it.
	if !h.keepFor(id, who.token, &heldLink{ref: ref, linkID: made.entry.ID, entry: made.entry, secret: []byte(made.url), until: until, digest: bearerDigest(who.token)}) {
		return false
	}
	out.Ref = ref
	out.HoldsUntil = &until
	return true
}

// ── copy again ─────────────────────────────────────────────────────────────

// FactoryLinkCopy retries the copy of the invitation the engine holds. It makes
// no write to the factory and no new invitation.
func (b *appBackend) FactoryLinkCopy(ctx context.Context, p appwire.FactoryLinkCopyParams) (appwire.FactoryLinkMade, error) {
	h := b.factoryHub()
	if !appwire.CheckLinkRef(p.Ref) {
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeBadParams, "ref is not one this engine made")
	}
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	defer release()
	// A FRESH OPERATOR CHECK, with the credential held now.
	who, err := b.linkWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	gone := appwire.Errorf(appwire.CodeNotFound, "that invitation is no longer held; revoke it and make a new one")
	secret, entry, until, ok := h.held(p.Factory, p.Ref, who.token)
	if !ok {
		return appwire.FactoryLinkMade{}, gone
	}
	if !b.copier().Available() {
		return appwire.FactoryLinkMade{}, appwire.Errorf(appwire.CodeConflict, "this machine cannot copy for you")
	}
	// The invitation may have been ended some other way since; copying a dead
	// one would be a lie. The list is read once, and a failed read copies nothing.
	heldID := entry.ID
	secret = "" // not used again: the list may take a while, and the caller may change in it
	rows, err := h.readLinks(ctx, who)
	if err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	// THE SECRET IS TAKEN AGAIN HERE, after the wait, and only if this is still
	// the caller, the ref, the bearer and the invitation it was before it.
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	secret, again, until, ok := h.held(p.Factory, p.Ref, who.token)
	if !ok || again.ID != heldID {
		return appwire.FactoryLinkMade{}, gone
	}
	live := false
	for _, l := range rows {
		if l.ID == heldID {
			live, entry = l.Status == "open", linkEntry(l)
		}
	}
	if !live {
		h.forgetHeldLink(p.Factory, heldID)
		return appwire.FactoryLinkMade{}, gone
	}
	outcome := b.copyOutcome(ctx, secret)
	// A caller who was replaced while the copier ran is told nothing of it.
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryLinkMade{}, err
	}
	out := appwire.FactoryLinkMade{Factory: p.Factory, Entry: entry, Outcome: outcome}
	if outcome == appwire.LinkCopied {
		h.forgetHeldLink(p.Factory, entry.ID)
	} else if _, _, _, still := h.held(p.Factory, p.Ref, who.token); still {
		out.Ref, out.HoldsUntil = p.Ref, &until
	}
	out.EarlierUnconfirmed = h.earlierUnknown(p.Factory)
	h.touch(p.Factory)
	return out, nil
}

// ── revoke ─────────────────────────────────────────────────────────────────

func (b *appBackend) FactoryLinkRevoke(ctx context.Context, p appwire.FactoryLinkRevokeParams) (appwire.FactoryLinkRevoked, error) {
	if !appwire.CheckLinkID(p.ID) {
		return appwire.FactoryLinkRevoked{}, appwire.Errorf(appwire.CodeBadParams, "id is not one a factory makes")
	}
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return appwire.FactoryLinkRevoked{}, err
	}
	defer release()
	// Revoking needs no GitHub client: it must work on a factory that lost its.
	who, err := b.linkWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryLinkRevoked{}, err
	}
	// THE FACTORY WILL DELETE ANY ROW ID, the operator's own claim and a script's
	// key among them. So the list is read immediately before, and only an id
	// that is a person's invitation in it is sent.
	rows, err := h.readLinks(ctx, who)
	if err != nil {
		return appwire.FactoryLinkRevoked{}, err
	}
	found := false
	for _, l := range rows {
		found = found || l.ID == p.ID
	}
	if !found {
		return appwire.FactoryLinkRevoked{}, appwire.Errorf(appwire.CodeNotFound, "no such invitation")
	}
	// The list was read with who's credential; the delete is sent with it too,
	// so it is not sent once that is no longer the caller.
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryLinkRevoked{}, err
	}
	r, err := h.linkDo(ctx, who, http.MethodDelete, appFactoryLinksRoute+"/"+url.PathEscape(p.ID), nil, appFactorySmall)
	switch {
	case errors.Is(err, errLinkNotSent):
		return appwire.FactoryLinkRevoked{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was revoked")
	case err != nil:
		return appwire.FactoryLinkRevoked{}, uncertainRevoke()
	}
	switch {
	case completedAdminStatus(r.status):
	case r.provedNoWrite():
		return appwire.FactoryLinkRevoked{}, linkRefusal(r, "that invitation is already ended or is no longer there")
	default:
		// The factory removes the row before it records the event, so a refusal
		// that does not say it came first may follow a revoke that happened.
		return appwire.FactoryLinkRevoked{}, uncertainRevoke()
	}
	h.forgetHeldLink(p.Factory, p.ID)
	h.touch(p.Factory)
	return appwire.FactoryLinkRevoked{Factory: p.Factory, ID: p.ID, Revoked: true}, nil
}

func uncertainRevoke() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the revoke; it may have happened, so read the list of invitations")
}
