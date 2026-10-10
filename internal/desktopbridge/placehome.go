package desktopbridge

// GET /places/{id}/home is the place Home the desktop draws (Places 8a, 8c, 9a).
//
// It is a different read from GET /places/{id}. That older door still answers
// the clients already on it. This one answers the Home itself: the place, the
// primary-parent breadcrumb, the chats filed here with the one line History
// already saved for each, the child tiles, and the attention rolled up from
// every place below. "Since you were last here" is composed only from recap
// lines that already exist. No model is called.

import (
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

func init() {
	registerPlacesRoute("GET /places/{id}/home", func(p *Places, w http.ResponseWriter, r *http.Request, id string) {
		p.home(w, id)
	})
}

// homeSince is the short note under a place's title. Text is at most two
// sentences of recap lines. Anchor is when the place was last opened, so a
// client can say "Since yesterday" without this door inventing the label.
type homeSince struct {
	Text   string `json:"text"`
	Anchor string `json:"anchor"`
}

// homeAttention is one chat in this home that needs the person, is running, or
// has a failed task. OriginPlaceID is the place the chat is filed in, which
// may be a descendant of the home being read.
type homeAttention struct {
	ChatID        string    `json:"chatId"`
	Title         string    `json:"title"`
	OriginPlaceID string    `json:"originPlaceId,omitempty"`
	State         string    `json:"state"`
	StartedAt     time.Time `json:"startedAt,omitzero"`
}

// homeChild is one tile. NeedsYou and Failed count chats in the place and
// below, each chat once; zero is omitted so a quiet tile draws no dot. AlsoIn
// is the names of the parents after the first.
type homeChild struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	EffectiveTint placegraph.Tint `json:"effectiveTint"`
	NeedsYou      int             `json:"needsYou,omitempty"`
	Failed        int             `json:"failed,omitempty"`
	Chats         int             `json:"chats,omitempty"`
	ChildPlaces   int             `json:"childPlaces,omitempty"`
	AlsoIn        []string        `json:"alsoIn,omitempty"`
}

// homeChat is one conversation on the home. Digest is the recap line or absent.
// State is needsYou, running or failed; an idle chat omits it.
type homeChat struct {
	ChatID       string    `json:"chatId"`
	SessionFile  string    `json:"sessionFile,omitempty"`
	Title        string    `json:"title"`
	Digest       string    `json:"digest,omitempty"`
	State        string    `json:"state,omitempty"`
	TasksRunning int       `json:"tasksRunning,omitempty"`
	At           time.Time `json:"at,omitzero"`
}

// homeLiveRow is one thing still happening on this Home. Kind is discussion,
// running, closed or needsYou. Turn and Of are set only for a discussion:
// how many turns it has taken, and the cap. A closed row is work that is
// still running after the chat's window let go of it.
type homeLiveRow struct {
	Kind        string    `json:"kind"`
	ChatID      string    `json:"chatId,omitempty"`
	SessionFile string    `json:"sessionFile,omitempty"`
	TaskID      string    `json:"taskId,omitempty"`
	CouncilID   string    `json:"councilId,omitempty"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail,omitempty"`
	Turn        *int      `json:"turn,omitempty"`
	Of          int       `json:"of,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitzero"`
}

// homeResponse is GET /places/{id}/home. Unplaced is present only for All
// places (id root), and only when at least one conversation is in no place.
// Live is the work still going, including discussions. Decided is how many
// discussions filed here have reached an outcome; zero is omitted.
type homeResponse struct {
	Place      *PlaceDetail    `json:"place,omitempty"`
	Breadcrumb []Crumb         `json:"breadcrumb"`
	Since      *homeSince      `json:"since,omitempty"`
	Attention  []homeAttention `json:"attention"`
	Live       []homeLiveRow   `json:"live"`
	Decided    int             `json:"decided,omitempty"`
	Children   []homeChild     `json:"children"`
	Chats      []homeChat      `json:"chats,omitempty"`
	Unplaced   []homeChat      `json:"unplaced,omitempty"`
}

func (p *Places) home(w http.ResponseWriter, id string) {
	if id == placegraph.NowID {
		failPlaces(w, 404, "not_found", "That place doesn't exist any more.")
		return
	}
	x, ok := p.open(w, true)
	if !ok {
		return
	}
	out := homeResponse{Breadcrumb: []Crumb{}, Attention: []homeAttention{}, Children: []homeChild{}, Live: []homeLiveRow{}}
	var direct, incl []string
	var visited time.Time
	subtree := map[string]bool{}
	if id == placegraph.RootID {
		for _, pl := range x.snap.Children(placegraph.RootID, false) {
			out.Children = append(out.Children, x.homeChild(pl))
		}
		for _, pl := range x.snap.Places {
			if !pl.Archived {
				subtree[pl.ID] = true
			}
		}
		incl = x.snap.ChatsIn(placegraph.RootID, true)
		out.Unplaced = p.homeChats(x, x.unplaced())
	} else {
		pl, found := x.snap.Place(id)
		if !found {
			failPlaces(w, 404, "not_found", "That place doesn't exist any more.")
			return
		}
		d := x.detail(pl)
		out.Place = &d
		visited = pl.LastOpenedAt
		for _, c := range x.snap.Breadcrumb(id) {
			tint, _ := x.snap.EffectiveTint(c.ID)
			out.Breadcrumb = append(out.Breadcrumb, Crumb{ID: c.ID, Name: c.Name, Tint: tint})
		}
		for _, pl := range x.snap.Children(id, false) {
			out.Children = append(out.Children, x.homeChild(pl))
		}
		subtree[id] = true
		for _, dsc := range x.snap.Descendants(id, false) {
			subtree[dsc.ID] = true
		}
		direct = x.snap.ChatsIn(id, false)
		if ids, active := x.incl[id]; active {
			incl = ids
		} else {
			incl = x.snap.ChatsIn(id, true)
		}
		out.Chats = p.homeChats(x, direct)
		out.Since = p.homeSince(x, incl, visited)
	}
	out.Attention = x.homeAttention(incl, subtree)
	out.Live, out.Decided = p.homeFeed(x, id, incl, subtree)
	write(w, out)
}

// homeChild is the tile. Counts cover the place and everything below it, each
// chat once, so a parent tile lights up for a question that lives in a child.
func (x *placeIndex) homeChild(pl placegraph.Place) homeChild {
	tint, _ := x.snap.EffectiveTint(pl.ID)
	c := homeChild{ID: pl.ID, Name: pl.Name, EffectiveTint: tint, ChildPlaces: len(x.snap.Children(pl.ID, false))}
	ids, active := x.incl[pl.ID]
	if !active {
		ids = x.snap.ChatsIn(pl.ID, true)
	}
	for _, id := range ids {
		r := x.rows[id]
		if r == nil || r.Archived {
			continue
		}
		c.Chats++
		if r.NeedsPerson() {
			c.NeedsYou++
		}
		if chatFailed(r) {
			c.Failed++
		}
	}
	for _, parent := range pl.Parents[min(1, len(pl.Parents)):] {
		if pp, ok := x.snap.Place(parent); ok {
			c.AlsoIn = append(c.AlsoIn, pp.Name)
		}
	}
	return c
}

// homeChats lists readable, current conversations newest-spoken first. An
// archived conversation is History's, not the home's. The cap is the same one
// the older home read uses, so one place cannot make this answer unbounded.
func (p *Places) homeChats(x *placeIndex, ids []string) []homeChat {
	rows := x.liveRows(ids)
	if len(rows) > homeChatsMax {
		rows = rows[:homeChatsMax]
	}
	if len(rows) == 0 {
		return nil
	}
	reader := p.recapReader()
	out := make([]homeChat, 0, len(rows))
	for _, r := range rows {
		chat := homeChat{ChatID: r.ID, SessionFile: r.Transcript, Title: r.Title, TasksRunning: r.Tasks.Running, At: r.At}
		chat.Digest = digestLine(reader.ReadRecap(r.ID, r.Dir))
		chat.State, _ = homeChatState(r)
		out = append(out, chat)
	}
	return out
}

// digestLine is the sentence under a chat's title.
//
// It is the recap line the history lane already wrote on Meta.Recap, which is
// the same sentence a history row shows as its one line. A history row with no
// recap line has no second line either, so an empty result is nothing: not the
// title, not the long account, and not a line composed here.
func digestLine(recap *placegraph.DigestRecap) string {
	if recap == nil {
		return ""
	}
	return strings.TrimSpace(recap.Line)
}

// homeSince quotes recap lines of chats that changed after the place was last
// opened, newest first, and stops at two sentences. A place that has never
// been opened has no "since", and neither does a visit nothing has moved past.
// The words are only lines that already exist. This does not call a model.
func (p *Places) homeSince(x *placeIndex, ids []string, visited time.Time) *homeSince {
	if visited.IsZero() {
		return nil
	}
	rows := x.liveRows(ids)
	if len(rows) > digestReadMax {
		rows = rows[:digestReadMax]
	}
	reader := p.recapReader()
	var lines []string
	for _, r := range rows {
		recap := reader.ReadRecap(r.ID, r.Dir)
		// "Changed" is the person speaking again, or the saved recap being
		// rewritten, after the visit. An older recap of a chat that has moved
		// is still the only sentence we have; nothing here asks for a new one.
		changed := r.At.After(visited) || (recap != nil && recap.UpdatedAt.After(visited))
		if !changed {
			continue
		}
		if line := digestLine(recap); line != "" {
			lines = append(lines, line)
		}
	}
	text := twoSentences(lines)
	if text == "" {
		return nil
	}
	return &homeSince{Text: text, Anchor: visited.UTC().Format(time.RFC3339Nano)}
}

// twoSentences keeps the first two sentences across already-ordered lines. A
// period, question mark or exclamation mark followed by a space or the end of
// the line ends a sentence; a chat that saved one sentence is one sentence.
func twoSentences(lines []string) string {
	var got []string
	for _, line := range lines {
		for _, sentence := range sentencesOf(line) {
			got = append(got, sentence)
			if len(got) == 2 {
				return strings.Join(got, " ")
			}
		}
	}
	return strings.Join(got, " ")
}

func sentencesOf(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var parts []string
	var b strings.Builder
	rs := []rune(line)
	for i, r := range rs {
		b.WriteRune(r)
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 == len(rs) || unicode.IsSpace(rs[i+1]) {
			if s := strings.TrimSpace(b.String()); s != "" {
				parts = append(parts, s)
			}
			b.Reset()
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		parts = append(parts, s)
	}
	return parts
}

// liveRows is the readable, current conversations among ids, newest spoken
// first. A chat the world could not read is left out rather than drawn idle.
func (x *placeIndex) liveRows(ids []string) []*session.SessionRow {
	seen := map[string]bool{}
	var rows []*session.SessionRow
	for _, id := range ids {
		r := x.rows[id]
		if r == nil || r.Archived || seen[id] {
			continue
		}
		seen[id] = true
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].At.Equal(rows[j].At) {
			return rows[i].At.After(rows[j].At)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// homeAttention rolls chats that need a person, are running, or failed up from
// the whole subtree. One row per chat. The origin is the deepest place in the
// subtree the chat is filed in, so a parent home names the child the chat
// actually lives in.
func (x *placeIndex) homeAttention(ids []string, subtree map[string]bool) []homeAttention {
	var out []homeAttention
	for _, r := range x.liveRows(ids) {
		state, started := homeChatState(r)
		if state == "" {
			continue
		}
		origin, _ := x.originIn(r.ID, subtree)
		out = append(out, homeAttention{ChatID: r.ID, Title: r.Title, OriginPlaceID: origin, State: state, StartedAt: started})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if homeStateRank(out[i].State) != homeStateRank(out[j].State) {
			return homeStateRank(out[i].State) < homeStateRank(out[j].State)
		}
		if out[i].State == "running" && !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ChatID < out[j].ChatID
	})
	if len(out) > attentionMax {
		out = out[:attentionMax]
	}
	if out == nil {
		out = []homeAttention{}
	}
	return out
}

func homeStateRank(state string) int {
	switch state {
	case "needsYou":
		return 0
	case "failed":
		return 1
	default:
		return 2
	}
}

// homeChatState picks one state. Waiting on the person wins, then live work,
// then a failed task. Idle is the empty string, which the home draws as nothing.
func homeChatState(r *session.SessionRow) (string, time.Time) {
	if r.NeedsPerson() {
		return "needsYou", time.Time{}
	}
	if rowRuns(r) {
		return "running", earliestRunningStart(r)
	}
	if ok, at := failedAt(r); ok {
		return "failed", at
	}
	return "", time.Time{}
}

func earliestRunningStart(r *session.SessionRow) time.Time {
	var at time.Time
	for _, entry := range r.Tasks.Rows {
		if !r.Runs(entry) || entry.StartedAt.IsZero() {
			continue
		}
		if at.IsZero() || entry.StartedAt.Before(at) {
			at = entry.StartedAt
		}
	}
	return at
}

// chatFailed reports a conversation that has a failed task, whether or not it
// is also running or waiting. The tile's red count and the row's state ask
// different questions, so a live chat can still add to the count.
func chatFailed(r *session.SessionRow) bool {
	ok, _ := failedAt(r)
	return ok
}

// failedAt is the newest failed task's start, or its end when the row never
// recorded a start. Tasks.Failed with no row still counts: the number is real
// and the time is unknown, so the time is left zero.
func failedAt(r *session.SessionRow) (bool, time.Time) {
	var at time.Time
	found := false
	for _, entry := range r.Tasks.Rows {
		if entry.Status != string(session.TaskFailed) {
			continue
		}
		found = true
		stamp := entry.StartedAt
		if stamp.IsZero() {
			stamp = entry.EndedAt
		}
		if !stamp.IsZero() && (at.IsZero() || stamp.After(at)) {
			at = stamp
		}
	}
	if !found && r.Tasks.Failed > 0 {
		return true, time.Time{}
	}
	return found, at
}

// originIn is the deepest place in the subtree that holds this chat. Depth
// follows Parents[0], the same chain the breadcrumb walks. Two places at the
// same depth keep the one the chat was filed in first.
func (x *placeIndex) originIn(chatID string, subtree map[string]bool) (string, int) {
	bestID := ""
	bestDepth := -1
	for _, m := range x.snap.PlacesOf(chatID) {
		if !subtree[m.PlaceID] {
			continue
		}
		pl, ok := x.snap.Place(m.PlaceID)
		if !ok || pl.Archived {
			continue
		}
		d := x.depthIn(pl.ID, subtree)
		if d > bestDepth {
			bestID, bestDepth = pl.ID, d
		}
	}
	return bestID, bestDepth
}

func (x *placeIndex) depthIn(id string, subtree map[string]bool) int {
	n := 0
	cur := id
	for i := 0; i <= len(x.snap.Places); i++ {
		pl, ok := x.snap.Place(cur)
		if !ok || len(pl.Parents) == 0 {
			return n
		}
		parent := pl.Parents[0]
		if !subtree[parent] {
			return n
		}
		n++
		cur = parent
	}
	return n
}
