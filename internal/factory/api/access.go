package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

type OrgPolicy = acl.Policy
type ResourceRef = acl.Resource
type AccessSubject = acl.Subject
type AccessPrincipal = acl.Principal
type AccessGrant = acl.Grant
type AccessAudience = acl.Audience

type OrgResource struct {
	ResourceRef
	Parent     *ResourceRef     `json:"parent,omitempty"`
	Owner      AccessSubject    `json:"owner"`
	Label      string           `json:"label,omitempty"`
	Visibility string           `json:"visibility"`
	MyRole     string           `json:"my_role"`
	MyVerbs    []string         `json:"my_verbs"`
	CreatedBy  *AccessPrincipal `json:"created_by,omitempty"`
	CreatedAt  *time.Time       `json:"created_at,omitempty"`
}

type ResourceList struct {
	Resources []OrgResource `json:"resources"`
}

type ResourceNew struct {
	Kind   string `json:"kind"`
	Parent string `json:"parent"`
	Label  string `json:"label"`
}

type GrantTuple struct {
	AccessGrant
	Source string `json:"source"`
	Live   bool   `json:"live"`
}

type ResourceGrants struct {
	Resource ResourceRef  `json:"resource"`
	Grants   []GrantTuple `json:"grants"`
}

type GrantNew struct {
	Subject   AccessSubject `json:"subject"`
	Role      string        `json:"role"`
	ExpiresAt *time.Time    `json:"expires_at,omitempty"`
	Note      string        `json:"note,omitempty"`
}

type GrantPreview struct {
	Gains          []string       `json:"gains"`
	Loses          []string       `json:"loses"`
	AudienceBefore AccessAudience `json:"audience_before"`
	AudienceAfter  AccessAudience `json:"audience_after"`
}

type OrgPolicyChanged struct {
	Rev int    `json:"rev"`
	By  string `json:"by,omitempty"`
}

type ResourceChanged = ResourceRef

// ResourceMove asks to move an item into another space: To is the id of a
// workstream, or of the caller's own space.
type ResourceMove struct {
	To string `json:"to"`
}

// ResourceMoved answers a move with the item as it now stands and the item's
// own grants that were dropped because the destination is private.
type ResourceMoved struct {
	Resource    OrgResource  `json:"resource"`
	Dropped     []GrantTuple `json:"dropped"`
	AgentUsable *bool        `json:"agent_usable,omitempty"`
	AgentWords  string       `json:"agent_words,omitempty"`
}

// SessionTranscript carries only the public read fields of a conversation.
type SessionTranscript struct {
	ResourceRef
	State   string       `json:"state"`
	Seq     int          `json:"seq"`
	Entries []AgentEntry `json:"entries"`
}
