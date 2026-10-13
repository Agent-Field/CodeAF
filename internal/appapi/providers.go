package appapi

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"net/http"
	"sync"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/openrouterauth"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

type browserSignIn struct {
	sync.Mutex
	flow                 interface{ Cancel() }
	uri                  string
	state                string
	key                  string
	error                string
	revision             uint64
	credentialRevision   uint64
	credentialGeneration uint64
	done                 chan struct{}
}

func (s *Server) registerSignIn() {
	s.mux.HandleFunc("/app/v1/providers/{provider}/sign-in", s.profileHandler(s.idempotent(s.providerSignIn)))
	s.mux.HandleFunc("/app/v1/providers/{provider}/sign-in/claim", s.profileHandler(s.claimSignIn))
}

func (s *Server) providerSignIn(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "GET", "POST", "DELETE") {
		return
	}
	if s.options.Credentials == nil {
		fail(w, 404, "not_found", "this engine has no app credentials")
		return
	}
	provider := r.PathValue("provider")
	if provider != modelsource.DefaultID && provider != "chatgpt" {
		fail(w, 422, "bad_input", "this provider has no app browser sign-in")
		return
	}
	if r.Method == "POST" {
		var body struct{}
		if !decodeObject(w, r, &body) {
			return
		}
	}
	state := s.browserState(provider)
	state.Lock()
	defer state.Unlock()
	switch r.Method {
	case "GET":
		if state.state == "" {
			state.state = "canceled"
			if key, _ := s.options.Credentials.Key(provider); key != "" {
				state.state = "signed_in"
			}
		}
		write(w, 200, map[string]any{"state": state.state, "error": state.error})
		return
	case "DELETE":
		if state.flow != nil {
			state.flow.Cancel()
			state.flow = nil
		}
		state.revision++
		state.key = ""
		state.state = "canceled"
		state.error = ""
		s.keyState.Lock()
		s.options.Credentials.Set(provider, "")
		s.keyState.revisions[provider]++
		delete(s.keyState.models, provider)
		delete(s.keyState.listedAt, provider)
		s.keyState.Unlock()
		_, _ = s.events.Publish(EventKeysChanged, KeysChangedEvent{Keys: []string{provider}})
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if state.flow != nil {
		fail(w, 409, "conflict", "browser sign-in is already waiting")
		return
	}
	ctx := s.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	flow, wait, err := s.beginBrowser(ctx, provider)
	if err != nil {
		fail(w, 422, "bad_input", "could not start browser sign-in")
		return
	}
	s.keyState.Lock()
	revision := s.keyState.revisions[provider]
	generation := s.options.Credentials.KeyRevision(provider)
	s.keyState.Unlock()
	state.revision++
	attempt := state.revision
	state.flow = flow
	state.uri = flow.URL()
	state.state = "waiting"
	state.error = ""
	state.key = ""
	done := make(chan struct{})
	state.done = done
	guard.Go("app/provider-sign-in", func() {
		defer close(done)
		key, err := wait(ctx)
		flow.Cancel()
		state.Lock()
		defer state.Unlock()
		if attempt != state.revision {
			return
		}
		state.flow = nil
		if err != nil {
			state.state = "failed"
			state.error = redact.Secrets(string(trace.Scrub([]byte(err.Error()))))
			return
		}
		s.keyState.Lock()
		defer s.keyState.Unlock()
		if revision != s.keyState.revisions[provider] || generation != s.options.Credentials.KeyRevision(provider) {
			state.state = "canceled"
			return
		}
		s.options.Credentials.Set(provider, key)
		s.keyState.revisions[provider]++
		delete(s.keyState.models, provider)
		delete(s.keyState.listedAt, provider)
		s.keyState.lastProvider = provider
		state.key = key
		state.state = "signed_in"
		state.credentialRevision = s.keyState.revisions[provider]
		state.credentialGeneration = s.options.Credentials.KeyRevision(provider)
		_, _ = s.events.Publish(EventKeysChanged, map[string]any{"keys": []string{provider}})
	})
	write(w, 200, map[string]string{"verification_uri": flow.URL()})
}

// claimSignIn transfers the browser credential once to the authenticated native
// core, which puts it in Keychain or Secret Service. Ordinary GETs stay masked.
func (s *Server) claimSignIn(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "POST") {
		return
	}
	if s.options.Credentials == nil {
		fail(w, 404, "not_found", "this engine has no app credentials")
		return
	}
	provider := r.PathValue("provider")
	if provider != modelsource.DefaultID && provider != "chatgpt" {
		fail(w, 422, "bad_input", "this provider has no app browser sign-in")
		return
	}
	var body struct{}
	if !decodeObject(w, r, &body) {
		return
	}
	state := s.browserState(provider)
	state.Lock()
	defer state.Unlock()
	if state.state != "signed_in" || state.key == "" {
		fail(w, 409, "conflict", "there is no browser credential to save")
		return
	}
	s.keyState.Lock()
	defer s.keyState.Unlock()
	key, _ := s.options.Credentials.Key(provider)
	if key == "" || state.credentialRevision != s.keyState.revisions[provider] || state.credentialGeneration != s.options.Credentials.KeyRevision(provider) {
		state.key = ""
		fail(w, 409, "stale", "the provider connection changed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]string{"provider": provider, "key": key})
	state.key = ""
}

// Close releases browser callbacks on stdin EOF, parent death and shutdown.
func (s *Server) Close() {
	s.closeAccounts()
	s.closeFactories()
	s.closeInbox()
	s.signIn.Lock()
	defer s.signIn.Unlock()
	s.signIn.revision++
	if s.signIn.flow != nil {
		s.signIn.flow.Cancel()
		s.signIn.flow = nil
	}
	s.signIn.key = ""
	s.chatGPT.Lock()
	s.chatGPT.revision++
	if s.chatGPT.flow != nil {
		s.chatGPT.flow.Cancel()
		s.chatGPT.flow = nil
	}
	s.chatGPT.key = ""
	s.chatGPT.Unlock()
	if s.options.Agents != nil {
		s.options.Agents.Close()
	}
	s.events.Close()
	s.watchers.Wait()
}

func (s *Server) browserState(provider string) *browserSignIn {
	if provider == "chatgpt" {
		return &s.chatGPT
	}
	return &s.signIn
}

// The app reuses the engine's PKCE flow and transfers the complete refreshable bundle.
func (s *Server) beginBrowser(ctx context.Context, provider string) (interface {
	Cancel()
	URL() string
}, func(context.Context) (string, error), error) {
	if provider == "chatgpt" {
		flow, err := codexauth.Begin(ctx, s.options.ChatGPTAuth)
		if err != nil {
			return nil, nil, err
		}
		return flow, func(ctx context.Context) (string, error) {
			tokens, err := flow.Wait(ctx)
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(tokens)
			return string(raw), err
		}, nil
	}
	flow, err := openrouterauth.Begin(ctx, s.options.OpenRouterAuth)
	if err != nil {
		return nil, nil, err
	}
	return flow, flow.Wait, nil
}
