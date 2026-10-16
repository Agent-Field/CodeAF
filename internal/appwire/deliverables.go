package appwire

import (
	"reflect"
	"strings"
)

// DELIVERABLES: the files an agent made for the person, from the artifacts
// index, as typed facts.
//
// Nothing here takes a path from the caller to READ: a deliverable is named by
// the id the engine issued, and the file behind it is the one the engine
// recorded, checked now against the same scope law media uses. The two
// exceptions are named plainly: keep takes a destination FOLDER, which is the
// native host's to pass and never a renderer's, and snapshot takes the path of
// a picture the host has just saved, which must lie inside that agent's own
// folders. Unknown stays unknown: absent fields are absent, and a legacy row's
// state, turn and inputs are never inferred.

const (
	EventDeliverableChanged = "deliverable.changed"
	// DeliverablePageDefault and DeliverablePageMax bound one page.
	DeliverablePageDefault = 50
	DeliverablePageMax     = 100
	// DeliverableReadMax is the most bytes deliverables.read answers.
	DeliverableReadMax = 1 << 20
	// BranchPageDefault and BranchPageMax bound one branches.list page.
	BranchPageDefault = 50
	BranchPageMax     = 100
)

// DeliverableChanged says an index row was successfully appended. Descriptors
// still come from deliverables.get.
type DeliverableChanged struct {
	Agent  string `json:"agent"`
	ID     string `json:"id"`
	Reason string `json:"reason" enum:"created,kept,removed,snapshot"`
}

// Kept is where a copy was taken out to.
type DeliverableKept struct {
	Path   string `json:"path"`
	At     string `json:"at"`
	Bytes  *int64 `json:"bytes,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	// How is copied (Keep…) or created_in_repo (made inside the repository).
	How string `json:"how" enum:"copied,created_in_repo"`
}

// Deliverable is one file made for the person and what the engine recorded.
type Deliverable struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Kind  string `json:"kind" enum:"markdown,mermaid,image,audio,video,export,document"`
	Title string `json:"title"`
	// Path is the recorded canonical path, for display; the native side may
	// open it only when Live is available or changed.
	Path    string `json:"path"`
	Created string `json:"created"`
	// Updated is when the newest row of this deliverable was appended, absent
	// for a legacy row that was never updated.
	Updated string `json:"updated,omitempty"`
	// Live is the file checked now.
	Live string `json:"live" enum:"available,changed,gone,on_branch,outside,unreadable"`
	// Open is the path the native side may open now, set only when Live is
	// available or changed. OpenFrom says whether that is the original or, when
	// the original is gone (a swept temporary folder), the copy Keep made.
	Open     string `json:"open,omitempty"`
	OpenFrom string `json:"open_from,omitempty" enum:"source,kept"`
	// Place is where the file sits NOW. It is not a state: a missing State is
	// unknown and an app must not read Draft or Kept out of Place.
	Place string `json:"place" enum:"agent_folder,repo,elsewhere"`
	// State is the RECORDED state; absent is unknown.
	State  string `json:"state,omitempty" enum:"draft,kept"`
	Origin string `json:"origin" enum:"agent,saved_by_you,unknown"`
	// Turn is the authored message the work answered; Run is the producing
	// turn's immutable id, the join to an answer's run. Either may be absent,
	// and Turn is never guessed from Run, time or the nearest message.
	Turn    string   `json:"turn,omitempty"`
	Run     string   `json:"run,omitempty"`
	Task    string   `json:"task,omitempty"`
	From    []string `json:"from,omitempty"`
	Profile string   `json:"profile,omitempty"`
	// Authored says a writer saw this file created.
	Authored bool `json:"authored,omitempty"`
	// Branch and Rel say the file was created on a task branch: Live is
	// on_branch while the file exists only there (a branch not merged into the
	// checkout), and deliverables.read then answers from git's object store.
	Branch      string `json:"branch,omitempty"`
	Rel         string `json:"rel,omitempty"`
	Version     string `json:"version,omitempty"`
	LiveVersion string `json:"live_version,omitempty"`
	Bytes       *int64 `json:"bytes,omitempty"`
	LiveBytes   *int64 `json:"live_bytes,omitempty"`
	MIME        string `json:"mime,omitempty"`
	// Media is the media source id of an image, sound or video: ask media.get
	// and the media lease for a preview.
	Media string `json:"media,omitempty"`
	// PreviewUnavailable says why an image, sound or video has no Media source
	// to preview through: source_gone is a kept file whose original is gone, so
	// its media source id (which names the ORIGINAL path) would be dead. The
	// kept copy is real and openable at Open; a lease for it is a separate
	// capability this engine does not yet offer, and none is forged.
	PreviewUnavailable string           `json:"preview_unavailable,omitempty" enum:"source_gone"`
	Kept               *DeliverableKept `json:"kept,omitempty"`
	// Teams and Ambiguous are present only under scope "teams". Teams is every
	// recorded membership of the row's exact owning conversation (never one
	// guessed assignment); Ambiguous says those memberships reach more than one
	// top-level team, so no single team made this row.
	Teams     []DeliverableTeam `json:"teams,omitempty"`
	Ambiguous bool              `json:"ambiguous,omitempty"`
}

// DeliverableTeam is one team holding a deliverable's owner, and the canonical
// top-level team above it (the team itself when it is top-level).
type DeliverableTeam struct {
	Team string `json:"team"`
	Top  string `json:"top"`
	// Root marks the real root's own roster (Top is then empty). It is not a
	// top-level team and never makes a row ambiguous.
	Root bool `json:"root,omitempty"`
}

// DeliverablesQuery is the one filter list and count share.
type DeliverablesQuery struct {
	Scope  string   `json:"scope" enum:"agent,crew,global,teams"`
	Agent  string   `json:"agent,omitempty"`
	Crew   string   `json:"crew,omitempty"`
	Member string   `json:"member,omitempty"`
	Kind   []string `json:"kind,omitempty"`
	// State matches the recorded state; "unknown" matches a row with none.
	State []string `json:"state,omitempty"`
	Live  []string `json:"live,omitempty"`
	// Query is a case-insensitive substring of the file name, title or kind.
	Query  string `json:"query,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ScopeTeams is the deliverables scope over every open team and descendant: the
// union of their members, managers included, loose agents excluded. Crew, when
// given with it, names one canonical top-level team and restricts the union to
// that team's whole subtree; the real root's id names the root's own roster.
const ScopeTeams = "teams"

// DeliverablesPage is deliverables.list's answer. Next absent means the whole
// history was read; Incomplete says a page is short because a bound was spent
// (or, with no Next, that superseded rows could not all be ruled out).
type DeliverablesPage struct {
	Deliverables []Deliverable `json:"deliverables"`
	Next         string        `json:"next,omitempty"`
	Incomplete   bool          `json:"incomplete,omitempty"`
	// Revision is the team membership the page was read against (scope teams).
	Revision string `json:"revision,omitempty"`
}

// DeliverablesCount is deliverables.count's answer: how many matched in the
// part of history this call read. Sum the counts while following Next.
type DeliverablesCount struct {
	Count      int    `json:"count"`
	Next       string `json:"next,omitempty"`
	Incomplete bool   `json:"incomplete,omitempty"`
	Revision   string `json:"revision,omitempty"`
}

// DeliverableText is deliverables.read's answer.
type DeliverableText struct {
	Text      string `json:"text"`
	Bytes     int64  `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
	// Version is the live version the text was read at.
	Version string `json:"version"`
}

// ConflictDetail is the typed account of a conflict.
type ConflictDetail struct {
	Reason string `json:"reason" enum:"exists,changed,already_kept,stale_revision,ambiguous_row"`
	Path   string `json:"path,omitempty"`
}

// TaskBranch is one task branch of the conversation that is not merged into the
// checkout. Counts are git's own.
type TaskBranch struct {
	Name  string `json:"name"`
	Task  string `json:"task,omitempty"`
	Title string `json:"title,omitempty"`
	// TitleTruncated says the task's title was longer than the engine sends.
	TitleTruncated bool   `json:"title_truncated,omitempty"`
	Head           string `json:"head"`
	Base           string `json:"base,omitempty"`
	// Files, Added and Removed are absent when git could not give them (no
	// merge base, a failed diff, or one not read in the request's time), never
	// zero for unknown. CountsCapped says the diff was longer than the engine
	// reads: the counts are of the part read, lower bounds.
	Files        *int `json:"files,omitempty"`
	Added        *int `json:"added,omitempty"`
	Removed      *int `json:"removed,omitempty"`
	CountsCapped bool `json:"counts_capped,omitempty"`
}

// BranchesQuery is branches.list's request: one page of the conversation's
// unmerged task branches, resumed by a cursor the engine issued.
type BranchesQuery struct {
	Agent  string `json:"agent"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// BranchesResult is branches.list's answer. Repo false means git said the
// conversation's workspace is not a checkout with a HEAD commit: unknown, not
// "no branches" (git that did not answer in time is busy, never repo false).
// Head is the commit the page's rows were judged against and Recorded how many
// branch names the conversation's task records give, which is what the cursor
// walks. Next absent means every recorded name was examined; Incomplete says
// the page is short because its time ran out, not because it was full.
type BranchesResult struct {
	Repo       bool         `json:"repo"`
	Head       string       `json:"head,omitempty"`
	Recorded   *int         `json:"recorded,omitempty"`
	Branches   []TaskBranch `json:"branches"`
	Next       string       `json:"next,omitempty"`
	Incomplete bool         `json:"incomplete,omitempty"`
}

// Deliverer is the optional engine half of deliverables.* and branches.list.
type Deliverer interface {
	ListDeliverables(DeliverablesQuery) (DeliverablesPage, error)
	CountDeliverables(DeliverablesQuery) (DeliverablesCount, error)
	GetDeliverable(agent, id string) (Deliverable, error)
	ReadDeliverable(agent, id, version string, max int) (DeliverableText, error)
	// KeepDeliverable is for the native host only; dir is a folder its picker
	// returned.
	KeepDeliverable(agent, id, dir, name, expectVersion string) (Deliverable, error)
	RemoveDeliverable(agent, id string) error
	SnapshotDeliverable(agent, path, url, process string) (Deliverable, error)
	TaskBranches(BranchesQuery) (BranchesResult, error)
}

func deliverer(e Engine, method string) (Deliverer, error) {
	d, ok := e.(Deliverer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", method)
	}
	return d, nil
}

func checkDeliverablesQuery(q DeliverablesQuery, method string) error {
	if q.Limit < 0 {
		return Errorf(CodeBadParams, "%s needs a limit of zero or more", method)
	}
	switch q.Scope {
	case "agent":
		if strings.TrimSpace(q.Agent) == "" {
			return Errorf(CodeBadParams, "%s with scope agent needs an agent", method)
		}
	case "crew":
		if strings.TrimSpace(q.Crew) == "" {
			return Errorf(CodeBadParams, "%s with scope crew needs a crew", method)
		}
	case "global":
	case ScopeTeams:
		if strings.TrimSpace(q.Agent) != "" || strings.TrimSpace(q.Member) != "" {
			return Errorf(CodeBadParams, "%s with scope teams takes no agent or member; crew may name one top-level team", method)
		}
	default:
		return Errorf(CodeBadParams, "%s needs a scope of agent, crew, global or teams", method)
	}
	return nil
}

func init() {
	eventShapes[EventDeliverableChanged] = reflect.TypeFor[DeliverableChanged]()
	methods["deliverables.list"] = typed(func(e Engine, p DeliverablesQuery) (*DeliverablesPage, error) {
		d, err := deliverer(e, "deliverables.list")
		if err != nil {
			return nil, err
		}
		if err := checkDeliverablesQuery(p, "deliverables.list"); err != nil {
			return nil, err
		}
		if p.Limit == 0 {
			p.Limit = DeliverablePageDefault
		}
		p.Limit = min(p.Limit, DeliverablePageMax)
		page, err := d.ListDeliverables(p)
		if err != nil {
			return nil, err
		}
		if page.Deliverables == nil {
			page.Deliverables = []Deliverable{}
		}
		return &page, nil
	})
	methods["deliverables.count"] = typed(func(e Engine, p DeliverablesQuery) (*DeliverablesCount, error) {
		d, err := deliverer(e, "deliverables.count")
		if err != nil {
			return nil, err
		}
		if err := checkDeliverablesQuery(p, "deliverables.count"); err != nil {
			return nil, err
		}
		count, err := d.CountDeliverables(p)
		return &count, err
	})
	methods["deliverables.get"] = typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}) (*Deliverable, error) {
		d, err := deliverer(e, "deliverables.get")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "deliverables.get needs an agent and an id")
		}
		got, err := d.GetDeliverable(p.Agent, p.ID)
		return &got, err
	})
	methods["deliverables.read"] = typed(func(e Engine, p struct {
		Agent    string `json:"agent"`
		ID       string `json:"id"`
		Version  string `json:"version"`
		MaxBytes int    `json:"max_bytes"`
	}) (*DeliverableText, error) {
		d, err := deliverer(e, "deliverables.read")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" || p.MaxBytes < 0 {
			return nil, Errorf(CodeBadParams, "deliverables.read needs an agent, an id and a max_bytes of zero or more")
		}
		if p.MaxBytes == 0 || p.MaxBytes > DeliverableReadMax {
			p.MaxBytes = DeliverableReadMax
		}
		text, err := d.ReadDeliverable(p.Agent, p.ID, p.Version, p.MaxBytes)
		return &text, err
	})
	methods["deliverables.keep"] = typed(func(e Engine, p struct {
		Agent         string `json:"agent"`
		ID            string `json:"id"`
		Dir           string `json:"dir"`
		Name          string `json:"name"`
		ExpectVersion string `json:"expect_version"`
	}) (*Deliverable, error) {
		d, err := deliverer(e, "deliverables.keep")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Dir) == "" {
			return nil, Errorf(CodeBadParams, "deliverables.keep needs an agent, an id and a dir")
		}
		got, err := d.KeepDeliverable(p.Agent, p.ID, p.Dir, p.Name, p.ExpectVersion)
		return &got, err
	})
	methods["deliverables.remove"] = typed(func(e Engine, p struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}) (*Empty, error) {
		d, err := deliverer(e, "deliverables.remove")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.ID) == "" {
			return nil, Errorf(CodeBadParams, "deliverables.remove needs an agent and an id")
		}
		return done(d.RemoveDeliverable(p.Agent, p.ID))
	})
	methods["deliverables.snapshot"] = typed(func(e Engine, p struct {
		Agent   string `json:"agent"`
		Path    string `json:"path"`
		URL     string `json:"url"`
		Process string `json:"process"`
	}) (*Deliverable, error) {
		d, err := deliverer(e, "deliverables.snapshot")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Path) == "" || strings.TrimSpace(p.URL) == "" {
			return nil, Errorf(CodeBadParams, "deliverables.snapshot needs an agent, a path and a url")
		}
		got, err := d.SnapshotDeliverable(p.Agent, p.Path, p.URL, p.Process)
		return &got, err
	})
	methods["branches.list"] = typed(func(e Engine, p BranchesQuery) (*BranchesResult, error) {
		d, err := deliverer(e, "branches.list")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Agent) == "" {
			return nil, Errorf(CodeBadParams, "branches.list needs an agent")
		}
		if p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "branches.list needs a limit of zero or more")
		}
		if p.Limit == 0 {
			p.Limit = BranchPageDefault
		}
		p.Limit = min(p.Limit, BranchPageMax)
		got, err := d.TaskBranches(p)
		if got.Branches == nil {
			got.Branches = []TaskBranch{}
		}
		return &got, err
	})
}
