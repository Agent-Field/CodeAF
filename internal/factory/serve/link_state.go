package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// Access flows are keyed by the link and initiating caller, so Cancel cannot end another person's sign-in.
type accessLinkFlow struct {
	Device  api.Device
	Expires time.Time
	Session string
}
type deviceReply struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *deviceReply) Header() http.Header    { return w.header }
func (w *deviceReply) WriteHeader(status int) { w.status = status }
func (w *deviceReply) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(data)
}
func captureDevice(action http.HandlerFunc, r *http.Request, body any) *deviceReply {
	data, _ := json.Marshal(body)
	r = r.Clone(r.Context())
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	reply := &deviceReply{header: http.Header{}}
	action(reply, r)
	return reply
}
func accessSecret(link, address string) (string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", errors.New("use an organization invite link")
	}
	origin := u.Query().Get("url")
	if u.Scheme != "codeaf" || u.Host != "factory" || u.Path != "/join" || strings.TrimRight(origin, "/") != strings.TrimRight(address, "/") {
		return "", errors.New("this access link belongs to another factory")
	}
	fragment, err := url.ParseQuery(u.EscapedFragment())
	if err != nil || fragment.Get("token") == "" {
		return "", errors.New("the access link has no token")
	}
	return fragment.Get("token"), nil
}
func (h *Handler) factoryAccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	// The answer can carry a session credential; nothing may keep a copy.
	noStore(w)
	link := r.URL.Query().Get("link")
	verb := ""
	var body api.FactoryAccessRequest
	if r.Method == "POST" {
		// Every verb's body fits in 8 KiB (a link is at most 4 KiB), and is
		// read strictly, so a field this server does not know is refused
		// rather than ignored.
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, passwordBodyMost))
		if err != nil {
			h.fail(w, http.StatusRequestEntityTooLarge, "the request is larger than 8 KiB")
			return
		}
		if !h.strictJSON(w, data, &body) {
			return
		}
		link, verb = body.Link, body.Verb
		switch verb {
		case "sign_in", "cancel", "check":
			if body.Username != "" || body.Password != "" || body.Name != "" {
				h.fail(w, 422, "only sign_up and reset take a username or a password")
				return
			}
		case "sign_up", "reset":
			// A password travels only as JSON, only over https or from this
			// machine.
			if media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type")); e != nil || media != "application/json" {
				h.fail(w, http.StatusUnsupportedMediaType, "send this as application/json")
				return
			}
			if !h.passwordTransport(w, r) {
				return
			}
		default:
			h.fail(w, 422, "choose sign_in, sign_up, reset, cancel or check")
			return
		}
	}
	// A CHECK READS AND POLLS, NEVER STARTS: GET (legacy, secret in the URL) and
	// POST check are the same read, so neither can mint a cookie, start OAuth or reset a flow.
	check := r.Method == http.MethodGet || verb == "check"
	secret, err := accessSecret(link, h.factory.Address)
	if err != nil {
		h.fail(w, 422, err.Error())
		return
	}
	out := api.FactoryAccess{State: "no_access", Link: link, Factory: api.AccessFactory{Name: h.factory.Name, Host: h.factory.Address, Repos: []string{}, SignIn: h.signInMethods()}, Detail: "Link for one person"}
	hash := sha256.Sum256([]byte(h.factory.Address))
	out.Factory.ID = "factory_" + hex.EncodeToString(hash[:8])
	claim, err := tokens.LoadOperator(h.config.TokensPath)
	if h.failed(w, err) {
		return
	}
	out.Factory.Operator = claim.Login
	rows, err := tokens.Load(h.config.TokensPath)
	if h.failed(w, err) {
		return
	}
	var tok *tokens.Token
	for i := range rows {
		if rows[i].Matches(secret) && (rows[i].AccessLink || rows[i].Operator) {
			tok = &rows[i]
			break
		}
	}
	if tok == nil {
		// An operator's password reset link is kept beside the accounts, not
		// among the access rows: its secret must never pass as a bearer.
		if accounts, e := h.loadAccounts(); e == nil && h.passwordsOn() {
			if reset, ok := accounts.FindReset(secret); ok && (verb == "" || verb == "check" || verb == "reset") {
				h.resetAccess(w, r, body, reset, out)
				return
			}
		}
		out.Message = "This access link is not valid."
		h.write(w, 200, out)
		return
	}
	if verb == "reset" {
		h.fail(w, 422, "this is not a password reset link")
		return
	}
	if tok.InvitedBy != "" {
		out.Factory.Operator = tok.InvitedBy
	}
	// A link made for one GitHub login is used by signing in with GitHub as
	// that person; it never offers a password or a passkey.
	if tok.InvitedLogin != "" {
		out.Factory.SignIn = slices.DeleteFunc(out.Factory.SignIn, func(m string) bool { return m == signInPassword || m == signInPasskey })
	}
	// Only a link that matched earns the host's face; the secret is the proof.
	if known, ok := h.signedInPeople([]string{out.Factory.Operator})[factory.NormalizeHandle(out.Factory.Operator)]; ok {
		out.Factory.OperatorName, out.Factory.OperatorAvatarURL = known.Name, known.AvatarURL
	}
	now := h.now()
	if tok.Human() {
		out.MaxUses, out.Used, out.Remaining = tok.Limit(), tok.Used(), tok.Remaining()
		if out.MaxUses > 1 {
			out.Detail = fmt.Sprintf("Link for up to %d people · %d left", out.MaxUses, out.Remaining)
		}
	}
	if !tok.Expires.IsZero() {
		out.Detail += " · expires " + tok.Expires.Format("2 Jan")
		expires := tok.Expires
		out.ExpiresAt = &expires
	}
	// The card is the first of these that applies: revoked, expired, used up,
	// then open. "Can't reach the factory" is decided by the app before it
	// asks; already in and no repo visible follow from the caller below.
	switch {
	case tok.Revoked:
		out.State = "revoked"
	case !tok.Expires.IsZero() && !now.Before(tok.Expires):
		out.State = "expired"
	case tok.Human() && tok.Remaining() == 0, !tok.Human() && !tok.Joined.IsZero():
		out.State = "used_up"
	default:
		out.State = "ready"
	}
	if tok.Human() && out.State != "ready" {
		ended := tok.EndedAt(now)
		out.EndedAt = &ended
	}
	// Signing up is its own door: it never starts GitHub, reads no caller
	// cookie, and a person already signed in may still make a new account.
	if verb == "sign_up" {
		if out.State != "ready" {
			h.write(w, 200, out)
			return
		}
		h.signUp(w, r, body, *tok, out)
		return
	}
	caller := ""
	if cookie, e := r.Cookie("factory_join"); e == nil {
		caller = cookie.Value
	}
	if caller == "" && verb == "sign_in" {
		caller, err = secretValue()
		if h.failed(w, err) {
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "factory_join", Value: caller, Path: "/v1/factory-access", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureRequest(r), MaxAge: int(accessSessionTTL.Seconds())})
	}
	keyHash := sha256.Sum256([]byte(secret + "\x00" + caller))
	key := hex.EncodeToString(keyHash[:])
	h.accessMu.Lock()
	defer h.accessMu.Unlock()
	if h.accessFlows == nil {
		h.accessFlows = map[string]accessLinkFlow{}
	}
	for k, pending := range h.accessFlows {
		if !h.now().Before(pending.Expires) {
			delete(h.accessFlows, k)
		}
	}
	flow, pending := h.accessFlows[key]
	// A joined response belongs only to the caller that completed this flow, never merely to possession of a used link.
	if pending && flow.Session != "" && out.State != "revoked" && out.State != "expired" {
		joined := r.Clone(r.Context())
		joined.Header = r.Header.Clone()
		joined.Header.Set("Authorization", "Bearer "+flow.Session)
		if session, ok := h.credential(joined); ok && session.profile != nil {
			out.State = "joined"
			out.AccessToken = flow.Session
			h.accessRepos(&out, joined)
			h.write(w, 200, out)
			return
		}
	}
	// Someone who is already in is told so and nothing changes: the link stays
	// unused, so it still works for the next person.
	if session, ok := h.credential(r); ok && session.profile != nil && out.State == "ready" {
		out.State = "already_joined"
		h.accessRepos(&out, r)
		h.write(w, 200, out)
		return
	}
	if out.State != "ready" {
		h.write(w, 200, out)
		return
	}
	auth := r.Clone(r.Context())
	auth.Header = r.Header.Clone()
	auth.Header.Del("Cookie")
	auth.Header.Set("Authorization", "Bearer "+secret)
	auth.URL = new(url.URL)
	*auth.URL = *r.URL
	auth.URL.Path = "/v1/auth/device"
	if h.seam.Load != nil {
		if snap, e := h.seam.Load(); e == nil {
			for _, repo := range snap.Repos {
				out.Factory.Repos = append(out.Factory.Repos, repo.Name)
			}
		}
	}
	if verb == "cancel" {
		if pending {
			h.auth.mu.Lock()
			delete(h.auth.pending, flow.Device.DeviceCode)
			h.auth.mu.Unlock()
			delete(h.accessFlows, key)
		}
		h.write(w, 200, out)
		return
	}
	if verb == "sign_in" && !pending {
		if len(h.accessFlows) >= deviceFlowLimit {
			h.fail(w, 429, "too many pending GitHub sign-ins")
			return
		}
		reply := captureDevice(h.startDevice, auth, struct{}{})
		if reply.status != 200 {
			for name, values := range reply.header {
				w.Header()[name] = values
			}
			w.WriteHeader(reply.status)
			_, _ = w.Write(reply.body.Bytes())
			return
		}
		if json.Unmarshal(reply.body.Bytes(), &flow.Device) != nil {
			h.fail(w, 502, "GitHub did not start sign-in")
			return
		}
		flow.Expires = h.now().Add(time.Duration(flow.Device.ExpiresIn) * time.Second)
		h.accessFlows[key] = flow
		pending = true
	} else if check && pending {
		auth.URL.Path = "/v1/auth/device/poll"
		receipt := &joinReceipt{}
		auth = auth.WithContext(context.WithValue(auth.Context(), joinReceiptKey{}, receipt))
		reply := captureDevice(h.pollDevice, auth, api.DevicePoll{DeviceCode: flow.Device.DeviceCode})
		if reply.status == 200 {
			var signed api.SignedIn
			if json.Unmarshal(reply.body.Bytes(), &signed) != nil {
				h.fail(w, 502, "GitHub did not complete sign-in")
				return
			}
			flow.Session = signed.AccessToken
			flow.Expires = h.now().Add(time.Duration(signed.ExpiresIn) * time.Second)
			h.accessFlows[key] = flow
			joined := r.Clone(r.Context())
			joined.Header = r.Header.Clone()
			joined.Header.Set("Authorization", "Bearer "+flow.Session)
			for _, cookie := range reply.header.Values("Set-Cookie") {
				w.Header().Add("Set-Cookie", cookie)
			}
			out.State = "joined"
			out.AccessToken = flow.Session
			if receipt.repos != nil {
				out.Factory.Repos = receipt.repos
			} else {
				h.accessRepos(&out, joined)
			}
			h.write(w, 200, out)
			return
		}
		if reply.status != 202 {
			delete(h.accessFlows, key)
			out.State = "no_access"
			out.Message = "GitHub did not complete sign-in. Try again."
			if reply.status == http.StatusForbidden {
				// The sign-in worked but the link refused them; no use was spent.
				var refused struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal(reply.body.Bytes(), &refused)
				switch refused.Error {
				case linkRevokedWords:
					out.State = "revoked"
				case linkExpiredWords:
					out.State = "expired"
				case linkUsedUpWords:
					out.State = "used_up"
				case linkNoRepoWords:
					out.Message = linkNoRepoWords
				case githubNameHeldWords:
					out.Message = githubNameHeldWords
				}
			}
			h.write(w, 200, out)
			return
		}
	}
	if pending {
		out.State = "waiting"
		out.SignInURL = flow.Device.VerificationURI
		out.UserCode = flow.Device.UserCode
	}
	h.write(w, 200, out)
}
func secretValue() (string, error) { return secret() }
func (h *Handler) accessRepos(out *api.FactoryAccess, r *http.Request) {
	out.Factory.Repos = []string{}
	if h.seam.Load == nil {
		return
	}
	snap, err := h.seam.Load()
	if err != nil {
		return
	}
	hasRepos := len(snap.Repos) > 0
	if h.config.Store != nil {
		watched, _ := h.config.Store.Repos()
		hasRepos = hasRepos || len(watched) > 0
	}
	snap, reason, _ := h.forViewer(r, snap)
	// A password person has no GitHub to see a repository with; that is a
	// thing they cannot do, never a reason to turn them away.
	if session, ok := h.credential(r); ok && session.profile != nil && session.profile.Kind == tokens.KindPassword {
		reason = ""
	}
	if reason != "" && hasRepos {
		out.State = "no_access"
		out.AccessToken = ""
		out.Message = "Your GitHub cannot see this factory's repositories."
		return
	}
	for _, repo := range snap.Repos {
		out.Factory.Repos = append(out.Factory.Repos, repo.Name)
	}
}
