package appapi

// EventType is the app vocabulary, independent of the engine's internal names.
// Resource lanes publish small facts here and keep complete objects on GET routes.
type EventType string

const (
	EventAgentCreated     EventType = "agent.created"
	EventAgentChanged     EventType = "agent.changed"
	EventAgentRemoved     EventType = "agent.removed"
	EventTurnStarted      EventType = "turn.started"
	EventTurnEnded        EventType = "turn.ended"
	EventTurnDelta        EventType = "turn.delta"
	EventTurnEntry        EventType = "turn.entry"
	EventTurnLong         EventType = "turn.long"
	EventQueueChanged     EventType = "queue.changed"
	EventAskCreated       EventType = "ask.created"
	EventAskResolved      EventType = "ask.resolved"
	EventAskWithdrawn     EventType = "ask.withdrawn"
	EventTaskCreated      EventType = "task.created"
	EventTaskChanged      EventType = "task.changed"
	EventQuickTaskChanged EventType = "quick_task.changed"
	EventProcessChanged   EventType = "process.changed"
	EventProcessOutput    EventType = "process.output"
	EventOrderChanged     EventType = "order.changed"
	EventOrderFired       EventType = "order.fired"
	EventCrewCreated      EventType = "crew.created"
	EventCrewChanged      EventType = "crew.changed"
	EventCrewLog          EventType = "crew.log"
	EventReportReady      EventType = "report.ready"
	EventRoleChanged      EventType = "role.changed"
	EventRuleCreated      EventType = "rule.created"
	EventRuleChanged      EventType = "rule.changed"
	EventMemoryChanged    EventType = "memory.changed"
	EventSettingsChanged  EventType = "settings.changed"
	EventKeysChanged      EventType = "keys.changed"
	EventInboxAdded       EventType = "inbox.added"
	EventInboxResolved    EventType = "inbox.resolved"
	EventInboxUndoWindow  EventType = "inbox.undo_window"
	EventSpendChanged     EventType = "spend.changed"
	EventFactoryChanged   EventType = "factory.changed"
	EventFactoryEvent     EventType = "factory.event"
	EventActivity         EventType = "activity"
	EventTitleChanged     EventType = "title.changed"
)

func (kind EventType) publishable() bool {
	switch kind {
	case EventAgentCreated, EventAgentChanged, EventAgentRemoved,
		EventTurnStarted, EventTurnEnded, EventTurnDelta, EventTurnEntry, EventTurnLong,
		EventQueueChanged, EventAskCreated, EventAskResolved, EventAskWithdrawn,
		EventTaskCreated, EventTaskChanged, EventQuickTaskChanged, EventProcessChanged,
		EventOrderChanged, EventOrderFired, EventCrewCreated, EventCrewChanged,
		EventCrewLog, EventReportReady, EventRoleChanged, EventRuleCreated, EventRuleChanged,
		EventMemoryChanged, EventSettingsChanged, EventKeysChanged, EventInboxAdded,
		EventInboxResolved, EventInboxUndoWindow, EventSpendChanged, EventFactoryChanged,
		EventFactoryEvent, EventActivity, EventTitleChanged:
		return true
	}
	// Process output and AI deltas belong to their own follow responses. Hello
	// and reset belong to the transport, so publishers cannot forge them.
	return false
}

// AgentEvent names changed fields without retaining another full agent snapshot.
type AgentEvent struct {
	Agent  string   `json:"agent"`
	Fields []string `json:"fields"`
}

// TurnDeltaEvent is the text-bearing exception to the small-facts rule.
type TurnDeltaEvent struct {
	Agent string `json:"agent"`
	Turn  string `json:"turn"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

type streamHello struct {
	Epoch         string `json:"epoch"`
	CodeafVersion string `json:"codeaf_version"`
	Contract      int    `json:"contract"`
}

type streamReset struct {
	Epoch  string `json:"epoch"`
	Reason string `json:"reason"`
}
