//go:build !windows

// Package bus is the in-process event bus. Subscriber snapshots are invoked
// synchronously in registration order. All mutable state is protected for
// concurrent publishers/subscribers.
package bus

import (
	"sync"

	idpkg "github.com/Agent-Field/codeaf/internal/seniordev/id"
)

// Payload is the wire event delivered to subscribers.
type Payload struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Properties any    `json:"properties"`
}

// Context is the instance metadata a bus is created for.
type Context struct {
	Directory string
	Project   string
	Workspace string
}

// PublishOptions lets a publisher pin the payload ID.
type PublishOptions struct {
	ID string
}

type subscriber struct {
	id       uint64
	callback func(Payload)
}

// Bus is an instance-scoped pub/sub bus.
type Bus struct {
	mu       sync.RWMutex
	nextID   uint64
	typed    map[string][]subscriber
	wildcard []subscriber
	context  Context
	createID func() string
	disposed bool
	streams  map[*Subscription]struct{}
}

// BusOption configures New.
type BusOption func(*Bus)

// WithIDGenerator pins payload IDs.
func WithIDGenerator(createID func() string) BusOption {
	return func(bus *Bus) { bus.createID = createID }
}

// New constructs an instance bus.
func New(context Context, options ...BusOption) *Bus {
	bus := &Bus{
		typed:    make(map[string][]subscriber),
		context:  context,
		createID: CreateID,
		streams:  make(map[*Subscription]struct{}),
	}
	for _, option := range options {
		option(bus)
	}
	return bus
}

// CreateID creates an ascending evt identifier.
func CreateID() string {
	value, err := idpkg.Create("evt", idpkg.AscendingDirection)
	if err != nil {
		panic(err)
	}
	return value
}

// Publish delivers to typed subscribers, then to wildcard subscribers, in
// that order.
func (b *Bus) Publish(def Definition, properties any, options ...PublishOptions) {
	id := ""
	if len(options) > 0 {
		id = options[0].ID
	}
	if id == "" {
		id = b.createID()
	}
	payload := Payload{ID: id, Type: def.Type, Properties: properties}

	b.mu.RLock()
	if b.disposed {
		b.mu.RUnlock()
		return
	}
	typed := append([]subscriber(nil), b.typed[def.Type]...)
	wildcard := append([]subscriber(nil), b.wildcard...)
	b.mu.RUnlock()

	deliver(typed, payload)
	deliver(wildcard, payload)
}

// SubscribeCallback subscribes to one event definition.
func (b *Bus) SubscribeCallback(def Definition, callback func(Payload)) func() {
	return b.subscribe(def.Type, callback, false)
}

// SubscribeAllCallback subscribes to every event.
func (b *Bus) SubscribeAllCallback(callback func(Payload)) func() {
	return b.subscribe("*", callback, true)
}

func (b *Bus) subscribe(eventType string, callback func(Payload), all bool) func() {
	b.mu.Lock()
	if b.disposed {
		b.mu.Unlock()
		return func() {}
	}
	b.nextID++
	id := b.nextID
	item := subscriber{id: id, callback: callback}
	if all {
		b.wildcard = append(b.wildcard, item)
	} else {
		b.typed[eventType] = append(b.typed[eventType], item)
	}
	b.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if all {
				b.wildcard = removeSubscriber(b.wildcard, id)
				return
			}
			b.typed[eventType] = removeSubscriber(b.typed[eventType], id)
		})
	}
}

func removeSubscriber(subscribers []subscriber, id uint64) []subscriber {
	for i, item := range subscribers {
		if item.id == id {
			return append(subscribers[:i], subscribers[i+1:]...)
		}
	}
	return subscribers
}

func deliver(subscribers []subscriber, payload Payload) {
	for _, item := range subscribers {
		func() {
			defer func() { _ = recover() }()
			item.callback(payload)
		}()
	}
}

// Dispose publishes InstanceDisposed to wildcard subscribers only, then closes
// streams and makes later publishes/subscriptions inert.
func (b *Bus) Dispose() {
	b.mu.Lock()
	if b.disposed {
		b.mu.Unlock()
		return
	}
	b.disposed = true
	wildcard := append([]subscriber(nil), b.wildcard...)
	streams := make([]*Subscription, 0, len(b.streams))
	for stream := range b.streams {
		streams = append(streams, stream)
	}
	b.typed = make(map[string][]subscriber)
	b.wildcard = nil
	b.streams = make(map[*Subscription]struct{})
	directory := b.context.Directory
	b.mu.Unlock()

	deliver(wildcard, Payload{
		ID:         b.createID(),
		Type:       InstanceDisposed.Type,
		Properties: map[string]any{"directory": directory},
	})
	for _, stream := range streams {
		stream.close()
	}
}

// Subscription is an unbounded ordered stream subscription.
type Subscription struct {
	C <-chan Payload

	out       chan Payload
	mu        sync.Mutex
	cond      *sync.Cond
	queue     []Payload
	closed    bool
	closeOnce sync.Once
	unsub     func()
}

// Subscribe returns a typed stream. Call Close when finished.
func (b *Bus) Subscribe(def Definition) *Subscription {
	return b.newStream(func(push func(Payload)) func() {
		return b.SubscribeCallback(def, push)
	})
}

// SubscribeAll returns a wildcard stream.
func (b *Bus) SubscribeAll() *Subscription {
	return b.newStream(func(push func(Payload)) func() {
		return b.SubscribeAllCallback(push)
	})
}

func (b *Bus) newStream(register func(func(Payload)) func()) *Subscription {
	out := make(chan Payload)
	subscription := &Subscription{out: out}
	subscription.C = out
	subscription.cond = sync.NewCond(&subscription.mu)
	subscription.unsub = register(subscription.push)
	b.mu.Lock()
	if b.disposed {
		b.mu.Unlock()
		subscription.close()
		return subscription
	}
	b.streams[subscription] = struct{}{}
	b.mu.Unlock()
	go subscription.run()
	return subscription
}

func (s *Subscription) push(payload Payload) {
	s.mu.Lock()
	if !s.closed {
		s.queue = append(s.queue, payload)
		s.cond.Signal()
	}
	s.mu.Unlock()
}

func (s *Subscription) run() {
	defer close(s.out)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if len(s.queue) == 0 && s.closed {
			s.mu.Unlock()
			return
		}
		payload := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		s.out <- payload
	}
}

// Close unsubscribes and closes C after already queued events are delivered.
func (s *Subscription) Close() { s.close() }

func (s *Subscription) close() {
	s.closeOnce.Do(func() {
		if s.unsub != nil {
			s.unsub()
		}
		s.mu.Lock()
		s.closed = true
		s.cond.Broadcast()
		s.mu.Unlock()
	})
}

// Default is the package-level runtime used by the convenience functions.
var Default = New(Context{})

// Publish emits on Default.
func Publish(def Definition, properties any, options ...PublishOptions) {
	Default.Publish(def, properties, options...)
}

// SubscribeCallback subscribes on Default.
func SubscribeCallback(def Definition, callback func(Payload)) func() {
	return Default.SubscribeCallback(def, callback)
}

// SubscribeAllCallback subscribes on Default.
func SubscribeAllCallback(callback func(Payload)) func() {
	return Default.SubscribeAllCallback(callback)
}
