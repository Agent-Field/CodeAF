package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// attachLab is a fresh codeaf home with a floor holding one item, and no
// service running in this process when the test ends.
func attachLab(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(factoryFixtureEnv, "")
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(factory.Item{Repo: "owner/repo", Title: "attach fixture", Kind: factory.KindIssue, State: factory.StateNew}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFactoryServingHere)
	return st
}

// serveElsewhere is another process's service on this machine, as seen
// through its receipt: a loopback listener serving st, with serve.json naming
// it and pid.
func serveElsewhere(t *testing.T, st *store.Store, pid int) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := startFactoryService(context.Background(), st, factory.LocalSeam(st, time.Now()), l, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	body, _ := json.Marshal(factoryDiscovery{URL: s.url, PID: pid})
	if err := writeFactoryPrivate(filepath.Join(st.Root(), "serve.json"), body); err != nil {
		t.Fatal(err)
	}
	return s.url
}

func loadTitles(t *testing.T, seam factory.Seam) []string {
	t.Helper()
	snap, err := seam.Load()
	if err != nil {
		t.Fatalf("the composed seam's Load failed: %v", err)
	}
	var titles []string
	for _, it := range snap.Items {
		titles = append(titles, it.Title)
	}
	return titles
}

// WITH NOTHING SERVING, THE WINDOW SERVES: a loopback service in this
// process, its receipt in serve.json, and the page reading the floor through it.
func TestFactoryLaunchServesInProcessAndWritesDiscovery(t *testing.T) {
	st := attachLab(t)
	seam, addr := factoryLaunchSeam(st, t.TempDir(), "")
	body, err := os.ReadFile(filepath.Join(st.Root(), "serve.json"))
	if err != nil {
		t.Fatalf("no serve.json after an in-process start: %v", err)
	}
	var d factoryDiscovery
	if err := json.Unmarshal(body, &d); err != nil || d.PID != os.Getpid() || d.URL != "http://"+addr() {
		t.Fatalf("serve.json = %s; want this pid and the address %q", body, addr())
	}
	if got := loadTitles(t, seam); len(got) != 1 || got[0] != "attach fixture" {
		t.Fatalf("the page read %q through the service", got)
	}
	if seam.Talk == nil || seam.Launch == nil {
		t.Fatal("the composed seam lost a local door (Talk) or a wired one (Launch)")
	}
}

// THE RECEIPT GOES WITH THE SERVICE: when the window that serves ends, its
// serve.json is gone and serve.lock is free for the next window to serve.
func TestFactoryServingHereTakesItsReceiptDown(t *testing.T) {
	st := attachLab(t)
	if _, err := serveFactoryHere(st, factory.LocalSeam(st, time.Now())); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(st.Root(), "serve.json")
	if _, err := os.Stat(receipt); err != nil {
		t.Fatalf("no serve.json while serving: %v", err)
	}
	closeFactoryServingHere()
	if _, err := os.Stat(receipt); !os.IsNotExist(err) {
		t.Fatalf("serve.json outlived its service: %v", err)
	}
	release, err := factoryFileLock(filepath.Join(st.Root(), "serve.lock"), true)
	defer release()
	if err != nil {
		t.Fatalf("serve.lock was not given back: %v", err)
	}
}

// A SECOND WINDOW USES THE FIRST'S SERVICE: a live serve.json is dialled, and
// no second service is started.
func TestFactoryLaunchAttachesToALiveDiscovery(t *testing.T) {
	st := attachLab(t)
	url := serveElsewhere(t, st, os.Getpid())
	seam, addr := factoryLaunchSeam(st, t.TempDir(), "")
	if "http://"+addr() != url {
		t.Fatalf("the window talks to %q; want the discovered %q", addr(), url)
	}
	if factoryServingHere.service != nil {
		t.Fatal("a second service was started beside a live one")
	}
	if got := loadTitles(t, seam); len(got) != 1 {
		t.Fatalf("the page read %q", got)
	}
}

// A RECEIPT A DEAD PROCESS LEFT IS IGNORED, and the window serves its own.
func TestFactoryLaunchIgnoresAStaleDiscovery(t *testing.T) {
	st := attachLab(t)
	body, _ := json.Marshal(factoryDiscovery{URL: "http://127.0.0.1:1", PID: 1 << 22})
	if err := writeFactoryPrivate(filepath.Join(st.Root(), "serve.json"), body); err != nil {
		t.Fatal(err)
	}
	seam, addr := factoryLaunchSeam(st, t.TempDir(), "")
	if addr() == "127.0.0.1:1" || factoryServingHere.service == nil {
		t.Fatalf("the window trusted a stale receipt: %q", addr())
	}
	loadTitles(t, seam)
}

// ATTACH.JSON WINS over a live local service, and detach gives the floor back.
func TestFactoryLaunchPrefersTheAttachment(t *testing.T) {
	st := attachLab(t)
	serveElsewhere(t, st, os.Getpid())
	team := httpFactory(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer team-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"jobs":[]}`))
	}))
	if err := runAttach([]string{team, "--name", "laptop", "--token", "team-secret"}); err != nil {
		t.Fatal(err)
	}
	a, ok := readFactoryAttachment()
	if !ok || a.URL != team || a.Name != "laptop" || a.Token != "team-secret" {
		t.Fatalf("attach.json = %+v", a)
	}
	seam, addr := factoryLaunchSeam(st, t.TempDir(), "")
	if "http://"+addr() != team {
		t.Fatalf("the window talks to %q; want the attached %q", addr(), team)
	}
	if got := loadTitles(t, seam); len(got) != 0 {
		t.Fatalf("the page read this machine's floor %q, not the attached one", got)
	}
	// --host and --at windows read the same attachment.
	if _, far := factoryAttachedSeam(""); far == nil || "http://"+far() != team {
		t.Fatal("a --host window did not take the attachment")
	}
	if err := runDetach(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFactoryAttachment(); ok {
		t.Fatal("detach left attach.json")
	}
	if seam, far := factoryAttachedSeam(""); far != nil || seam.Load != nil {
		t.Fatal("a --host window with nothing attached got a factory")
	}
}

// A WINDOW WHOSE SERVING WINDOW WENT AWAY TAKES THE SERVING OVER on its next
// read, without being handed a new seam.
func TestFactoryConnectionTakesOverWhenTheServerGoes(t *testing.T) {
	st := attachLab(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := startFactoryService(context.Background(), st, factory.LocalSeam(st, time.Now()), l, "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(factoryDiscovery{URL: gone.url, PID: os.Getpid()})
	if err := writeFactoryPrivate(filepath.Join(st.Root(), "serve.json"), body); err != nil {
		t.Fatal(err)
	}
	seam, addr := factoryLaunchSeam(st, t.TempDir(), "")
	if "http://"+addr() != gone.url {
		t.Fatalf("talks to %q; want %q", addr(), gone.url)
	}
	gone.close()
	if got := loadTitles(t, seam); len(got) != 1 {
		t.Fatalf("after the server went the page read %q", got)
	}
	if "http://"+addr() == gone.url || factoryServingHere.service == nil {
		t.Fatalf("the window did not take the serving over: %q", addr())
	}
}

// httpFactory is a stand-in factory at a loopback url.
func httpFactory(t *testing.T, h http.Handler) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + l.Addr().String()
}
