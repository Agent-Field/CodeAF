package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// This file is where a signed-in person keeps a model key or a GitHub
// credential of their own on the organization's server, and where the server
// finds it again when that person's chat needs it. The secrets themselves are
// sealed by the store (store/person_keys.go); what lives here is the server key
// that seals them, the routes a person uses, and the one question the model and
// git paths ask: whose key should this call go out with?
//
// A PERSON'S OWN KEY IS NEVER REPLACED BY THE ORGANIZATION'S. When a key exists
// and cannot be opened, the answer is an error and the call is refused. Quietly
// falling back would spend the organization's money, or act as the
// organization, for a person who chose to use their own.

// personKeys is the sealed store of people's own keys.
type personKeys = store.PersonKeys

const (
	// The sentences a person may read from the key routes. None of them names
	// the machinery behind the storage.
	hostedKeysNoSuchKey  = "There is no such key to keep."
	hostedKeysCannotKeep = "This server cannot keep personal keys safe yet, so it did not store yours. Its operator can set that up."
	hostedKeysTooLarge   = "That is too large to be a key."
	hostedKeysBadRequest = "Send the key you want to keep."
	hostedKeysNotStored  = "Your key could not be stored just now."

	// hostedKeysBodyCap bounds a request to the key routes. A key is at most
	// 512 characters, so four kilobytes is generous and no more.
	hostedKeysBodyCap = 4 << 10
)

// The operator's own sentences, which are allowed to name settings. They are
// returned from [HostedConfig.Check] and printed by the command that starts the
// server.
const (
	serverKeyForm    = "CODEAF_SERVER_KEY must be 64 hexadecimal characters or a base64 text of 32 bytes."
	serverKeyMissing = "Keys that people stored are on this server, but CODEAF_SERVER_KEY is not set, so they cannot be opened. Set the key they were stored with."
	serverKeyWrong   = "CODEAF_SERVER_KEY is not the key this server's stored keys were sealed with, so they cannot be opened. Set the key they were stored with; the stored keys were left untouched."
	gitHostsForm     = "CODEAF_HOSTED_GIT_HOSTS must be a comma-separated list of http or https origins such as https://github.com, with no path."
)

var (
	errServerKeyForm = errors.New(serverKeyForm)
	// errNoServerKey and errWrongServerKey are what hostedKeyFor answers for a
	// key that exists and cannot be opened. They are for the caller's log and
	// its refusal sentence; neither is shown to a person as it stands.
	errNoServerKey    = errors.New(serverKeyMissing)
	errWrongServerKey = errors.New(serverKeyWrong)
)

// parseServerKey reads CODEAF_SERVER_KEY: 64 hexadecimal characters, or base64
// (standard or URL alphabet, padded or not) of exactly 32 bytes. The error
// names the setting and never repeats the value.
func parseServerKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 64 {
		if key, err := hex.DecodeString(raw); err == nil {
			return key, nil
		}
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if key, err := encoding.DecodeString(raw); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errServerKeyForm
}

// normalizeGitOrigin is one entry of the git hosts setting as a chat and the git
// routes will compare it: scheme and host (and port) in lower case, no trailing
// slash. Anything with a path, a query, a fragment or sign-in details is not an
// origin.
func normalizeGitOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New(gitHostsForm)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// hostedGitOrigins is the origins a chat's git may reach: the operator's list,
// normalized and without repeats, or github.com alone when they set none. An
// entry that is not an origin is left out (Check refuses it at start), so a
// bad entry can only shrink what a chat may reach and never widen it.
func hostedGitOrigins(cfg *HostedConfig) []string {
	if cfg == nil || len(cfg.GitHosts) == 0 {
		return []string{"https://github.com"}
	}
	out := make([]string, 0, len(cfg.GitHosts))
	for _, raw := range cfg.GitHosts {
		origin, err := normalizeGitOrigin(raw)
		if err != nil {
			continue
		}
		seen := false
		for _, have := range out {
			seen = seen || have == origin
		}
		if !seen {
			out = append(out, origin)
		}
	}
	return out
}

// Check is the start-up gate for what this file adds, and it never changes a
// file. It refuses, in the operator's words, a server key that does not parse,
// keys that people stored with no server key to open them, a server key that is
// not the one the stored keys were sealed with, and a git host that is not an
// origin. st may be nil, which leaves only the settings to check.
func (hc *HostedConfig) Check(st *store.Store) error {
	if hc == nil {
		return nil
	}
	for _, raw := range hc.GitHosts {
		if _, err := normalizeGitOrigin(raw); err != nil {
			return err
		}
	}
	var kek []byte
	if strings.TrimSpace(hc.ServerKey) != "" {
		var err error
		if kek, err = parseServerKey(hc.ServerKey); err != nil {
			return err
		}
		defer wipe(kek)
	}
	if st == nil {
		return nil
	}
	keys, err := store.OpenPersonKeys(st, kek)
	switch {
	case errors.Is(err, store.ErrWrongServerKey):
		return errWrongServerKey
	case err != nil:
		return err
	}
	if kek == nil && keys.AnyPeople() {
		return errNoServerKey
	}
	return nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// hostedOpenKeys opens the sealed store for a handler. It never fails the
// handler: with no server key, a wrong one or an unreadable file it keeps a
// locked store and remembers why, which makes saving a key refuse with a
// sentence and makes a key that exists refuse a call instead of being replaced
// (the start-up check has already stopped a real server in those cases).
func (h *Handler) hostedOpenKeys(hs *hostedState, serverKey string) {
	st := h.config.Store
	if st == nil {
		return
	}
	var kek []byte
	if strings.TrimSpace(serverKey) != "" {
		var err error
		if kek, err = parseServerKey(serverKey); err != nil {
			hs.keysErr = err
		}
	}
	defer wipe(kek)
	keys, err := store.OpenPersonKeys(st, kek)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrWrongServerKey):
		hs.keysErr = errWrongServerKey
		keys, _ = store.OpenPersonKeys(st, nil)
	default:
		hs.keysErr = err
		keys, _ = store.OpenPersonKeys(st, nil)
	}
	if keys != nil && keys.Locked() && hs.keysErr == nil {
		hs.keysErr = errNoServerKey
	}
	hs.keys = keys
}

// hostedKeyFor is the key a call for person goes out with: the person's own key
// for provider ("openrouter" or "github") when they have stored one, and
// otherwise "", false. err is non-nil when an own key EXISTS but cannot be
// opened, or when the store cannot even say whether it does: the caller must
// refuse, never fall back to the organization's key. Keys are looked up by the
// exact normalized handle, so no other person's key can be returned.
func (h *Handler) hostedKeyFor(person, provider string) (key string, own bool, err error) {
	hc := h.config.Hosted
	if hc == nil || hc.state == nil || h.config.Store == nil {
		return "", false, nil
	}
	hs := hc.state
	if hs.keys == nil {
		// A file the store could not even open: whether this person has a key
		// is unknown, and unknown is not "no".
		return "", false, hs.keysErr
	}
	person = factory.NormalizeHandle(person)
	has, err := hs.keys.Has(person, provider)
	if err != nil {
		return "", false, err
	}
	if !has {
		return "", false, nil
	}
	if hs.keysErr != nil {
		return "", false, hs.keysErr
	}
	secret, found, err := hs.keys.Get(person, provider)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}
	return secret, true, nil
}

// keepSignInGitHub keeps the GitHub credential a person's sign-in carries, so a
// chat of theirs can reach their repositories without them storing anything. It
// never replaces a key the person stored themselves, never logs the value, and
// ignores a failure to store: the wire opens either way.
func (h *Handler) keepSignInGitHub(r *http.Request, person string) {
	hs := h.config.Hosted.state
	if hs.keys == nil || hs.keys.Locked() || hs.keysErr != nil {
		return
	}
	token, viaSignIn := h.personGitHubToken(r)
	if !viaSignIn || len(token) < 8 || len(token) > 512 || strings.IndexFunc(token, func(c rune) bool { return c < 0x21 || c > 0x7e }) >= 0 {
		return
	}
	for _, row := range hs.keys.List(person) {
		if row.Provider == "github" && row.Source == store.PersonKeyOwn {
			return
		}
	}
	if held, ok, err := hs.keys.Get(person, "github"); err == nil && ok && held == token {
		return
	}
	_ = hs.keys.Put(person, "github", token, store.PersonKeySignIn, h.clock())
}

// hostedKeyRow is one stored key as the person's screen may see it.
type hostedKeyRow struct {
	Provider string `json:"provider"`
	Last4    string `json:"last4"`
	At       string `json:"at"`
	Source   string `json:"source"`
}

// hostedKeyList answers the caller's own rows and nobody else's.
func (h *Handler) hostedKeyList(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok || !h.hostedKeysBodyOK(w, r) {
		return
	}
	rows := []hostedKeyRow{}
	if keys := h.config.Hosted.state.keys; keys != nil {
		for _, row := range keys.List(person) {
			rows = append(rows, hostedKeyRow{Provider: row.Provider, Last4: row.Last4, At: row.At.UTC().Format(time.RFC3339), Source: row.Source})
		}
	}
	h.write(w, http.StatusOK, map[string]any{"keys": rows})
}

// hostedKeysBodyOK refuses a request that announces more than a key could need.
func (h *Handler) hostedKeysBodyOK(w http.ResponseWriter, r *http.Request) bool {
	if r.ContentLength > hostedKeysBodyCap {
		h.fail(w, http.StatusRequestEntityTooLarge, hostedKeysTooLarge)
		return false
	}
	return true
}

// hostedKeyPut stores the caller's own key for one provider.
func (h *Handler) hostedKeyPut(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok || !h.hostedKeysBodyOK(w, r) {
		return
	}
	provider := r.PathValue("provider")
	if !store.KnownPersonKeyProvider(provider) {
		h.fail(w, http.StatusNotFound, hostedKeysNoSuchKey)
		return
	}
	hs := h.config.Hosted.state
	if hs.keys == nil || hs.keys.Locked() || hs.keysErr != nil {
		h.fail(w, http.StatusConflict, hostedKeysCannotKeep)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, hostedKeysBodyCap))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(w, http.StatusRequestEntityTooLarge, hostedKeysTooLarge)
		} else {
			h.fail(w, http.StatusUnprocessableEntity, hostedKeysBadRequest)
		}
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.More() {
		h.fail(w, http.StatusUnprocessableEntity, hostedKeysBadRequest)
		return
	}
	least := 12
	if provider == "github" {
		least = 8
	}
	key := strings.TrimSpace(body.Key)
	if len(key) < least || len(key) > 512 || strings.IndexFunc(key, func(c rune) bool { return c < 0x21 || c > 0x7e }) >= 0 {
		h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("That does not look like a valid key: one word of %d to 512 characters.", least))
		return
	}
	if err := hs.keys.Put(person, provider, key, store.PersonKeyOwn, h.clock()); err != nil {
		log.Printf("hosted chats: a personal key could not be stored: %v", err)
		h.fail(w, http.StatusInternalServerError, hostedKeysNotStored)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// hostedKeyDelete forgets the caller's own key for one provider, and answers
// the same whether or not there was one.
func (h *Handler) hostedKeyDelete(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok || !h.hostedKeysBodyOK(w, r) {
		return
	}
	provider := r.PathValue("provider")
	if !store.KnownPersonKeyProvider(provider) {
		h.fail(w, http.StatusNotFound, hostedKeysNoSuchKey)
		return
	}
	hs := h.config.Hosted.state
	if hs.keys == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if has, _ := hs.keys.Has(person, provider); !has {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if hs.keys.Locked() || hs.keysErr != nil {
		h.fail(w, http.StatusConflict, hostedKeysCannotKeep)
		return
	}
	if _, err := hs.keys.Delete(person, provider); err != nil {
		log.Printf("hosted chats: a personal key could not be removed: %v", err)
		h.fail(w, http.StatusInternalServerError, hostedKeysNotStored)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
