package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type QuickTask struct {
	ID      string          `json:"id"`
	AgentID string          `json:"agent_id"`
	Title   string          `json:"title"`
	Files   []QuickTaskFile `json:"files"`
	Added   int             `json:"added"`
	Removed int             `json:"removed"`
	State   string          `json:"state" jsonschema:"enum=running,enum=done,enum=undone,enum=promoted"`
}
type QuickTaskFile struct {
	Path       string `json:"path"`
	BeforeHash string `json:"before_hash"`
	AfterHash  string `json:"after_hash"`
}
type QuickTasksResponse []QuickTask
type UndoQuickTaskResponse struct {
	Undone []string `json:"undone"`
}
type QuickTaskChangedEvent struct {
	Agent     string `json:"agent"`
	QuickTask string `json:"quick_task"`
	State     string `json:"state"`
}

func quickTaskContract() resourceContract {
	return resourceContract{Name: "quick_task", Types: []any{QuickTask{}, QuickTaskFile{}, QuickTasksResponse{}, UndoQuickTaskResponse{}, QuickTaskChangedEvent{}}, Routes: quickTaskContractRoutes, Events: []eventContract{{"quick_task.changed", QuickTaskChangedEvent{}}}}
}
