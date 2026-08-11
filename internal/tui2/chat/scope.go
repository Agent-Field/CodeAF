package chat

import (
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The scope adapter: the store's board and plan sight, in the rail's words.
//
// internal/tui2/rail declares exactly one data contract ([rail.ScopeSource]) and
// its doc comment states the mapping this file implements field for field. That
// is the whole of the seam: the rail never learns what a store is, this file
// never learns what a rail looks like, and the two meet on a value type.
//
// Three properties are load-bearing.
//
//   - IT IS BUILT ONCE PER JOURNAL MOVE, never per frame. The rail repaints on
//     every keystroke and on the animation clock; a source that read the graph
//     from Scope() would turn a cursor move into a snapshot query. The cache is
//     stamped with the journal watermark the app already polls, so "has anything
//     changed" is a comparison rather than a read.
//   - IT IS TOTAL. Every scope the model can ask for is built in the same pass
//     that builds home, so descending into a task cannot fail halfway and cannot
//     issue a second read. A source that cannot answer returns false, and the
//     rail treats that as "this row has no room to descend into" — which is
//     exactly what a leaf worker is.
//   - IT NEVER HANDS BACK AN ID TO DRAW. [rail.Row.ID] is the cursor's handle
//     and the sub-scope key; every Name and Status on it is a human word. 5.14's
//     never-shown tier is enforced by there being nothing else to show, and
//     13.3.4 is the same rule caught in the wild: a raw session id on screen is
//     a bug, not a placeholder.

// Graph is the optional slice of the durable store the scope map reads.
//
// It is deliberately NOT part of [Backend]. Backend is the three calls a chat
// surface cannot exist without, and every test fake in this package implements
// it; a rail that widened it would have made "can this window draw a
// conversation" depend on "can this window read a DAG". A backend that does not
// implement Graph gets an honest empty rail — `aforge`, the room list, and the
// doors that are not built yet — which is the same thing a brand new database
// shows.
//
// *store.Store satisfies it, structurally, with no adapter at the entry point.
type Graph interface {
	// ActiveSnapshot is the board: the live nodes and the edges between them.
	ActiveSnapshot() (store.Snapshot, error)
	// TopLevelJobUsage is the money per top-level job — 5.9's one number the
	// user never forgives us for hiding.
	TopLevelJobUsage() (map[string]store.JobUsage, error)
	// OpenQuestions is every question still waiting on the user, surfaced or
	// not. A question is a fact about the work, not about whether a room has
	// shown it yet, so it is asked for the whole window and attributed to the
	// subtree that raised it.
	OpenQuestions(sessionID string, limit int) ([]store.AgentQuestion, error)
	// NodeMessages is a task room's transcript: the node-anchored trail 4.6
	// says a v1 room IS — a view over the same journal, not a second one.
	NodeMessages(nodeID string, afterSeq int64, limit int) ([]store.Message, error)
}

// Rooms is the optional thread-switcher slice (12.1.3 item 3, 5.24).
//
// OpenSession is what makes an EMPTY room possible: until that event existed a
// session was a projection of the messages naming it, so a switcher could not
// mint a room before its first message. RenameSession is named here because the
// same door retitles one, and a room's title is what 13.3.4 says the rail must
// draw instead of its id.
type Rooms interface {
	Sessions() ([]store.Session, error)
	OpenSession(id, title, surface string) (store.Session, error)
	RenameSession(id, title string) (store.Session, error)
}

// Row id prefixes. They are the vocabulary the shell's navigation switches on,
// and they exist because one cursor moves over four different kinds of thing —
// a room, a job, a door that is not built, and the head itself. A prefix is
// never rendered (5.14); it is how [rail.Row.ID] stays a handle.
const (
	rowHomeID     = "home"
	rowNewRoomID  = "new-room"
	rowMoreID     = "more"
	rowRoomPrefix = "room:"
	rowTaskPrefix = "task:"
)

// scopeLimits bound what one rail can hold. None of them is a policy about what
// exists — they are what a 28-column column can show before the fold line does
// a better job of accounting for the rest (8.1.7).
const (
	// maxRoomRows is how many other rooms the home thread list carries. The
	// current room is always among them.
	maxRoomRows = 6
	// maxTaskRows bounds the task cards. Unsettled work is kept first, so the
	// cap only ever drops history.
	maxTaskRows = 24
	// maxQuestionRead is the question page. A window with more open questions
	// than this has a bigger problem than a rail.
	maxQuestionRead = 200
	// maxSubtreeRows bounds one task scope's DAG rendering.
	maxSubtreeRows = 60
	// waitsOnCap is how many upstreams one row names before the list stops
	// being the reason it is sitting still (head.go's boardWaitsCap, same
	// number for the same reason).
	waitsOnCap = 3
)

// scopeSource is the [rail.ScopeSource] over a real store.
type scopeSource struct {
	graph   Graph
	rooms   Rooms
	now     func() time.Time
	session string

	// built scopes, keyed by scope id. home is held apart because it is the one
	// scope that always exists, even when nothing else does.
	home  rail.Scope
	tasks map[string]rail.Scope

	// stamp is the journal position the cache was built at, and ready says a
	// build has happened. Together they are the whole invalidation rule: the
	// poll already knows when the journal moved, so the rail never asks.
	stamp int64
	ready bool

	// titles is the room-title lookup the status line reads, so a breadcrumb
	// never has to fall back to an id (13.3.4).
	titles map[string]string
	// label caches each node's drawable name, so the DAG walk names a waits-on
	// edge without re-deriving the label per edge.
	label map[string]string

	// spendCost is what the CURRENT room has cost, and haveSpend is whether the
	// journal has a run to show for it. See SetRoomSpend.
	spendCost float64
	haveSpend bool
}

var _ rail.ScopeSource = (*scopeSource)(nil)

// newScopeSource builds a source over whatever the backend turned out to be.
// Both interfaces are optional and independent: a store with a graph and no
// session table, or the reverse, each degrades to the half it can answer.
func newScopeSource(backend Backend, session string, now func() time.Time) *scopeSource {
	source := &scopeSource{
		now:     now,
		session: session,
		tasks:   make(map[string]rail.Scope, 8),
		titles:  make(map[string]string, 8),
		label:   make(map[string]string, 32),
	}
	if graph, ok := backend.(Graph); ok {
		source.graph = graph
	}
	if rooms, ok := backend.(Rooms); ok {
		source.rooms = rooms
	}
	return source
}

// Scope implements [rail.ScopeSource].
//
// It is a map lookup and nothing else. Every read this source makes happens in
// refresh; a Scope call that reached the store would be a store read per
// keystroke, which is the production bar this wave is held to.
func (s *scopeSource) Scope(id string) (rail.Scope, bool) {
	if !s.ready {
		return rail.Scope{}, false
	}
	if id == rail.HomeScopeID {
		return s.home, true
	}
	scope, ok := s.tasks[id]
	return scope, ok
}

// liveScope is the HUD's scope: the work, and nothing else.
//
// The bounded summary of 8.2.8 carries "one line per live thing", and the home
// rail's members are not all things — the room list, the `+ new` door and the
// collapsed group are NAVIGATION, and a summary that counted them would tell a
// reader with an idle window that three things were pending. The rail's own
// hudWorthy filter reads a row's lifecycle, which is the right question to ask
// of work and a meaningless one to ask of a door; so the doors never reach it.
func (s *scopeSource) liveScope() rail.Scope {
	rows := make([]rail.Row, 0, len(s.home.Rows))
	rows = append(rows, rail.Row{Kind: rail.RowSurface, Name: "aforge"})
	for _, row := range s.home.Rows {
		if strings.HasPrefix(row.ID, rowTaskPrefix) {
			rows = append(rows, row)
		}
	}
	return rail.Scope{ID: rail.HomeScopeID, Title: "aforge", Rows: rows}
}

// hudSource is the [rail.ScopeSource] the bounded summary is built over. It has
// exactly one scope and no rooms to descend into, which is the HUD's whole job
// description: it carries the live summary, never the scope map.
type hudSource struct{ src *scopeSource }

var _ rail.ScopeSource = hudSource{}

// Scope implements [rail.ScopeSource].
func (h hudSource) Scope(id string) (rail.Scope, bool) {
	if h.src == nil || !h.src.ready || id != rail.HomeScopeID {
		return rail.Scope{}, false
	}
	return h.src.liveScope(), true
}

// SetSession re-points the source at the room the window is now in. It changes
// which room row says "you are here" and nothing else — the board is the same
// board from every room.
func (s *scopeSource) SetSession(session string) {
	s.session = session
	s.haveSpend = false
}

// SetRoomSpend records what THIS room has cost, for the room row's own money
// cell (5.9: money is always visible).
//
// Only the current room gets one, and deliberately: the poll already reads this
// room's window on every cycle that moved, so the cell costs nothing extra,
// while asking the same question of six rooms would put six windowed queries
// behind every journal move to fill six cells nobody is looking at. The other
// rooms' rows carry no money rather than a stale one — 8.2.20's missing glyph is
// an honest answer and a cached number is not.
//
// It does not itself invalidate the cache: money moves with the journal, so the
// next rebuild picks it up, and a rebuild forced by a money read alone would be
// a snapshot query per poll.
func (s *scopeSource) SetRoomSpend(session string, cost float64, known bool) {
	if session != s.session {
		return
	}
	s.spendCost, s.haveSpend = cost, known
}

// RoomTitle is the humane name of a room, and never its id (13.3.4). An
// untitled room says so; a room nobody has heard of is named by the only honest
// thing left, which is that it is this window's.
func (s *scopeSource) RoomTitle(id string) string {
	if title := strings.TrimSpace(s.titles[id]); title != "" {
		return title
	}
	return untitledRoom
}

// untitledRoom is what a room with no title is called. The scribe that names
// rooms is head-side; until it runs, "untitled" is the true answer and a
// truncated uuid is not.
const untitledRoom = "untitled room"

// refresh rebuilds every scope from one pass over the store, unless the journal
// has not moved since the last build.
//
// It reports whether anything was rebuilt, so the caller can skip the rail's own
// Refresh — which walks the scope stack and re-merges row order — on a quiet
// poll. force exists for the first build and for a room switch, where the
// journal has not moved but what the rail must say about it has.
func (s *scopeSource) refresh(journal int64, force bool) bool {
	if s.ready && !force && journal == s.stamp {
		return false
	}
	s.stamp = journal
	s.ready = true

	sessions := s.readSessions()
	snapshot, usage, questions := s.readGraph()

	s.tasks = make(map[string]rail.Scope, len(s.tasks))
	s.home = s.buildHome(sessions, snapshot, usage, questions)
	return true
}

// readSessions reads the room list and refreshes the title lookup.
func (s *scopeSource) readSessions() []store.Session {
	if s.rooms == nil {
		return nil
	}
	sessions, err := s.rooms.Sessions()
	if err != nil {
		return nil
	}
	for k := range s.titles {
		delete(s.titles, k)
	}
	for _, session := range sessions {
		s.titles[session.ID] = session.Title
	}
	return sessions
}

// readGraph reads the board in one go. Every read is independent and each one
// degrades on its own: a usage table that will not answer costs the money cell,
// not the rail.
func (s *scopeSource) readGraph() (store.Snapshot, map[string]store.JobUsage, []store.AgentQuestion) {
	if s.graph == nil {
		return store.Snapshot{}, nil, nil
	}
	snapshot, err := s.graph.ActiveSnapshot()
	if err != nil {
		snapshot = store.Snapshot{}
	}
	usage, err := s.graph.TopLevelJobUsage()
	if err != nil {
		usage = nil
	}
	questions, err := s.graph.OpenQuestions("", maxQuestionRead)
	if err != nil {
		questions = nil
	}
	return snapshot, usage, questions
}

// -- the home scope ----------------------------------------------------------

// buildHome assembles row 0 and its members in the order 5.24 sets out: the
// head, the home thread list with its `+ new` door at the foot, the task cards,
// and the collapsed dim group that houses the rest of the product.
//
// The order is deliberate and it is not "most important first". The rooms are
// the thing a person switches between; the tasks are the thing they watch; the
// doors that are not built yet are last, dim, and collapsed, so live work keeps
// the top of the rail exactly as 5.24 asks.
func (s *scopeSource) buildHome(sessions []store.Session, snapshot store.Snapshot,
	usage map[string]store.JobUsage, questions []store.AgentQuestion) rail.Scope {

	rows := make([]rail.Row, 0, 8+len(snapshot.Nodes))
	rows = append(rows, rail.Row{
		ID:       rowHomeID,
		Kind:     rail.RowSurface,
		Name:     "aforge",
		Composer: rail.ComposerChat,
		Status:   s.headStatus(snapshot),
	})
	rows = append(rows, s.roomRows(sessions)...)
	rows = append(rows, s.taskRows(snapshot, usage, questions)...)
	rows = append(rows, rail.Row{
		// 5.24's collapsed dim group. The rows behind it are real parts of the
		// product with real rooms coming; the honest rendering today is one row
		// that says what is in there and opens nothing, rather than four rows
		// that look like doors and are not.
		ID:       rowMoreID,
		Kind:     rail.RowStep,
		Name:     "more",
		Status:   "notebook, self, standing, services",
		Composer: rail.ComposerNone,
	})
	return rail.Scope{ID: rail.HomeScopeID, Title: "aforge", Rows: rows}
}

// headStatus is row 0's first-person line (5.9). It counts the board rather
// than describing it, because the description is the head's to write and this
// surface must not invent one.
//
// 13.3.3 is the reason it counts QUEUED work as well as running: the rail said
// "no live work" beside a reply that had just queued a job, and a board reading
// that only sees `running` is a board that calls an admitted, unstarted job
// nothing at all.
func (s *scopeSource) headStatus(snapshot store.Snapshot) string {
	running, queued, blocked := 0, 0, 0
	for i := range snapshot.Nodes {
		switch snapshot.Nodes[i].Status {
		case store.Running, store.Claimed:
			running++
		case store.Pending:
			if snapshot.Nodes[i].Held {
				blocked++
				continue
			}
			queued++
		}
	}
	if running == 0 && queued == 0 && blocked == 0 {
		return "nothing running"
	}
	parts := make([]string, 0, 3)
	if running > 0 {
		parts = append(parts, plural(running, "running", "running"))
	}
	if queued > 0 {
		parts = append(parts, plural(queued, "queued", "queued"))
	}
	if blocked > 0 {
		parts = append(parts, plural(blocked, "held", "held"))
	}
	return strings.Join(parts, ", ")
}

// roomRows are the home thread list plus the `+ new` door at its foot (5.24).
//
// The current room is always present even when it has aged out of the top of
// the list, because a switcher that cannot show you where you are is a switcher
// that can strand you.
func (s *scopeSource) roomRows(sessions []store.Session) []rail.Row {
	rows := make([]rail.Row, 0, maxRoomRows+1)
	seen := false
	for _, session := range sessions {
		if len(rows) >= maxRoomRows && session.ID != s.session {
			continue
		}
		if session.ID == s.session {
			seen = true
		}
		rows = append(rows, s.roomRow(session))
	}
	if !seen && s.session != "" {
		rows = append([]rail.Row{s.roomRow(store.Session{ID: s.session,
			Title: s.titles[s.session]})}, rows...)
	}
	rows = append(rows, rail.Row{
		ID:       rowNewRoomID,
		Kind:     rail.RowStep,
		Name:     "+ new room",
		Composer: rail.ComposerNone,
		Status:   "start a fresh conversation",
	})
	return rows
}

func (s *scopeSource) roomRow(session store.Session) rail.Row {
	row := rail.Row{
		ID:       rowRoomPrefix + session.ID,
		Kind:     rail.RowStep,
		Name:     roomLabel(session),
		Composer: rail.ComposerChat,
	}
	if session.ID == s.session {
		row.Status = "you are here"
		if s.haveSpend {
			row.Meta.Cost, row.Meta.HasCost = s.spendCost, true
		}
	}
	return row
}

// roomLabel is 13.3.4 made a function: a room is drawn by its title, and a room
// with no title says it has none. The id never reaches a cell.
func roomLabel(session store.Session) string {
	if title := strings.TrimSpace(session.Title); title != "" {
		return title
	}
	return untitledRoom
}

// -- the task cards ----------------------------------------------------------

// taskRows are the top-level job cards, and building them also builds every
// task scope the rail can descend into.
//
// One walk, one set of maps, every scope. The alternative — deriving a task's
// DAG when the cursor enters it — would put a snapshot query behind the enter
// key and would let the card and the scope disagree about the same job, because
// they would have been read at two different times.
func (s *scopeSource) taskRows(snapshot store.Snapshot, usage map[string]store.JobUsage,
	questions []store.AgentQuestion) []rail.Row {

	if len(snapshot.Nodes) == 0 {
		return nil
	}
	byID := make(map[string]store.Node, len(snapshot.Nodes))
	children := make(map[string][]string, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	for _, node := range snapshot.Nodes {
		if node.Parent == "" || node.Parent == store.RootID {
			continue
		}
		children[node.Parent] = append(children[node.Parent], node.ID)
	}
	for k := range s.label {
		delete(s.label, k)
	}
	for _, node := range snapshot.Nodes {
		s.label[node.ID] = nodeLabelOf(node)
	}

	waits := s.waitsOn(snapshot, byID)
	asks := questionCounts(questions, byID)
	now := s.now()

	roots := make([]store.Node, 0, 8)
	for _, node := range snapshot.Nodes {
		if isJobRoot(node, byID) && node.Group != store.TerritoryGroup {
			roots = append(roots, node)
		}
	}
	// Newest first: a rail is read from the top, and the job you just
	// commissioned is the one you are looking for. The rail's own stable-order
	// merge keeps a row from moving once it is visible (7.2), so this order is
	// only ever the order NEW rows arrive in.
	sort.SliceStable(roots, func(i, j int) bool {
		return roots[i].CreatedSeq > roots[j].CreatedSeq
	})

	rows := make([]rail.Row, 0, len(roots))
	for _, root := range roots {
		if len(rows) >= maxTaskRows {
			break
		}
		rows = append(rows, s.taskCard(root, byID, children, usage, waits, asks, now))
		s.tasks[rowTaskPrefix+root.ID] = s.taskScope(root, byID, children, waits, asks, now)
	}
	return rows
}

// taskCard is 5.9's three-line anatomy filled from the graph.
func (s *scopeSource) taskCard(root store.Node, byID map[string]store.Node,
	children map[string][]string, usage map[string]store.JobUsage,
	waits map[string][]string, asks map[string]int, now time.Time) rail.Row {

	row := rail.Row{
		ID:        rowTaskPrefix + root.ID,
		Kind:      rail.RowTask,
		Name:      s.label[root.ID],
		Status:    nodeStatusLine(root),
		Life:      lifeOf(root),
		Composer:  rail.ComposerSteer,
		Seed:      root.ID,
		WaitsOn:   waits[root.ID],
		Questions: asks[root.ID],
	}
	workers, running, longest := subtree(root, byID, children, now)
	if job, ok := usage[root.ID]; ok {
		row.Meta.Cost, row.Meta.HasCost = job.Cost, true
	}
	if workers == 0 {
		row.Meta.Atomic = true
	} else {
		row.Meta.Workers, row.Meta.HasWorkers = workers, true
	}
	if longest > 0 {
		row.Meta.Elapsed, row.Meta.HasElapsed = longest, true
	}
	// A job root usually carries no status of its own worth showing — its parts
	// do the work — so the subtree answers "what is happening" when the node
	// itself has nothing to say. 13.3.3 again: queued parts count.
	if row.Status == "" && running > 0 {
		row.Status = plural(running, "part running", "parts running")
	}
	// A planned job has a chat because it has a plan to redirect; an atomic one
	// is a hand and takes steering mail only (5.11). The mark on the card is a
	// preview of the composer the row will bind, so this is the same decision
	// twice and never two decisions.
	if workers > 0 {
		row.Composer = rail.ComposerChat
	}
	return row
}

// taskScope is the room behind a card: the orchestrator surface, then the plan
// steps and workers as an indented tree with the waits-on structure visible
// (5.15).
func (s *scopeSource) taskScope(root store.Node, byID map[string]store.Node,
	children map[string][]string, waits map[string][]string,
	asks map[string]int, now time.Time) rail.Scope {

	rows := make([]rail.Row, 0, 8)
	rows = append(rows, rail.Row{
		ID:       rowTaskPrefix + root.ID,
		Kind:     rail.RowSurface,
		Name:     s.label[root.ID],
		Status:   nodeStatusLine(root),
		Composer: composerFor(root, children),
		Life:     lifeOf(root),
		Seed:     root.ID,
	})
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		kids := append([]string(nil), children[id]...)
		sort.SliceStable(kids, func(i, j int) bool {
			return byID[kids[i]].CreatedSeq < byID[kids[j]].CreatedSeq
		})
		for _, kid := range kids {
			if len(rows) >= maxSubtreeRows {
				return
			}
			node := byID[kid]
			kind := rail.RowWorker
			if len(children[kid]) > 0 {
				kind = rail.RowStep
			}
			rows = append(rows, rail.Row{
				ID:        rowTaskPrefix + kid,
				Kind:      kind,
				Depth:     depth,
				Name:      s.label[kid],
				Status:    nodeStatusLine(node),
				Life:      lifeOf(node),
				Composer:  rail.ComposerSteer,
				WaitsOn:   waits[kid],
				Questions: asks[kid],
				Seed:      root.ID,
			})
			walk(kid, depth+1)
		}
	}
	walk(root.ID, 1)
	// A scope with members is a room to descend into; one without is a leaf,
	// and the rail's Enter opens its surface in the main pane instead.
	return rail.Scope{ID: rowTaskPrefix + root.ID, Title: s.label[root.ID], Seed: root.ID, Rows: rows}
}

// composerFor is 5.11's fork: a job with a plan has a chat, an atomic one has a
// steer line. It is asked of the graph rather than stored on the row so the two
// can never disagree.
func composerFor(root store.Node, children map[string][]string) rail.ComposerMode {
	if len(children[root.ID]) > 0 {
		return rail.ComposerChat
	}
	return rail.ComposerSteer
}

// -- readings off the graph --------------------------------------------------

// isJobRoot is the store's own definition, read off a snapshot: parented on the
// permanent spine, or on a territory that packed it away. A node whose parent is
// not in the snapshot is a root, because there is nothing to attribute it to and
// dropping it is never an option (internal/head/head.go states the same rule for
// the same reason).
func isJobRoot(node store.Node, byID map[string]store.Node) bool {
	parent := strings.TrimSpace(node.Parent)
	if parent == "" || parent == store.RootID {
		return true
	}
	owner, ok := byID[parent]
	return !ok || owner.Group == store.TerritoryGroup
}

// subtree counts a job's parts and finds the longest thing running under it.
// Queued parts are counted (13.3.3): a job with three admitted, unstarted
// workers is not an empty job.
func subtree(root store.Node, byID map[string]store.Node, children map[string][]string,
	now time.Time) (workers, running int, longest time.Duration) {

	seen := make(map[string]bool, 8)
	stack := []string{root.ID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		node := byID[id]
		if id != root.ID {
			workers++
		}
		switch node.Status {
		case store.Running, store.Claimed:
			running++
			if !node.StartedAt.IsZero() {
				if elapsed := now.Sub(node.StartedAt); elapsed > longest {
					longest = elapsed
				}
			}
		case store.Pending:
			running++
		}
		stack = append(stack, children[id]...)
	}
	return workers, running, longest
}

// waitsOn derives "what is this row sitting behind" from the edges the snapshot
// already carries, in NAMES rather than ids (5.14). Only unsettled upstreams
// count: an edge from work that has landed records where an input came from, and
// calling that a wait would make a moving job read as a stuck one.
func (s *scopeSource) waitsOn(snapshot store.Snapshot, byID map[string]store.Node) map[string][]string {
	if len(snapshot.Edges) == 0 {
		return nil
	}
	out := make(map[string][]string, 8)
	named := make(map[string]bool, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		source, ok := byID[edge.From]
		if !ok || source.FoldRoot || settled(source.Status) {
			continue
		}
		if _, ok := byID[edge.To]; !ok {
			continue
		}
		pair := edge.To + "\x00" + edge.From
		if named[pair] || len(out[edge.To]) >= waitsOnCap {
			continue
		}
		named[pair] = true
		out[edge.To] = append(out[edge.To], s.label[edge.From])
	}
	return out
}

// questionCounts attributes every open question to the job root that raised it,
// walking up the parents so a question asked by a worker four levels down still
// puts the amber `?` on the card the user can see (5.24's consent-desk rule).
//
// The count lands on every ancestor as well as on the asking node, so a
// collapsed card and the expanded row inside it agree.
func questionCounts(questions []store.AgentQuestion, byID map[string]store.Node) map[string]int {
	if len(questions) == 0 {
		return nil
	}
	out := make(map[string]int, len(questions))
	for _, question := range questions {
		id := strings.TrimSpace(question.OriginNodeID)
		for hops := 0; id != "" && hops < 16; hops++ {
			node, ok := byID[id]
			if !ok {
				break
			}
			out[id]++
			id = strings.TrimSpace(node.Parent)
			if id == store.RootID {
				break
			}
		}
	}
	return out
}

// lifeOf maps store.Status onto the rail's lifecycle, one for one, exactly as
// rail.Lifecycle's doc comment specifies. Held is scheduling control rather than
// status, so it outranks the status it is holding.
func lifeOf(node store.Node) rail.Lifecycle {
	if node.Held && !settled(node.Status) {
		return rail.LifePaused
	}
	switch node.Status {
	case store.Running:
		return rail.LifeWorking
	case store.Done:
		return rail.LifeSettled
	case store.Failed:
		return rail.LifeFailed
	case store.Cancelled:
		return rail.LifeCancelled
	}
	return rail.LifeQueued
}

func settled(status store.Status) bool {
	return status == store.Done || status == store.Failed || status == store.Cancelled
}

// nodeStatusLine is line 2 of a card: the orchestrator's own words where it
// wrote them, and nothing where it did not. 5.9 calls the status line a duty,
// and the answer to a duty nobody performed is a missing line — never an
// invented one.
func nodeStatusLine(node store.Node) string {
	if summary := firstLine(node.Summary); summary != "" {
		return summary
	}
	if node.Status == store.Failed {
		if reason := firstLine(node.Error); reason != "" {
			return reason
		}
	}
	return ""
}

// nodeLabelOf is the name a row wears. A node's title is what a person called
// it; its brief is the sentence it was commissioned with, cut to a name's
// length; its id is never drawn (5.14), so the last resort is the id's own last
// segment, which is what the spawn actually named.
func nodeLabelOf(node store.Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	if brief := firstLine(node.Brief); brief != "" {
		return brief
	}
	return nodeLabel(node.ID)
}

// plural spells a count with the right noun, so a rail never says "1 parts".
func plural(n int, one, many string) string {
	word := many
	if n == 1 {
		word = one
	}
	return itoa(n) + " " + word
}

func itoa(n int) string {
	if n >= 0 && n < 10 {
		return string(rune('0' + n))
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
