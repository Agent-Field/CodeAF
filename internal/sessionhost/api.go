//go:build linux

package sessionhost

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type refusal int

const (
	noChat refusal = iota
	cannotIsolate
	noModel
	hostFull
	personFull
	badRequest
	badID
	alreadyExists
	noRoom
	createFailed
	readFailed
	stopFailed
	deleteFailed
	unavailable
	badWire
	noRoute
	badMethod
)

var refusalText = [...]string{
	noChat:        "There is no such chat.",
	cannotIsolate: "This server cannot keep your chat apart from other people's, so it did not start one.",
	noModel:       "This chat has no model access yet.",
	hostFull:      "Waiting for a free slot (%d of %d in use).",
	personFull:    "You already have %d chats running; stop one first.",
	badRequest:    "This chat needs a valid model address and access token.",
	badID:         "This chat has an invalid identifier.",
	alreadyExists: "This chat already exists.",
	noRoom:        "This server has no room for another chat right now.",
	createFailed:  "This chat could not be created.",
	readFailed:    "The server could not check which chats are running.",
	stopFailed:    "This chat could not be stopped.",
	deleteFailed:  "This chat could not be deleted.",
	unavailable:   "This chat is unavailable right now.",
	badWire:       "This request could not connect to the chat.",
	noRoute:       "This address does not exist.",
	badMethod:     "This request is unavailable at that address.",
}

const warningUID = "This server keeps your chat apart from other people's, but does not limit what its commands can reach."

// Create and credential updates accept an optional git address and its
// allowed origins. These values are held in memory with the model settings.
type credentials struct {
	ModelURL   string   `json:"model_url"`
	ModelToken string   `json:"model_token"`
	GitURL     string   `json:"git_url,omitempty"`
	GitHosts   []string `json:"git_hosts,omitempty"`
}

func (c credentials) valid() bool {
	u, err := url.Parse(c.ModelURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Hostname() == "" || u.User != nil || c.ModelToken == "" || strings.ContainsRune(c.ModelToken, 0) {
		return false
	}
	if c.GitURL != "" {
		git, ok := credentialGitURL(c.GitURL)
		if !ok || strings.HasSuffix(git.Path, "/") {
			return false
		}
	}
	if len(c.GitHosts) > 16 {
		return false
	}
	for _, origin := range c.GitHosts {
		if len(origin) > 200 || strings.Contains(origin, ",") {
			return false
		}
		git, ok := credentialGitURL(origin)
		if !ok || git.Path != "" {
			return false
		}
	}
	return true
}

func credentialGitURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	return u, err == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && !strings.Contains(raw, "#")
}

type createdChat struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Created time.Time `json:"created"`
	Warning string    `json:"warning,omitempty"`
}

type chatStatus struct {
	createdChat
	LastActive time.Time `json:"last_active"`
	// The fingerprint lets the organization verify its issued credential
	// is current without this host ever sending back the credential itself.
	ModelBearer string `json:"model_bearer"`
}

func (s *supervisor) status(row sessionRow, live processTable) chatStatus {
	state := "suspended"
	if !row.Missing && len(live[row.UID]) > 0 {
		state = "running"
	}
	fingerprint := ""
	if token := s.creds[row.ID].ModelToken; token != "" {
		sum := sha256.Sum256([]byte(token))
		fingerprint = hex.EncodeToString(sum[:8])
	}
	return chatStatus{
		createdChat: createdChat{ID: row.ID, State: state, Created: row.Created, Warning: s.warning()},
		LastActive:  row.LastActive,
		ModelBearer: fingerprint,
	}
}

func (s *supervisor) warning() string {
	if s.report.Mode == modeUID {
		return warningUID
	}
	return ""
}

type requestWriter struct {
	http.ResponseWriter
	status int
}

func (w *requestWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *requestWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func respondError(w http.ResponseWriter, status int, code refusal, args ...any) {
	sentence := refusalText[code]
	if len(args) > 0 {
		sentence = fmt.Sprintf(sentence, args...)
	}
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{sentence})
}

func decode(w http.ResponseWriter, r *http.Request, data any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	return dec.Decode(data) == nil && errors.Is(dec.Decode(new(any)), io.EOF)
}

func (s *supervisor) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	w := &requestWriter{ResponseWriter: out}
	start := time.Now()
	method := "OTHER"
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		method = r.Method
	}
	path := "/unknown"
	// An upgraded connection is logged at handshake, before its long-lived
	// body drains. Other requests use the same one-line log on return.
	logRequest := sync.OnceFunc(func() {
		s.log.Info("request", "method", method, "path", path, "status", w.status, "duration", time.Since(start))
	})
	defer logRequest()
	token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	provided := sha256.Sum256([]byte(token))
	person := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Codeaf-Person")))
	health := r.URL.Path == "/v1/health"
	if !bearer || subtle.ConstantTimeCompare(provided[:], s.secret[:]) != 1 ||
		(!health && !personHandle.MatchString(person)) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if health {
		path = "/v1/health"
		if r.Method != http.MethodGet {
			respondError(w, http.StatusMethodNotAllowed, badMethod)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			OK     bool   `json:"ok"`
			Mode   string `json:"mode"`
			Reason string `json:"reason"`
		}{s.report.OK, s.report.Mode, s.report.Reason})
		return
	}
	if r.URL.Path == "/v1/sessions" {
		path = "/v1/sessions"
		switch r.Method {
		case http.MethodGet:
			s.list(w, person)
		case http.MethodPost:
			s.create(w, r, person)
		default:
			respondError(w, http.StatusMethodNotAllowed, badMethod)
		}
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v1/sessions/")
	if !ok {
		respondError(w, http.StatusNotFound, noRoute)
		return
	}
	id, suffix, _ := strings.Cut(rest, "/")
	if !sessionID.MatchString(id) {
		respondError(w, http.StatusNotFound, noChat)
		return
	}
	path = "/v1/sessions/" + id
	switch suffix {
	case "wire", "stop", "credentials":
		path += "/" + suffix
	}
	if suffix == "wire" && r.Method == http.MethodGet {
		s.wire(w, r, person, id, logRequest)
		return
	}
	s.session(w, r, id, suffix, person)
}

func (s *supervisor) create(w http.ResponseWriter, r *http.Request, person string) {
	if !s.report.OK {
		respondError(w, http.StatusServiceUnavailable, cannotIsolate)
		return
	}
	var body struct {
		ID string `json:"id"`
		credentials
	}
	if !decode(w, r, &body) || !body.credentials.valid() {
		respondError(w, http.StatusBadRequest, badRequest)
		return
	}
	if !sessionID.MatchString(body.ID) {
		respondError(w, http.StatusBadRequest, badID)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		respondError(w, http.StatusServiceUnavailable, unavailable)
		return
	}
	if _, exists := s.registry.sessions[body.ID]; exists {
		respondError(w, http.StatusConflict, alreadyExists)
		return
	}
	live, err := s.processes.snapshot()
	if err != nil {
		respondError(w, http.StatusInternalServerError, readFailed)
		return
	}
	now := s.now().UTC()
	uid := s.registry.allocate(s.cfg, live, now)
	if uid == 0 {
		respondError(w, http.StatusServiceUnavailable, noRoom)
		return
	}
	row := sessionRow{ID: body.ID, Person: person, UID: uid, Created: now, LastActive: now}
	made, err := s.buildLayout(row)
	if err == nil {
		err = s.registry.put(row)
	}
	if err != nil {
		if made {
			s.rollbackLayout(row)
		}
		respondError(w, http.StatusInternalServerError, createFailed)
		return
	}
	s.creds[row.ID] = body.credentials
	writeJSON(w, http.StatusCreated, s.status(row, live).createdChat)
}

func (s *supervisor) list(w http.ResponseWriter, person string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var owned []sessionRow
	for _, row := range s.registry.sessions {
		if row.Person == person {
			owned = append(owned, row)
		}
	}
	statuses := make([]chatStatus, 0, len(owned))
	if len(owned) > 0 {
		live, err := s.processes.snapshot()
		if err != nil {
			respondError(w, http.StatusInternalServerError, readFailed)
			return
		}
		for _, row := range owned {
			statuses = append(statuses, s.status(row, live))
		}
		slices.SortFunc(statuses, func(a, b chatStatus) int {
			if cmp := b.LastActive.Compare(a.LastActive); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Sessions []chatStatus `json:"sessions"`
	}{statuses})
}

func (s *supervisor) session(w http.ResponseWriter, r *http.Request, id, suffix, person string) {
	// Read an update body before taking the state lock. Ownership still
	// determines the response before any validation error is exposed.
	var creds credentials
	valid := false
	if suffix == "credentials" && r.Method == http.MethodPut {
		valid = decode(w, r, &creds) && creds.valid()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, exists := s.registry.sessions[id]
	if !exists || row.Person != person {
		respondError(w, http.StatusNotFound, noChat)
		return
	}
	switch {
	case suffix == "" && r.Method == http.MethodGet:
		live, err := s.processes.snapshot()
		if err != nil {
			respondError(w, http.StatusInternalServerError, readFailed)
			return
		}
		writeJSON(w, http.StatusOK, s.status(row, live))
	case suffix == "" && r.Method == http.MethodDelete:
		s.stop(w, row, true)
	case suffix == "stop" && r.Method == http.MethodPost:
		s.stop(w, row, false)
	case suffix == "credentials" && r.Method == http.MethodPut:
		if !valid {
			respondError(w, http.StatusBadRequest, badRequest)
			return
		}
		s.creds[id] = creds
		w.WriteHeader(http.StatusNoContent)
	default:
		respondError(w, http.StatusMethodNotAllowed, badMethod)
	}
}

func (s *supervisor) stop(w http.ResponseWriter, row sessionRow, deleting bool) {
	delete(s.creds, row.ID)
	wireErr := s.stopWires(row.ID)
	if err := s.processes.KillAll(row.UID, stopGrace); err != nil || wireErr != nil {
		if deleting {
			s.retainDoomed(row)
		}
		respondError(w, http.StatusInternalServerError, stopFailed)
		return
	}
	row.LastActive = s.now().UTC()
	if deleting {
		if !s.deleteSession(row) {
			respondError(w, http.StatusInternalServerError, deleteFailed)
			return
		}
	} else {
		folders, err := s.folders(row)
		row.Missing = errors.Is(err, os.ErrNotExist)
		if err == nil {
			err = s.launcher.reap(row.UID, folders, reapTmp)
		}
		if (err != nil && !row.Missing) || s.registry.put(row) != nil {
			respondError(w, http.StatusInternalServerError, stopFailed)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *supervisor) deleteSession(row sessionRow) bool {
	// Persist the cleanup intent before removing files so a restart after
	// partial removal cannot reopen the chat.
	row.Doomed = true
	if s.registry.put(row) != nil {
		s.registry.sessions[row.ID] = row
		return false
	}
	if s.removeLayout(row) != nil || s.registry.remove(row.ID, s.now().UTC()) != nil {
		return false
	}
	s.removeEmptyPersonFolder(row)
	return true
}

// removeEmptyPersonFolder takes away the folder that held a person's chats once the
// last of them is gone, so removing someone from the organization leaves no trace of
// their name on the volume (found on a production run: an empty p_<hash> folder stayed
// behind). THE EMPTINESS TEST IS THE KERNEL'S: rmdir refuses a folder with anything in
// it, so a person's other chats, and anything this code does not know about, stay put.
// This runs under the supervisor's lock, as does every create, so a chat being made
// for the same person cannot find its parent folder gone halfway through.
func (s *supervisor) removeEmptyPersonFolder(row sessionRow) {
	folders, err := layoutFor(s.cfg.Data, row.Person, row.ID)
	if err != nil {
		return
	}
	err = unix.Rmdir(filepath.Dir(folders.dir))
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
		s.log.Warn("could not remove a person's empty chat folder", "error", err)
	}
}
