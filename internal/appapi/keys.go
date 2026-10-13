package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

type KeyStatus struct {
	Provider string   `json:"provider"`
	Present  bool     `json:"present"`
	Last4    string   `json:"last4"`
	Models   []string `json:"models"`
}

type KeyCheckResult struct {
	OK        bool     `json:"ok"`
	Models    []string `json:"models"`
	Error     string   `json:"error,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

// keyState serializes replacement and removal so a late check cannot undo a
// person's later disconnect. The map contains only the provider's model ids.
type keyState struct {
	sync.Mutex
	models       map[string][]string
	revisions    map[string]uint64
	listedAt     map[string]time.Time
	lastProvider string
}

func (s *Server) registerKeys() {
	if s.options.Credentials == nil {
		return
	}
	s.keyState.models = make(map[string][]string)
	s.keyState.revisions = make(map[string]uint64)
	s.keyState.listedAt = make(map[string]time.Time)
	s.mux.HandleFunc("/app/v1/keys", s.keys)
	s.mux.HandleFunc("/app/v1/keys/{provider}", s.providerKey)
}

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		fail(w, 405, "not_allowed", "this route does not accept that method")
		return
	}
	s.keyState.Lock()
	defer s.keyState.Unlock()
	keys, _ := s.options.Credentials.Snapshot()
	ids := map[string]bool{modelsource.DefaultID: true, "chatgpt": true, "github": true, "anthropic": true}
	for _, source := range modelsource.Vendored() {
		if source.ID != "codex" && !modelsource.IsCustomID(source.ID) {
			ids[source.ID] = true
		}
	}
	for id := range keys {
		ids[id] = true
	}
	var sorted []string
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	rows := make([]KeyStatus, 0, len(sorted))
	for _, id := range sorted {
		key := keys[id]
		last := ""
		if id != "chatgpt" && len([]rune(key)) > 4 {
			last = string([]rune(key)[len([]rune(key))-4:])
		}
		rows = append(rows, KeyStatus{id, key != "", last, append([]string{}, s.keyState.models[id]...)})
	}
	write(w, 200, rows)
}

func (s *Server) providerKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "PUT, DELETE")
		fail(w, 405, "not_allowed", "this route does not accept that method")
		return
	}
	id := r.PathValue("provider")
	if id == "" || id != strings.ToLower(strings.TrimSpace(id)) || strings.ContainsAny(id, "/\\") {
		fail(w, 422, "bad_input", "unknown key provider")
		return
	}
	endpoint, ok := s.keyEndpoint(id)
	if !ok {
		fail(w, 422, "bad_input", "unknown key provider")
		return
	}
	s.keyState.Lock()
	defer s.keyState.Unlock()
	if r.Method == http.MethodDelete {
		s.options.Credentials.Set(id, "")
		s.keyState.revisions[id]++
		_, _ = s.events.Publish(EventKeysChanged, map[string]any{"keys": []string{id}})
		delete(s.keyState.models, id)
		delete(s.keyState.listedAt, id)
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	var body *struct {
		Key string `json:"key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body == nil || strings.TrimSpace(body.Key) == "" || strings.ContainsAny(body.Key, "\r\n\x00") || decoder.Decode(new(any)) != io.EOF {
		fail(w, 422, "bad_input", "key needs a JSON object with a nonempty key")
		return
	}
	key := strings.TrimSpace(body.Key)
	trace.Secret(key)
	redact.Register(key)
	ctx, cancel := context.WithTimeout(r.Context(), modelsource.ProbeTimeout)
	defer cancel()
	models, err := checkKey(ctx, s.options.KeyHTTPClient, endpoint, id, key)
	if err != nil {
		write(w, 422, KeyCheckResult{Models: []string{}, Error: redact.Secrets(string(trace.Scrub([]byte(err.Error())))), Reason: "bad_input", RequestID: fmt.Sprintf("%08x", requestSequence.Add(1))})
		return
	}
	s.options.Credentials.Set(id, key)
	s.keyState.models[id] = models
	s.keyState.listedAt[id] = time.Now()
	s.keyState.lastProvider = id
	s.keyState.revisions[id]++
	_, _ = s.events.Publish(EventKeysChanged, map[string]any{"keys": []string{id}})
	write(w, 200, KeyCheckResult{OK: true, Models: models})
}

func (s *Server) keyEndpoint(id string) (string, bool) {
	switch id {
	case modelsource.DefaultID:
		return strings.TrimRight(config.DefaultBaseURL, "/") + "/models/user", true
	case "github":
		return "https://api.github.com/user", true
	case "anthropic":
		return "https://api.anthropic.com/v1/models", true
	}
	for _, service := range config.ResolveSources(s.options.ProfileDir, "", config.DefaultBaseURL).All() {
		if service.Source.ID == id && id != "codex" && service.Address != "" {
			return strings.TrimRight(service.Address, "/") + "/models", true
		}
	}
	for _, source := range modelsource.Vendored() {
		if source.ID != id || id == "codex" || modelsource.IsCustomID(id) {
			continue
		}
		address := source.Address
		if address == "" && len(source.Regions) > 0 {
			address = source.Regions[0].Address
		}
		if address != "" {
			return strings.TrimRight(address, "/") + "/models", true
		}
	}
	return "", false
}

// checkKey makes one authenticated listing call. OpenRouter's public listing
// cannot prove a credential; its user-filtered listing is the authenticated door.
func checkKey(ctx context.Context, client *http.Client, endpoint, id, key string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("key check could not reach the provider")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	if id == "anthropic" {
		request.Header.Del("Authorization")
		request.Header.Set("x-api-key", key)
		request.Header.Set("anthropic-version", "2023-06-01")
	}
	if client == nil {
		client = http.DefaultClient
	}
	// A redirect must never turn this one call into a credential sent elsewhere.
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := local.Do(request)
	if err != nil {
		return nil, errors.New("No answer · couldn't reach the provider in 10s · Try again")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, errors.New("key check received an unreadable provider answer")
	}
	if response.StatusCode != http.StatusOK {
		var body struct {
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(raw, &body)
		if len(body.Error) > 0 {
			var nested struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(body.Error, &nested) == nil {
				body.Message = nested.Message
			} else {
				_ = json.Unmarshal(body.Error, &body.Message)
			}
		}
		if body.Message == "" {
			body.Message = http.StatusText(response.StatusCode)
		}
		return nil, fmt.Errorf("Key didn't work · the provider said: %s (%d)", body.Message, response.StatusCode)
	}
	if id == "github" {
		var person struct {
			Login string `json:"login"`
		}
		if json.Unmarshal(raw, &person) != nil || person.Login == "" {
			return nil, errors.New("key check received an unreadable provider answer")
		}
		return []string{}, nil
	}
	var listing struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &listing) != nil || listing.Data == nil {
		return nil, errors.New("key check received an unreadable provider answer")
	}
	models := []string{}
	for _, model := range *listing.Data {
		if model.ID == "" {
			return nil, errors.New("key check received an unreadable provider answer")
		}
		models = append(models, redact.Secrets(string(trace.ScrubRegistered([]byte(model.ID)))))
	}
	return models, nil
}
