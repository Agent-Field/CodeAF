package main

// ── codeaf app: THE PASSKEY LOOPBACK (RFC 8252 §7.3) ────────────────────────
//
// A passkey flow's session goes to whoever started the flow only if the
// ceremony ran on this computer. Every start binds a one-shot listener on the
// literal 127.0.0.1 (never "localhost", 0.0.0.0 or ::, so no firewall asks
// and nothing off this machine can reach it), at a random 256-bit path. When
// the ceremony finishes, the server's page sends the browser here with a
// one-time code; the poll needs that code as well as the verifier. A browser
// on a phone or another laptop reaches its own 127.0.0.1, never this one, so
// a ceremony anyone runs elsewhere signs nobody in here.
//
// The listener takes the first well-formed code at its exact path, answers
// "Done — go back to codeaf.", and closes; it lives no longer than its flow.

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// passkeyLoopback is one flow's listener.
type passkeyLoopback struct {
	ln   net.Listener
	srv  *http.Server
	path string

	mu   sync.Mutex
	code string
}

const passkeyLoopbackPage = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="dark light">` +
	`<title>Done · codeaf</title></head><body style="font:16px/1.5 system-ui,sans-serif;margin:12vh auto;max-width:380px;padding:16px">` +
	`<h1 style="font-size:22px;font-weight:600">Done</h1><p>Go back to codeaf.</p></body></html>`

// openPasskeyLoopback binds the listener and serves it until closed.
func openPasskeyLoopback() (*passkeyLoopback, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the engine could not make a passkey sign-in")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the engine could not listen on 127.0.0.1 for the passkey sign-in")
	}
	lb := &passkeyLoopback{ln: ln, path: base64.RawURLEncoding.EncodeToString(raw[:])}
	redact.Register(lb.path)
	trace.Secret(lb.path)
	lb.srv = &http.Server{Handler: http.HandlerFunc(lb.serve), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 8 << 10}
	guard.Go("app/passkey-loopback", func() { _ = lb.srv.Serve(ln) })
	return lb, nil
}

// port is the listener's port, sent in the server's start.
func (lb *passkeyLoopback) port() int { return lb.ln.Addr().(*net.TCPAddr).Port }

func (lb *passkeyLoopback) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if r.Method != http.MethodGet || r.URL.Path != "/"+lb.path {
		http.NotFound(w, r)
		return
	}
	code := r.URL.Query().Get("code")
	if b, err := base64.RawURLEncoding.DecodeString(code); err != nil || len(b) != 32 {
		http.Error(w, "This is not a codeaf sign-in address.", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !lb.take(code) {
		w.WriteHeader(http.StatusGone)
	}
	_, _ = w.Write([]byte(passkeyLoopbackPage))
	// One shot: nothing after the first code is listened for.
	guard.Go("app/passkey-loopback", func() { _ = lb.ln.Close() })
}

// take keeps the first code; false means one was kept already.
func (lb *passkeyLoopback) take(code string) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.code != "" {
		return false
	}
	redact.Register(code)
	trace.Secret(code)
	lb.code = code
	return true
}

// received is the code the browser brought, or "".
func (lb *passkeyLoopback) received() string {
	if lb == nil {
		return ""
	}
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.code
}

// close stops listening; safe more than once, and on nil.
func (lb *passkeyLoopback) close() {
	if lb != nil {
		_ = lb.srv.Close()
	}
}
