package bridge

import "encoding/json"

// Surface identifies the format of a document.
type Surface string

const (
	SurfaceCanvas   Surface = "canvas"
	SurfaceText     Surface = "text"
	SurfaceGrid     Surface = "grid"
	SurfaceTimeline Surface = "timeline"
	SurfaceGraph    Surface = "graph"
	SurfaceMap      Surface = "map"
)

// Document methods are app-scoped.
const (
	MethodDocsList        = "docs.list"
	MethodDocsGet         = "docs.get"
	MethodDocsCreate      = "docs.create"
	MethodDocsReorder     = "docs.reorder"
	MethodPartsGet        = "parts.get"
	MethodPartsLock       = "parts.lock"
	MethodPartsTakeover   = "parts.takeover"
	MethodPartsAnswer     = "parts.answer"
	MethodPartsRelease    = "parts.release"
	MethodPartsWrite      = "parts.write"
	MethodPartsAdd        = "parts.add"
	MethodPartsRemove     = "parts.remove"
	MethodPartsVersions   = "parts.versions"
	MethodPartsDiff       = "parts.diff"
	MethodPartsRestore    = "parts.restore"
	MethodPresenceSet     = "presence.set"
	MethodCommentsList    = "comments.list"
	MethodCommentsAdd     = "comments.add"
	MethodCommentsResolve = "comments.resolve"
	MethodCommentsSend    = "comments.send"
	MethodShareSet        = "share.set"
	MethodShareOpen       = "share.open"
)

// TopicDoc groups all changes to one document in one subscription.
func TopicDoc(app, doc string) string { return "app/" + app + "/doc/" + doc }

type CommentFor string

const (
	CommentForAgent  CommentFor = "agent"
	CommentForPerson CommentFor = "person"
)

type CommentState string

const (
	CommentStateOpen     CommentState = "open"
	CommentStateResolved CommentState = "resolved"
	CommentStateSent     CommentState = "sent"
)

type PartVersionKind string

const (
	PartVersionKindCreate  PartVersionKind = "create"
	PartVersionKindAsk     PartVersionKind = "ask"
	PartVersionKindNudge   PartVersionKind = "nudge"
	PartVersionKindRestore PartVersionKind = "restore"
	PartVersionKindReorder PartVersionKind = "reorder"
)

type DocEventKind string

const (
	DocEventKindPart     DocEventKind = "part"
	DocEventKindOrder    DocEventKind = "order"
	DocEventKindLock     DocEventKind = "lock"
	DocEventKindPresence DocEventKind = "presence"
	DocEventKindComment  DocEventKind = "comment"
	DocEventKindShare    DocEventKind = "share"
)

type PartRef struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
}

type Part struct {
	Doc       string     `json:"doc"`
	Part      string     `json:"part"`
	Surface   Surface    `json:"surface"`
	Version   int        `json:"version"`
	Status    string     `json:"status,omitempty"`
	Owner     string     `json:"owner,omitempty"`
	Text      string     `json:"text"`
	Extra     *PartExtra `json:"extra,omitempty"`
	UpdatedAt string     `json:"updated_at"`
	UpdatedBy string     `json:"updated_by"`
	ByAgent   bool       `json:"by_agent"`
}

type PartExtra struct {
	Formulas map[string]string `json:"formulas,omitempty"`
}

type DocInfo struct {
	Doc     string        `json:"doc"`
	Surface Surface       `json:"surface"`
	Title   string        `json:"title"`
	Parts   []PartSummary `json:"parts"`
	Share   *Share        `json:"share,omitempty"`
}

type PartSummary struct {
	Part         string `json:"part"`
	Version      int    `json:"version"`
	Status       string `json:"status,omitempty"`
	Title        string `json:"title,omitempty"`
	Lock         *Lock  `json:"lock,omitempty"`
	CommentsOpen int    `json:"comments_open"`
}

type Lock struct {
	Doc       string    `json:"doc"`
	Part      string    `json:"part"`
	Holder    string    `json:"holder"`
	Agent     bool      `json:"agent"`
	Since     string    `json:"since"`
	IdleUntil string    `json:"idle_until"`
	Takeover  *Takeover `json:"takeover,omitempty"`
}

type Takeover struct {
	By    string `json:"by"`
	Until string `json:"until"`
}

type Presence struct {
	Doc    string `json:"doc"`
	Member string `json:"member"`
	Part   string `json:"part,omitempty"`
	At     string `json:"at"`
}

type Comment struct {
	ID         string        `json:"id"`
	Doc        string        `json:"doc"`
	Part       string        `json:"part"`
	Anchor     CommentAnchor `json:"anchor"`
	Text       string        `json:"text"`
	By         string        `json:"by"`
	For        CommentFor    `json:"for"`
	State      CommentState  `json:"state"`
	At         string        `json:"at"`
	ResolvedBy string        `json:"resolved_by,omitempty"`
}

// CommentAnchor leaves text ranges opaque because the surface contract does not specify their shape.
type CommentAnchor struct {
	Box     string          `json:"box,omitempty"`
	Range   json.RawMessage `json:"range,omitempty"`
	Cell    string          `json:"cell,omitempty"`
	Node    string          `json:"node,omitempty"`
	Feature string          `json:"feature,omitempty"`
}

type PartVersion struct {
	Doc     string          `json:"doc"`
	Part    string          `json:"part"`
	Version int             `json:"version"`
	Kind    PartVersionKind `json:"kind"`
	Label   string          `json:"label"`
	By      string          `json:"by"`
	ByAgent bool            `json:"by_agent"`
	At      string          `json:"at"`
}

type Share struct {
	Doc  string `json:"doc"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
	On   bool   `json:"on"`
	By   string `json:"by"`
	At   string `json:"at"`
}

type DocEvent struct {
	Kind     DocEventKind `json:"kind"`
	Part     string       `json:"part,omitempty"`
	Version  int          `json:"version,omitempty"`
	Lock     *Lock        `json:"lock,omitempty"`
	Presence *Presence    `json:"presence,omitempty"`
	Comment  *Comment     `json:"comment,omitempty"`
	Share    *Share       `json:"share,omitempty"`
	By       string       `json:"by"`
}

type ListDocsParams struct {
	Surface Surface `json:"surface,omitempty"`
}

type ListDocsResult struct {
	Docs []DocInfo `json:"docs"`
}

type GetDocParams struct {
	Doc string `json:"doc"`
}

type CreateDocParams struct {
	Doc     string          `json:"doc"`
	Surface Surface         `json:"surface"`
	Title   string          `json:"title"`
	Parts   []CreateDocPart `json:"parts,omitempty"`
}

type CreateDocPart struct {
	Part string `json:"part"`
	Text string `json:"text"`
}

type ReorderDocParams struct {
	Doc   string   `json:"doc"`
	Parts []string `json:"parts"`
}

type GetPartParams struct {
	Doc     string `json:"doc"`
	Part    string `json:"part"`
	Version int    `json:"version,omitempty"`
}

type LockPartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
}

type TakeoverPartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
}

type AnswerPartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
	Keep bool   `json:"keep"`
}

type ReleasePartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
}

type WritePartParams struct {
	Doc         string          `json:"doc"`
	Part        string          `json:"part"`
	Text        string          `json:"text"`
	BaseVersion int             `json:"base_version"`
	Kind        PartVersionKind `json:"kind"`
	Label       string          `json:"label"`
	Extra       *PartExtra      `json:"extra,omitempty"`
}

type AddPartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
	Text string `json:"text,omitempty"`
}

type RemovePartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
	Text string `json:"text,omitempty"`
}

type ListPartVersionsParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
}

type ListPartVersionsResult struct {
	Versions []PartVersion `json:"versions"`
}

type DiffPartParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

type DiffPartResult struct {
	FromText string     `json:"from_text"`
	ToText   string     `json:"to_text"`
	Lines    []DiffLine `json:"lines"`
}

type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

type RestorePartParams struct {
	Doc     string `json:"doc"`
	Part    string `json:"part"`
	Version int    `json:"version"`
}

type SetPresenceParams struct {
	Doc  string `json:"doc"`
	Part string `json:"part,omitempty"`
}

type ListCommentsParams struct {
	Doc   string       `json:"doc"`
	Part  string       `json:"part,omitempty"`
	State CommentState `json:"state,omitempty"`
}

type ListCommentsResult struct {
	Comments []Comment `json:"comments"`
}

type AddCommentParams struct {
	Doc    string        `json:"doc"`
	Part   string        `json:"part"`
	Anchor CommentAnchor `json:"anchor"`
	Text   string        `json:"text"`
	For    CommentFor    `json:"for"`
}

type ResolveCommentParams struct {
	ID string `json:"id"`
}

type SendCommentParams struct {
	ID string `json:"id"`
}

type SetShareParams struct {
	Doc string `json:"doc"`
	On  bool   `json:"on"`
}

type OpenShareParams struct {
	Slug string `json:"slug"`
}

// SendCommentResult is the agent turn started by comments.send.
type SendCommentResult = AgentSendResult
