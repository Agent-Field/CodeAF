package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type MemoryShelf struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type MemoryItem struct {
	// Native status and journal sequence make conditional edits usable by clients.
	State   string    `json:"state" jsonschema:"enum=active,enum=forgotten,enum=superseded"`
	Version uint64    `json:"version"`
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Shelf   string    `json:"shelf"`
	MadeBy  string    `json:"made_by"`
	At      time.Time `json:"at"`
}
type MemoryResponse struct {
	Shelves []MemoryShelf `json:"shelves"`
	Items   []MemoryItem  `json:"items"`
}
type PatchMemoryRequest struct {
	Text   *string      `json:"text,omitempty"`
	Shelf  *string      `json:"shelf,omitempty"`
	Expect *AgentExpect `json:"expect,omitempty"`
}

func memoryContract() resourceContract {
	return resourceContract{Name: "memory", Types: []any{MemoryShelf{}, MemoryItem{}, MemoryResponse{}, PatchMemoryRequest{}}, Routes: memoryContractRoutes, Events: []eventContract{{"memory.changed", IDChangedEvent{}}}}
}
