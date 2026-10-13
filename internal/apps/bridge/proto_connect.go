package bridge

const (
	MethodConnectAccounts        = "connect.accounts"
	MethodConnectAllow           = "connect.allow"
	MethodConnectAllowSheet      = "connect.allow.sheet"
	MethodConnectRevoke          = "connect.revoke"
	MethodConnectWorkspaceList   = "connect.workspace.list"
	MethodConnectWorkspaceAdd    = "connect.workspace.add"
	MethodConnectWorkspaceWho    = "connect.workspace.who"
	MethodConnectWorkspaceImpact = "connect.workspace.impact"
	MethodConnectWorkspaceRemove = "connect.workspace.remove"
	MethodConnectNeeds           = "connect.needs"
	MethodConnectWaits           = "connect.waits"
	MethodConnectFakes           = "connect.fakes"
	MethodImportsSources         = "imports.sources"
	MethodImportsPreview         = "imports.preview"
	MethodImportsRun             = "imports.run"
	MethodImportsUndo            = "imports.undo"
)

func TopicConnectAllow(member string) string { return "connect/" + member + "/allow" }
func TopicConnectWaits(member string) string { return "connect/" + member + "/waits" }
func TopicConnectWorkspace() string          { return "connect/workspace" }
func TopicFakes(app string) string           { return "app/" + app + "/fakes" }

// Connect error reasons travel in Error.Details rather than changing wave-1 codes.
const (
	ReasonNotAllowedForApp = "not_allowed_for_app"
	ReasonAdminOnly        = "admin_only"
	ReasonNeedsAllow       = "needs_allow"
	ReasonExpired          = "expired"
	ReasonNeedsChoice      = "needs_choice"
)

type Connection struct {
	ID      string `json:"id"`
	Service string `json:"service"`
	Account string `json:"account"`
	Level   string `json:"level"`
	Status  string `json:"status"`
	Since   string `json:"since"`
	Detail  string `json:"detail,omitempty"`
}

type Grant struct {
	Member  string   `json:"member"`
	App     string   `json:"app"`
	AppName string   `json:"app_name"`
	Space   string   `json:"space"`
	Service string   `json:"service"`
	Can     []string `json:"can"`
	At      string   `json:"at"`
}

type AccountsResult struct {
	Connections []Connection `json:"connections"`
	Grants      []Grant      `json:"grants"`
}

type AllowParams struct {
	Service string `json:"service"`
	Answer  string `json:"answer"`
}

type AllowSheetParams struct {
	Service string `json:"service"`
}

type AllowSheet struct {
	App        string   `json:"app"`
	AppName    string   `json:"app_name"`
	Member     string   `json:"member"`
	MemberName string   `json:"member_name"`
	Service    string   `json:"service"`
	Account    string   `json:"account"`
	Title      string   `json:"title"`
	Can        []string `json:"can"`
	Cant       []string `json:"cant"`
}

type RevokeParams struct {
	App     string `json:"app"`
	Space   string `json:"space"`
	Service string `json:"service"`
}

type WorkspaceConnectionUse struct {
	App     string `json:"app"`
	AppName string `json:"app_name"`
	For     string `json:"for"`
}

type WorkspaceConnection struct {
	ID      string                   `json:"id"`
	Service string                   `json:"service"`
	Account string                   `json:"account"`
	AddedBy string                   `json:"added_by"`
	Who     string                   `json:"who"`
	Apps    []string                 `json:"apps"`
	UsedBy  []WorkspaceConnectionUse `json:"used_by"`
}

type WorkspaceConnectionsResult struct {
	Connections []WorkspaceConnection `json:"connections"`
	CanEdit     bool                  `json:"can_edit"`
}

type AddWorkspaceConnectionParams struct {
	Service string   `json:"service"`
	Who     string   `json:"who"`
	Apps    []string `json:"apps,omitempty"`
}

type SetWhoParams struct {
	ID   string   `json:"id"`
	Who  string   `json:"who"`
	Apps []string `json:"apps,omitempty"`
}

type WorkspaceConnectionParams struct {
	ID string `json:"id"`
}

type RemovalImpactApp struct {
	App          string `json:"app"`
	AppName      string `json:"app_name"`
	For          string `json:"for"`
	RunsThisWeek int    `json:"runs_this_week"`
}

type RemovalImpact struct {
	ID      string             `json:"id"`
	Service string             `json:"service"`
	Apps    []RemovalImpactApp `json:"apps"`
}

type ConnectionAllowed struct {
	N  int `json:"n"`
	Of int `json:"of"`
}

type ConnectionNeed struct {
	Service string             `json:"service"`
	Level   string             `json:"level"`
	Label   string             `json:"label"`
	For     string             `json:"for"`
	Status  string             `json:"status"`
	Allowed *ConnectionAllowed `json:"allowed,omitempty"`
}

type ConnectionNeeds struct {
	Connections []ConnectionNeed `json:"connections"`
}

type ConnectorWait struct {
	App     string `json:"app"`
	RunID   string `json:"run_id"`
	Step    string `json:"step"`
	Service string `json:"service"`
	Member  string `json:"member"`
	Reason  string `json:"reason"`
	Since   string `json:"since"`
	StopsAt string `json:"stops_at"`
}

type ConnectWaitsResult struct {
	Waits []ConnectorWait `json:"waits"`
}

type FakeCall struct {
	App       string `json:"app"`
	RunID     string `json:"run_id,omitempty"`
	Tool      string `json:"tool"`
	VerbLabel string `json:"verb_label"`
	Summary   string `json:"summary"`
	At        string `json:"at"`
	Fake      bool   `json:"fake"`
}

type ConnectFakesParams struct {
	RunID string `json:"run_id,omitempty"`
}

type ConnectFakesResult struct {
	Calls []FakeCall `json:"calls"`
}

type ImportSource struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Connected bool   `json:"connected"`
	Detail    string `json:"detail"`
}

type ImportSourcesParams struct {
	Type string `json:"type"`
}

type ImportSourcesResult struct {
	Sources []ImportSource `json:"sources"`
}

type ImportSourceSelection struct {
	Kind   string `json:"kind"`
	File   string `json:"file,omitempty"`
	Base   string `json:"base,omitempty"`
	Table  string `json:"table,omitempty"`
	Object string `json:"object,omitempty"`
}

type ImportPreviewParams struct {
	Type    string                `json:"type"`
	Source  ImportSourceSelection `json:"source"`
	Mapping []Mapping             `json:"mapping,omitempty"`
}

type Mapping struct {
	Column string            `json:"column"`
	Field  string            `json:"field,omitempty"`
	As     string            `json:"as,omitempty"`
	Values map[string]string `json:"values,omitempty"`
	People map[string]string `json:"people,omitempty"`
}

type ImportPreview struct {
	ImportID   string         `json:"import_id"`
	Type       string         `json:"type"`
	Total      int            `json:"total"`
	RowsShown  []Record       `json:"rows_shown"`
	Columns    []ColumnMap    `json:"columns"`
	NeedChoice int            `json:"need_choice"`
	Duplicates int            `json:"duplicates"`
	NewRefs    map[string]int `json:"new_refs"`
	CanImport  bool           `json:"can_import"`
	Count      int            `json:"count"`
}

type ColumnMap struct {
	Column      string `json:"column"`
	First       any    `json:"first"`
	Field       string `json:"field,omitempty"`
	Match       string `json:"match"`
	Note        string `json:"note"`
	NeedsChoice bool   `json:"needs_choice"`
	Options     any    `json:"options,omitempty"`
}

type ImportRunParams struct {
	ImportID string    `json:"import_id"`
	Mapping  []Mapping `json:"mapping"`
}

type ImportResult struct {
	ImportID string         `json:"import_id"`
	Created  int            `json:"created"`
	Skipped  int            `json:"skipped"`
	NewRefs  map[string]int `json:"new_refs"`
	Activity string         `json:"activity"`
}

type ImportUndoParams struct {
	ImportID string `json:"import_id"`
}

type ImportUndoResult struct {
	Removed     int      `json:"removed"`
	KeptChanged []string `json:"kept_changed"`
}
