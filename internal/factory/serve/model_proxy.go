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
	// given a fresh one at its next attach. It is seven days because the saved
	// table lets a bearer outlive a restart, and a chat a person leaves for a
	// weekend should still be answering on Monday.
	mintedBearerTTL = 7 * 24 * time.Hour
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

// persistedRow is the part of a bearerRow that is saved: whose chat it is, whose
// person, and when it ends. It is keyed by the token's hash in the saved table
// and carries no token.
type persistedRow struct {
	Session string
	Person  string
	Expires time.Time
}

// bearerTable holds every live model bearer. It is saved through persist when
// one is set, so a restart of this server does not end the bearers the running
// chats already hold; with no persist (a server with no store) it is memory
// only, and a restart ends them all, which is safe: the next attach mints a
// new one.
type bearerTable struct {
	mu      sync.Mutex
	rows    map[string]*bearerRow // sha256 hex of the token
	current map[string]string     // chat id to the hash of its newest token
	// persist saves the live rows, replacing the saved table. It is called
	// under mu after every mint and every revoke, with hashes only and never a
	// token. Nil means memory only.
	persist func(map[string]persistedRow) error
}

// bearerHash is the key a token is kept under.
func bearerHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// bearerFingerprint is the short form of a token's hash that a chat's host
// reports for the token it holds and this server compares against its own: the
// first sixteen hex characters. It identifies a token without being one.
func bearerFingerprint(hash string) string {
	if len(hash) > 16 {
		return hash[:16]
	}
	return hash
}

// snapshotLocked is every row that should be saved: live ones only. With a
// non-zero now it also leaves out rows that have run out, so an expired row is
// not saved again. A zero now is for a revoke, which has no clock and leaves
// expiry to the next mint and to the load.
func (t *bearerTable) snapshotLocked(now time.Time) map[string]persistedRow {
	out := make(map[string]persistedRow, len(t.rows))
	for hash, row := range t.rows {
		if row.revoked || (!now.IsZero() && !now.Before(row.expires)) {
			continue
		}
		out[hash] = persistedRow{Session: row.session, Person: row.person, Expires: row.expires}
	}
	return out
}

// saveLocked saves after a revoke. It cannot fail the revoke, which has
// already happened in memory and is what stops the bearer from working, so a
// failure is logged for the operator. Every save writes the whole table, so
// the next mint or revoke puts the saved file right.
func (t *bearerTable) saveLocked() {
	if t.persist == nil {
		return
	}
	if err := t.persist(t.snapshotLocked(time.Time{})); err != nil {
		log.Printf("hosted chats: the saved model access could not be updated, so a restart could bring back an ended one until the next save: %v", err)
	}
}

// load fills the table from the saved rows when the server starts. Expired rows
// are dropped, and a chat keeps only the row that ends last, because a chat
// never holds two bearers and a second row is one that should have been ended.
// It answers how many saved rows it did not keep, so the caller can save the
// table without them.
func (t *bearerTable) load(saved map[string]persistedRow, now time.Time) (dropped int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows, t.current = map[string]*bearerRow{}, map[string]string{}
	for hash, row := range saved {
		if len(hash) != 64 || row.Session == "" || row.Person == "" || !now.Before(row.Expires) {
			dropped++
			continue
		}
		if best, ok := t.current[row.Session]; ok {
			kept := t.rows[best]
			dropped++
			if !row.Expires.After(kept.expires) && !(row.Expires.Equal(kept.expires) && hash < best) {
				continue
			}
			delete(t.rows, best)
		}
		t.rows[hash] = &bearerRow{session: row.Session, person: row.Person, expires: row.Expires}
		t.current[row.Session] = hash
	}
	return dropped
}

// save writes the table as it stands, for the start-up that dropped rows.
func (t *bearerTable) save(now time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.persist == nil {
		return nil
	}
	return t.persist(t.snapshotLocked(now))
}

// mint issues a bearer for one chat and ends the chat's previous one first,
// so a chat never holds two. Expired and revoked rows are dropped here, which
// is the only housekeeping the table needs: a dropped row answers "unknown",
// and unknown, expired and revoked are refused identically. It fails, and
// changes nothing, when the new table cannot be saved: a bearer that would not
// survive a restart is worse than a chat that is told to try again.
func (t *bearerTable) mint(session, person string, now time.Time) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mintLocked(session, person, now)
}

func (t *bearerTable) mintLocked(session, person string, now time.Time) (string, error) {
	if t.rows == nil {
		t.rows, t.current = map[string]*bearerRow{}, map[string]string{}
	}
	var raw [32]byte
	// crypto/rand does not return an error on a supported platform; it stops
	// the program rather than hand back bytes that are not random.
	_, _ = rand.Read(raw[:])
	token := mintedBearerPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	hash := bearerHash(token)
	oldHash, hadOld := t.current[session]
	var old *bearerRow
	wasRevoked := false
	if hadOld {
		if old = t.rows[oldHash]; old != nil {
			wasRevoked = old.revoked
		}
	}
	t.revokeLocked(session)
	t.rows[hash] = &bearerRow{session: session, person: person, expires: now.Add(mintedBearerTTL)}
	t.current[session] = hash
	if t.persist != nil {
		if err := t.persist(t.snapshotLocked(now)); err != nil {
			// Put it all back as it was: the previous bearer stands, and no
			// trace of the new one is left in memory.
			delete(t.rows, hash)
			delete(t.current, session)
			if hadOld {
				t.current[session] = oldHash
				if old != nil {
					old.revoked = wasRevoked
				}
			}
			return "", err
		}
	}
	for hash, row := range t.rows {
		if row.revoked || !now.Before(row.expires) {
			delete(t.rows, hash)
		}
	}
	return token, nil
}

// ensure answers whether the chat has a live bearer. It mints one only when
// none is live, and says so (minted), because a new bearer is the one moment
// the chat's host must be told. mine is false when a live bearer belongs to
// another person: nothing is minted then, because minting would end the
// owner's bearer, and a person who is not the owner must never be able to cost
// the owner their model access by naming their chat.
func (t *bearerTable) ensure(session, person string, now time.Time) (token string, minted, mine bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.current[session]; ok {
		if row := t.rows[hash]; row != nil && !row.revoked && now.Before(row.expires) {
			return "", false, row.person == person, nil
		}
	}
	token, err = t.mintLocked(session, person, now)
	return token, err == nil, true, err
}

// ensureCurrent is ensure for a chat whose host says which bearer it holds: it
// compares the host's fingerprint with the live bearer's and mints (ending the
// old one) unless they are equal, so a host that restarted and forgot, or an
// organization table that was lost, is put right before the chat is used and
// not after it fails. A live bearer that belongs to someone else is left alone,
// as in ensure.
func (t *bearerTable) ensureCurrent(session, person string, now time.Time, hostFingerprint string) (token string, minted bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.current[session]; ok {
		if row := t.rows[hash]; row != nil && !row.revoked && now.Before(row.expires) {
			if row.person != person || (hostFingerprint != "" && hostFingerprint == bearerFingerprint(hash)) {
				return "", false, nil
			}
		}
	}
	token, err = t.mintLocked(session, person, now)
	return token, err == nil, err
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

// revokeLocked ends a chat's current bearer in memory, and says whether it had
// one. Saving is the caller's, so that mint can save the ending and the new
// bearer in one write.
func (t *bearerTable) revokeLocked(session string) bool {
	hash, ok := t.current[session]
	if !ok {
		return false
	}
	if row := t.rows[hash]; row != nil {
		row.revoked = true
	}
	delete(t.current, session)
	return true
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
	if t.revokeLocked(session) {
		t.saveLocked()
	}
}

// revokeToken ends one specific token, but only while it is still the chat's
// current one: a push that failed must not end a newer bearer that another
// attach has minted since.
func (t *bearerTable) revokeToken(session, token string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.current[session]; ok && hash == bearerHash(token) {
		if t.revokeLocked(session) {
			t.saveLocked()
		}
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
	row, ok := hs.bearers.lookup(token, h.clock())
	if !ok {
		h.modelAccessEnded(w)
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
	// A chat can list models or complete a message. Account routes such as
	// /key, /credits, /generation and /auth stay private to the organization.
	switch {
	case r.Method == http.MethodPost && "/"+rest == hostedCompletionPath:
		if h.config.Hosted.CompleterFor != nil {
			h.serveHostedCompletion(w, r, token, row)
			return
		}
	case r.Method == http.MethodGet && (rest == "models" || strings.HasPrefix(rest, "models/") && len(rest) > len("models/")):
	default:
		modelRefuse(w, http.StatusNotFound, modelNoSuchPlace)
		return
	}
	if hs.upstream == nil {
		modelRefuse(w, http.StatusBadGateway, modelNotAnswer)
		return
	}
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
