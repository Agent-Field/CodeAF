package main

import (
	"strings"
	"sync"
)

// factorySessions holds the signed-in factory sessions this process works
// with, one per factory address.
//
// A SESSION IS A PERSON'S CREDENTIAL, never GitHub's token: it is what the
// factory issued once that person finished GitHub's device sign-in
// (internal/factory/serve's device flow), and the runner that carries it works
// as that person. It lives in memory only, so it is never written to a file a
// scan for secrets would read; the one file that holds one is attach.json,
// which `codeaf runner join` writes privately (factory_attach.go).
type factorySessions interface {
	// FactorySession is the session held for address, or "".
	FactorySession(address string) string
	// SetFactorySession keeps token as address's session; "" forgets it.
	SetFactorySession(address, token string)
	// Sessions is a copy of every session held, keyed by address.
	Sessions() map[string]string
}

// memoryFactorySessions is the process's own book of sessions.
type memoryFactorySessions struct {
	mu   sync.Mutex
	rows map[string]string
}

func newFactorySessions() *memoryFactorySessions {
	return &memoryFactorySessions{rows: map[string]string{}}
}

func (m *memoryFactorySessions) FactorySession(address string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[address]
}

func (m *memoryFactorySessions) SetFactorySession(address, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(token) == "" {
		delete(m.rows, address)
		return
	}
	m.rows[address] = token
}

func (m *memoryFactorySessions) Sessions() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.rows))
	for k, v := range m.rows {
		out[k] = v
	}
	return out
}

// factoryWorkerPath names the routes a runner authenticates with its own
// credential. A proxy in front of the factory must pass that credential
// through rather than substitute the host's token for it.
func factoryWorkerPath(path string) bool {
	return path == "/v1/workers/pull" || strings.HasPrefix(path, "/v1/workers/leases/") ||
		path == "/v1/runners/pull" || path == "/v1/runners/presence" || strings.HasPrefix(path, "/v1/runners/leases/")
}
