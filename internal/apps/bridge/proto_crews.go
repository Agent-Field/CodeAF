package bridge

const (
	MethodAppsArchive                  = "apps.archive"
	MethodAppsRestore                  = "apps.restore"
	MethodAppsDelete                   = "apps.delete"
	MethodAppsCopy                     = "apps.copy"
	MethodAppsLifecycle                = "apps.lifecycle"
	MethodTriggersList                 = "triggers.list"
	MethodTriggersPause                = "triggers.pause"
	MethodBudgetGet                    = "budget.get"
	MethodBudgetSet                    = "budget.set"
	MethodLinksAdd                     = "links.add"
	MethodLinksRemove                  = "links.remove"
	MethodLinksForRecord               = "links.for_record"
	MethodLinksForCrew                 = "links.for_crew"
	MethodCrewAgentBadge               = "crewagent.badge"
	MethodCrewAgentCandidates          = "crewagent.candidates"
	MethodAppsPrebuilt                 = "apps.prebuilt"
	MethodCustomBundle                 = "custom.bundle"
	StateArchived             AppState = "archived"
	StateDeleted              AppState = "deleted"
)

func TopicLinks(app string) string { return "app/" + app + "/links" }

type DeleteAppParams struct {
	Confirm string `json:"confirm"`
}
type CopyAppParams struct {
	Name string `json:"name,omitempty"`
}
type LifecycleApp struct {
	App     AppSummary `json:"app"`
	At      string     `json:"at"`
	By      string     `json:"by"`
	PurgeAt string     `json:"purge_at,omitempty"`
}
type LifecycleResult struct {
	Archived []LifecycleApp `json:"archived"`
	Deleted  []LifecycleApp `json:"deleted"`
}

type TriggerSource string

const (
	TriggerSourceRecord    TriggerSource = "record"
	TriggerSourceSchedule  TriggerSource = "schedule"
	TriggerSourceConnector TriggerSource = "connector"
)

type TriggerInfo struct {
	ID        string        `json:"id"`
	On        string        `json:"on"`
	Type      string        `json:"type,omitempty"`
	Run       string        `json:"run"`
	Source    TriggerSource `json:"source"`
	Label     string        `json:"label"`
	AI        bool          `json:"ai"`
	Paused    bool          `json:"paused"`
	PausedBy  string        `json:"paused_by,omitempty"`
	PausedAt  string        `json:"paused_at,omitempty"`
	LastFired string        `json:"last_fired,omitempty"`
	NextFire  string        `json:"next_fire,omitempty"`
}
type ListTriggersResult struct {
	Triggers []TriggerInfo `json:"triggers"`
}
type PauseTriggerParams struct {
	ID     string `json:"id"`
	Paused bool   `json:"paused"`
}
type BudgetInfo struct {
	Month    float64   `json:"month"`
	Spent    float64   `json:"spent"`
	Currency string    `json:"currency"`
	ResetsAt string    `json:"resets_at"`
	AtCap    string    `json:"at_cap"`
	Paused   bool      `json:"paused"`
	CanEdit  bool      `json:"can_edit"`
	Presets  []float64 `json:"presets"`
}
type SetBudgetParams struct {
	Month float64 `json:"month"`
}

type CrewLinkState string

const (
	CrewLinkOpen     CrewLinkState = "open"
	CrewLinkFinished CrewLinkState = "finished"
	CrewLinkUnlinked CrewLinkState = "unlinked"
)

type CrewLinkAccess string

const (
	CrewLinkAccessOK CrewLinkAccess = "ok"
	CrewLinkNoAccess CrewLinkAccess = "no_access"
	CrewLinkDeleted  CrewLinkAccess = "deleted"
	CrewLinkArchived CrewLinkAccess = "archived"
)

type CrewLink struct {
	Crew     string         `json:"crew"`
	CrewName string         `json:"crew_name"`
	App      string         `json:"app"`
	AppName  string         `json:"app_name"`
	Type     string         `json:"type"`
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	By       string         `json:"by"`
	LinkedAt string         `json:"linked_at"`
	State    CrewLinkState  `json:"state"`
	Access   CrewLinkAccess `json:"access"`
}
type AddLinkParams struct {
	Crew string `json:"crew"`
	Type string `json:"type"`
	ID   string `json:"id"`
}
type RemoveLinkParams = AddLinkParams
type LinksForRecordParams struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type LinksForCrewParams struct {
	Crew string `json:"crew"`
}
type ListLinksResult struct {
	Links []CrewLink `json:"links"`
}

type Badge struct {
	App       string `json:"app"`
	AppName   string `json:"app_name"`
	AgentName string `json:"agent_name"`
	Can       string `json:"can"`
	Role      string `json:"role"`
}
type CrewAgentCandidatesResult struct {
	Agents []Badge `json:"agents"`
}
type PrebuiltApp struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Space   string `json:"space"`
}
type PrebuiltResult struct {
	Apps []PrebuiltApp `json:"apps"`
}
type CustomBundleParams struct {
	File string `json:"file"`
}
type CustomBundleResult struct {
	JS   string `json:"js"`
	Hash string `json:"hash"`
}

// CrewRecordItem opens a linked record in its own app tab.
type CrewRecordItem struct {
	Kind    string         `json:"kind"`
	Ref     string         `json:"ref"`
	App     string         `json:"app"`
	AppName string         `json:"app_name"`
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Detail  string         `json:"detail"`
	State   CrewLinkState  `json:"state"`
	Access  CrewLinkAccess `json:"access"`
	Href    string         `json:"href"`
}
