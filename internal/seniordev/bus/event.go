//go:build !windows

// Event-definition registry. Registry iteration preserves first-definition
// order.
package bus

import "sync"

// Definition identifies an event type and carries its consumer-supplied
// property schema/descriptor.
type Definition struct {
	Type       string `json:"type"`
	Properties any    `json:"properties"`
}

// PayloadDefinition describes one registered event type and its property
// schema.
type PayloadDefinition struct {
	Type       string `json:"type"`
	Properties any    `json:"properties"`
	Identifier string `json:"identifier"`
}

var definitions = struct {
	sync.RWMutex
	order []string
	byID  map[string]Definition
}{byID: make(map[string]Definition)}

// Define registers and returns an event definition. Redefining a type updates
// its schema without changing its original insertion position.
func Define(eventType string, properties any) Definition {
	definitions.Lock()
	defer definitions.Unlock()
	if _, exists := definitions.byID[eventType]; !exists {
		definitions.order = append(definitions.order, eventType)
	}
	result := Definition{Type: eventType, Properties: properties}
	definitions.byID[eventType] = result
	return result
}

// Payloads returns the payload descriptors in registry order.
func Payloads() []PayloadDefinition {
	return payloadDefinitions()
}

func payloadDefinitions() []PayloadDefinition {
	definitions.RLock()
	defer definitions.RUnlock()
	out := make([]PayloadDefinition, 0, len(definitions.order))
	for _, eventType := range definitions.order {
		def := definitions.byID[eventType]
		out = append(out, PayloadDefinition{
			Type:       eventType,
			Properties: def.Properties,
			Identifier: "Event." + eventType,
		})
	}
	return out
}

// InstanceDisposed is published to wildcard subscribers during Bus.Dispose.
var InstanceDisposed = Define("server.instance.disposed", struct {
	Directory string `json:"directory"`
}{})
