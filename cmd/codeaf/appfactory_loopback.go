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
// THE LISTENER TAKES ONLY ITS OWN FLOW'S CODE. Each flow has a 256-bit key
// the engine sends the server with the start; the server's code is 16 random
// bytes and the first 16 bytes of their HMAC-SHA256 under that key. A request
// with another Host, another path or a code without that mark (another local
// user's, a scanner's) is answered "not found" and the listener keeps
// waiting, at most passkeyLoopbackRefusals of them a second, until its flow
// ends. Only its own code is taken; it answers a 303 to a clean /done, so the
// code does not stay in the address bar, shows "Done" there, and closes.
//
// THE RESIDUAL (RFC 8252 §8.3): the port is chosen by this computer's
// kernel, and another person signed in to the same computer can see which
// ports listen. They still need the path and the key, which never leave
// this engine and the organization's server.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
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

	key []byte

	mu       sync.Mutex
	code     string
	window   time.Time
	refusals int
}

// passkeyLoopbackRefusals is how many foreign requests a second the
// listener answers at all; past it they get 429 until the next second.
const passkeyLoopbackRefusals = 20

// passkeyLoopbackDoneWait is how long the listener stays for /done after
// its code.
const passkeyLoopbackDoneWait = 30 * time.Second

const passkeyLoopbackPage = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="dark light">` +
	`<title>Done · codeaf</title></head><body style="font:16px/1.5 system-ui,sans-serif;margin:12vh auto;max-width:380px;padding:16px">` +
	`<h1 style="font-size:22px;font-weight:600">Done</h1><p>Go back to codeaf.</p></body></html>`

// openPasskeyLoopback binds the listener and serves it until closed.
func openPasskeyLoopback() (*passkeyLoopback, error) {
	var raw, key [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the engine could not make a passkey sign-in")
	}
	if _, err := rand.Read(key[:]); err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the engine could not make a passkey sign-in")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the engine could not listen on 127.0.0.1 for the passkey sign-in")
	}
	lb := &passkeyLoopback{ln: ln, path: base64.RawURLEncoding.EncodeToString(raw[:]), key: key[:]}
	redact.Register(lb.path)
	trace.Secret(lb.path)
	redact.Register(lb.returnKey())
	trace.Secret(lb.returnKey())
	lb.srv = &http.Server{Handler: http.HandlerFunc(lb.serve), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 8 << 10}
	guard.Go("app/passkey-loopback", func() { _ = lb.srv.Serve(ln) })
	return lb, nil
}

// port is the listener's port, sent in the server's start.
func (lb *passkeyLoopback) port() int { return lb.ln.Addr().(*net.TCPAddr).Port }

// returnKey is the key sent in the server's start, base64url.
func (lb *passkeyLoopback) returnKey() string { return base64.RawURLEncoding.EncodeToString(lb.key) }

// ours is whether code carries this flow's mark.
func (lb *passkeyLoopback) ours(code string) bool {
	c, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(c) != 32 {
		return false
	}
	mac := hmac.New(sha256.New, lb.key)
	mac.Write(c[:16])
	return hmac.Equal(mac.Sum(nil)[:16], c[16:])
}

// refused counts a foreign request; false means this second's allowance is
// spent.
func (lb *passkeyLoopback) refused(now time.Time) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if now.Sub(lb.window) >= time.Second {
		lb.window, lb.refusals = now, 0
	}
	lb.refusals++
	return lb.refusals <= passkeyLoopbackRefusals
}

func (lb *passkeyLoopback) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if r.Method == http.MethodGet && r.Host == "127.0.0.1:"+strconv.Itoa(lb.port()) && r.URL.Path == "/done" && r.URL.RawQuery == "" && lb.received() != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(passkeyLoopbackPage))
		// One shot: nothing after its own Done is listened for.
		guard.Go("app/passkey-loopback", func() { _ = lb.ln.Close() })
		return
	}
	code := r.URL.Query().Get("code")
	if r.Method != http.MethodGet || r.Host != "127.0.0.1:"+strconv.Itoa(lb.port()) || r.URL.Path != "/"+lb.path || !lb.ours(code) {
		// Not this flow's: answered and forgotten, never closing.
		if !lb.refused(time.Now()) {
			http.Error(w, "Too many requests.", http.StatusTooManyRequests)
			return
		}
		http.NotFound(w, r)
		return
	}
	lb.take(code)
	http.Redirect(w, r, "/done", http.StatusSeeOther)
	// If the browser never asks for /done, the listener still ends.
	time.AfterFunc(passkeyLoopbackDoneWait, func() { _ = lb.ln.Close() })
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
