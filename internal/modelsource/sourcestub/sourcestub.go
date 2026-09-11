// Package sourcestub provides a small OpenAI-shaped service for source-routing
// tests. It models a direct service, not a router with serving lanes.
package sourcestub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/modelsource"
)

// Request is one request observed by the service.
type Request struct {
	Method string
	Path   string
	Host   string
	Bearer string
	Body   []byte
}

// Server is a switchable fake direct model service.
type Server struct {
	mu               sync.Mutex
	http             *httptest.Server
	models           []string
	requests         []Request
	status           int
	body             string
	completionStatus int
	completionBody   string
	hang             time.Duration
	listingless      bool
}

// New starts a service that publishes models and answers completions.
func New(models ...string) *Server {
	server := &Server{models: append([]string(nil), models...)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", server.serveModels)
	mux.HandleFunc("POST /v1"+modelsource.ChatCompletionsPath, server.serveCompletion)
	server.http = httptest.NewServer(mux)
	return server
}

// URL is the OpenAI-compatible base URL.
func (s *Server) URL() string { return s.http.URL + "/v1" }

// Close shuts down the service.
func (s *Server) Close() { s.http.Close() }

// Refuse makes both routes answer with the chosen status and body.
func (s *Server) Refuse(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body, s.hang = status, body, 0
}

// RefuseCompletion leaves the listing healthy and refuses only generation.
// It models an authenticated key whose account cannot fund a billable call.
func (s *Server) RefuseCompletion(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completionStatus, s.completionBody = status, body
}

// Hang delays both routes long enough for a caller's timeout to fire.
func (s *Server) Hang(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hang, s.status, s.body = delay, 0, ""
}

// Healthy clears a staged refusal or delay.
func (s *Server) Healthy() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hang, s.status, s.body = 0, 0, ""
	s.completionStatus, s.completionBody = 0, ""
}

// Listingless makes only GET /models answer 404. Completions remain healthy,
// which models vendors whose key can be proved only by a one-token call.
func (s *Server) Listingless() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listingless = true
}

// Requests returns a copy of every request in arrival order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) stage(request *http.Request) (int, string, time.Duration) {
	body, _ := io.ReadAll(request.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, Request{
		Method: request.Method, Path: request.URL.Path, Host: request.Host,
		Bearer: request.Header.Get("Authorization"), Body: body,
	})
	if request.Method == http.MethodPost && s.completionStatus != 0 {
		return s.completionStatus, s.completionBody, s.hang
	}
	return s.status, s.body, s.hang
}

func (s *Server) staged(writer http.ResponseWriter, request *http.Request) bool {
	status, body, delay := s.stage(request)
	if delay > 0 {
		time.Sleep(delay)
	}
	if status == 0 {
		return false
	}
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, body)
	return true
}

func (s *Server) serveModels(writer http.ResponseWriter, request *http.Request) {
	if s.staged(writer, request) {
		return
	}
	s.mu.Lock()
	models := append([]string(nil), s.models...)
	listingless := s.listingless
	s.mu.Unlock()
	if listingless {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	rows := make([]map[string]string, 0, len(models))
	for _, model := range models {
		rows = append(rows, map[string]string{"id": model})
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"data": rows})
}

func (s *Server) serveCompletion(writer http.ResponseWriter, request *http.Request) {
	if s.staged(writer, request) {
		return
	}
	requests := s.Requests()
	var envelope struct {
		Stream bool `json:"stream"`
	}
	if len(requests) > 0 {
		_ = json.Unmarshal(requests[len(requests)-1].Body, &envelope)
	}
	if envelope.Stream {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"model\":\"stub/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(writer, `{"model":"stub/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
}
