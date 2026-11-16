package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryJobContextSetter = (*appBackend)(nil)

// FactoryJobContextSet carries one replacement through the existing hub write.
func (b *appBackend) FactoryJobContextSet(ctx context.Context, p appwire.FactoryJobContextSetParams) (appwire.FactoryJobActed, error) {
	attachments := make([]factory.JobContext, len(p.Context))
	for i, c := range p.Context {
		attachments[i] = factory.JobContext{Title: c.Title, Markdown: c.Markdown, Source: c.Source}
	}
	return b.jobWrite(ctx, p.Factory, appwire.JobActContextSet, p.Job, "context", api.JobContextRequest{Context: attachments, Expect: p.Expect})
}

// factoryJobContextFailure distinguishes the mux's missing route from a real
// Factory JSON refusal. Every other job write keeps its existing mapping.
func factoryJobContextFailure(req *http.Request, reply appFactoryReply) error {
	if req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/v1/jobs/") && strings.HasSuffix(req.URL.Path, "/context") &&
		(reply.status == http.StatusNotFound || reply.status == http.StatusMethodNotAllowed) && missingRouteBody(reply.body) {
		return tooOldRefusal()
	}
	return factoryCreateFailure(req, reply)
}

// missingRouteBody says whether a 404 or 405 body is a route the Factory does
// not have: its mux's [api.EndpointNotFound], or not the Factory's JSON at all.
// Any other JSON error is the Factory refusing the request itself.
func missingRouteBody(body []byte) bool {
	var refusal struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &refusal) != nil || refusal.Error == api.EndpointNotFound
}
