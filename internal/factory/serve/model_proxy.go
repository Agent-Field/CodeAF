package serve

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"path"
	"strings"
	"sync"
	"time"
)

// This file is the MODEL PROXY SEAM of a hosted chat. A chat that runs on the
// organization's own machine is given no model key of its own: it is given the
// address of this server's /v1/model and a short-lived bearer minted for that
// one chat, and this server forwards what it sends to the one upstream the
// operator configured. P0 adds no metering and holds no real key, because the
// point of the seam is that they can arrive here later without touching the
// chat; what it already guarantees is that a bearer dies with its chat.

const (
	// mintedBearerPrefix is how a chat's model bearer announces itself, so a
	// leaked one is recognisable in a paste and refused by any scanner.
	mintedBearerPrefix = "cmt_"
	// mintedBearerTTL is how long one bearer lives. A chat that outlasts it is
	// given a fresh one at its next attach.
	mintedBearerTTL = 12 * time.Hour
	// modelRequestCap bounds the body one model call may carry, so a chat
	// cannot make this server buffer or relay an unbounded upload.
	modelRequestCap = 16 << 20
	// modelPrefix is the address the chats' model calls arrive on.
	modelPrefix = "/v1/model/"

	// The sentences below are the ONLY words the model proxy ever says, and
	// none of them carries a detail a chat could use to tell a bearer that was
	// never issued from one that was revoked.
	modelEnded       = "This chat's model access has ended."
	modelNotAnswer   = "The model is not answering."
	modelMethod      = "The model accepts GET and POST only."
	modelTooLarge    = "That request is too large for the model."
	modelNoSuchPlace = "There is nothing at that address."
)

// bearerRow is what the table remembers about one minted bearer. The token
// itself is never kept: only its hash is the key, so a dump of this process
// holds nothing that can be presented.
type bearerRow struct {
	session string
	person  string
	expires time.Time
	revoked bool
}

// bearerTable holds every live model bearer, in memory only. A restart of the
// server ends them all, which is safe: the next attach mints a new one.
type bearerTable struct {
	mu      sync.Mutex
	rows    map[string]*bearerRow // sha256 hex of the token
	current map[string]string     // chat id to the hash of its newest token
}

// bearerHash is the key a token is kept under.
func bearerHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// mint issues a bearer for one chat and ends the chat's previous one first,
// so a chat never holds two. Expired and revoked rows are dropped here, which
// is the only housekeeping the table needs: a dropped row answers "unknown",
// and unknown, expired and revoked are refused identically.
func (t *bearerTable) mint(session, person string, now time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mintLocked(session, person, now)
}

func (t *bearerTable) mintLocked(session, person string, now time.Time) string {
	if t.rows == nil {
		t.rows, t.current = map[string]*bearerRow{}, map[string]string{}
	}
	t.revokeLocked(session)
	for hash, row := range t.rows {
		if row.revoked || !now.Before(row.expires) {
			delete(t.rows, hash)
		}
	}
	var raw [32]byte
	// crypto/rand does not return an error on a supported platform; it stops
	// the program rather than hand back bytes that are not random.
	_, _ = rand.Read(raw[:])
	token := mintedBearerPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	hash := bearerHash(token)
	t.rows[hash] = &bearerRow{session: session, person: person, expires: now.Add(mintedBearerTTL)}
	t.current[session] = hash
	return token
}

// ensure answers whether the chat has a live bearer. It mints one only when
// none is live, and says so (minted), because a new bearer is the one moment
// the chat's host must be told. mine is false when a live bearer belongs to
// another person: nothing is minted then, because minting would end the
// owner's bearer, and a person who is not the owner must never be able to cost
// the owner their model access by naming their chat.
func (t *bearerTable) ensure(session, person string, now time.Time) (token string, minted, mine bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.current[session]; ok {
		if row := t.rows[hash]; row != nil && !row.revoked && now.Before(row.expires) {
			return "", false, row.person == person
		}
	}
	return t.mintLocked(session, person, now), true, true
}

// lookup fails closed: an unknown, expired or revoked token gets no row.
func (t *bearerTable) lookup(token string, now time.Time) (bearerRow, bool) {
	if !strings.HasPrefix(token, mintedBearerPrefix) {
		return bearerRow{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	row := t.rows[bearerHash(token)]
	if row == nil || row.revoked || !now.Before(row.expires) {
		return bearerRow{}, false
	}
	return *row, true
}

func (t *bearerTable) revokeLocked(session string) {
	if hash, ok := t.current[session]; ok {
		if row := t.rows[hash]; row != nil {
			row.revoked = true
		}
		delete(t.current, session)
	}
}

// revokeFor ends a chat's bearer on behalf of a person. A bearer that belongs
// to someone else is left alone, so naming another person's chat in a stop or
// a delete (which the chat's host answers as "no such chat") also costs its
// owner nothing here.
func (t *bearerTable) revokeFor(session, person string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.current[session]; ok {
		if row := t.rows[hash]; row != nil && row.person != person {
			return
		}
	}
	t.revokeLocked(session)
}

// revokeToken ends one specific token, but only while it is still the chat's
// current one: a push that failed must not end a newer bearer that another
// attach has minted since.
func (t *bearerTable) revokeToken(session, token string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.current[session]; ok && hash == bearerHash(token) {
		t.revokeLocked(session)
	}
}

// modelPath says a request is a chat's model call. It is false whenever the
// hosted setting is off, so a server that never set it answers /v1/model/ as
// it answers any other unknown path (contract C8).
func (h *Handler) modelPath(r *http.Request) bool {
	hc := h.config.Hosted
	return hc != nil && hc.state != nil && strings.HasPrefix(r.URL.Path, modelPrefix)
}

// modelRefuse writes one of the proxy's sentences with nothing else: no
// request id, no factory facts, because the caller is a chat that proved
// nothing about itself and learns only the sentence.
func modelRefuse(w http.ResponseWriter, status int, words string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":"`+words+`"}`+"\n")
}

// modelProxy forwards one chat's model call to the configured upstream. THE
// MINTED BEARER IS THE ONLY GATE ON THIS PATH: it runs before the organization
// sign-in on purpose, because a chat has no sign-in, and an organization
// credential is never accepted here, so a person's own sign-in cannot be used
// to spend the model through a chat's address.
func (h *Handler) modelProxy(w http.ResponseWriter, r *http.Request) {
	hs := h.config.Hosted.state
	token := bearer(r)
	if _, ok := hs.bearers.lookup(token, h.clock()); !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="model"`)
		modelRefuse(w, http.StatusUnauthorized, modelEnded)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		modelRefuse(w, http.StatusMethodNotAllowed, modelMethod)
		return
	}
	// This runs before the mux's own path cleaning, so a path that climbs out
	// of the upstream's base address is refused here: the upstream's base
	// holds the model's routes, and nothing above it is a chat's to reach.
	rest := strings.TrimPrefix(r.URL.Path, modelPrefix)
	if strings.ContainsAny(rest, "\\\x00") || rest != "" && path.Clean("/"+rest) != "/"+strings.TrimSuffix(rest, "/") {
		modelRefuse(w, http.StatusNotFound, modelNoSuchPlace)
		return
	}
	if r.ContentLength > modelRequestCap {
		modelRefuse(w, http.StatusRequestEntityTooLarge, modelTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, modelRequestCap)
	(&httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Path = "/" + rest
			pr.Out.URL.RawPath = ""
			// SetURL points the request at the upstream and its Host at the
			// upstream's too, joining the upstream's base path to ours.
			pr.SetURL(hs.upstream)
			hostedStripIdentity(pr.Out.Header)
			if hs.upstreamKey != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+hs.upstreamKey)
			}
		},
		// A model's reply streams as server-sent events; a negative interval
		// flushes after every write, so the first token is not held back.
		FlushInterval: -1,
		Transport:     hs.modelTransport,
		ErrorLog:      log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			modelRefuse(w, http.StatusBadGateway, modelNotAnswer)
		},
	}).ServeHTTP(w, r)
}

// hostedStripIdentity removes from an outbound request everything that names
// who is asking or how they arrived: the caller's credentials, every
// forwarding header, and every header this server's own protocol uses for
// identity. Nothing of a person may reach a host this server forwards to.
func hostedStripIdentity(header http.Header) {
	for key := range header {
		lower := strings.ToLower(key)
		switch {
		case strings.HasPrefix(lower, "x-codeaf-"), strings.HasPrefix(lower, "x-forwarded-"):
			delete(header, key)
		case lower == "authorization", lower == "proxy-authorization", lower == "cookie",
			lower == "forwarded", lower == "x-real-ip", lower == "true-client-ip":
			delete(header, key)
		}
	}
}
