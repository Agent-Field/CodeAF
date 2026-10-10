package desktopbridge

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The engine-wide world feed. It answers one question for the whole window —
// what is every conversation on this machine doing, and which of them is
// stopped on the person — from the same persisted record every surface reads
// (session.ReadHome), so a closed tab, a background conversation and a
// conversation whose window is gone are all visible.
//
// THE FEED OWNS NO STATE OF ITS OWN. A row is a projection of a
// session.SessionRow and nothing is added to it: no status is invented, no
// transcript is carried, and no model is called. Until the engine can notify the
// bridge of a change, one bounded poll of the persisted world (worldInterval) runs
// while at least one reader is attached and stops with the last.

// WorldRow is one conversation as the window reads it. Every field is lifted
// from the canonical SessionRow; a field the engine could not say is omitted.
type WorldRow struct {
	Session string `json:"session"`
	Title   string `json:"title"`
	// Project is the bucket's display name and Workspace the recorded tools root.
	Project   string `json:"project"`
	Workspace string `json:"workspace,omitempty"`
	// SessionFile is the conversation's journal, the path a window reattaches with (POST /sessions); omitted when unknown.
	SessionFile string `json:"sessionFile,omitempty"`
	// SourceFolders are referenced project folders, NOT memberships in the Places graph. The project comes first, then folders it
	// turned out to be about, newest first. Paths only; never content.
	SourceFolders []string `json:"sourceFolders"`
	Model         string   `json:"model,omitempty"`
	// State is the presence file's own word ("working", "waiting on you", "idle")
	// while the claim is fresh, "open" when a window holds the journal without a
	// fresh claim, and "closed" otherwise. Nothing translates the engine's word.
	State string `json:"state"`
	// Live is the session's fresh claim; Open is the kernel's lock answer.
	Live bool `json:"live"`
	Open bool `json:"open"`
	// Running is true only while a fresh presence says a turn is in flight.
	Running bool `json:"running"`
	// NeedsYou is true only for a live conversation stopped on a question.
	NeedsYou bool `json:"needsYou"`
	// Failed counts landed-failed task rows; it never says a conversation failed.
	Failed int `json:"failed"`
	// UnseenFailed counts landed-failed tasks that arrived after the failure mark every window shares
	// (session.Meta.FailuresSeen); Failure names the newest one so a window marks exactly what it was shown.
	// Neither changes Running, NeedsYou or Failed: a seen failure hides nothing else.
	UnseenFailed int         `json:"unseenFailed"`
	Failure      *WorldFail  `json:"failure,omitempty"`
	Tasks        WorldTasks  `json:"tasks"`
	LiveTasks    []WorldTask `json:"liveTasks,omitempty"`
	At           string      `json:"at,omitempty"`
	Archived     bool        `json:"archived,omitempty"`
	Pending      bool        `json:"deletionPending,omitempty"`
	Reason       string      `json:"reason,omitempty"`
}

// WorldFail is a failure's identity: the task and the instant it landed (RFC 3339, nanoseconds), spelled exactly as
// POST /world/failures/seen wants them back.
type WorldFail struct {
	Task string `json:"task"`
	At   string `json:"at"`
}

// WorldTasks are the project index's counts for the conversation's tasks.
type WorldTasks struct {
	Running    int `json:"running"`
	Incomplete int `json:"incomplete"`
	Done       int `json:"done"`
	Failed     int `json:"failed"`
	Total      int `json:"total"`
}

// WorldTask is one task the conversation genuinely has out right now
// (SessionRow.Runs); finished work stays in the index.
type WorldTask struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
	Phase string `json:"phase,omitempty"`
}

// AttentionItem is one thing waiting on the person, from a live presence file
// that names a question. A conversation that stopped refreshing asks nothing.
type AttentionItem struct {
	// Key is stable for the life of the question: session, kind and id.
	Key     string `json:"key"`
	Session string `json:"session"`
	Kind    string `json:"kind"`
	// ID is the token an answer would name; zero when the lane gave none.
	ID            uint64   `json:"id,omitempty"`
	Text          string   `json:"text"`
	SourceFolders []string `json:"sourceFolders"`
	Title         string   `json:"title,omitempty"`
	// Answerable says the asking window described options another window may
	// answer. This feed never answers; the answer door is the session endpoint.
	Answerable bool   `json:"answerable"`
	Asked      string `json:"asked,omitempty"`
	// Full-only fields preserve the wire shape of older presence files.
	Blocking   *AttentionBlocking   `json:"blocking,omitempty"`
	Stakes     session.Stakes       `json:"stakes,omitempty"`
	Suggestion *AttentionSuggestion `json:"suggestion,omitempty"`
	PlaceIDs   []string             `json:"placeIds,omitempty"`
	PlaceNames []string             `json:"placeNames,omitempty"`
	HoldingUp  []string             `json:"holdingUp,omitempty"`
}

// AttentionBlocking carries the question's own account of what is paused.
type AttentionBlocking struct {
	Turn  bool     `json:"turn"`
	Tasks []string `json:"tasks,omitempty"`
}

// AttentionSuggestion names an offered answer, never a label guessed from its key.
type AttentionSuggestion struct {
	Key     string `json:"key"`
	Label   string `json:"label,omitempty"`
	Percent int    `json:"percent,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// decideLedger is the folder of per-place decision files beside the graph
// this bridge was given. It is read when the world is projected, not when the
// feed is built, so a graph attached a moment later is still the one consulted.
func (b *Bridge) decideLedger() string {
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil || p.door == nil || strings.TrimSpace(p.door.Path) == "" {
		return ""
	}
	return session.DecideLedgerDir(p.door.Path)
}

// attentionGraph reads the graph attached to this bridge, rather than a global store.
func (b *Bridge) attentionGraph() *placegraph.Snapshot {
	b.mu.Lock()
	p := b.places
	b.mu.Unlock()
	if p == nil || p.Store == nil {
		return nil
	}
	graph, err := p.Store.Snapshot()
	if err != nil {
		return nil
	}
	return graph
}

// worldDelta is the payload of a "world" record: changed rows and vanished ids.
type worldDelta struct {
	Rows    []WorldRow `json:"rows"`
	Removed []string   `json:"removed"`
}

// attentionDelta is the payload of an "attention" record. It carries the whole
// current set, because the set is small and a delta of questions invites a
// reader to keep a question the engine withdrew.
type attentionDelta struct {
	Items []AttentionItem `json:"items"`
}

// worldFull is the payload of a "reset" record and the body of GET /world.
type worldFull struct {
	Rows  []WorldRow      `json:"rows"`
	Items []AttentionItem `json:"items"`
}

const maxLiveTasks = 20

func projectWorld(w session.World, ledger string, graphs ...*placegraph.Snapshot) ([]WorldRow, []AttentionItem) {
	rows := []WorldRow{}
	items := []AttentionItem{}
	for _, project := range w.Projects {
		for _, r := range project.Sessions {
			row := projectRow(project, r)
			rows = append(rows, row)
			if r.NeedsPerson() {
				// A settled question is no longer for the person, even while
				// an older presence stamp is still fresh.
				if !attentionPending(r) {
					continue
				}
				// A place ledger is the other proof an answer was delivered.
				// Confidence is not: a sure place that never wrote the answer
				// still needs the person.
				full := r.Presence.Question.Full
				if ledger != "" && full != nil && len(graphs) > 0 && graphs[0] != nil {
					var ids []string
					for _, membership := range graphs[0].PlacesOf(r.ID) {
						ids = append(ids, membership.PlaceID)
					}
					if session.LedgerDecided(ledger, r.ID, string(full.Kind), full.Token(), ids) {
						continue
					}
				}
				item := attentionFor(r, row)
				if r.Presence.Question.Full != nil && len(graphs) > 0 && graphs[0] != nil {
					for _, membership := range graphs[0].PlacesOf(r.ID) {
						if place, ok := graphs[0].Place(membership.PlaceID); ok {
							item.PlaceIDs = append(item.PlaceIDs, place.ID)
							item.PlaceNames = append(item.PlaceNames, place.Name)
						}
					}
				}
				items = append(items, item)
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Session < rows[j].Session })
	sort.SliceStable(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return rows, items
}

// attentionPending trusts recorded answers and withdrawals, never a place's
// confidence or policy as proof that an answer was delivered.
func attentionPending(r session.SessionRow) bool {
	full := r.Presence.Question.Full
	if full == nil {
		return true
	}
	if full.Withdrawn != nil {
		return false
	}
	if r.Dir == "" {
		return true
	}
	records, err := session.ReadDecisions(r.Dir)
	if err != nil {
		return true
	}
	for _, record := range records {
		if record.Kind == full.Kind && record.ID == full.ID && record.Ref == full.Ref &&
			record.Head == full.Head && record.Subject == full.Subject &&
			(full.Asked.IsZero() || !record.At.Before(full.Asked)) {
			return false
		}
	}
	return true
}

func projectRow(p session.Project, r session.SessionRow) WorldRow {
	place := []string{}
	if p.Path != "" {
		place = append(place, p.Path)
	}
	for _, ref := range r.Places {
		place = append(place, ref.Path)
	}
	state := "closed"
	switch {
	case r.Live:
		state = string(r.Presence.State)
	case r.Open:
		state = "open"
	}
	row := WorldRow{
		Session: r.ID, Title: r.Title, Project: p.Name, Workspace: r.Workspace, SessionFile: r.Transcript, SourceFolders: place, Model: r.Model,
		State: state, Live: r.Live, Open: r.Open,
		Running:  r.Live && r.Presence.State == session.PresenceWorking,
		NeedsYou: r.NeedsPerson(), Failed: r.Tasks.Failed, UnseenFailed: r.UnseenFailures(),
		Tasks:    WorldTasks{Running: r.Tasks.Running, Incomplete: r.Tasks.Incomplete, Done: r.Tasks.Done, Failed: r.Tasks.Failed, Total: r.Tasks.Total()},
		Archived: r.Archived, Pending: r.DeletionPending, Reason: r.Reason(),
	}
	if !r.At.IsZero() {
		row.At = r.At.UTC().Format(time.RFC3339)
	}
	if newest, ok := r.NewestFailure(); ok {
		row.Failure = &WorldFail{Task: newest.ID, At: newest.EndedAt.UTC().Format(time.RFC3339Nano)}
	}
	for _, entry := range r.Tasks.Rows {
		if len(row.LiveTasks) == maxLiveTasks {
			break
		}
		if r.Runs(entry) {
			row.LiveTasks = append(row.LiveTasks, WorldTask{ID: entry.ID, Label: entry.Label, State: entry.Status, Phase: string(r.Phase(entry))})
		}
	}
	return row
}

func attentionFor(r session.SessionRow, row WorldRow) AttentionItem {
	q := r.Presence.Question
	text := strings.TrimSpace(q.Text)
	if text == "" {
		text = r.Reason()
	}
	item := AttentionItem{Session: r.ID, Kind: "question", ID: q.ID, Text: text, SourceFolders: row.SourceFolders, Title: r.Title, Answerable: q.Answerable()}
	if q.Kind != "" {
		item.Kind = string(q.Kind)
	}
	item.Key = r.ID + ":" + item.Kind + ":" + uitoa(q.ID)
	if !q.Asked.IsZero() {
		item.Asked = q.Asked.UTC().Format(time.RFC3339)
	}
	if full := q.Full; full != nil {
		item.Blocking = &AttentionBlocking{Turn: full.Blocking.Turn, Tasks: append([]string(nil), full.Blocking.Tasks...)}
		item.Stakes = full.Stakes
		item.HoldingUp = append([]string(nil), full.Blocking.Tasks...)
		if pick := full.Pick; pick != nil {
			suggestion := &AttentionSuggestion{Key: pick.Key, Percent: pick.ResolvedPercent(), Reason: pick.Reason}
			if option, ok := full.Option(pick.Key); ok {
				suggestion.Label = option.Label
			}
			item.Suggestion = suggestion
		}
	}
	return item
}

func uitoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
