package desktopbridge

import (
	"net"
	"net/http"
	"strconv"
)

// foreignOriginSentence is what a refused page is told. It names nothing about
// the engine, so a foreign page learns only that the door is not for it.
const foreignOriginSentence = "this connection is only for the codeaf app"

// allowedOrigins are the only pages that may talk to the bridge: the packaged
// shell on each platform and the Vite dev server on both loopback spellings.
var allowedOrigins = map[string]bool{
	"tauri://localhost":      true,
	"http://tauri.localhost": true,
	"http://localhost:1420":  true,
	"http://127.0.0.1:1420":  true,
}

// guardRequest decides whether a request may reach the token check. It returns
// a zero status to let it through. It runs BEFORE the token check so a foreign
// page, even one holding the token, learns nothing about the engine.
//
// A request with no Origin header (the Rust side, the Vite proxy talking
// server-side) passes this guard and still needs the token. A Host that is not
// loopback with a port is refused with 421, which defeats DNS rebinding.
func guardRequest(r *http.Request) (status int, sentence string) {
	if origin, present := r.Header["Origin"]; present && (len(origin) != 1 || !allowedOrigins[origin[0]]) {
		return http.StatusForbidden, foreignOriginSentence
	}
	if !loopbackHost(r.Host) {
		return http.StatusMisdirectedRequest, foreignOriginSentence
	}
	return 0, ""
}

// Handler is the bridge behind the origin guard. The desktop command serves
// this, so a web page's fetch (its Origin is the page, not the app) is refused
// before the token is read and learns nothing about the engine.
func (b *Bridge) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, sentence := guardRequest(r); status != 0 {
			fail(w, status, sentence)
			return
		}
		b.ServeHTTP(w, r)
	})
}

// loopbackHost accepts only 127.0.0.1, [::1] or localhost with a numeric port.
func loopbackHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return false
	}
	switch name {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}
