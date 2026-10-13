package bridge

// Shape methods share the app-scoped envelope with the wave-1 methods.
const (
	MethodShapeOpen          = "shape.open"
	MethodShapeGet           = "shape.get"
	MethodShapeRequest       = "shape.request"
	MethodShapePlanEdit      = "shape.plan.edit"
	MethodShapeChangeFix     = "shape.change.fix"
	MethodShapeChangeCancel  = "shape.change.cancel"
	MethodShapeAnswer        = "shape.answer"
	MethodShapeDiff          = "shape.diff"
	MethodShapePreview       = "shape.preview"
	MethodShapeApply         = "shape.apply"
	MethodShapeDiscard       = "shape.discard"
	MethodShapeReplayDismiss = "shape.replay.dismiss"
	MethodShapeAsk           = "shape.ask"
	MethodVersionsList       = "versions.list"
	MethodVersionsDiff       = "versions.diff"
	MethodVersionsRestore    = "versions.restore"
	MethodAppsHealth         = "apps.health"
)

func TopicShape(app, member string) string { return "app/" + app + "/shape/" + member }
func TopicVersion(app string) string       { return "app/" + app + "/version" }
func TopicHealth(app string) string        { return "app/" + app + "/health" }

type Draft struct {
	App       string   `json:"app"`
	Member    string   `json:"member"`
	Base      int      `json:"base"`
	State     string   `json:"state"`
	Changes   []Change `json:"changes"`
	Replayed  *Replay  `json:"replayed,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}
type Change struct {
	ID       string         `json:"id"`
	N        int            `json:"n"`
	Request  string         `json:"request"`
	From     string         `json:"from"`
	State    string         `json:"state"`
	Round    int            `json:"round"`
	Steps    []ShapeStep    `json:"steps"`
	Files    []string       `json:"files"`
	Summary  string         `json:"summary,omitempty"`
	Check    *CheckReport   `json:"check,omitempty"`
	Question *ShapeQuestion `json:"question,omitempty"`
	Error    string         `json:"error,omitempty"`
	At       string         `json:"at"`
}
type ShapeStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Log    string `json:"log"`
}
type ShapeQuestion struct {
	Text    string                `json:"text"`
	Options []ShapeQuestionOption `json:"options"`
}
type ShapeQuestionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
}
type Replay struct {
	From      int    `json:"from"`
	To        int    `json:"to"`
	By        string `json:"by"`
	Requests  int    `json:"requests"`
	Failed    int    `json:"failed"`
	Dismissed bool   `json:"dismissed"`
}
type FileDiff struct {
	Path   string `json:"path"`
	Op     string `json:"op"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type ApplyPreview struct {
	From           int             `json:"from"`
	To             int             `json:"to"`
	Changes        []ApplyChange   `json:"changes"`
	Audience       string          `json:"audience"`
	NeedsChanged   bool            `json:"needs_changed"`
	Needs          []NeedLine      `json:"needs"`
	Migration      []MigrationLine `json:"migration"`
	RecordsChecked int             `json:"records_checked"`
}
type ApplyChange struct {
	N       int    `json:"n"`
	Request string `json:"request"`
}
type MigrationLine struct {
	Op       string `json:"op"`
	Type     string `json:"type"`
	Field    string `json:"field"`
	To       string `json:"to,omitempty"`
	Affected int    `json:"affected"`
	Line     string `json:"line"`
	Note     string `json:"note"`
}
type Version struct {
	N          int      `json:"n"`
	Requests   []string `json:"requests"`
	By         string   `json:"by"`
	At         string   `json:"at"`
	Changes    int      `json:"changes"`
	Note       string   `json:"note,omitempty"`
	Current    bool     `json:"current"`
	Restorable bool     `json:"restorable"`
}
type AppHealth struct {
	Version int     `json:"version"`
	Broken  *Broken `json:"broken,omitempty"`
}
type Broken struct {
	Version  int    `json:"version"`
	Request  string `json:"request"`
	Error    string `json:"error"`
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Affected int    `json:"affected"`
	LastGood int    `json:"last_good"`
}
type ShapeEvent struct {
	Kind    string   `json:"kind"`
	Draft   *Draft   `json:"draft,omitempty"`
	Change  *Change  `json:"change,omitempty"`
	Version *Version `json:"version,omitempty"`
	At      string   `json:"at"`
}
type VersionEvent struct {
	Version int    `json:"version"`
	By      string `json:"by"`
	Request string `json:"request"`
	At      string `json:"at"`
}

type ShapeGetResult struct {
	Draft *Draft `json:"draft"`
}
type ShapeRequestParams struct {
	Text string `json:"text"`
	From string `json:"from,omitempty"`
}
type ShapePlanEditParams struct {
	Section string `json:"section"`
	ID      string `json:"id"`
	Text    string `json:"text"`
}
type ShapeChangeFixParams struct {
	Change string `json:"change"`
	Hint   string `json:"hint"`
}
type ShapeChangeCancelParams struct {
	Change string `json:"change"`
}
type ShapeAnswerParams struct {
	Change string `json:"change"`
	Option string `json:"option,omitempty"`
	Text   string `json:"text,omitempty"`
}
type ShapeDiffParams struct {
	Change string `json:"change,omitempty"`
}
type ShapeDiffResult struct {
	Files []FileDiff `json:"files"`
}
type ShapeApplyParams struct {
	Base int `json:"base"`
}
type ShapeAskParams struct {
	Text string `json:"text"`
}
type ShapeAskResult struct {
	To string `json:"to"`
}
type VersionsListResult struct {
	Versions []Version `json:"versions"`
}
type VersionsDiffParams struct {
	Version int `json:"version"`
}
type VersionsDiffResult struct {
	Files []FileDiff `json:"files"`
}
type VersionsRestoreParams struct {
	Version int  `json:"version"`
	Confirm bool `json:"confirm,omitempty"`
}
type ShapeOpenParams = Empty
type ShapeOpenResult = Draft
type ShapeGetParams = Empty
type ShapeRequestResult = Change
type ShapePlanEditResult = Change
type ShapeChangeFixResult = Change
type ShapeChangeCancelResult = Draft
type ShapeAnswerResult = Change
type ShapePreviewParams = Empty
type ShapePreviewResult = ApplyPreview
type ShapeApplyResult = AppSummary
type ShapeDiscardParams = Empty
type ShapeDiscardResult = Empty
type ShapeReplayDismissParams = Empty
type ShapeReplayDismissResult = Draft
type VersionsListParams = Empty
type VersionsRestoreResult = AppSummary
type AppsHealthParams = Empty
type AppsHealthResult = AppHealth
