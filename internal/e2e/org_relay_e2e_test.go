//go:build e2e

package e2e

// org_relay_e2e_test.go is THE KEYLESS PROOF THAT AN ORGANIZATION'S OWN RELAY CARRIES EVERYTHING THE PUBLIC ONE DOES, driven as a
// member would drive it: the relay that deploy/org starts (its role script, scripts/relay.sh, with nothing set but the port and
// where to keep its files, so these are the defaults an operator gets), two computers that have never heard of any other relay,
// the real binary in real terminals, and a model that is a script.
//
//   - The relay answers its health page, and says nothing of which computers are connected.
//   - One computer offers a code with `codeaf pair --via <the organization's relay>` and another joins with the line it is told
//     to type. `--via` is what `codeaf pair --org <organization>` runs once it has looked the address up (cmd/codeaf/pair_org.go
//     has its own tests for the lookup), and from then on NO WINDOW OF THIS TEST SETS CODEAF_SYNC_URL: each computer finds the
//     relay only because the pairing told it, which is how a member's computer finds it on the day after.
//   - The relay says it keeps no chats, and the computers believe it by themselves: nothing here writes their storage answer, so a
//     relay that answered "on" (or said nothing) would have them upload and be refused, and the test would fail on the disk.
//   - A chat the first computer is running opens live on the second, a line typed there reaches the first, and both show the
//     reply. Nothing the viewer does needs an upload.
//   - When the relay is this test's own, its folder holds the two computers' device list and not one byte of a chat.
//
// CODEAF_E2E_ORG_RELAY=<address> points the test at a relay that is already running, such as the one in the deploy/org stack
// (smoke.sh does it with SMOKE_RELAY_LIVE=1), instead of starting one. The folder checks are then skipped, because they need the
// relay's disk; everything else is asked of it over the wire.
//
// IT SKIPS RATHER THAN FAILS without tmux or bin/codeaf, like every scenario in this package:
//
//	go test -tags e2e -count=1 -run TestOrgRelayE2E -v ./internal/e2e/

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// orgRelayEnv names the relay a run is pointed at when it should not start its own.
const orgRelayEnv = "CODEAF_E2E_ORG_RELAY"

func TestOrgRelayE2E(t *testing.T) {
	rcRequire(t)
	rc, store := newOrgRelayRig(t)

	// Pairing: the first computer offers, the second types what it is told. Neither has a relay variable.
	if _, err := devname.Set(rc.host.home, rcHostName); err != nil {
		t.Fatal(err)
	}
	offer := startCommand(t, orgRelayEnvOf(rc), "org-offer", rc.host.home, rc.host.work, "pair", "--via", rc.address, "--code")
	var code string
	if !waitUntil(30*time.Second, func() bool {
		code = rcCodeShape.FindString(offer.capture())
		return code != ""
	}) {
		t.Fatalf("pair --code never showed a code:\n%s", offer.capture())
	}
	if !strings.Contains(offer.capture(), "--via "+rc.address) {
		t.Errorf("the offering computer did not tell the other one which relay to use:\n%s", offer.capture())
	}
	join := startCommand(t, orgRelayEnvOf(rc), "org-join", rc.viewers[0].home, rc.viewers[0].work, "pair", "--name", rcViewerName, "--via", rc.address, code)
	offer.waitFor(30*time.Second, "wants your chats")
	rc.abortIfFailed()
	offer.lit("y")
	offer.keys("Enter")
	join.waitFor(30*time.Second, "Paired")
	rc.abortIfFailed()

	// Each computer remembers the relay it was paired through, so no variable is needed from here on.
	for _, h := range rc.homes() {
		saved, err := os.ReadFile(filepath.Join(h.home, syncsetup.SavedURLFile))
		if err != nil || strings.TrimRight(strings.TrimSpace(string(saved)), "/") != rc.address {
			t.Errorf("%s did not keep the relay it was paired through (%q, %v)", h.name, saved, err)
		}
	}
	id, err := identity.Load(rc.host.home)
	if err != nil {
		t.Fatalf("the pairing left the host with no identity: %v", err)
	}
	rc.identity = id.ID()

	// The relay said storage is off and the computers took its word: a person never turned it off on either.
	host := rc.startHost2()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	for _, h := range rc.homes() {
		h := h
		if !waitUntil(60*time.Second, func() bool { return syncsetup.StorageState(h.home, rc.address, rc.identity) == "off" }) {
			t.Errorf("%s never learned from the relay that chat storage is off (it believes %q)", h.name, syncsetup.StorageState(h.home, rc.address, rc.identity))
		}
	}
	rc.abortIfFailed()
	// From here on, nothing may ask the relay to keep a chat.
	rc.audit.armed.Store(true)

	viewer := rc.startViewer2(0)
	rc.openLive(viewer)
	rc.send(viewer, rcLaptopAsk)
	viewer.waitFor(rcReplyWithin, rcLaptopReply)
	host.waitFor(rcReplyWithin, rcLaptopReply)
	rc.assertLiveDial()

	if store != "" {
		orgRelayHoldsNoChats(t, store)
	}
}

// orgRelayHoldsNoChats checks the relay's own folder: the device list the two computers made is there, and no file of chat content is.
func orgRelayHoldsNoChats(t *testing.T, store string) {
	t.Helper()
	if dbs, _ := filepath.Glob(filepath.Join(store, "directory", "*.db")); len(dbs) == 0 {
		t.Errorf("the relay kept no device list under %s", store)
	}
	var held []string
	_ = filepath.Walk(filepath.Join(store, "blobs"), func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			held = append(held, path)
		}
		return nil
	})
	if len(held) > 0 {
		t.Errorf("the relay holds chat content with storage off: %d file(s), first %s", len(held), held[0])
	}
}

// newOrgRelayRig builds the world of this scenario: a short root under /tmp, the scripted model, the organization's relay behind a
// proxy that writes down what it sees (and, unlike remote control's, answers nothing on the relay's behalf), and two homes that
// have no identity and have never been told a relay. The second answer is the relay's own folder, or "" for a relay that was not
// started here.
func newOrgRelayRig(t *testing.T) (*rcRig, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "org-relay-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	// A relay variable in the person's own shell must not decide which relay this scenario's computers use.
	t.Setenv(syncsetup.URLVar, "")

	rc := &rcRig{t: t, root: root, model: startRCModel(t)}
	upstream, store := os.Getenv(orgRelayEnv), ""
	if upstream == "" {
		upstream, store = startOrgRelayRole(t, root)
	}
	orgRelayAnswersItsHealthPage(t, upstream)
	rc.address, rc.audit = startPlainAuditProxy(t, upstream)

	rc.host = rcHome{name: rcHostName, home: filepath.Join(root, "h"), work: filepath.Join(root, "hw")}
	rc.viewers = []rcHome{{name: rcViewerName, home: filepath.Join(root, "v0"), work: filepath.Join(root, "vw0")}}
	for _, h := range rc.homes() {
		if err := os.MkdirAll(h.home, 0o700); err != nil {
			t.Fatal(err)
		}
		workspaceAt(t, h.work, false)
		rc.writeConfig(h.home)
	}
	// Engines belong to these temporary homes, including any a move brings along.
	t.Cleanup(func() {
		pids := engineDaemonsUnder(root)
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		deadline := time.Now().Add(10 * time.Second)
		for _, pid := range pids {
			for !terminalProcessExited(pid) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if !terminalProcessExited(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return rc, store
}

// startOrgRelayRole starts the relay the way the deploy/org stack does, by running its role script with the product's own binary
// on the path. Only the port, the address to listen on and the folder are given; the script's defaults decide storage, the status
// page and the proxy, so a default that changed under the operator changes this scenario too. It answers the relay's address and
// its folder.
func startOrgRelayRole(t *testing.T, root string) (string, string) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "deploy", "org", "scripts", "relay.sh")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("no relay role script at %s", script)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary(t), filepath.Join(bin, "codeaf")); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	store := filepath.Join(root, "relay-store")

	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CODEAF_RELAY_") && !strings.HasPrefix(kv, "PORT=") {
			env = append(env, kv)
		}
	}
	env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CODEAF_RELAY_LISTEN="+addr,
		"CODEAF_RELAY_STORE="+store, "CODEAF_HOME="+filepath.Join(root, "relay-home"), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1")
	cmd := guardedCommand(t, context.Background(), filepath.Join(root, "relay-home"), env, "sh", script)
	log, err := os.Create(filepath.Join(root, "relay.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = log.Close() })
	// The port is open before the relay is ready to answer, so ask for the page that says it is.
	if !waitUntil(30*time.Second, func() bool {
		resp, err := http.Get("http://" + addr + "/p/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}) {
		raw, _ := os.ReadFile(filepath.Join(root, "relay.log"))
		t.Fatalf("the relay role never answered its health page on %s:\n%s", addr, raw)
	}
	return "http://" + addr, store
}

// orgRelayAnswersItsHealthPage is what the stack's health check asks, and what it must not say: which computers are connected.
func orgRelayAnswersItsHealthPage(t *testing.T, address string) {
	t.Helper()
	get := func(path string) int {
		resp, err := http.Get(address + path)
		if err != nil {
			t.Fatalf("GET %s%s: %v", address, path, err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if got := get("/p/health"); got != http.StatusOK {
		t.Errorf("the relay's health page answered %d, not 200", got)
	}
	if os.Getenv(orgRelayEnv) == "" {
		if got := get("/status"); got != http.StatusNotFound && got != http.StatusForbidden {
			t.Errorf("the relay publishes which computers are connected (GET /status answered %d) when nobody asked it to", got)
		}
	}
}

// startPlainAuditProxy is remote control's auditing proxy without its fiction: every request is written down, and every
// answer is the relay's own, including the storage answer, which is the point of this scenario.
func startPlainAuditProxy(t *testing.T, upstreamAddress string) (string, *rcAudit) {
	t.Helper()
	upstream, err := url.Parse(upstreamAddress)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	audit := &rcAudit{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		audit.mu.Lock()
		audit.calls = append(audit.calls, rcCall{method: r.Method, path: r.URL.Path, door: r.Header.Get(relay.HeaderDoor)})
		audit.mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" && audit.armed.Load() {
			audit.storeWrites.Add(1)
		}
		if r.Header.Get(relay.HeaderDoor) == relay.DoorLive {
			w = &rcCountedWriter{ResponseWriter: w, total: &audit.live}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		if n := audit.storeWrites.Load(); n > 0 {
			t.Errorf("a computer asked the relay to keep a chat %d time(s) after it had said storage is off: %s", n, audit.summary())
		}
	})
	return server.URL, audit
}

// orgRelayEnvOf is what every window of this scenario runs with: the rig's environment minus the one variable that names a relay,
// because the point is that the pairing is what tells a computer which relay to use.
func orgRelayEnvOf(rc *rcRig) []string {
	var env []string
	for _, kv := range rc.env() {
		if !strings.HasPrefix(kv, syncsetup.URLVar+"=") {
			env = append(env, kv)
		}
	}
	return env
}

// startHost2 and startViewer2 are the rig's windows with that environment, numbered the same way.
func (rc *rcRig) startHost2() *rig { return rc.orgWindow("host", rc.host) }

func (rc *rcRig) startViewer2(i int) *rig { return rc.orgWindow("viewer", rc.viewers[i]) }

func (rc *rcRig) orgWindow(name string, h rcHome) *rig {
	rc.t.Helper()
	rc.windows++
	return startWithEnv(rc.t, orgRelayEnvOf(rc), fmt.Sprintf("org-%s-%d", name, rc.windows), h.home, h.work, 180, 55)
}
