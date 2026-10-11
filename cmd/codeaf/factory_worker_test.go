package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestFactoryWorkerAttachRequiresOwnLoginOffLoopback(t *testing.T) {
	for _, tc := range []struct {
		name, base, login string
		wantError         bool
	}{
		{"remote login", "https://factory.example", " @WORKER-PERSON ", false},
		{"remote no login", "https://factory.example", "", true},
		{"remote blank login", "https://factory.example", " @ ", true},
		{"IPv4 loopback", "http://127.0.0.1:8080", "", false},
		{"IPv6 loopback", "http://[::1]:8080", "", false},
		{"localhost", "http://localhost:8080", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			st, err := store.Open(v3FactoryRoot())
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetCheckout("owner/repo", t.TempDir()); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(factoryAttachment{URL: tc.base, Name: "my-worker"})
			if err != nil {
				t.Fatal(err)
			}
			if err := writeFactoryPrivate(factoryAttachPath(), body); err != nil {
				t.Fatal(err)
			}
			person := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) {
				return factory.GitHubLink{Login: tc.login}, nil
			}}
			attach, err := newFactoryWorkerDial(st, nil).attach(context.Background(), person)
			if tc.wantError {
				if err == nil || err.Error() != "codeaf needs your GitHub login to work for this factory" {
					t.Fatalf("missing login refusal: %v", err)
				}
				if !reflect.DeepEqual(attach, api.WorkerAttach{}) {
					t.Fatalf("refused worker still has an introduction: %+v", attach)
				}
				return
			}
			if err != nil || attach.Handle != factory.NormalizeHandle(tc.login) {
				t.Fatalf("worker introduction: %+v, %v", attach, err)
			}
			wantTakes := map[string]bool{lease.EveryRepo: true}
			if !factoryLoopback(tc.base) {
				wantTakes = map[string]bool{"repo": true}
			}
			if !reflect.DeepEqual(attach.TakesUnassigned, wantTakes) {
				t.Fatalf("unassigned work: %v; want %v", attach.TakesUnassigned, wantTakes)
			}
		})
	}
}

// This drives the worker's real introduction and HTTP lease door because a
// desk-only fixture cannot catch the OS account replacing the forge login.
func TestFactoryWorkerLeasesTheJobItsForgePersonTook(t *testing.T) {
	for _, localLogin := range []string{" @FORGE-PERSON ", ""} {
		t.Run(fmt.Sprintf("local_login=%q", localLogin), func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			st, err := store.Open(v3FactoryRoot())
			if err != nil {
				t.Fatal(err)
			}
			job, err := st.Create(factory.Item{Repo: "owner/repo", Title: "taken work", State: factory.StateQueued})
			if err != nil {
				t.Fatal(err)
			}
			person := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) {
				return factory.GitHubLink{Login: " @FORGE-PERSON "}, nil
			}}
			person.Load = func() (factory.Snapshot, error) {
				items, err := st.List()
				return factory.Snapshot{Items: items}, err
			}
			person.Answer = func(int, bool, string) error { return nil }
			rec, err := record.Open(st, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			h := serve.New(person, serve.Config{Store: st, Record: rec, PullWait: time.Millisecond})
			t.Cleanup(h.Close)
			server := httptest.NewServer(h)
			defer server.Close()
			ctx := context.Background()
			response, err := server.Client().Post(fmt.Sprintf("%s/v1/jobs/%d/take", server.URL, job.ID), "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("take: HTTP %d", response.StatusCode)
			}
			c := client.Open(server.URL, "")
			other := c.Worker("")
			if _, err := other.Attach(ctx, api.WorkerAttach{Name: "other", Handle: "another-person"}); err != nil {
				t.Fatal(err)
			}
			if pulled, ok, err := other.Pull(ctx); err != nil || ok {
				t.Fatalf("another person's worker pulled taken work: %+v, %t, %v", pulled, ok, err)
			}
			local := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) {
				return factory.GitHubLink{Login: localLogin}, nil
			}}
			dial := newFactoryWorkerDial(st, nil)
			attach, err := dial.attach(ctx, local)
			if err != nil {
				t.Fatal(err)
			}
			if attach.Handle != factory.NormalizeHandle(localLogin) {
				t.Fatalf("worker introduced %q instead of its forge login %q", attach.Handle, localLogin)
			}
			worker := c.Worker("")
			if _, err := worker.Attach(ctx, attach); err != nil {
				t.Fatal(err)
			}
			pulled, ok, err := worker.Pull(ctx)
			if err != nil || !ok || pulled.Lease.Job != job.ID || pulled.Lease.Handle != "forge-person" {
				t.Fatalf("taker's worker cannot lease the job: %+v, %t, %v", pulled, ok, err)
			}
			response, err = server.Client().Get(server.URL + "/v1/me")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var me struct{ Handle string }
			if err := json.NewDecoder(response.Body).Decode(&me); err != nil || me.Handle != pulled.Lease.Handle {
				t.Fatalf("viewer and worker disagree: %+v, %v", me, err)
			}
			events, err := rec.Since(ctx, time.Time{})
			if err != nil || len(events) != 1 || events[0].Handle != me.Handle || events[0].Kind != record.Took {
				t.Fatalf("take receipt and worker disagree: %+v, %v", events, err)
			}
			approval, err := st.Create(factory.Item{Repo: "owner/repo", Title: "approve work", State: factory.StateNeedsYou, QKind: factory.QKindApprove})
			if err != nil {
				t.Fatal(err)
			}
			response, err = server.Client().Post(fmt.Sprintf("%s/v1/jobs/%d/approve", server.URL, approval.ID), "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("approve: HTTP %d", response.StatusCode)
			}
			events, err = rec.Since(ctx, time.Time{})
			if err != nil || len(events) != 2 || events[1].Handle != me.Handle || events[1].Kind != record.Approved {
				t.Fatalf("approval receipt and worker disagree: %+v, %v", events, err)
			}
		})
	}
}
