package desktopbridge

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/council"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The two derived fields a Home carries beyond the structure: "Since yesterday"
// (a roll-up of what its conversations wrote about themselves) and the context
// line (what the place carries into a chat). NEITHER CALLS A MODEL, and neither
// is drawn from anything but a file this engine already keeps.

// RecapReader hands back the recap a conversation persisted about itself, or
// nil when it has none that can be trusted. It is the seam between this door and
// the conversation's own record: tests inject one, and the default
// ([metaRecaps]) reads the "recap" member of the session folder's meta.json —
// the same bytes the History tab's engine writes (internal/session/recap.go) —
// through a local decode, so this file depends on the persisted JSON and not on
// the session package's Go type for it.
type RecapReader interface {
	ReadRecap(chatID, dir string) *placegraph.DigestRecap
}

const (
	// digestReadMax bounds how many conversations' meta.json one GET may look at.
	// The newest-spoken come first, so a place with hundreds of chats costs the
	// same as one with sixty.
	digestReadMax = 60
	// recapMetaMax bounds the bytes read from one meta.json.
	recapMetaMax = 1 << 20
	// recapCacheMax and recapCacheTTL bound the default reader's memory: an entry
	// not looked at for the TTL is dropped when the cache is full.
	recapCacheMax = 512
	recapCacheTTL = 10 * time.Minute
)

// metaRecaps is the default RecapReader. It stats a conversation's meta.json on
// every call (one cheap syscall) and re-reads it only when its size or mtime
// moved, so a recap that was rewritten is seen on the next GET and an unchanged
// one costs no read.
type metaRecaps struct {
	mu    sync.Mutex
	now   func() time.Time
	cache map[string]recapEntry
}

type recapEntry struct {
	mtime time.Time
	size  int64
	recap *placegraph.DigestRecap
	seen  time.Time
}

func newMetaRecaps(now func() time.Time) *metaRecaps {
	return &metaRecaps{now: now, cache: map[string]recapEntry{}}
}

func (m *metaRecaps) ReadRecap(chatID, dir string) *placegraph.DigestRecap {
	// A chat id is a folder name and the row's directory must be that folder: a
	// row whose Dir does not end in its own id is not read at all, so a damaged
	// or hostile index can only ever name a meta.json inside its own folder.
	if chatID == "" || dir == "" || strings.ContainsAny(chatID, `/\`) || filepath.Base(filepath.Clean(dir)) != chatID {
		return nil
	}
	path := filepath.Join(dir, "meta.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > recapMetaMax {
		return nil
	}
	now := m.now()
	m.mu.Lock()
	if e, ok := m.cache[path]; ok && e.size == info.Size() && e.mtime.Equal(info.ModTime()) {
		e.seen = now
		m.cache[path] = e
		m.mu.Unlock()
		return e.recap
	}
	m.mu.Unlock()
	recap := readMetaRecap(path, chatID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cache) >= recapCacheMax {
		for k, e := range m.cache {
			if now.Sub(e.seen) > recapCacheTTL {
				delete(m.cache, k)
			}
		}
		if len(m.cache) >= recapCacheMax {
			m.cache = map[string]recapEntry{}
		}
	}
	m.cache[path] = recapEntry{mtime: info.ModTime(), size: info.Size(), recap: recap, seen: now}
	return recap
}

// readMetaRecap decodes only the identity and the recap. A file that is not
// JSON, names a different conversation, or has no usable recap answers nil.
func readMetaRecap(path, chatID string) *placegraph.DigestRecap {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, recapMetaMax+1))
	if err != nil || len(raw) > recapMetaMax {
		return nil
	}
	var meta struct {
		ID    string `json:"id"`
		Recap *struct {
			Line      string    `json:"line"`
			Outcome   string    `json:"outcome"`
			UpdatedAt time.Time `json:"updatedAt"`
			Messages  int       `json:"messages"`
		} `json:"recap"`
	}
	if json.Unmarshal(raw, &meta) != nil || meta.ID != chatID || meta.Recap == nil {
		return nil
	}
	r := meta.Recap
	return &placegraph.DigestRecap{Line: r.Line, Outcome: r.Outcome, UpdatedAt: r.UpdatedAt, Messages: r.Messages}
}

func (p *Places) recapReader() RecapReader {
	if p.Recaps != nil {
		return p.Recaps
	}
	p.recapOnce.Do(func() { p.recapDefault = newMetaRecaps(p.now) })
	return p.recapDefault
}

// whereIn names the active place in the subtree a chat is filed in, the way
// attention attributes it.
func (x *placeIndex) whereIn(id string, subtree map[string]bool) placegraph.Place {
	for _, m := range x.snap.PlacesOf(id) {
		if !subtree[m.PlaceID] {
			continue
		}
		if pl, ok := x.snap.Place(m.PlaceID); ok && !pl.Archived {
			return pl
		}
	}
	return placegraph.Place{}
}

// sinceYesterday rolls up the recaps of the given chats. It returns nil when
// there is no evidence, so the Home draws no section.
func (p *Places) sinceYesterday(x *placeIndex, ids []string, subtree map[string]bool) *placegraph.Digest {
	cands := make([]*session.SessionRow, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if r := x.rows[id]; r != nil && !r.Archived && !seen[id] {
			seen[id] = true
			cands = append(cands, r)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if !cands[i].At.Equal(cands[j].At) {
			return cands[i].At.After(cands[j].At)
		}
		return cands[i].ID < cands[j].ID
	})
	if len(cands) > digestReadMax {
		cands = cands[:digestReadMax]
	}
	reader := p.recapReader()
	in := placegraph.DigestInput{Now: x.now}
	for _, r := range cands {
		where := x.whereIn(r.ID, subtree)
		in.Chats = append(in.Chats, placegraph.DigestChat{
			ID: r.ID, Title: r.Title, PlaceID: where.ID, PlaceName: where.Name,
			NeedsYou: r.NeedsPerson(), Running: rowRuns(r), FailedTasks: r.Tasks.Failed,
			Recap: reader.ReadRecap(r.ID, r.Dir),
		})
	}
	d := placegraph.RollUp(in)
	if d.Empty() {
		return nil
	}
	return &d
}

// contextLine says, in the engine's own counts, what a place carries into a
// chat: whether it has instructions, how many sources, and how many of those
// could not be found or read. It is empty for a place that carries nothing —
// never a sentence saying so (the emptiness law).
func contextLine(d PlaceDetail) string {
	var parts []string
	if strings.TrimSpace(d.Instructions) != "" {
		parts = append(parts, "Instructions")
	}
	missing, unreadable := 0, 0
	for _, s := range d.Sources {
		switch s.Check.State {
		case "missing":
			missing++
		case "unreadable":
			unreadable++
		}
	}
	if n := len(d.Sources); n > 0 {
		parts = append(parts, plural(n, "source"))
	}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing", missing))
	}
	if unreadable > 0 {
		parts = append(parts, fmt.Sprintf("%d unreadable", unreadable))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Live row kinds. They are the Home's own words for what is still going:
// a discussion between two places, a task a window still holds, work that
// kept going after the window let go, and a question waiting on the person.
const (
	liveKindDiscussion = "discussion"
	liveKindRunning    = "running"
	liveKindClosed     = "closed"
	liveKindNeedsYou   = "needsYou"
)

// homeFeed is the Live list and the decided count for one Home.
//
// Live holds running tasks, work that is still running though the chat is
// closed, discussions that have not ended (with the turns taken and the
// cap), and chats that need the person. A discussion that has decided or
// been escalated is not live: it is a chat in History. Decided counts
// discussions filed in this place or below that reached an outcome. Zero
// is returned as 0 so the field is omitted.
//
// A council chat is not also listed as a task or a needs-you row. The
// discussion row is that chat.
func (p *Places) homeFeed(x *placeIndex, homeID string, ids []string, subtree map[string]bool) ([]homeLiveRow, int) {
	var discussions []liveDiscussion
	var running, closed, needs []homeLiveRow
	councilChats := map[string]bool{}
	decided := 0
	if p.Discussions != nil {
		all, err := p.Discussions()
		if err == nil {
			for _, c := range all {
				if !councilOnHome(c, homeID, subtree) {
					continue
				}
				if c.State == council.StateDecided {
					decided++
					continue
				}
				// Escalated, and anything that is not still going, leaves Live.
				// A paused discussion stays: the person started typing, and it
				// has not ended.
				if c.State != council.StateRunning && c.State != council.StatePaused {
					continue
				}
				councilChats[c.ChatID] = true
				row := homeLiveRow{
					Kind: liveKindDiscussion, CouncilID: c.ID, ChatID: c.ChatID,
					Title: c.Label, Detail: c.Topic, Turn: liveTurn(c.Turns), Of: c.Cap,
					StartedAt: c.OpenedAt,
				}
				if p.CouncilFile != nil {
					row.SessionFile = p.CouncilFile(c.ChatID)
				}
				discussions = append(discussions, liveDiscussion{row: row, paused: c.State == council.StatePaused, at: c.OpenedAt})
			}
		}
	}
	sort.SliceStable(discussions, func(i, j int) bool {
		if discussions[i].paused != discussions[j].paused {
			return !discussions[i].paused
		}
		if !discussions[i].at.Equal(discussions[j].at) {
			return discussions[i].at.After(discussions[j].at)
		}
		return discussions[i].row.CouncilID < discussions[j].row.CouncilID
	})

	for _, r := range x.liveRows(ids) {
		if councilChats[r.ID] {
			continue
		}
		if r.NeedsPerson() {
			title := r.Reason()
			if title == "" {
				title = strings.TrimSpace(r.Title)
			}
			if title == "" {
				continue
			}
			needs = append(needs, homeLiveRow{Kind: liveKindNeedsYou, ChatID: r.ID, SessionFile: r.Transcript, Title: title})
			continue
		}
		emitted := false
		for _, entry := range r.Tasks.Rows {
			if entry.Parent != "" || !r.Runs(entry) {
				continue
			}
			emitted = true
			running, closed = appendLiveTask(running, closed, r, entry)
		}
		if emitted || !rowRuns(r) {
			continue
		}
		row := homeLiveRow{ChatID: r.ID, SessionFile: r.Transcript, Title: strings.TrimSpace(r.Title)}
		if row.Title == "" {
			continue
		}
		if r.Open {
			row.Kind = liveKindRunning
			running = append(running, row)
		} else {
			row.Kind = liveKindClosed
			closed = append(closed, row)
		}
	}
	sortLiveTasks(running)
	sortLiveTasks(closed)
	sort.SliceStable(needs, func(i, j int) bool { return needs[i].ChatID < needs[j].ChatID })

	// Needs-you is kept ahead of the cap, then discussions, then tasks.
	// The list is drawn discussions, running, closed, needs-you.
	kept := make([]homeLiveRow, 0, len(needs)+len(discussions)+len(running)+len(closed))
	kept = append(kept, needs...)
	for _, d := range discussions {
		kept = append(kept, d.row)
	}
	kept = append(kept, running...)
	kept = append(kept, closed...)
	if len(kept) > attentionMax {
		kept = kept[:attentionMax]
	}
	sort.SliceStable(kept, func(i, j int) bool { return liveKindRank(kept[i].Kind) < liveKindRank(kept[j].Kind) })
	if kept == nil {
		kept = []homeLiveRow{}
	}
	return kept, decided
}

// liveDiscussion is one discussion row plus the facts the sort needs.
type liveDiscussion struct {
	row    homeLiveRow
	paused bool
	at     time.Time
}

func liveTurn(n int) *int {
	v := n
	return &v
}

// councilOnHome reports that a discussion is filed in this Home. All places
// sees every discussion. A place sees one when either side sits in its subtree.
func councilOnHome(c council.Council, homeID string, subtree map[string]bool) bool {
	if homeID == placegraph.RootID {
		return true
	}
	return subtree[c.Places[0]] || subtree[c.Places[1]]
}

func appendLiveTask(running, closed []homeLiveRow, r *session.SessionRow, entry session.TaskIndexEntry) ([]homeLiveRow, []homeLiveRow) {
	title := strings.TrimSpace(entry.Title)
	if title == "" {
		title = strings.TrimSpace(entry.Label)
	}
	if title == "" {
		title = strings.TrimSpace(r.Title)
	}
	if title == "" {
		return running, closed
	}
	row := homeLiveRow{ChatID: r.ID, SessionFile: r.Transcript, TaskID: entry.ID, Title: title, StartedAt: entry.StartedAt}
	if chat := strings.TrimSpace(r.Title); chat != "" && chat != title {
		row.Detail = chat
	}
	if r.Open {
		row.Kind = liveKindRunning
		return append(running, row), closed
	}
	row.Kind = liveKindClosed
	return running, append(closed, row)
}

func sortLiveTasks(rows []homeLiveRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].StartedAt.Equal(rows[j].StartedAt) {
			return rows[i].StartedAt.After(rows[j].StartedAt)
		}
		if rows[i].TaskID != rows[j].TaskID {
			return rows[i].TaskID < rows[j].TaskID
		}
		return rows[i].ChatID < rows[j].ChatID
	})
}

func liveKindRank(kind string) int {
	switch kind {
	case liveKindDiscussion:
		return 0
	case liveKindRunning:
		return 1
	case liveKindClosed:
		return 2
	default:
		return 3
	}
}
