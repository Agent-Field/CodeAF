package desktopbridge

import (
	"net/http"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// StatusRollup is what a place says about the work inside it. Every number is a
// count of chats the canonical world could read; a chat it could not read is
// counted nowhere here (see missingChats) and is never drawn as idle or running.
type StatusRollup struct {
	// Chats is how many readable, non-archived conversations are counted.
	Chats int `json:"chats"`
	// Running is chats with live task work, or whose live presence says working.
	Running int `json:"running"`
	// NeedsYou is chats whose live presence says they are waiting on a person.
	NeedsYou int `json:"needsYou"`
	// Incomplete is chats with task rows that were under way when a window went.
	Incomplete int `json:"incomplete"`
	// FailedTasks is the all-time sum of failed task rows. It is a count of
	// history, not of news: no "new" claim is made from it.
	FailedTasks int `json:"failedTasks"`
}

// PlaceView is one place as the rail, the tiles and the palette draw it.
type PlaceView struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Parents         []string          `json:"parents"`
	Tint            placegraph.Tint   `json:"tint"`
	EffectiveTint   placegraph.Tint   `json:"effectiveTint"`
	Archived        bool              `json:"archived"`
	CreatedAt       time.Time         `json:"createdAt,omitzero"`
	LastOpenedAt    time.Time         `json:"lastOpenedAt,omitzero"`
	ArchivedAt      time.Time         `json:"archivedAt,omitzero"`
	Pinned          bool              `json:"pinned"`
	Counts          placegraph.Counts `json:"counts"`
	Status          StatusRollup      `json:"status"`
	StatusInclusive StatusRollup      `json:"statusInclusive"`
	HasInstructions bool              `json:"hasInstructions"`
	SourceCount     int               `json:"sourceCount"`
	AlsoIn          []PlaceRef        `json:"alsoIn"`
}

// PlaceRef names a place without its weight.
type PlaceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PlaceDetail is a place with its whole context, for its Home.
type PlaceDetail struct {
	PlaceView
	Instructions string            `json:"instructions"`
	Sources      []SourceView      `json:"sources"`
	Policy       placegraph.Policy `json:"policy"`
}

// RailView is the rail's two sections. Open is derived, never stored.
type RailView struct {
	Pinned          []PlaceView `json:"pinned"`
	Open            []PlaceView `json:"open"`
	OpenWindowHours int         `json:"openWindowHours"`
}

const (
	// railOpenWindow is how long a visited place stays in the rail's Open
	// section without work inside it (Places 6d: a place idle for 12 h goes quiet).
	railOpenWindow = 12 * time.Hour
	railOpenMax    = 12
	homeChatsMax   = 100
	attentionMax   = 50
)

// ChatPlace is one active place a chat is filed in.
type ChatPlace struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Tint    placegraph.Tint    `json:"tint"`
	AddedBy placegraph.AddedBy `json:"addedBy"`
}

// ChatTasks is a conversation's task counts as the index records them.
type ChatTasks struct {
	Running    int `json:"running"`
	Incomplete int `json:"incomplete"`
	Done       int `json:"done"`
	Failed     int `json:"failed"`
}

// ChatRow is one conversation on a place's Home.
type ChatRow struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Project   string `json:"project"`
	Workspace string `json:"workspace"`
	// SessionFile is the conversation's journal, the path a window reattaches with (POST /sessions). It is how a Home
	// row opens the chat it names; nothing else is derived from it.
	SessionFile string             `json:"sessionFile,omitempty"`
	At          time.Time          `json:"at,omitzero"`
	Created     time.Time          `json:"created,omitzero"`
	Model       string             `json:"model,omitempty"`
	Archived    bool               `json:"archived"`
	Live        bool               `json:"live"`
	Doing       string             `json:"doing"`
	NeedsYou    bool               `json:"needsYou"`
	Reason      string             `json:"reason,omitempty"`
	Tasks       ChatTasks          `json:"tasks"`
	Places      []ChatPlace        `json:"places"`
	AddedBy     placegraph.AddedBy `json:"addedBy,omitempty"`
}

// Attention is one thing in a place that is running or waiting on the person.
type Attention struct {
	Kind      string    `json:"kind"`
	ChatID    string    `json:"chatId"`
	ChatTitle string    `json:"chatTitle"`
	PlaceID   string    `json:"placeId"`
	PlaceName string    `json:"placeName"`
	Text      string    `json:"text"`
	TaskID    string    `json:"taskId,omitempty"`
	Since     time.Time `json:"since,omitzero"`
}

// placeIndex is one consistent reading: a graph snapshot and a world, with the
// per-place chat sets worked out once.
type placeIndex struct {
	snap *placegraph.Snapshot
	rows map[string]*session.SessionRow
	// order is every readable session id, newest-spoken first, for Now.
	order  []string
	direct map[string][]string // active place -> chats filed in it
	incl   map[string][]string // active place -> chats in it or below, once each
	now    time.Time
}

func newPlaceIndex(snap *placegraph.Snapshot, world session.World, now time.Time) *placeIndex {
	x := &placeIndex{snap: snap, rows: map[string]*session.SessionRow{}, direct: map[string][]string{}, incl: map[string][]string{}, now: now}
	sessions := world.Sessions()
	for i := range sessions {
		row := &sessions[i]
		if row.DeletionPending || row.ID == "" {
			continue
		}
		x.rows[row.ID] = row
		x.order = append(x.order, row.ID)
	}
	active := map[string]bool{}
	for _, pl := range snap.Places {
		if !pl.Archived {
			active[pl.ID] = true
		}
	}
	for _, m := range snap.Memberships {
		if active[m.PlaceID] {
			x.direct[m.PlaceID] = append(x.direct[m.PlaceID], m.ChatID)
		}
	}
	for _, pl := range snap.Places {
		if !pl.Archived {
			x.incl[pl.ID] = x.inclusive(pl.ID)
		}
	}
	return x
}

// inclusive unions a place's chats with those of every active descendant, each
// chat once (a diamond's shared child does not double anything).
func (x *placeIndex) inclusive(id string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(pid string) {
		for _, c := range x.direct[pid] {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	add(id)
	for _, d := range x.snap.Descendants(id, false) {
		add(d.ID)
	}
	return out
}

func rowRuns(r *session.SessionRow) bool {
	return r.Tasks.Running > 0 || (r.Live && r.Presence.State == session.PresenceWorking)
}

// rollup counts the readable chats among ids.
func (x *placeIndex) rollup(ids []string) StatusRollup {
	var s StatusRollup
	for _, id := range ids {
		r := x.rows[id]
		if r == nil || r.Archived {
			continue
		}
		s.Chats++
		if r.NeedsPerson() {
			s.NeedsYou++
		}
		if rowRuns(r) {
			s.Running++
		}
		if r.Tasks.Incomplete > 0 {
			s.Incomplete++
		}
		s.FailedTasks += r.Tasks.Failed
	}
	return s
}

func (x *placeIndex) counts(id string) placegraph.Counts {
	return placegraph.Counts{
		Children:       len(x.snap.Children(id, false)),
		Descendants:    len(x.snap.Descendants(id, false)),
		Chats:          len(x.direct[id]),
		ChatsInclusive: len(x.incl[id]),
	}
}

func (x *placeIndex) pinned(id string) bool {
	for _, p := range x.snap.Pinned {
		if p == id {
			return true
		}
	}
	return false
}

func (x *placeIndex) view(pl placegraph.Place) PlaceView {
	tint, _ := x.snap.EffectiveTint(pl.ID)
	v := PlaceView{
		ID: pl.ID, Name: pl.Name, Parents: append([]string{}, pl.Parents...), Tint: pl.Tint, EffectiveTint: tint,
		Archived: pl.Archived, CreatedAt: pl.CreatedAt, LastOpenedAt: pl.LastOpenedAt, ArchivedAt: pl.ArchivedAt,
		Pinned: x.pinned(pl.ID), HasInstructions: pl.Context.Instructions != "", SourceCount: len(pl.Context.Sources),
		AlsoIn: []PlaceRef{},
	}
	if pl.Archived {
		v.Counts = placegraph.Counts{Chats: len(x.snap.ChatsIn(pl.ID, false))}
		v.Status = x.rollup(x.snap.ChatsIn(pl.ID, false))
		v.StatusInclusive = v.Status
	} else {
		v.Counts = x.counts(pl.ID)
		v.Status = x.rollup(x.direct[pl.ID])
		v.StatusInclusive = x.rollup(x.incl[pl.ID])
	}
	for _, parent := range pl.Parents[min(1, len(pl.Parents)):] {
		if pp, ok := x.snap.Place(parent); ok {
			v.AlsoIn = append(v.AlsoIn, PlaceRef{ID: pp.ID, Name: pp.Name})
		}
	}
	return v
}

func (x *placeIndex) detail(pl placegraph.Place) PlaceDetail {
	d := PlaceDetail{PlaceView: x.view(pl), Instructions: pl.Context.Instructions, Policy: pl.Policy, Sources: []SourceView{}}
	for _, src := range pl.Context.Sources {
		d.Sources = append(d.Sources, x.sourceView(src))
	}
	return d
}

func (x *placeIndex) views(places []placegraph.Place) []PlaceView {
	out := make([]PlaceView, 0, len(places))
	for _, pl := range places {
		out = append(out, x.view(pl))
	}
	return out
}

// rail derives the two sections. The store keeps only the pinned order; "open"
// is the places the person has visited lately or that have work in them.
func (x *placeIndex) rail() RailView {
	rv := RailView{Pinned: []PlaceView{}, Open: []PlaceView{}, OpenWindowHours: int(railOpenWindow / time.Hour)}
	for _, pl := range x.snap.PinnedPlaces() {
		if !pl.Archived {
			rv.Pinned = append(rv.Pinned, x.view(pl))
		}
	}
	var open []PlaceView
	for _, pl := range x.snap.Places {
		if pl.Archived || x.pinned(pl.ID) || pl.LastOpenedAt.IsZero() {
			continue
		}
		v := x.view(pl)
		busy := v.StatusInclusive.Running > 0 || v.StatusInclusive.NeedsYou > 0
		if busy || x.now.Sub(pl.LastOpenedAt) < railOpenWindow {
			open = append(open, v)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].LastOpenedAt.After(open[j].LastOpenedAt) })
	if len(open) > railOpenMax {
		open = open[:railOpenMax]
	}
	if open != nil {
		rv.Open = open
	}
	return rv
}

// Totals are the numbers a footer or the first-run decision reads.
type Totals struct {
	Places       int `json:"places"`
	Placed       int `json:"placed"`
	Unplaced     int `json:"unplaced"`
	Running      int `json:"running"`
	NeedsYou     int `json:"needsYou"`
	MissingChats int `json:"missingChats"`
}

// NowView is the unplaced bucket's own rollup.
type NowView struct {
	Chats  int          `json:"chats"`
	Status StatusRollup `json:"status"`
}

func (x *placeIndex) unplaced() []string { return x.snap.Unplaced(x.order) }

func (x *placeIndex) totals() Totals {
	t := Totals{}
	for _, pl := range x.snap.Places {
		if !pl.Archived {
			t.Places++
		}
	}
	placed := x.snap.ChatsIn(placegraph.RootID, true)
	t.Placed = len(placed)
	for _, c := range placed {
		if x.rows[c] == nil {
			t.MissingChats++
		}
	}
	t.Unplaced = len(x.unplaced())
	for _, r := range x.rows {
		if r.Archived {
			continue
		}
		if r.NeedsPerson() {
			t.NeedsYou++
		}
		if rowRuns(r) {
			t.Running++
		}
	}
	return t
}

func (x *placeIndex) nowView() NowView {
	ids := x.unplaced()
	return NowView{Chats: len(ids), Status: x.rollup(ids)}
}

// chatRow builds one conversation's row, or false when the world cannot read it.
func (x *placeIndex) chatRow(id string, in string) (ChatRow, bool) {
	r := x.rows[id]
	if r == nil {
		return ChatRow{}, false
	}
	row := ChatRow{
		ID: r.ID, Title: r.Title, Project: r.Project, Workspace: r.Workspace, SessionFile: r.Transcript, At: r.At, Created: r.Created, Model: r.Model,
		Archived: r.Archived, Live: r.Live, Doing: r.Doing(), NeedsYou: r.NeedsPerson(), Reason: r.Reason(),
		Tasks:  ChatTasks{Running: r.Tasks.Running, Incomplete: r.Tasks.Incomplete, Done: r.Tasks.Done, Failed: r.Tasks.Failed},
		Places: []ChatPlace{},
	}
	for _, m := range x.snap.PlacesOf(id) {
		pl, ok := x.snap.Place(m.PlaceID)
		if !ok || pl.Archived {
			continue
		}
		tint, _ := x.snap.EffectiveTint(pl.ID)
		row.Places = append(row.Places, ChatPlace{ID: pl.ID, Name: pl.Name, Tint: tint, AddedBy: m.AddedBy})
		if pl.ID == in {
			row.AddedBy = m.AddedBy
		}
	}
	return row, true
}

// chatRows lists readable rows newest-spoken first, capped, and says how many
// ids the world could not read.
func (x *placeIndex) chatRows(ids []string, in string) (rows []ChatRow, truncated bool, missing int) {
	rows = []ChatRow{}
	for _, id := range ids {
		if row, ok := x.chatRow(id, in); ok {
			rows = append(rows, row)
		} else {
			missing++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })
	if len(rows) > homeChatsMax {
		rows, truncated = rows[:homeChatsMax], true
	}
	return rows, truncated, missing
}

// attention lists what is waiting on the person or running in the given chats:
// the needs-you conversations first, then live task rows. Each is attributed to
// the place in this subtree the chat is filed in.
func (x *placeIndex) attention(ids []string, subtree map[string]bool) []Attention {
	var needs, running []Attention
	for _, id := range ids {
		r := x.rows[id]
		if r == nil || r.Archived {
			continue
		}
		where := placegraph.Place{}
		for _, m := range x.snap.PlacesOf(id) {
			if !subtree[m.PlaceID] {
				continue
			}
			if pl, ok := x.snap.Place(m.PlaceID); ok && !pl.Archived {
				where = pl
				break
			}
		}
		if r.NeedsPerson() {
			needs = append(needs, Attention{Kind: "needsYou", ChatID: r.ID, ChatTitle: r.Title, PlaceID: where.ID, PlaceName: where.Name, Text: r.Reason()})
		}
		for _, entry := range r.Tasks.Rows {
			if r.Runs(entry) {
				running = append(running, Attention{Kind: "running", ChatID: r.ID, ChatTitle: r.Title, PlaceID: where.ID, PlaceName: where.Name, Text: entry.Label, TaskID: entry.ID, Since: entry.StartedAt})
			}
		}
	}
	sort.SliceStable(running, func(i, j int) bool { return running[i].Since.After(running[j].Since) })
	out := append(needs, running...)
	if len(out) > attentionMax {
		out = out[:attentionMax]
	}
	if out == nil {
		out = []Attention{}
	}
	return out
}

// ---- read routes -----------------------------------------------------------

type graphResponse struct {
	Revision uint64               `json:"revision"`
	Places   []PlaceView          `json:"places"`
	Rail     RailView             `json:"rail"`
	Now      NowView              `json:"now"`
	Totals   Totals               `json:"totals"`
	ReadAt   time.Time            `json:"readAt"`
	Recovery *placegraph.Recovery `json:"recovery,omitempty"`
}

func (p *Places) graph(w http.ResponseWriter, r *http.Request) {
	x, ok := p.open(w, false)
	if !ok {
		return
	}
	withArchived := r.URL.Query().Get("archived") == "1"
	out := graphResponse{Revision: x.snap.Revision, Places: []PlaceView{}, Rail: x.rail(), Now: x.nowView(), Totals: x.totals(), ReadAt: x.now, Recovery: p.Store.LastRecovery()}
	for _, pl := range x.snap.Places {
		if pl.Archived && !withArchived {
			continue
		}
		out.Places = append(out.Places, x.view(pl))
	}
	write(w, out)
}

type statusEntry struct {
	Status          StatusRollup `json:"status"`
	StatusInclusive StatusRollup `json:"statusInclusive"`
}

func (p *Places) status(w http.ResponseWriter) {
	x, ok := p.open(w, false)
	if !ok {
		return
	}
	places := map[string]statusEntry{}
	for _, pl := range x.snap.Places {
		if pl.Archived {
			continue
		}
		places[pl.ID] = statusEntry{Status: x.rollup(x.direct[pl.ID]), StatusInclusive: x.rollup(x.incl[pl.ID])}
	}
	write(w, map[string]any{"revision": x.snap.Revision, "readAt": x.now, "places": places, "now": x.nowView(), "totals": x.totals()})
}

func (p *Places) railRoute(w http.ResponseWriter) {
	x, ok := p.open(w, false)
	if !ok {
		return
	}
	write(w, x.rail())
}

// Crumb is one step of a breadcrumb.
type Crumb struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Tint placegraph.Tint `json:"tint"`
}

type digestResponse struct {
	Kind           string            `json:"kind"`
	Title          string            `json:"title"`
	Place          *PlaceDetail      `json:"place,omitempty"`
	Breadcrumb     []Crumb           `json:"breadcrumb"`
	Children       []PlaceView       `json:"children"`
	Chats          []ChatRow         `json:"chats"`
	ChatsTruncated bool              `json:"chatsTruncated"`
	Attention      []Attention       `json:"attention"`
	Status         StatusRollup      `json:"status"`
	Counts         placegraph.Counts `json:"counts"`
	MissingChats   int               `json:"missingChats"`
	// Recap is "Since yesterday": a roll-up of what this Home's conversations
	// wrote about themselves in the last day. Absent when there is no evidence.
	Recap *placegraph.Digest `json:"recap,omitempty"`
	// ContextLine is what the place carries into a chat, in counts. Absent for a
	// place with no instructions and no sources, and for root and Now.
	ContextLine string    `json:"contextLine,omitempty"`
	Revision    uint64    `json:"revision"`
	ReadAt      time.Time `json:"readAt"`
}

// digest is a place's Home (and All places' and Now's): its children as tiles,
// its chats, and what inside it is waiting or running. One page, whichever door
// opened it.
func (p *Places) digest(w http.ResponseWriter, id string) {
	x, ok := p.open(w, true)
	if !ok {
		return
	}
	out := digestResponse{Breadcrumb: []Crumb{}, Revision: x.snap.Revision, ReadAt: x.now}
	var ids []string
	subtree := map[string]bool{}
	switch id {
	case placegraph.RootID:
		out.Kind, out.Title = "root", "All places"
		top := x.snap.Children(placegraph.RootID, false)
		out.Children = x.views(top)
		ids = x.snap.ChatsIn(placegraph.RootID, true)
		for _, pl := range x.snap.Places {
			subtree[pl.ID] = true
		}
		out.Counts = placegraph.Counts{Children: len(top), Descendants: len(x.snap.Descendants(placegraph.RootID, false)), ChatsInclusive: len(ids)}
	case placegraph.NowID:
		out.Kind, out.Title = "now", "Now"
		out.Children = []PlaceView{}
		ids = x.unplaced()
		out.Counts = placegraph.Counts{Chats: len(ids), ChatsInclusive: len(ids)}
	default:
		pl, found := x.snap.Place(id)
		if !found {
			failPlaces(w, 404, "not_found", "That place doesn't exist any more.")
			return
		}
		out.Kind, out.Title = "place", pl.Name
		d := x.detail(pl)
		out.Place = &d
		for _, c := range x.snap.Breadcrumb(id) {
			tint, _ := x.snap.EffectiveTint(c.ID)
			out.Breadcrumb = append(out.Breadcrumb, Crumb{ID: c.ID, Name: c.Name, Tint: tint})
		}
		out.Children = x.views(x.snap.Children(id, false))
		ids = x.snap.ChatsIn(id, false)
		subtree[id] = true
		for _, dsc := range x.snap.Descendants(id, false) {
			subtree[dsc.ID] = true
		}
		out.Counts = d.Counts
	}
	out.Chats, out.ChatsTruncated, out.MissingChats = x.chatRows(ids, id)
	// Attention reaches below the place: a child's waiting chat is this place's news.
	attn := ids
	if out.Kind == "place" {
		if inc, active := x.incl[id]; active {
			attn = inc
		} else {
			attn = x.snap.ChatsIn(id, true) // an archived Home is still readable
		}
	}
	out.Attention = x.attention(attn, subtree)
	if out.Kind == "place" {
		out.ContextLine = contextLine(*out.Place)
	}
	out.Recap = p.sinceYesterday(x, attn, subtree)
	out.Status = x.rollup(ids)
	write(w, out)
}

func (p *Places) deletePreview(w http.ResponseWriter, id string) {
	snap, err := p.Store.Snapshot()
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	imp, err := snap.PreviewDelete(id)
	if err != nil {
		p.failStore(w, err, "", nil)
		return
	}
	write(w, imp)
}

type chatPlacesResponse struct {
	Workspace   string           `json:"workspace,omitempty"`
	SessionFile string           `json:"sessionFile,omitempty"`
	ChatID      string           `json:"chatId"`
	Known       bool             `json:"known"`
	Places      []chatMembership `json:"places"`
}

type chatMembership struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Tint     placegraph.Tint    `json:"tint"`
	AddedBy  placegraph.AddedBy `json:"addedBy"`
	At       time.Time          `json:"at,omitzero"`
	Archived bool               `json:"archived"`
}

func (p *Places) chatPlaces(w http.ResponseWriter, chatID string) {
	x, ok := p.open(w, false)
	if !ok {
		return
	}
	out := chatPlacesResponse{ChatID: chatID, Known: x.knowsChat(p, chatID), Places: []chatMembership{}}
	if row := x.rows[chatID]; row != nil {
		out.Workspace = row.Workspace
		out.SessionFile = row.Transcript
	}
	for _, m := range x.snap.PlacesOf(chatID) {
		pl, found := x.snap.Place(m.PlaceID)
		if !found {
			continue
		}
		tint, _ := x.snap.EffectiveTint(pl.ID)
		out.Places = append(out.Places, chatMembership{ID: pl.ID, Name: pl.Name, Tint: tint, AddedBy: m.AddedBy, At: m.At, Archived: pl.Archived})
	}
	write(w, out)
}

// knowsChat reports whether a chat id names a real conversation: one the world
// has read, or one this bridge holds open right now.
func (x *placeIndex) knowsChat(p *Places, id string) bool {
	if x.rows[id] != nil {
		return true
	}
	if p.live != nil {
		for _, live := range p.live() {
			if live == id {
				return true
			}
		}
	}
	return false
}
