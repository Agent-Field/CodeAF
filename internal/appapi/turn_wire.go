package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
// DisplayEntry is shaped by the app codec; session.DisplayEntry is not the app wire.
type DisplayEntry struct {
	ID       string     `json:"id,omitempty"`
	Kind     string     `json:"kind"`
	Role     string     `json:"role,omitempty"`
	Text     string     `json:"text"`
	By       string     `json:"by,omitempty"`
	At       *time.Time `json:"at,omitempty"`
	Link     *SourceRef `json:"link,omitempty"`
	CallID   string     `json:"call_id,omitempty"`
	Tool     string     `json:"tool,omitempty"`
	Args     string     `json:"args,omitempty"`
	Output   string     `json:"output,omitempty"`
	Answered bool       `json:"answered,omitempty"`
	Card     *Ask       `json:"card,omitempty"`
}
type AgentResponse struct {
	Agent
	Snapshot AgentSnapshot `json:"snapshot"`
}
type AgentSnapshot struct {
	Asleep        bool           `json:"asleep"`
	DayCapReached bool           `json:"day_cap_reached"`
	Entries       []DisplayEntry `json:"entries"`
	Queue         []QueueItem    `json:"queue"`
	OpenAsks      []Ask          `json:"open_asks"`
	Tasks         TasksResponse  `json:"tasks"`
	Usage         Usage          `json:"usage"`
}
type Usage struct {
	In    int    `json:"in"`
	Out   int    `json:"out"`
	Cost  *Money `json:"cost,omitempty"`
	Turns int    `json:"turns"`
	Calls int    `json:"calls"`
}
type TurnRequest struct {
	Text    string         `json:"text"`
	Mode    string         `json:"mode" jsonschema:"enum=send,enum=steer,enum=queue"`
	Context []AgentContext `json:"context,omitempty"`
	Files   []TurnFile     `json:"files,omitempty"`
	Expect  *AgentExpect   `json:"expect,omitempty"`
}
type TurnFile struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	MIME string `json:"mime,omitempty"`
}
type TurnResponse struct {
	Accepted bool   `json:"accepted"`
	TurnID   string `json:"turn_id"`
}
type ReorderQueueRequest struct {
	Order  []string     `json:"order"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type QueueItem struct {
	ID      string         `json:"id"`
	Text    string         `json:"text"`
	Context []AgentContext `json:"context,omitempty"`
	Files   []TurnFile     `json:"files,omitempty"`
}
type MoveReplyResponse struct {
	TaskID string `json:"task_id"`
}
type EntriesResponse struct {
	Entries []DisplayEntry `json:"entries"`
	Before  string         `json:"before,omitempty"`
	HasMore bool           `json:"has_more"`
}
type ToolResponse struct {
	Output string `json:"output"`
	Full   bool   `json:"full"`
}
type TurnLifecycleEvent struct {
	Agent   string        `json:"agent"`
	Turn    string        `json:"turn"`
	How     string        `json:"how,omitempty" jsonschema:"enum=done,enum=stopped,enum=failed"`
	Receipt *DisplayEntry `json:"receipt,omitempty"`
}
type TurnEntryEvent struct {
	Agent string       `json:"agent"`
	Entry DisplayEntry `json:"entry"`
}
type TurnLongEvent struct {
	Agent  string `json:"agent"`
	Turn   string `json:"turn"`
	AfterS int    `json:"after_s" jsonschema:"enum=180"`
}
type QueueChangedEvent struct {
	Agent string      `json:"agent"`
	Queue []QueueItem `json:"queue"`
}
type TitleChangedEvent struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
}

func turnContract() resourceContract {
	return resourceContract{Name: "turn", Types: []any{DisplayEntry{}, AgentResponse{}, AgentSnapshot{}, Usage{}, TurnRequest{}, TurnFile{}, TurnResponse{}, ReorderQueueRequest{}, QueueItem{}, MoveReplyResponse{}, EntriesResponse{}, ToolResponse{}, TurnLifecycleEvent{}, TurnEntryEvent{}, TurnLongEvent{}, QueueChangedEvent{}, TitleChangedEvent{}, TurnDeltaEvent{}}, Routes: turnContractRoutes, Events: []eventContract{{"turn.started", TurnLifecycleEvent{}}, {"turn.ended", TurnLifecycleEvent{}}, {"turn.delta", TurnDeltaEvent{}}, {"turn.entry", TurnEntryEvent{}}, {"turn.long", TurnLongEvent{}}, {"queue.changed", QueueChangedEvent{}}, {"title.changed", TitleChangedEvent{}}}}
}
