package appapi

import (
	"context"
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
	flow     *openrouterauth.Flow
	uri      string
	state    string
	key      string
	error    string
	revision uint64
	done     chan struct{}
}

func (s *Server) registerSignIn() {
	s.mux.HandleFunc("/app/v1/providers/{provider}/sign-in", s.profileHandler(s.idempotent(s.providerSignIn)))
	s.mux.HandleFunc("/app/v1/providers/{provider}/sign-in/claim", s.profileHandler(s.idempotent(s.claimSignIn)))
}

func (s *Server) providerSignIn(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "GET", "POST", "DELETE") {
		return
	}
	if s.options.Credentials == nil {
		fail(w, 404, "not_found", "this engine has no app credentials")
		return
	}
	if r.PathValue("provider") != modelsource.DefaultID {
		fail(w, 422, "bad_input", "this provider has no app browser sign-in")
		return
	}
	if r.Method == "POST" {
		var body struct{}
		if !decodeObject(w, r, &body) {
			return
		}
	}
	s.signIn.Lock()
	defer s.signIn.Unlock()
	switch r.Method {
	case "GET":
		write(w, 200, map[string]any{"state": s.signIn.state, "error": s.signIn.error})
		return
	case "DELETE":
		if s.signIn.flow != nil {
			s.signIn.flow.Cancel()
			s.signIn.flow = nil
		}
		s.signIn.revision++
		s.signIn.key = ""
		s.signIn.state = "canceled"
		s.signIn.error = ""
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if s.signIn.flow != nil {
		fail(w, 409, "conflict", "browser sign-in is already waiting")
		return
	}
	ctx := s.options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	flow, err := openrouterauth.Begin(ctx, s.options.OpenRouterAuth)
	if err != nil {
		fail(w, 422, "bad_input", "could not start browser sign-in")
		return
	}
	s.keyState.Lock()
	revision := s.keyState.revisions[modelsource.DefaultID]
	s.keyState.Unlock()
	s.signIn.revision++
	attempt := s.signIn.revision
	s.signIn.flow = flow
	s.signIn.uri = flow.URL()
	s.signIn.state = "waiting"
	s.signIn.error = ""
	s.signIn.key = ""
	done := make(chan struct{})
	s.signIn.done = done
	guard.Go("app/provider-sign-in", func() {
		defer close(done)
		key, err := flow.Wait(ctx)
		s.signIn.Lock()
		defer s.signIn.Unlock()
		if attempt != s.signIn.revision {
			return
		}
		s.signIn.flow = nil
		if err != nil {
			s.signIn.state = "failed"
			s.signIn.error = redact.Secrets(string(trace.Scrub([]byte(err.Error()))))
			return
		}
		s.keyState.Lock()
		defer s.keyState.Unlock()
		if revision != s.keyState.revisions[modelsource.DefaultID] {
			s.signIn.state = "canceled"
			return
		}
		s.options.Credentials.Set(modelsource.DefaultID, key)
		s.keyState.revisions[modelsource.DefaultID]++
		delete(s.keyState.models, modelsource.DefaultID)
		delete(s.keyState.listedAt, modelsource.DefaultID)
		s.keyState.lastProvider = modelsource.DefaultID
		s.signIn.key = key
		s.signIn.state = "signed_in"
		_, _ = s.events.Publish(EventKeysChanged, map[string]any{"keys": []string{modelsource.DefaultID}})
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
	if r.PathValue("provider") != modelsource.DefaultID {
		fail(w, 422, "bad_input", "this provider has no app browser sign-in")
		return
	}
	var body struct{}
	if !decodeObject(w, r, &body) {
		return
	}
	s.signIn.Lock()
	defer s.signIn.Unlock()
	if s.signIn.state != "signed_in" || s.signIn.key == "" {
		fail(w, 409, "conflict", "there is no browser credential to save")
		return
	}
	s.keyState.Lock()
	defer s.keyState.Unlock()
	key, _ := s.options.Credentials.Key(modelsource.DefaultID)
	if key != s.signIn.key {
		s.signIn.key = ""
		fail(w, 409, "stale", "the provider connection changed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]string{"provider": modelsource.DefaultID, "key": s.signIn.key})
	s.signIn.key = ""
}

// Close releases browser callbacks on stdin EOF, parent death and shutdown.
func (s *Server) Close() {
	s.signIn.Lock()
	defer s.signIn.Unlock()
	s.signIn.revision++
	if s.signIn.flow != nil {
		s.signIn.flow.Cancel()
		s.signIn.flow = nil
	}
	s.signIn.key = ""
	if s.options.Agents != nil {
		s.options.Agents.Close()
	}
	s.events.Close()
}
