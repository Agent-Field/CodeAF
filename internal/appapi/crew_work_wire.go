package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type CrewItem struct {
	Ref string `json:"ref,omitempty"`
	URL string `json:"url,omitempty"`
}
type CrewItemRequest struct {
	Ref    string       `json:"ref,omitempty"`
	URL    string       `json:"url,omitempty"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type CrewLogEntry struct {
	Crew string     `json:"crew"`
	From string     `json:"from"`
	To   string     `json:"to"`
	Text string     `json:"text"`
	At   *time.Time `json:"at,omitempty"`
}
type CrewLogResponse struct {
	Entries []CrewLogEntry `json:"entries"`
	Before  string         `json:"before,omitempty"`
}
type CrewSummaryResponse struct {
	Since time.Time   `json:"since"`
	Text  string      `json:"text"`
	Items []SourceRef `json:"items"`
}
type CrewReport struct {
	Shipped   string           `json:"shipped"`
	Decisions []string         `json:"decisions"`
	Files     []FileChange     `json:"files"`
	LeftOpen  []ReportOpenItem `json:"left_open"`
	Cost      Money            `json:"cost"`
	Markdown  string           `json:"markdown"`
}
type ReportOpenItem struct {
	Text    string     `json:"text"`
	HandOff *SourceRef `json:"hand_off,omitempty"`
}
type CrewLogEvent struct {
	Crew string `json:"crew"`
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
}
type ReportReadyEvent struct {
	Crew string `json:"crew"`
}

func crewWorkContract() resourceContract {
	return resourceContract{Name: "crew_work", Types: []any{CrewItem{}, CrewItemRequest{}, CrewLogEntry{}, CrewLogResponse{}, CrewSummaryResponse{}, CrewReport{}, ReportOpenItem{}, CrewLogEvent{}, ReportReadyEvent{}}, Routes: crewWorkContractRoutes, Events: []eventContract{{"crew.log", CrewLogEvent{}}, {"report.ready", ReportReadyEvent{}}}}
}
