package serve_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A PERSONAL FACTORY RUNS WITHOUT GITHUB (2026-11-17): with no forge login,
// its runner on this machine attaches under factory.LocalRunnerHandle and
// takes an unassigned job, such as a folder job made before GitHub was
// connected. Before, the attach was refused for want of a handle, the runner
// retried in silence, and the job waited in the queue forever. A team factory
// and any remote peer are still refused.
func TestSoloRunnerAttachesWithoutGitHub(t *testing.T) {
	for _, tc := range []struct {
		name, peer string
		mode       string
		status     int
	}{
		{"solo on loopback runs", "127.0.0.1:1234", api.FactorySolo, http.StatusCreated},
		{"solo from another machine is refused", "192.0.2.1:1234", api.FactorySolo, http.StatusUnprocessableEntity},
		{"team on loopback is refused", "127.0.0.1:1234", api.FactoryTeam, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			// No GitHub: the seam has no forge at all, as on a Mac that skipped it.
			cfg := config
			cfg.Store, cfg.PullWait, cfg.Mode = st, time.Millisecond, tc.mode
			h := serve.New(factory.Seam{}, cfg)
			t.Cleanup(h.Close)
			body, err := json.Marshal(api.WorkerAttach{Name: "laptop", Kind: api.RunnerThisMac, TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/v1/workers/attach", bytes.NewReader(body))
			r.RemoteAddr, r.Host = tc.peer, "localhost"
			r.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("attach: HTTP %d %s; want %d", w.Code, w.Body.String(), tc.status)
			}
			if tc.status != http.StatusCreated {
				return
			}
			var attached api.WorkerAttached
			if err := json.Unmarshal(w.Body.Bytes(), &attached); err != nil {
				t.Fatal(err)
			}
			job, err := st.Create(factory.Item{Title: "make greet say hi", Repo: "owner/repo", State: factory.StateQueued,
				Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true, Until: factory.UntilDone}}})
			if err != nil {
				t.Fatal(err)
			}
			r = httptest.NewRequest("GET", "/v1/workers/pull", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("Authorization", "Bearer "+attached.Credential)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			var pulled api.WorkerPull
			if err := json.Unmarshal(w.Body.Bytes(), &pulled); err != nil || w.Code != http.StatusOK || pulled.Job.ID != job.ID {
				t.Fatalf("the runner did not take the unassigned job: HTTP %d %s, %v", w.Code, w.Body.String(), err)
			}
			if pulled.Lease.Handle != factory.LocalRunnerHandle {
				t.Fatalf("lease handle %q; want the local runner name %q", pulled.Lease.Handle, factory.LocalRunnerHandle)
			}
		})
	}
}

// The local runner's name is one the factory's history accepts: a name it
// refuses would fail every attach on a factory that keeps a record.
func TestLocalRunnerHandleFitsTheRecord(t *testing.T) {
	for _, c := range factory.LocalRunnerHandle {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			t.Fatalf("%q has %q, which the record refuses", factory.LocalRunnerHandle, c)
		}
	}
}

// The whole chain on Personal with no GitHub: a folder job made from the app
// has no assignee, and the runner on this machine, attached under the local
// name, takes it once it is queued. This is the job that used to wait forever.
func TestPersonalFolderJobRunsWithoutGitHub(t *testing.T) {
	f := personalFolders(t)
	_, inner := gitFolder(t, "")
	raw, _ := json.Marshal(api.NewJob{Folder: inner, Title: "Tidy the readme", Source: factory.AskApp})
	w := verbRequest(f.h, "POST", "/v1/jobs", string(raw))
	checkJSON(t, w, 201)
	var made struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	saved, err := f.st.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Person() != "" {
		t.Fatalf("a folder job made with no GitHub is assigned to %q; want nobody, so this machine's runner takes it", saved.Person())
	}
	if err := f.st.Update(made.ID, func(it *factory.Item) error { it.State = factory.StateQueued; return nil }); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(api.WorkerAttach{Name: "laptop", Kind: api.RunnerThisMac, TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	w = verbRequest(f.h, "POST", "/v1/workers/attach", string(body))
	checkJSON(t, w, http.StatusCreated)
	var attached api.WorkerAttached
	if err := json.Unmarshal(w.Body.Bytes(), &attached); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/workers/pull", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer "+attached.Credential)
	pw := httptest.NewRecorder()
	f.h.ServeHTTP(pw, r)
	var pulled api.WorkerPull
	if err := json.Unmarshal(pw.Body.Bytes(), &pulled); err != nil || pw.Code != http.StatusOK || pulled.Job.ID != made.ID || pulled.Lease.Handle != factory.LocalRunnerHandle {
		t.Fatalf("this machine's runner did not take the folder job: HTTP %d %s, %v", pw.Code, pw.Body.String(), err)
	}
}
