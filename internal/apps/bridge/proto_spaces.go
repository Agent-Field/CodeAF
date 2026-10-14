package bridge

import (
	"encoding/json"
	"time"
)

type AuditEntry struct {
	Seq      int64           `json:"seq"` // assigned by Append, increasing
	At       time.Time       `json:"at"`  // assigned by Append
	Space    string          `json:"space"`
	App      string          `json:"app,omitempty"`
	Member   string          `json:"member"` // who; "builder" or "system" for codeaf
	Agent    bool            `json:"agent,omitempty"`
	Action   string          `json:"action"`           // method or verb: "records.update", "app.apply", "role.set", "member.remove", "approval.decide", "trigger.pause"
	Object   string          `json:"object,omitempty"` // "deal/d_12", "member/raj", "v12"
	Decision string          `json:"decision"`         // allow | deny
	Detail   json.RawMessage `json:"detail,omitempty"`
	Activity *ActivityLine   `json:"activity,omitempty"` // set = also an Activity line
}

type ActivityLine struct {
	Text string      `json:"text"`           // "Mona shaped CRM · v12"
	Undo *UndoAction `json:"undo,omitempty"` // shown as "Undo"
	View string      `json:"view,omitempty"` // open target, e.g. "versions/v12"
}

type UndoAction struct {
	Method string          `json:"method"`
	App    string          `json:"app,omitempty"`
	Params json.RawMessage `json:"params"`
}

const (
	MethodSpaceInfo           = "space.info"
	MethodSpacesList          = "spaces.list"
	MethodSpacesJoin          = "spaces.join"
	MethodSpacesSignin        = "spaces.signin"
	MethodSpacesConnect       = "spaces.connect"
	MethodSpacesNew           = "spaces.new"
	MethodSpacesForget        = "spaces.forget"
	MethodMembersList         = "members.list"
	MethodMembersInvite       = "members.invite"
	MethodMembersInviteCancel = "members.invite.cancel"
	MethodMembersInviteResend = "members.invite.resend"
	MethodMembersRole         = "members.role"
	MethodMembersRemove       = "members.remove"
	MethodMembersLeave        = "members.leave"
	MethodAppRolesList        = "approles.list"
	MethodAppRolesSet         = "approles.set"
	MethodAppRolesAsk         = "approles.ask"
	MethodSignoffRoles        = "signoff.roles"
	MethodSignoffRolesSet     = "signoff.roles.set"
	MethodSignoffState        = "signoff.state"
	MethodSignoffSign         = "signoff.sign"
	MethodAuditList           = "audit.list"
	MethodActivityList        = "activity.list"
	MethodAppsMove            = "apps.move"
	TopicSpaceMembers         = "space/members"
	TopicSpaceActivity        = "space/activity"
	TopicSpaceWaiting         = "space/waiting"
)

func TopicRoles(app string) string   { return "app/" + app + "/roles" }
func TopicSignoff(app string) string { return "app/" + app + "/signoff" }

type SpaceInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Me      Member `json:"me"`
	Apps    int    `json:"apps"`
}

type SpaceEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Role    string `json:"role"`
	Apps    int    `json:"apps"`
	Waiting bool   `json:"waiting"`
	Order   int    `json:"order"`
}

type SpacesListResult struct {
	Spaces []SpaceEntry `json:"spaces"`
}

type SpaceJoinParams struct {
	Link string `json:"link"`
}

type SpaceJoinResult struct {
	Space  SpaceEntry      `json:"space"`
	Invite SpaceJoinInvite `json:"invite"`
	Signin SpaceSignin     `json:"signin"`
	State  string          `json:"state"`
}

type SpaceJoinInvite struct {
	By      string    `json:"by"`
	Role    string    `json:"role"`
	Server  string    `json:"server"`
	Expires time.Time `json:"expires"`
}

type SpaceSignin struct {
	Kind      string `json:"kind"`
	URL       string `json:"url,omitempty"`
	EmailHint string `json:"email_hint,omitempty"`
}

type SpacesSigninParams struct {
	Space string `json:"space"`
	Code  string `json:"code,omitempty"`
}

type SpaceConnectParams struct {
	Space string `json:"space"`
}

type SpaceConnection struct {
	URL     string    `json:"url"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

type SpaceNewParams struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	SetupCode string `json:"setup_code"`
}

type SpaceRef struct {
	Space string `json:"space"`
}

type Member struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Role  string `json:"role"`
}

type MembersListResult struct {
	Members []Member `json:"members"`
	Invites []Invite `json:"invites"`
}

type Invite struct {
	ID      string    `json:"id"`
	Email   string    `json:"email,omitempty"`
	Role    string    `json:"role"`
	Link    string    `json:"link"`
	By      string    `json:"by"`
	Expires time.Time `json:"expires"`
	State   string    `json:"state"`
}

type InviteParams struct {
	Email string `json:"email,omitempty"`
	Role  string `json:"role"`
}

type InviteRef struct {
	Invite string `json:"invite"`
}

type MemberRoleParams struct {
	Member string `json:"member"`
	Role   string `json:"role"`
}

type MemberRef struct {
	Member string `json:"member"`
}

type AppMembers struct {
	Everyone string      `json:"everyone"`
	People   []AppMember `json:"people"`
}

type AppMember struct {
	Member string     `json:"member"`
	Role   string     `json:"role"`
	From   string     `json:"from"`
	SetBy  string     `json:"set_by,omitempty"`
	At     *time.Time `json:"at,omitempty"`
	Maker  bool       `json:"maker,omitempty"`
}

type AppRoleSetParams struct {
	Member string  `json:"member"`
	Role   *string `json:"role"`
}

type AskEditorParams struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type SignoffChainRef struct {
	Chain string `json:"chain"`
}

type SignoffRoles struct {
	Chain    string            `json:"chain"`
	Steps    []SignoffRoleStep `json:"steps"`
	Warnings []string          `json:"warnings"`
}

type SignoffRoleStep struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	People []string `json:"people"`
}

type SignoffRecordRef struct {
	Chain string `json:"chain"`
	Type  string `json:"type"`
	ID    string `json:"id"`
}

type SignoffState struct {
	Chain string             `json:"chain"`
	Steps []SignoffStateStep `json:"steps"`
	Done  bool               `json:"done"`
}

type SignoffStateStep struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	State    string     `json:"state"`
	By       string     `json:"by,omitempty"`
	At       *time.Time `json:"at,omitempty"`
	Eligible []string   `json:"eligible"`
}

type SignoffSignParams struct {
	Chain  string `json:"chain"`
	Type   string `json:"type"`
	ID     string `json:"id"`
	Step   string `json:"step"`
	Answer string `json:"answer"`
	Note   string `json:"note,omitempty"`
}

type AuditListParams struct {
	App    string `json:"app,omitempty"`
	Member string `json:"member,omitempty"`
	Action string `json:"action,omitempty"`
	Before int64  `json:"before,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type AuditListResult struct {
	Entries []AuditEntry `json:"entries"`
	Next    int64        `json:"next,omitempty"`
}

type ActivityListParams struct {
	App    string `json:"app,omitempty"`
	Before int64  `json:"before,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ActivityListResult struct {
	Lines []ActivityEntry `json:"lines"`
	Next  int64           `json:"next,omitempty"`
}

type ActivityEntry struct {
	Seq    int64       `json:"seq"`
	At     time.Time   `json:"at"`
	App    string      `json:"app,omitempty"`
	Member string      `json:"member"`
	Text   string      `json:"text"`
	Undo   *UndoAction `json:"undo,omitempty"`
	View   string      `json:"view,omitempty"`
}

type MoveAppParams struct {
	To      string   `json:"to"`
	Editors []string `json:"editors"`
}
