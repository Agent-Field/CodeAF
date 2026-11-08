package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageOptInAdminSignedRequestAndRefusals(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.key")
	if err := os.WriteFile(path, []byte(base64.RawURLEncoding.EncodeToString(key.Seed())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code := "id_" + strings.Repeat("b", 32)
	calls, refused := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		signature, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("codeaf-admin-signature"))
		if !ed25519.Verify(pub, append([]byte(adminDomain), raw...), signature) {
			t.Error("bad signature")
		}
		var body approval
		if json.Unmarshal(raw, &body) != nil || body.Host != r.Host || body.Identity != code || body.Nonce == "" || time.Since(time.UnixMilli(body.At)).Abs() > time.Minute {
			t.Errorf("bad body: %+v", body)
		}
		if r.URL.Path != "/fabric/v1/admin/storage" {
			t.Errorf("lost prefix: %s", r.URL.Path)
		}
		if refused {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"err":"stale"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"identity": code, "storage": body.Storage})
	}))
	defer srv.Close()
	for _, action := range []string{"approve", "revoke"} {
		var out bytes.Buffer
		if err := run(t.Context(), []string{action, code, "--key", path, "--relay", srv.URL + "/fabric"}, &out, srv.Client()); err != nil {
			t.Fatal(err)
		}
		state := "on"
		if action == "revoke" {
			state = "off"
		}
		if out.String() != code+" storage "+state+"\n" {
			t.Fatalf("answer: %q", out.String())
		}
	}
	refused = true
	if err := run(t.Context(), []string{"approve", code, "--key", path, "--relay", srv.URL + "/fabric"}, io.Discard, srv.Client()); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("refusal: %v", err)
	}
	if err := run(t.Context(), []string{"approve", "bad", "--key", path, "--relay", srv.URL}, io.Discard, srv.Client()); err == nil || calls != 3 {
		t.Fatal("invalid code sent a request")
	}
	var out bytes.Buffer
	if err := run(t.Context(), []string{"public-key", "--key", path}, &out, srv.Client()); err != nil || strings.TrimSpace(out.String()) != base64.RawURLEncoding.EncodeToString(pub) {
		t.Fatalf("public key: %v %q", err, out.String())
	}
	_ = os.Chmod(path, 0640)
	if _, err := readKey(path); err == nil {
		t.Fatal("group-readable key accepted")
	}
	for name, want := range map[string]string{"prod": syncsetup.ProductionRelayURL, "staging": syncsetup.StagingRelayURL, "dev": syncsetup.DevRelayURL} {
		got, err := relayURL(name)
		if err != nil || got.String() != want {
			t.Fatalf("relay %s: %v %v", name, got, err)
		}
	}
}
