// Process-global event emitter — port of src/bus/global.ts:4-30
// (swe-pro 3b25a1a).
package bus

import (
	"reflect"
	"sync"

	idpkg "github.com/Agent-Field/swe-pro-go/internal/id"
)

// GlobalEvent is the event envelope shared across instance buses.
type GlobalEvent struct {
	Directory string `json:"directory,omitempty"`
	Project   string `json:"project,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Payload   any    `json:"payload"`
}

// GlobalEmitter is a goroutine-safe, registration-ordered EventEmitter for
// the sole "event" event name used by the TS source.
type GlobalEmitter struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers []globalSubscriber
	createID    func() string
}

type globalSubscriber struct {
	id       uint64
	callback func(GlobalEvent)
}

// NewGlobalEmitter constructs an isolated global emitter.
func NewGlobalEmitter(createID func() string) *GlobalEmitter {
	if createID == nil {
		createID = CreateID
	}
	return &GlobalEmitter{createID: createID}
}

// On subscribes to "event" and returns an idempotent unsubscribe function.
func (g *GlobalEmitter) On(callback func(GlobalEvent)) func() {
	g.mu.Lock()
	g.nextID++
	id := g.nextID
	g.subscribers = append(g.subscribers, globalSubscriber{id: id, callback: callback})
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			for i, subscriber := range g.subscribers {
				if subscriber.id == id {
					g.subscribers = append(g.subscribers[:i], g.subscribers[i+1:]...)
					return
				}
			}
		})
	}
}

// Emit stamps an absent payload id, then invokes current subscribers in
// registration order. A subscriber panic is isolated like the bus callback
// bridge's caught rejection.
func (g *GlobalEmitter) Emit(event GlobalEvent) bool {
	stampPayloadID(event.Payload, g.createID)
	g.mu.RLock()
	snapshot := append([]globalSubscriber(nil), g.subscribers...)
	g.mu.RUnlock()
	for _, subscriber := range snapshot {
		safeGlobalCallback(subscriber.callback, event)
	}
	return len(snapshot) > 0
}

func safeGlobalCallback(callback func(GlobalEvent), event GlobalEvent) {
	defer func() { _ = recover() }()
	callback(event)
}

func stampPayloadID(payload any, createID func() string) {
	value, ok := payload.(map[string]any)
	if ok {
		if _, exists := value["id"]; exists {
			return
		}
		id := ""
		if syncEvent, ok := value["syncEvent"].(map[string]any); ok {
			id, _ = syncEvent["id"].(string)
		}
		if id == "" {
			id = createID()
		}
		value["id"] = id
		return
	}
	// Accommodate named map types without treating structs/slices as mutable
	// JavaScript objects.
	rv := reflect.ValueOf(payload)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return
	}
	idKey := reflect.ValueOf("id").Convert(rv.Type().Key())
	if rv.MapIndex(idKey).IsValid() {
		return
	}
	if rv.Type().Elem().Kind() == reflect.Interface {
		rv.SetMapIndex(idKey, reflect.ValueOf(createID()))
	}
}

// GlobalBus is the process-global emitter from global.ts.
var GlobalBus = NewGlobalEmitter(nil)

func defaultCreateID() string {
	value, err := idpkg.Create("evt", idpkg.AscendingDirection)
	if err != nil {
		panic(err)
	}
	return value
}
