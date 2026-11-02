//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestQuitContinuationFirstSend proves, with the shipped terminal and the native
// service, that a chat continued by a bare start takes its first message with no
// continuation card and without releasing this computer's own hold.
func TestQuitContinuationFirstSend(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("", "l2lfix-continue-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	const first = "Remember the amber lighthouse."
	const second = "Continue with the amber lighthouse."
	const reply = "AMBER_LIGHTHOUSE_ANSWER"
	const continuedReply = "AMBER_LIGHTHOUSE_CONTINUED"
	var answered atomic.Bool
	brain := &automationBrain{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", brain.serveCatalog)
	mux.HandleFunc("/api/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		text := reply
		if strings.Contains(string(raw), second) {
			answered.Store(true)
			text = continuedReply
		}
		send, ok := automationStream(w)
		if !ok {
			return
		}
		send(fmt.Sprintf(`{"content":%q}`, text), "")
		send(`{}`, "stop")
		send(`{}`, "[DONE]")
	})
	model := httptest.NewServer(mux)
	t.Cleanup(model.Close)
	relay := quitContinuationRelay(t, root)
	upstream, _ := url.Parse(relay)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var releasesA, releasesB, acquiresB atomic.Int32
	var devA, devB string
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.StatusCode == http.StatusNoContent && strings.HasSuffix(response.Request.URL.Path, "/release") {
			raw, _ := base64.RawURLEncoding.DecodeString(response.Request.Header.Get(reqsign.HeaderCert))
			var cert identity.Cert
			_ = json.Unmarshal(raw, &cert)
			if cert.DeviceID() == devA {
				releasesA.Add(1)
			}
		}
		return nil
	}
	audit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raw, _ := base64.RawURLEncoding.DecodeString(r.Header.Get(reqsign.HeaderCert))
			var cert identity.Cert
			_ = json.Unmarshal(raw, &cert)
			device := cert.DeviceID()
			if strings.HasSuffix(r.URL.Path, "/release") || strings.HasSuffix(r.URL.Path, "/acquire") {
				t.Logf("directory %s device=%s first=%s second=%s", r.URL.Path, device, devA, devB)
			}
			if strings.HasSuffix(r.URL.Path, "/release") {
				if device == devB {
					releasesB.Add(1)
				}
			}
			if strings.HasSuffix(r.URL.Path, "/acquire") && device == devB {
				acquiresB.Add(1)
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(audit.Close)
	t.Setenv(syncsetup.URLVar, audit.URL)
	homeA, homeB := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, home := range []string{homeA, homeB} {
		if err := os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		rows := map[string]any{"api_key": "stub-key", "model.talk": automationStubModel, "memory.enabled": false, "tools.approvalMode": "allow", "setup_seen_at": time.Now().UTC().Format(time.RFC3339), config.KeyIcons: config.IconsPlain}
		for _, key := range []string{config.KeyTaskModel, config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel, config.KeyTierReflexModel, config.KeyTierMastermindModel} {
			rows[key] = automationStubModel
		}
		raw, _ := json.Marshal(rows)
		if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Paired homes share the identity while retaining distinct device keys.
	id, err := identity.Ensure(homeA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(homeB, id, false); err != nil {
		t.Fatal(err)
	}
	for index, home := range []string{homeA, homeB} {
		name := []string{"tower", "laptop"}[index]
		if _, err := devname.Set(home, name); err != nil {
			t.Fatal(err)
		}
		s, ok, err := syncsetup.Open(home)
		if err != nil || !ok {
			t.Fatalf("paired home: %v %v", ok, err)
		}
		if index == 0 {
			devA = s.Device.ID()
		} else {
			devB = s.Device.ID()
		}
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, name); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{config.APIKeyEnv + "=stub-key", "CODEAF_BASE_URL=" + model.URL + "/api/v1", "CODEAF_PROFILE_DIR=", "CODEAF_SYNC_URL=" + audit.URL, "CODEAF_SYNC_INTERVAL_MS=100", "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_NO_UPDATE_CHECK=1", "CODEAF_NO_AUTOMATIONS=1"}
	wsA, wsB := filepath.Join(root, "work-a"), filepath.Join(root, "work-b")
	for _, ws := range []string{wsA, wsB} {
		if err := os.Mkdir(ws, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Engines belong to these temporary homes, including any moved workspaces.
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
	a := startWithEnv(t, env, "quit-continue-a", homeA, wsA, 160, 45)
	a.lit(first)
	a.keys("Enter")
	a.waitFor(35*time.Second, reply)
	a.lit("/quit")
	a.keys("Enter")
	if !waitUntil(15*time.Second, func() bool { return releasesA.Load() > 0 }) {
		t.Fatalf("clean quit did not release the saved chat:\n%s", a.capture())
	}
	a.kill()
	b := startWithEnv(t, env, "quit-continue-b", homeB, wsB, 160, 45)
	b.waitFor(35*time.Second, "Moved from tower")
	if acquiresB.Load() == 0 {
		t.Fatal("startup did not acquire")
	}
	b.lit(second)
	b.keys("Enter")
	deadline := time.Now().Add(35 * time.Second)
	for {
		screen := b.capture()
		if strings.Contains(screen, "Continue this chat here?") {
			t.Fatalf("first send raised the continuation card:\n%s", screen)
		}
		if strings.Contains(screen, "this window now only shows it") {
			t.Fatalf("the window that continued the chat said another computer holds it:\n%s", screen)
		}
		if answered.Load() && strings.Contains(screen, continuedReply) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first send did not reach the scripted model:\n%s", screen)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if releasesB.Load() != 0 {
		t.Fatalf("this window released its own hold before sending: %d", releasesB.Load())
	}
	t.Logf("first send answered; acquires=%d releases before send=%d", acquiresB.Load(), releasesB.Load())
}

func quitContinuationRelay(t *testing.T, root string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	env := append(os.Environ(), "CODEAF_HOME="+filepath.Join(root, "relay-home"), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1")
	cmd := guardedCommand(t, context.Background(), filepath.Join(root, "relay-home"), env, binary(t), "relay", "--listen", addr, "--store", filepath.Join(root, "store"), "--quiet")
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
	if !waitUntil(10*time.Second, func() bool {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	}) {
		t.Fatal("native service did not listen")
	}
	return "http://" + addr
}
