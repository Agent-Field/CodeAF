package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Handler runs one bridge method after its permission check.
type Handler func(ctx context.Context, all *svc.Services, scope svc.Scope, params json.RawMessage) (any, error)

type AllowFunc = caps.AllowFunc

// Method carries its own permission; nil Allow retains the wave-1 capability checks.
type Method struct {
	Global bool
	Handle Handler
	Allow  AllowFunc
}

var registry = struct {
	sync.RWMutex
	methods    map[string]Method
	disconnect []func(svc.Caller)
}{methods: make(map[string]Method)}

// Register reserves names at startup so tracks cannot replace each other's methods.
func Register(name string, method Method) {
	registry.Lock()
	defer registry.Unlock()
	if _, exists := registry.methods[name]; exists {
		panic("gateway: method " + name + " registered twice")
	}
	caps.RegisterAllow(name, method.Allow)
	registry.methods[name] = method
}

func lookupMethod(name string) (Method, bool) {
	registry.RLock()
	defer registry.RUnlock()
	method, ok := registry.methods[name]
	return method, ok
}

// OnDisconnect registers socket cleanup in order, independent of the app last called.
func OnDisconnect(f func(svc.Caller)) {
	registry.Lock()
	defer registry.Unlock()
	registry.disconnect = append(registry.disconnect, f)
}

func runDisconnectHooks(caller svc.Caller) {
	registry.RLock()
	hooks := append([]func(svc.Caller){}, registry.disconnect...)
	registry.RUnlock()
	for _, hook := range hooks {
		runDisconnectHook(hook, caller)
	}
}

func runDisconnectHook(hook func(svc.Caller), caller svc.Caller) {
	defer func() {
		if value := recover(); value != nil {
			slog.Error("apps disconnect hook panicked", "panic", value)
		}
	}()
	hook(caller)
}
