//go:build !windows

// Event names and payloads. The runtime's bus owns registration and
// delivery; this package owns the public names, versions, aggregate key, and
// wire payload shapes.
package msgmodel

const (
	EventMessageUpdated     = "message.updated"
	EventMessageRemoved     = "message.removed"
	EventMessagePartUpdated = "message.part.updated"
	EventMessagePartDelta   = "message.part.delta"
	EventMessagePartRemoved = "message.part.removed"

	SyncEventVersion = 1
	SyncAggregateKey = "sessionID"
)

type UpdatedEvent struct {
	SessionID string `json:"sessionID"`
	Info      Info   `json:"info"`
}

type RemovedEvent struct {
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
}

type PartUpdatedEvent struct {
	SessionID string `json:"sessionID"`
	Part      Part   `json:"part"`
	Time      uint64 `json:"time"`
}

type PartDeltaEvent struct {
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	PartID    string `json:"partID"`
	Field     string `json:"field"`
	Delta     string `json:"delta"`
}

type PartRemovedEvent struct {
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	PartID    string `json:"partID"`
}
