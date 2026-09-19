package session

// RoleOrganize is the tick's organizer: evidence in, a typed plan out, spend
// through callRoleChecked. It never routes through RoleAuditor.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func init() {
	roles.Register(roles.RoleOrganize, roles.TierLow, "files a chat in folders from cited evidence")
}

const (
	// PlanNoAction is first-class organizer output: record the proposal, write
	// no membership.
	PlanNoAction     = "no-action"
	PlanAdd          = "add"
	PlanRemove       = "remove"
	PlanMove         = "move"
	PlanCreateFolder = "create-folder"
)

// OrganizeRequest is what the tick hands the runner after it has leased a job
// and gathered lexical plus vector (or labelled expansion) candidates.
// Hierarchy is the live folder id/name list CONTRACTS.md names; without it the
// model can only return no-action, because add needs a collection_id.
type OrganizeRequest struct {
	ChatID, SourceRev, Evidence, Hierarchy string
	High                                   bool // restructuring or instruction conflicts
	Degraded                               bool
}

// OrganizePlan is the typed organizer output the tick validates and applies.
// Unknown Kind is invalid. no-action carries empty Actions.
type OrganizePlan struct {
	Kind, ChatID, SourceRev, Model, PromptVersion string
	Actions                                       []json.RawMessage
	Degraded                                      bool
}

const organizeSystem = "You organize chats into folders from cited evidence only. Similarity scores are never membership. Answer with one JSON object."

const organizePrompt = `Given the evidence and folders, return one JSON object:
{"kind":"no-action"|"add"|"remove"|"move"|"create-folder","chat_id":"...","source_rev":"...","actions":[],"degraded":false}
kind add requires actions like [{"kind":"add","collection_id":"<id from folders>","ref":{"kind":"conversation","id":"<chat_id>"},"reason":"..."}].
Add this chat to a folder when cited evidence from a chat already in that folder shows the same purpose. Dual membership is allowed. kind no-action when already filed there, overlap is weak, or no folder id to cite. Cite only the evidence. Do not invent folder ids.`

// Organize is the runner the tick calls. Spend is tagged RoleOrganize. A high
// floor is the high-tier model when the request says the work is restructuring
// or a conflict; the default registration stays low. The high model is pinned
// for that call so the low-tier title model cannot win the ladder.
func (a *Agent) Organize(ctx context.Context, req OrganizeRequest) (OrganizePlan, error) {
	if a == nil {
		return OrganizePlan{}, errNoAgent
	}
	restore := a.pinOrganizeHigh(req.High)
	defer restore()
	floor := a.organizeFloor(req.High)
	user := organizePrompt + "\nchat_id " + strings.TrimSpace(req.ChatID) +
		"\nsource_rev " + strings.TrimSpace(req.SourceRev)
	if hierarchy := strings.TrimSpace(req.Hierarchy); hierarchy != "" {
		user += "\n" + hierarchy
	}
	user += "\nevidence\n" + strings.TrimSpace(req.Evidence)
	response, named, err := a.callRoleChecked(ctx, roles.RoleOrganize, floor,
		[]ai.Message{
			textMessage("system", organizeSystem),
			textMessage("user", user),
		}, organizeAccept)
	if err != nil {
		return OrganizePlan{}, err
	}
	plan := parseOrganizePlan(response.Text())
	plan.Model = named
	plan.ChatID = strings.TrimSpace(req.ChatID)
	plan.SourceRev = strings.TrimSpace(req.SourceRev)
	if req.Degraded {
		plan.Degraded = true
	}
	return plan, nil
}

var errNoAgent = errOrganize("organize needs a session")

func errOrganize(text string) error { return &organizeError{text} }

type organizeError struct{ text string }

func (e *organizeError) Error() string { return e.text }

func (a *Agent) organizeFloor(high bool) string {
	a.mu.Lock()
	floor := a.model
	source := a.config.RolesSource
	a.mu.Unlock()
	if !high || source == nil {
		return floor
	}
	if named, ok := source(roles.TierKey(roles.TierHigh)); ok && strings.TrimSpace(named) != "" {
		return strings.TrimSpace(named)
	}
	return floor
}

// pinOrganizeHigh makes a restructuring call resolve on the high-tier model.
// RoleOrganize stays registered low; without a pin the cheap title model would
// win the ladder even when the caller passed a high floor.
func (a *Agent) pinOrganizeHigh(high bool) func() {
	if a == nil || !high {
		return func() {}
	}
	floor := a.organizeFloor(true)
	if strings.TrimSpace(floor) == "" {
		return func() {}
	}
	a.mu.Lock()
	prev := a.config.RolesSource
	a.config.RolesSource = func(key string) (string, bool) {
		if key == roles.PinKey(roles.RoleOrganize) {
			return floor, true
		}
		if prev != nil {
			return prev(key)
		}
		return "", false
	}
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		a.config.RolesSource = prev
		a.mu.Unlock()
	}
}

func organizeAccept(response *ai.Response, _ string) bool {
	if response == nil {
		return false
	}
	plan := parseOrganizePlan(response.Text())
	return plan.Kind != ""
}

func parseOrganizePlan(raw string) OrganizePlan {
	var parsed struct {
		Kind     string            `json:"kind"`
		ChatID   string            `json:"chat_id"`
		Source   string            `json:"source_rev"`
		Actions  []json.RawMessage `json:"actions"`
		Degraded bool              `json:"degraded"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil {
		return OrganizePlan{}
	}
	kind := strings.TrimSpace(parsed.Kind)
	switch kind {
	case PlanNoAction, PlanAdd, PlanRemove, PlanMove, PlanCreateFolder:
	default:
		return OrganizePlan{}
	}
	if kind == PlanNoAction {
		parsed.Actions = nil
	}
	return OrganizePlan{
		Kind: kind, ChatID: parsed.ChatID, SourceRev: parsed.Source,
		Actions: parsed.Actions, Degraded: parsed.Degraded,
	}
}
