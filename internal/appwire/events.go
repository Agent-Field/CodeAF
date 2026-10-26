package appwire

import "reflect"

// eventShapes is the type each event's data is. An event emitted with any
// other type is a contract break; the test recorders check every event they
// see against this table, and the schema is generated from it.
var eventShapes = map[string]reflect.Type{
	EventSyncChanged:       reflect.TypeFor[SyncChanged](),
	EventHello:             reflect.TypeFor[Hello](),
	EventAgentChanged:      reflect.TypeFor[AgentChanged](),
	EventTurnStarted:       reflect.TypeFor[TurnStarted](),
	EventTurnDelta:         reflect.TypeFor[TurnDelta](),
	EventTurnTool:          reflect.TypeFor[TurnTool](),
	EventTurnDone:          reflect.TypeFor[TurnDone](),
	EventTurnThinking:      reflect.TypeFor[TurnThinking](),
	EventTurnThinkingDone:  reflect.TypeFor[TurnThinkingDone](),
	EventTurnRetrying:      reflect.TypeFor[TurnRetrying](),
	EventTurnLong:          reflect.TypeFor[TurnLong](),
	EventCrewProposalDelta: reflect.TypeFor[CrewProposalDelta](),
	EventAskOpened:         reflect.TypeFor[AskOpened](),
	EventAskClosed:         reflect.TypeFor[AskClosed](),
	EventEntryAdded:        reflect.TypeFor[EntryAdded](),
	EventSpendChanged:      reflect.TypeFor[SpendChanged](),
	EventTaskChanged:       reflect.TypeFor[TaskChanged](),
	EventQueueChanged:      reflect.TypeFor[QueueChanged](),
	EventNoteChanged:       reflect.TypeFor[NoteChanged](),
	EventQuickChanged:      reflect.TypeFor[QuickChangedEvent](),
	EventProcessChanged:    reflect.TypeFor[ProcessChanged](),
	EventProcessOutput:     reflect.TypeFor[ProcessOutput](),
	EventToolOutput:        reflect.TypeFor[ToolOutputEvent](),
	EventCrewChanged:       reflect.TypeFor[CrewChanged](),
	EventCrewRemoved:       reflect.TypeFor[CrewRemoved](),
	EventCrewLog:           reflect.TypeFor[CrewLogEvent](),
	EventMemberChanged:     reflect.TypeFor[MemberChanged](),
	EventDecisionOpened:    reflect.TypeFor[DecisionEvent](),
	EventDecisionChanged:   reflect.TypeFor[DecisionEvent](),
	EventInboxAdded:        reflect.TypeFor[InboxAddedEvent](),
	EventInboxResolved:     reflect.TypeFor[InboxResolvedEvent](),
	EventInboxCounts:       reflect.TypeFor[InboxCounts](),
	EventOrderChanged:      reflect.TypeFor[OrderChanged](),
	EventOrderRemoved:      reflect.TypeFor[OrderRemoved](),
	EventOrderFired:        reflect.TypeFor[OrderFired](),
	EventOrderMissed:       reflect.TypeFor[OrderChanged](),
	EventOrderAsking:       reflect.TypeFor[OrderFired](),
	EventSettingsChanged:   reflect.TypeFor[SettingsChanged](),
	EventProviderChanged:   reflect.TypeFor[ProviderChanged](),
	EventLog:               reflect.TypeFor[Log](),

	EventCollectionChanged: reflect.TypeFor[CollectionChanged](),
}
