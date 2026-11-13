package devbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// R3.4: polling checks every in-progress status and sends credentials only in the header.
func TestDevboxR34DeploymentsSettleAndKeepTokenInHeader(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.RawQuery != "" || r.Method != http.MethodPost {
			t.Error("wrong authorization or method")
		}
		var body struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Contains(body.Query, "fixture-token") || strings.Contains(fmt.Sprint(body.Variables), "fixture-token") {
			t.Error("token in body")
		}
		if body.Variables["s"] != "service" || body.Variables["e"] != "environment" || !strings.Contains(body.Query, "deployments(first: 10") || !strings.Contains(body.Query, "id status createdAt") {
			t.Errorf("query: %+v", body)
		}
		statuses := []string{"WAITING", "QUEUED", "INITIALIZING", "BUILDING", "DEPLOYING", "REMOVING"}
		n := int(calls.Add(1))
		status := "SUCCESS"
		if n <= len(statuses) {
			status = statuses[n-1]
		}
		fmt.Fprintf(w, `{"data":{"deployments":{"edges":[{"node":{"id":"deployment","status":%q,"createdAt":"2026-10-30T12:00:00Z"}},{"node":{"id":"old","status":"REMOVED"}}]}}}`, status)
	}))
	defer server.Close()
	if err := pollDeployments(context.Background(), server.Client(), server.URL, "fixture-token", "service", "environment", time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 7 {
		t.Fatalf("polls = %d, want 7", calls.Load())
	}
}

// R3.4: an unsettled service, bad response, or hanging request cannot outlive the wait bound.
func TestDevboxR34DeploymentsWaitIsBoundedAndErrorsHideResponse(t *testing.T) {
	for _, mode := range []string{"busy", "bad-json", "graphql-error", "http-error", "hanging"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				// Consume the request so net/http can observe disconnects while the response hangs.
				_, _ = io.Copy(io.Discard, r.Body)
				switch mode {
				case "hanging":
					<-r.Context().Done()
				case "busy":
					fmt.Fprint(w, `{"data":{"deployments":{"edges":[{"node":{"status":"DEPLOYING"}}]}}}`)
				case "bad-json":
					fmt.Fprint(w, `fixture-token`)
				case "graphql-error":
					fmt.Fprint(w, `{"errors":[{"message":"fixture-token"}]}`)
				case "http-error":
					w.WriteHeader(503)
					fmt.Fprint(w, `fixture-token`)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := pollDeployments(ctx, server.Client(), server.URL, "fixture-token", "service", "environment", time.Millisecond, 20*time.Millisecond)
			if err == nil || strings.Contains(err.Error(), "fixture-token") || calls.Load() == 0 || ctx.Err() != nil {
				t.Fatalf("bounded wait: %v (%d requests, parent %v)", err, calls.Load(), ctx.Err())
			}
		})
	}
}

// R3.4: the real up flow polls the overridable GraphQL endpoint before sending a code.
func TestDevboxR34UpUsesDeploymentEndpoint(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	f := fixtureFlow(t, dir, &out)
	var polled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing token header")
		}
		polled.Store(true)
		fmt.Fprint(w, `{"data":{"deployments":{"edges":[]}}}`)
	}))
	defer server.Close()
	f.GraphQLEndpoint = server.URL
	f.Settle = nil
	joined := make(chan struct{})
	f.Pair = func(ctx context.Context, _ string, ready func(Offer)) error {
		if !polled.Load() {
			t.Error("pairing before polling")
		}
		ready(Offer{Code: "42-123-456", Via: "https://relay.example", RemoteKey: "fixture-remote-key", RemoteDevices: "fixture-remote-devices"})
		select {
		case <-joined:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
		err := RunTerraform(ctx, path, dir, args, env, stdout, stderr)
		if args[0] == "apply" && slices.Contains(env, "TF_VAR_pair_code=42-123-456") {
			close(joined)
		}
		return err
	}
	if err := f.Run(context.Background(), "up", Options{Name: "box", Dir: dir, Yes: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "fixture-token") {
		t.Fatal("token in polling output")
	}
}
