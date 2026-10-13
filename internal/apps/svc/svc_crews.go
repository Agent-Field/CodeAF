package svc

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"time"
)

type Lifecycle interface {
	Archive(context.Context, Scope) (bridge.AppSummary, error)
	Restore(context.Context, Scope) (bridge.AppSummary, error)
	Delete(context.Context, Scope, bridge.DeleteAppParams) error
	Copy(context.Context, Scope, bridge.CopyAppParams) (bridge.AppSummary, error)
	List(context.Context, Caller) (bridge.LifecycleResult, error)
}
type Settings interface {
	ListTriggers(context.Context, Scope) (bridge.ListTriggersResult, error)
	PauseTrigger(context.Context, Scope, bridge.PauseTriggerParams) (bridge.TriggerInfo, error)
	GetBudget(context.Context, Scope) (bridge.BudgetInfo, error)
	SetBudget(context.Context, Scope, bridge.SetBudgetParams) (bridge.BudgetInfo, error)
}
type CrewLinks interface {
	Add(context.Context, Scope, bridge.AddLinkParams) (bridge.CrewLink, error)
	Remove(context.Context, Scope, bridge.RemoveLinkParams) error
	ForRecord(context.Context, Scope, bridge.LinksForRecordParams) (bridge.ListLinksResult, error)
	ForCrew(context.Context, Caller, bridge.LinksForCrewParams) (bridge.ListLinksResult, error)
}
type CrewAgent interface {
	Badge(context.Context, Scope) (bridge.Badge, error)
	Candidates(context.Context, Caller) (bridge.CrewAgentCandidatesResult, error)
	Ask(context.Context, CrewAsk) (CrewAnswer, error)
}
type CrewAsk struct {
	App, Crew, CrewName string
	Asker               Caller
	Text                string
	Linked              []bridge.Record
}
type CrewAnswer struct {
	Text      string
	Changed   []ChangedRecord
	Approvals []bridge.Approval
	ShapeThis string
	Cost      float64
}
type ChangedRecord struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Label   string `json:"label"`
	What    string `json:"what"`
	Version int    `json:"version"`
}
type Prebuilt interface {
	List(context.Context, Caller) (bridge.PrebuiltResult, error)
}
type Custom interface {
	Bundle(context.Context, Scope, bridge.CustomBundleParams) (bridge.CustomBundleResult, error)
}

// TriggerGate records a fire and skips events while paused or archived.
type TriggerGate interface {
	Allow(app, trigger string, at time.Time) bool
}

// BudgetCap overrides the monthly budget from the app definition.
type BudgetCap interface {
	Month(app string) (usd float64, ok bool)
}

// ActivityWriter is a placeholder until the spaces track supplies its writer.
type ActivityWriter interface {
	Write(context.Context, Scope, string) error
}

type CrewsServices struct {
	Lifecycle   Lifecycle
	Settings    Settings
	Links       CrewLinks
	Agent       CrewAgent
	Prebuilt    Prebuilt
	Custom      Custom
	TriggerGate TriggerGate
	BudgetCap   BudgetCap
	Activity    ActivityWriter
}

// RoleOf keeps crews checks on the caller seam as workspace roles arrive.
func RoleOf(caller Caller) Role { return caller.Role }
