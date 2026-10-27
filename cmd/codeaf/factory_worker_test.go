package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
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

// Two real processes must not register separate workers for one home. The
// helper holds the same lock as startFactoryWorker and uses its credential door.
func TestSharedFactoryWorkerHelper(t *testing.T) {
	if os.Getenv("CODEAF_SHARED_WORKER_HELPER") != "1" {
		return
	}
	t.Setenv(home.EnvVar, os.Getenv("CODEAF_SHARED_WORKER_HOME"))
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		os.Exit(2)
	}
	release, ok := st.TryWorker()
	result := struct {
		Owned      bool
		Credential string
	}{Owned: ok}
	if ok {
		defer release()
		base := os.Getenv("CODEAF_SHARED_WORKER_URL")
		dial := newFactoryWorkerDial(st, func() (factoryTarget, error) { return factoryTarget{url: base}, nil })
		w := dial.dial(0)
		if w.Credential() == "" {
			if _, err := w.Attach(context.Background(), api.WorkerAttach{Machine: "shared-machine", Name: "shared-machine", Handle: "shared-person"}); err != nil {
				os.Exit(3)
			}
			dial.keep(0, w)
		}
		if _, _, err := w.Pull(context.Background()); err != nil {
			os.Exit(4)
		}
		result.Credential = w.Credential()
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(5)
	}
	if ok {
		_, _ = io.Copy(io.Discard, os.Stdin)
		release()
	}
	os.Exit(0)
}

func TestSharedFactoryRunnerRegistersOnceAcrossProcessesAndTakeover(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.LocalSeam(st, time.Now()), serve.Config{Store: st, PullWait: time.Millisecond})
	defer h.Close()
	var attaches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/workers/attach" {
			attaches.Add(1)
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	launch := func() (*exec.Cmd, io.WriteCloser, bool, string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestSharedFactoryWorkerHelper$")
		cmd.Env = []string{"CODEAF_SHARED_WORKER_HELPER=1", "CODEAF_SHARED_WORKER_URL=" + server.URL, "CODEAF_SHARED_WORKER_HOME=" + home.Dir(), home.EnvVar + "=" + filepath.Join(home.Dir(), "inherited"), "HOME=" + home.Dir(), "GOMAXPROCS=2"}
		cmd.Stderr = os.Stderr
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { input.Close(); cmd.Process.Kill() })
		var result struct {
			Owned      bool
			Credential string
		}
		if err := json.NewDecoder(bufio.NewReader(output)).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return cmd, input, result.Owned, result.Credential
	}
	first, input, owned, credential := launch()
	if !owned || credential == "" {
		t.Fatal("first process did not register")
	}
	second, _, owned, _ := launch()
	if owned {
		t.Fatal("second process acquired the runner")
	}
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	if attaches.Load() != 1 {
		t.Fatalf("attach count = %d", attaches.Load())
	}
	// A killed process cannot run cleanup; the kernel must release its lock.
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	input.Close()
	_ = first.Wait()
	third, input, owned, reused := launch()
	if !owned || reused != credential || attaches.Load() != 1 {
		t.Fatalf("takeover: owned=%t reused=%t attaches=%d", owned, reused == credential, attaches.Load())
	}
	input.Close()
	if err := third.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestWaitingFactoryRunnerCancelsOrTakesOverOnInjectedTick(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, takeover := range []bool{false, true} {
		release, ok := st.TryWorker()
		if !ok {
			t.Fatal("cannot elect initial runner")
		}
		ctx, cancel := context.WithCancel(context.Background())
		ticks := make(chan time.Time, 1)
		done := make(chan bool, 1)
		go func() {
			unlock, ok := waitFactoryWorker(ctx, st, ticks)
			if ok {
				unlock()
			}
			done <- ok
		}()
		if takeover {
			release()
			ticks <- time.Time{}
		} else {
			cancel()
		}
		select {
		case ok := <-done:
			if ok != takeover {
				t.Fatalf("acquired=%t, takeover=%t", ok, takeover)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("waiting runner did not finish")
		}
		cancel()
		if !takeover {
			release()
		}
	}
}

func TestSharedWorkerCredentialWritersKeepEverySlot(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const base = "http://127.0.0.1:12345"
	var writers sync.WaitGroup
	for slot := range 24 {
		writers.Go(func() {
			newFactoryWorkerDial(st, nil).keep(slot, client.Open(base, "").Worker(fmt.Sprintf("credential-%d", slot)))
		})
	}
	writers.Wait()
	creds := newFactoryWorkerDial(st, nil).creds(base)
	if len(creds) != 24 {
		t.Fatalf("saved %d slots", len(creds))
	}
	for slot, credential := range creds {
		if credential != fmt.Sprintf("credential-%d", slot) {
			t.Fatalf("slot %d lost its credential", slot)
		}
	}
	info, err := os.Stat(filepath.Join(st.Root(), "worker.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v %v", info, err)
	}
}

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
				wantTakes = map[string]bool{"owner/repo": true}
			}
			if !reflect.DeepEqual(attach.TakesUnassigned, wantTakes) {
				t.Fatalf("unassigned work: %v; want %v", attach.TakesUnassigned, wantTakes)
			}
			if attach.OS != runtime.GOOS || attach.CodeafVersion == "" {
				t.Fatalf("machine facts: %+v", attach)
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
			var clock atomic.Int64
			clock.Store(time.Now().UnixNano())
			h := serve.New(person, serve.Config{Mode: api.FactoryTeam, Store: st, Record: rec, PullWait: time.Millisecond, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
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
			if err != nil || len(events) != 3 || events[0].Handle != me.Handle || events[0].Kind != record.Took {
				t.Fatalf("take receipt and worker disagree: %+v, %v", events, err)
			}
			approval, err := st.Create(factory.Item{Repo: "owner/repo", Title: "approve work", Assignee: me.Handle, State: factory.StateNeedsYou, QKind: factory.QKindApprove})
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
			clock.Add(int64(factory.ApprovalUndo))
			settle, err := server.Client().Get(server.URL + "/v1/queue")
			if err != nil {
				t.Fatal(err)
			}
			settle.Body.Close()
			events, err = rec.Since(ctx, time.Time{})
			if err != nil || len(events) != 4 || events[3].Handle != me.Handle || events[3].Kind != record.Approved {
				t.Fatalf("approval receipt and worker disagree: %+v, %v", events, err)
			}
		})
	}
}

// TestDesiredSlotsReadsWithTheWorkersOwnCredential: the capacity read goes out
// with slot 0's saved worker credential and never the person's token, and
// anything short of a clear answer is "unknown" so the worker keeps its number.
func TestDesiredSlotsReadsWithTheWorkersOwnCredential(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	status, slots := 200, 5
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		if status == 200 {
			_ = json.NewEncoder(w).Encode(api.RunnerSelf{Slots: slots})
		}
	}))
	defer server.Close()
	dial := newFactoryWorkerDial(st, func() (factoryTarget, error) { return factoryTarget{url: server.URL, token: "person-token"}, nil })
	if n, ok := dial.desiredSlots(context.Background()); ok || n != 0 || auth != "" {
		t.Fatalf("before attach: %d %v, asked with %q", n, ok, auth)
	}
	dial.keep(0, client.Open(server.URL, "").Worker("worker-credential"))
	if n, ok := dial.desiredSlots(context.Background()); !ok || n != 5 || auth != "Bearer worker-credential" {
		t.Fatalf("saved credential: %d %v, asked with %q", n, ok, auth)
	}
	slots = api.MaxRunnerSlots + 50
	if n, _ := dial.desiredSlots(context.Background()); n != api.MaxRunnerSlots {
		t.Fatalf("an over-ceiling answer gave %d", n)
	}
	for _, code := range []int{401, 404, 500} {
		status = code
		if n, ok := dial.desiredSlots(context.Background()); ok || n != 0 {
			t.Fatalf("status %d must be unknown, got %d %v", code, n, ok)
		}
	}
}

// A machine's own factory listens on a new port at each start: saving a
// credential for it forgets the dead loopback addresses, never a factory
// elsewhere.
func TestWorkerCredentialsForgetDeadLoopbackFactories(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dial := newFactoryWorkerDial(st, nil)
	for _, base := range []string{"http://127.0.0.1:50001", "http://localhost:50002", "http://[::1]:50003", "https://factory.example.com"} {
		dial.keep(0, client.Open(base, "").Worker("old-"+base))
	}
	dial.keep(0, client.Open("http://127.0.0.1:50004", "").Worker("now"))
	all := dial.readCreds()
	if len(all) != 2 || all["http://127.0.0.1:50004"][0] != "now" || all["https://factory.example.com"][0] != "old-https://factory.example.com" {
		t.Fatalf("kept %v, want the live loopback factory and the remote one", all)
	}
	dial.keep(1, client.Open("https://factory.example.com", "").Worker("remote-1"))
	if all := dial.readCreds(); len(all) != 2 || all["http://127.0.0.1:50004"][0] != "now" {
		t.Fatalf("a remote attach forgot the local factory: %v", all)
	}
}
