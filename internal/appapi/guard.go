// Package appapi serves the local API used by the codeaf app.
package appapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
)

const foreignOriginSentence = "this connection is only for the codeaf app"

// Token creates a launch credential. The caller keeps it in memory and hands
// it to the app on stdout; no environment override or persisted token is used.
func Token() (string, error) {
	var bytes [32]byte
	_, err := rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:]), err
}

// Listen refuses DNS names and wildcard addresses before opening a socket, so
// name resolution can never turn a local launch into a public listener.
func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errLoopback
	}
	return net.Listen("tcp", address)
}

func allowedOrigin(origin string) bool {
	switch origin {
	case "tauri://localhost", "http://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420":
		return true
	}
	return false
}

func loopbackHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return false
	}
	if name == "localhost" {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

// Guard is the shared boundary for app routes and future factory mounts.
// ORIGIN AND HOST COME BEFORE AUTHENTICATION, even for an unknown route.
// Browser preflight carries no credential; an allowed preflight describes
// the transport only and never invokes a resource handler.
func Guard(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		origins, present := r.Header["Origin"]
		if present && (len(origins) != 1 || !allowedOrigin(origins[0])) {
			fail(w, http.StatusForbidden, "not_allowed", foreignOriginSentence)
			return
		}
		if !loopbackHost(r.Host) {
			fail(w, http.StatusMisdirectedRequest, "not_allowed", foreignOriginSentence)
			return
		}
		if present {
			w.Header().Set("Access-Control-Allow-Origin", origins[0])
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		authorization := r.Header.Values("Authorization")
		if token == "" || len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization[0], "Bearer ")), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			fail(w, http.StatusUnauthorized, "not_allowed", "engine connection required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
