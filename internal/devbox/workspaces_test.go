package devbox

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Contract 2: workspace resolution uses only the public query, with no live account.
func TestDevboxWorkspaceLookupAndSelection(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer fixture-token" || string(body) != `{"query":"{ me { workspaces { id name } } }"}` {
			t.Fatalf("request: %s %s", r.Header, body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"me":{"workspaces":[{"id":"one","name":"Personal"},{"id":"two","name":"Team"}]}}}`))}, nil
	})}
	lookup := func(ctx context.Context, token string) ([]Workspace, error) {
		return lookupWorkspaces(ctx, client, token)
	}
	if _, err := resolveWorkspace(context.Background(), "fixture-token", "", lookup); err == nil || !strings.Contains(err.Error(), "Personal, Team") || !strings.Contains(err.Error(), "--workspace") {
		t.Fatalf("ambiguity: %v", err)
	}
	if id, err := resolveWorkspace(context.Background(), "fixture-token", "Team", lookup); err != nil || id != "two" {
		t.Fatalf("named: %s %v", id, err)
	}
	id := "11111111-2222-3333-4444-555555555555"
	if got, err := resolveWorkspace(context.Background(), "fixture-token", id, func(context.Context, string) ([]Workspace, error) { t.Fatal("id looked up"); return nil, nil }); err != nil || got != id {
		t.Fatalf("id: %s %v", got, err)
	}
}

// Contract 2: a workspace-scoped token may create without looking up me.
func TestDevboxWorkspaceTokenCanOmitWorkspace(t *testing.T) {
	for _, response := range []string{`{"errors":[{"message":"Not Authorized","path":["me"]}]}`, `{"data":{"me":null}}`} {
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
		})}
		got, err := lookupWorkspaces(context.Background(), client, "fixture-token")
		if err != nil || len(got) != 0 {
			t.Fatalf("workspace token: %v %v", got, err)
		}
	}
}
