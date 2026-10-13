package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type SearchRow struct {
	Kind       string     `json:"kind"`
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Source     *SourceRef `json:"source,omitempty"`
	Path       string     `json:"path,omitempty"`
	State      string     `json:"state,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}
type SearchGroup struct {
	Kind string      `json:"kind" jsonschema:"enum=agents_crews,enum=jobs,enum=files"`
	Rows []SearchRow `json:"rows" jsonschema:"maxItems=4"`
}
type SearchResponse struct {
	Groups []SearchGroup `json:"groups"`
}
type HistoryResponse struct {
	Agents []Agent `json:"agents"`
	Crews  []Crew  `json:"crews"`
}
type Activity struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Verb   string    `json:"verb"`
	Object SourceRef `json:"object"`
	Detail string    `json:"detail"`
}
type ActivityResponse struct {
	Items []Activity `json:"items"`
}

func searchContract() resourceContract {
	return resourceContract{Name: "search", Types: []any{SearchRow{}, SearchGroup{}, SearchResponse{}, HistoryResponse{}, Activity{}, ActivityResponse{}}, Routes: searchContractRoutes, Events: []eventContract{{"activity", Activity{}}}}
}
