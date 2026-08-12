package chat

import (
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
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
//
// ONE READ 5.9 ASKS FOR AND THIS SEAM STILL CANNOT MAKE. It is named here rather
// than faked on a row, because 8.2.20's missing glyph is an honest answer and an
// invented number is not:
//
//   - PER-SURFACE CONTEXT. The card's ctx% is the orchestrator's window and each
//     worker row shows its own; 5.9 already records that executors must journal
//     window-size high-water marks, and until they do there is nothing to read.
//     No gauge is drawn anywhere on this rail for that reason.
//
// It is an absent cell, never a wrong one. The model word is the same story one
// step further out: it belongs to the role binding, not to the snapshot.
//
// PER-PART MONEY used to be the second name on this list — "TopLevelJobUsage
// answers per JOB ROOT, so a worker row carries its clock and no dollars". The
// read landed (store.SubtreeReceipts) and [Receipts] is where it enters.
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

// Subtrees is the optional read that makes a FILED job's plan visible again.
//
// THE DEFECT, in the reporter's words: "I still don't see any tree hierarchy
// when I click on a task." Measured on their own journal, and it is not the
// renderer: [store.ActiveNodes] — which is what [Graph.ActiveSnapshot] is built
// from — returns a fold's outermost representative and NOT its members. That is
// correct for a home rail, which must not list a settled job's four parts as
// four jobs. It is wrong for the job itself, because folding is what happens to
// every job shortly after it settles, and from that moment the snapshot says the
// job has no children at all. Their `task-1961` is a real four-part job with
// `folded = 1` on all five rows: the card said the job had no parts, the scope
// has one row,
// the room draws no part and no per-part result. Their `craft-2088`, the same
// shape but not yet folded, draws all four. **The tree does not break when a job
// is complicated; it breaks when a job gets old.**
//
// [store.SubtreeNodes] is the read that ignores folding, and it is bounded by
// the subtree it is given. It is asked ONCE PER ROOM THE READER ENTERS rather
// than on every poll: a home rail is a list of jobs and does not need any job's
// parts, and paying for every subtree on the board to fix the one room a reader
// is standing in would be the wrong trade in the one place this design cannot
// afford one.
//
// It is separate from [Graph] for the reason [Trace] is separate from
// [Commander]: adding a method to Graph would make a Backend that lacks it lose
// the WHOLE rail, and a capability some hosts have belongs behind its own
// assertion. *store.Store satisfies it with no adapter.
type Subtrees interface {
	// SubtreeNodes is root and every descendant, folded or not, in splice order.
	SubtreeNodes(root string) ([]store.Node, error)
}

// Receipts is the optional read that puts MONEY AND A CLOCK ON EVERY PART.
//
// It is [Subtrees]' twin and it is asked in the same breath, because the two
// answer the same question about the same rows: SubtreeNodes says what the parts
// ARE and SubtreeReceipts says what each of them cost and how long it has been
// at it, in the same recursion, in the same order, equally indifferent to
// folding. §13 is what wants it — "every tree row carries its own `$ · elapsed`;
// collapsed parents carry the rollup" — and until it existed a worker row could
// only ever carry a clock, because TopLevelJobUsage answers per job root.
//
// THE ONE READ IS PER ROOM ENTERED, and it rides [scopeSource.opened] for
// exactly that reason: a home rail is a list of jobs whose money the board
// already answers in one query, and paying for a per-node ledger of every job on
// the board to fill the one room a reader is standing in is the trade 257800f
// refused. Entering a job costs one more query than it did; from then on the
// ledger is re-read with the journal, because money moves with the journal.
//
// ABSENT IS NOT ZERO, all the way down. A ledger with no receipt for a node, or
// a receipt the journal has never billed, leaves the money cell OFF the row —
// not `$0.00`, which is a measurement, and not `—`, which would spend a column
// on the fact that a column is empty. *store.Store satisfies this with no
// adapter, and so does command.Commander.
type Receipts interface {
	SubtreeReceipts(root string) (store.SubtreeLedger, error)
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

// RoomReuser is the `+ new room` door for a backend that can tell an empty room
// from a conversation — *store.Store, and nothing a window has to be built with.
//
// It is asked for by type assertion rather than added to [Rooms] for the reason
// the model palette's control is (chatv2.go's ModelControl note): a window whose
// backend cannot answer is a window that mints, which is exactly what it did
// before, and a visitor over a slice of rows should not have to grow a door it
// has no store to answer with.
//
// The capability is one question — is there already an empty unnamed room to
// walk into? — and the reason it belongs to the store rather than to this
// package is that this package cannot see the messages that would prove a room
// empty. The boolean says the room came back rather than being born, so a
// caller may word it; nothing words it today, because a reader who asked for a
// new room and got an empty one got what they asked for.
type RoomReuser interface {
	OpenOrReuseSession(id, surface string) (store.Session, bool, error)
}

// Row id prefixes. They are the vocabulary the shell's navigation switches on,
// and they exist because one cursor moves over four different kinds of thing —
// a room, a job, a door that is not built, and the head itself. A prefix is
// never rendered (5.14); it is how [rail.Row.ID] stays a handle.
const (
	rowHomeID     = "home"
	rowNewRoomID  = "new-room"
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
	graph    Graph
	subtrees Subtrees
	receipts Receipts
	models   Models
	rooms    Rooms
	now      func() time.Time
	session  string

	// opened is the full subtree of every task room the reader has entered this
	// session, keyed by root — the rows [Subtrees] answers with and
	// ActiveSnapshot cannot. It is spliced into the board on every rebuild
	// (readGraph), so the card and the tree agree about how many parts a job has
	// rather than one of them learning it and the other not (12.14 finding 1).
	opened map[string][]store.Node

	// ledgers is one [store.SubtreeLedger] per entered job, keyed by the same
	// root `opened` is keyed by — what each of that job's parts cost and how
	// long it has been at it (§13). It is rebuilt with the rest of the cache, so
	// a row's money ages with the journal and never with the frame.
	ledgers map[string]store.SubtreeLedger

	// built scopes, keyed by scope id. home is held apart because it is the one
	// scope that always exists, even when nothing else does.
	home  rail.Scope
	tasks map[string]rail.Scope

	// homes is 5.24's other half: the four rooms that are not the conversation
	// and not the work. The state is the wiring's to fill and this source's to
	// carry; homeSource answers the rail for every scope under it.
	homes      homes.State
	homeSource *homes.Source

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
	// nodes is the board itself, kept from the same walk the rows were built
	// from, so a task room can read the PROSE a 28-column rail had no room for
	// — the whole brief, the whole summary, the whole error (record.go).
	//
	// It is the same map `taskRows` already builds to answer parent edges, held
	// rather than dropped. A room that re-read the graph for its own transcript
	// would be a second opinion about the same subtree taken at a different
	// time, which is the exact fault scope.go's first property exists to
	// prevent.
	nodes map[string]store.Node

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
	if subtrees, ok := backend.(Subtrees); ok {
		source.subtrees = subtrees
	}
	if receipts, ok := backend.(Receipts); ok {
		source.receipts = receipts
	}
	// [Models] is asked separately from [Receipts] even though one object
	// answers both in production, for the reason every assertion in this
	// function is separate: a host that has half of a capability keeps the half
	// it has (record.go).
	if models, ok := backend.(Models); ok {
		source.models = models
	}
	if rooms, ok := backend.(Rooms); ok {
		source.rooms = rooms
	}
	source.homeSource = homes.NewSource(source.homes)
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
	// The homes answer for their own ids and refuse every other, which is what
	// keeps this from being a second registry of what a scope id means.
	if s.homeSource != nil {
		if scope, ok := s.homeSource.Scope(id); ok {
			return scope, true
		}
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
	// Before buildHome, deliberately: buildHome consumes the ledgers, so an
	// entered job's rows carry this frame's money. The row-0 roots this pass
	// walks come from LAST frame's home — the one-poll lag the top-up below
	// exists to close.
	s.readReceipts()

	s.homes.Now = s.now()
	s.homeSource.SetState(s.homes)
	s.tasks = make(map[string]rail.Scope, len(s.tasks))
	s.home = s.buildHome(sessions, snapshot, usage, questions)
	// The top-up: a task that JOINED the home this frame (or the first frame,
	// when there was no last home at all) gets its ledger now, so a delivery
	// card's tokens never wait a poll. Bounded by the same card-root cap.
	s.topUpCardReceipts()
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
	return s.withOpenedSubtrees(snapshot), usage, questions
}

// readReceipts reads what every part of an ENTERED job cost, one query per room
// the reader has opened (§13, and see [Receipts] for why the set is bounded that
// way rather than by the board).
//
// Each ledger degrades on its own: a job whose receipts will not read loses its
// money cells and keeps its rows, because a tree with no dollars on it is a
// smaller loss than no tree.
func (s *scopeSource) readReceipts() {
	if s.receipts == nil {
		s.ledgers = nil
		return
	}
	roots := make(map[string]struct{}, len(s.opened)+receiptCardRoots)
	for root := range s.opened {
		roots[root] = struct{}{}
	}
	// The first few row-0 tasks on the home rail too: the thread's delivery
	// card spells a task's ~tokens (jobcard.go) whether or not anyone ever
	// entered its room. Display order is working-then-recent, so the bound
	// lands on exactly the tasks whose cards are still on screen.
	added := 0
	for _, row := range s.home.Rows {
		if row.Kind != rail.RowTask || row.ID == "" {
			continue
		}
		// The rail's id is prefixed ("task:job-1"); the ledger is keyed by the
		// NODE, which is what every consumer looks up with. Querying by the
		// prefixed spelling returned an empty ledger under a key nobody reads.
		root := strings.TrimPrefix(row.ID, rowTaskPrefix)
		if _, held := roots[root]; held {
			continue
		}
		roots[root] = struct{}{}
		if added++; added >= receiptCardRoots {
			break
		}
	}
	if len(roots) == 0 {
		s.ledgers = nil
		return
	}
	ledgers := make(map[string]store.SubtreeLedger, len(roots))
	for root := range roots {
		ledger, err := s.receipts.SubtreeReceipts(root)
		if err != nil {
			continue
		}
		ledgers[root] = ledger
	}
	s.ledgers = ledgers
}

// receiptCardRoots bounds how many un-entered tasks get a receipts read per
// poll, so the board's history can grow without the poll growing with it.
const receiptCardRoots = 12

// topUpCardReceipts reads the ledger for any card-bearing row-0 task the main
// pass has not covered — a task new to the home this frame, or every task on
// the very first frame. It never re-reads a root the ledgers already hold.
func (s *scopeSource) topUpCardReceipts() {
	if s.receipts == nil {
		return
	}
	added := 0
	for _, row := range s.home.Rows {
		if row.Kind != rail.RowTask || row.ID == "" {
			continue
		}
		// Same trim as readReceipts: the ledger's keys are node ids, never the
		// rail's prefixed spelling of them.
		root := strings.TrimPrefix(row.ID, rowTaskPrefix)
		if _, held := s.ledgers[root]; held {
			continue
		}
		ledger, err := s.receipts.SubtreeReceipts(root)
		if err != nil {
			continue
		}
		if s.ledgers == nil {
			s.ledgers = make(map[string]store.SubtreeLedger, receiptCardRoots)
		}
		s.ledgers[root] = ledger
		if added++; added >= receiptCardRoots {
			return
		}
	}
}

// spend is one job's ledger and whether there is one, passed DOWN the build
// rather than looked up per row — so a card and every row of the tree under it
// are spelling out one read taken at one moment, which is the property this
// whole file exists to keep.
type spend struct {
	ledger store.SubtreeLedger
	have   bool
}

// rollup is what a node and everything under it cost and how long the work has
// occupied the wall. The second return is presence: a node the ledger has never
// heard of has no figures here, which a caller must draw as nothing rather than
// as a job that was free.
//
// Every row asks for its ROLLUP and not for its own receipt, deliberately. The
// store is explicit that a leaf's rollup is its own receipt — so one call covers
// both — and it is the only way a collapsed parent and the rows it expands into
// can be made to agree.
func (m spend) rollup(id string) (store.SubtreeRollup, bool) {
	if !m.have {
		return store.SubtreeRollup{}, false
	}
	return m.ledger.Rollup(id)
}

// withOpenedSubtrees puts the parts of an entered job back on the board.
//
// ActiveSnapshot answers with a fold's outermost representative and not its
// members, which is right for a list of jobs and wrong for a job (see
// [Subtrees]). Splicing the rooms the reader has actually entered is the whole
// repair: it changes nothing about which jobs the home rail lists — every id
// added here is a DESCENDANT of a row that was already on it — and it makes the
// card, the tree and the room agree, because all three are built from this one
// slice.
//
// The snapshot's own rows WIN on a collision. A node that is live is described
// by the live read; the subtree read is a photograph taken when the room was
// entered, and a stale status drawn over a fresh one is the one way this could
// make a surface lie.
func (s *scopeSource) withOpenedSubtrees(snapshot store.Snapshot) store.Snapshot {
	if len(s.opened) == 0 {
		return snapshot
	}
	known := make(map[string]bool, len(snapshot.Nodes))
	for i := range snapshot.Nodes {
		known[snapshot.Nodes[i].ID] = true
	}
	for _, nodes := range s.opened {
		for i := range nodes {
			if known[nodes[i].ID] {
				continue
			}
			known[nodes[i].ID] = true
			snapshot.Nodes = append(snapshot.Nodes, nodes[i])
		}
	}
	return snapshot
}

// rememberSubtree records one entered job's full plan and reports whether it
// told the board anything it did not already know.
func (s *scopeSource) rememberSubtree(root string, nodes []store.Node) bool {
	root = strings.TrimSpace(root)
	if root == "" || len(nodes) == 0 {
		return false
	}
	if s.opened == nil {
		s.opened = make(map[string][]store.Node, 4)
	}
	fresh := false
	for i := range nodes {
		if _, seen := s.nodes[nodes[i].ID]; !seen {
			fresh = true
			break
		}
	}
	s.opened[root] = nodes
	return fresh
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
	// 5.24's collapsed dim group, and the four rooms behind it. It used to be a
	// placeholder row that cited this section by name and opened nothing;
	// internal/tui2/homes is what it was a placeholder FOR.
	rows = append(rows, homes.Rows(s.homes)...)
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
// 13.8 finding 4 is the other half of the count: the permanent spine is a node
// with `status='running'` that never finishes, so a board reading that counts
// every node says "1 running" at a window where nothing is. The head has always
// filtered it (beltAddressable), the rail did not, and the two then disagreed on
// one screen — rail "1 running", head "nothing is running right now". The spine
// is plumbing (5.14, and prompt.go says the same in the head's own words), so it
// is not counted here and gets no card below.
func (s *scopeSource) headStatus(snapshot store.Snapshot) string {
	running, queued, blocked := 0, 0, 0
	for i := range snapshot.Nodes {
		if snapshot.Nodes[i].ID == store.RootID {
			continue
		}
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
		s.nodes = nil
		return nil
	}
	byID := make(map[string]store.Node, len(snapshot.Nodes))
	children := make(map[string][]string, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	// The room's own reading of the same board (record.go). It is assigned and
	// never mutated after this pass, so the transcript and the rail are looking
	// at one snapshot rather than two.
	s.nodes = byID
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
	// One post-order pass answers "how many parts, how many of them are moving,
	// how long has the longest been at it, and is a worker running under this
	// row" for every node at once. Asking each row for itself would walk the
	// subtree once per row, and the tree rows below need the same four numbers
	// the cards do.
	rolls := rollups(snapshot.Nodes, byID, children, now)

	roots := make([]store.Node, 0, 8)
	for _, node := range snapshot.Nodes {
		// The permanent spine is not a job (13.8 finding 4). It is the thing
		// jobs hang from, it is `running` forever, and a card for it is a
		// mystery job with a clock nobody can stop — which is also the room
		// 12.14's screenshots kept landing in.
		if node.ID == store.RootID {
			continue
		}
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
		money := spend{}
		if ledger, ok := s.ledgers[root.ID]; ok {
			money = spend{ledger: ledger, have: true}
		}
		card := s.taskCard(root, byID, children, usage, waits, asks, rolls, money, now)
		rows = append(rows, card)
		s.tasks[rowTaskPrefix+root.ID] =
			s.taskScope(card, root, byID, children, waits, asks, rolls, money, now)
	}
	return rows
}

// taskCard is 5.9's three-line anatomy filled from the graph, plus what the card
// expands into when the cursor rests on it.
//
// PROGRESSIVE DISCLOSURE IS DATA, NOT A MODE (5.9: "collapsed = 3 lines;
// focused/entered expands"). The rail decides how many of these lines to draw
// from whether the row is selected (rail's shapeOf); this builder always carries
// the census and the worker rows, because a card that only learned its own plan
// once the cursor arrived would be a store read behind a keystroke — the one
// thing this file exists to prevent.
//
// LINE 3 SAYS NOTHING ABOUT THE JOB'S SHAPE (§14). It used to end `4 workers`,
// or `atomic` when the job had no plan, and both were banned by the same
// sentence: a single-part job says nothing about its shape. What line 3 carries
// now is the CENSUS of the parts — `2◐ 2✓` — which is empty for a one-part job
// and therefore silent, with no special case anywhere for the silence.
func (s *scopeSource) taskCard(root store.Node, byID map[string]store.Node,
	children map[string][]string, usage map[string]store.JobUsage,
	waits map[string][]string, asks map[string]int, rolls map[string]roll,
	money spend, now time.Time) rail.Row {

	row := rail.Row{
		ID:        rowTaskPrefix + root.ID,
		Kind:      rail.RowTask,
		Name:      s.label[root.ID],
		Status:    nodeStatusLine(root),
		Life:      litLife(root, rolls),
		Composer:  rail.ComposerSteer,
		Seed:      root.ID,
		WaitsOn:   waits[root.ID],
		Questions: asks[root.ID],
		Workers:   s.workerRows(root, byID, children, waits, asks, rolls, money, now),
	}
	sum := rolls[root.ID]
	parts, running := sum.parts, sum.active
	row.Meta.Counts = sum.states
	// The ledger wins when the reader has entered this job, because then the
	// card and every row of the tree are quoting one read: a card whose total
	// came from one query and whose parts came from another would be the two
	// disagreeing about the same money in adjacent columns. The board's own
	// per-job figure answers for every job nobody has opened.
	if roll, ok := money.rollup(root.ID); ok && roll.Billed() {
		row.Meta.Cost, row.Meta.HasCost = roll.Cost, true
	} else if job, ok := usage[root.ID]; ok && job.Runs > 0 {
		// Runs guards the same doctrine Billed() does above: the usage query
		// COALESCEs every job to $0 from its first frame, and an unbilled part
		// has no cost at all — not $0.00, which reads as a settled figure.
		row.Meta.Cost, row.Meta.HasCost = job.Cost, true
	}
	if elapsed, ok := jobWall(root, sum, money, now); ok {
		row.Meta.Elapsed, row.Meta.HasElapsed = elapsed, true
	}
	// A job root usually carries no status of its own worth showing — its parts
	// do the work — so the subtree answers "what is happening" when the node
	// itself has nothing to say. 13.3.3 again: queued parts count.
	if row.Status == "" && running > 0 {
		row.Status = plural(running, "part running", "parts running")
	}
	// A planned job has a chat because it has a plan to redirect; a job that is
	// one hand takes steering mail only (5.11). The mark on the card is a
	// preview of the composer the row will bind, so this is the same decision
	// twice and never two decisions.
	if parts > 0 {
		row.Composer = rail.ComposerChat
	}
	return row
}

// taskScope is the room behind a card: the orchestrator surface, then the plan
// steps and workers as an indented tree with the waits-on structure visible
// (5.15).
//
// ROW 0 IS THE CARD, RE-KINDED. It was built here from the node a second time,
// and the two builders did not know the same things: the card falls back to
// "1 part running" when the root itself says nothing (13.3.3) and carries the
// cost, the clock and the census, and this one carried nodeStatusLine(root) and
// no telemetry at all. On a job root — which "usually carries no status of its
// own worth showing", as taskCard says in its own comment — that difference is
// the whole row: a screenshot of an entered one-part job showed a room whose
// surface row was a bare name, under a card that had said "1 part running · 1s"
// one keystroke earlier.
//
// Entering a task must never know LESS about it than the card you entered from.
// 5.15 makes the card a PREVIEW of the room ("selecting a task card shows a
// preview of that task"), and a preview that outranks the thing it previews is
// the affordance lying in the one direction nobody checks. So the card is the
// row, and the only thing that changes is what it IS here: a scope's
// conversational surface rather than a member of the scope above.
// THE MEMBERS ARE THE WIREFRAME'S TREE. A plan step or a worker is one line
// with its receipt flush right (`◐ H2   $0.37 · 28m`) and a `waits: H2` line
// under it when it is sitting behind a sibling — rail's own tree-row anatomy —
// and the DEPTH is what says which worker belongs to which step: the rail draws
// it as v1's connector grammar, ├─ and ╰─ with │ guides, working the branches
// out from the depths alone. The root's own parts therefore sit at depth 0,
// flush under the surface row exactly as 5.15 draws them.
func (s *scopeSource) taskScope(card rail.Row, root store.Node, byID map[string]store.Node,
	children map[string][]string, waits map[string][]string,
	asks map[string]int, rolls map[string]roll, money spend, now time.Time) rail.Scope {

	surface := card
	surface.Kind = rail.RowSurface
	surface.Depth = 0
	surface.Composer = composerFor(root, children)
	// The card's per-worker expansion is what the HOME rail shows instead of the
	// tree. In here the tree IS on screen, and a surface row that expanded into
	// the same workers a hairline below it would draw every part twice — 12.13's
	// tripled name in the other axis. The census stays: `2◐ 2✓` is a summary of
	// the tree, not a copy of it, and it survives the fold that hides rows.
	surface.Workers = nil

	rows := make([]rail.Row, 0, 8)
	rows = append(rows, surface)
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		for _, kid := range childrenInOrder(children[id], byID) {
			if len(rows) >= maxSubtreeRows {
				return
			}
			node := byID[kid]
			kind := rail.RowWorker
			if len(children[kid]) > 0 {
				kind = rail.RowStep
			}
			row := s.treeRow(node, kind, waits, asks, rolls, money, now)
			row.Depth = depth
			row.Seed = root.ID
			rows = append(rows, row)
			walk(kid, depth+1)
		}
	}
	walk(root.ID, 0)
	// A scope with members is a room to descend into; one without is a leaf,
	// and the rail's Enter opens its surface in the main pane instead.
	return rail.Scope{ID: rowTaskPrefix + root.ID, Title: s.label[root.ID], Seed: root.ID, Rows: rows}
}

// treeRow is one plan step or one worker, in the anatomy 5.15's wireframe draws:
// state glyph, name, elapsed at the right, and the waits-on flag when the row is
// sitting behind a sibling.
//
// It is the ONE builder for a member row, and the focused card's per-worker rows
// come out of it too — so a worker says the same thing about itself under a card
// as it does inside the room, which is 12.14's law ("a preview that outranks the
// thing it previews is the affordance lying") pointed the other way.
func (s *scopeSource) treeRow(node store.Node, kind rail.RowKind, waits map[string][]string,
	asks map[string]int, rolls map[string]roll, money spend, now time.Time) rail.Row {

	return rail.Row{
		ID:        rowTaskPrefix + node.ID,
		Kind:      kind,
		Name:      s.label[node.ID],
		Status:    nodeStatusLine(node),
		Life:      litLife(node, rolls),
		Composer:  rail.ComposerSteer,
		WaitsOn:   waits[node.ID],
		Questions: asks[node.ID],
		Meta:      treeMeta(node, rolls[node.ID], money, now),
	}
}

// treeMeta is a tree row's receipt: what this part cost and how long it has been
// at it (§13), plus the census of anything under it.
//
// THE LEDGER IS ASKED FOR THE ROLLUP, NOT THE RECEIPT, so a step and the workers
// it expands into cannot disagree — a collapsed parent carries the money under
// it and the WALL it occupied, which is earliest start to latest settle and
// never a sum: four workers that each took ten minutes side by side took ten
// minutes, not forty.
//
// WITHOUT A LEDGER THERE IS A CLOCK AND NO MONEY, which is what every row of an
// un-entered job has and what every row had before store.SubtreeReceipts landed.
// The clock is then the node's own wall — asked of [store.NodeReceipt] rather
// than recomputed here, so a row drawn from the snapshot and the same row drawn
// from the ledger cannot come to different conclusions about a missing stamp —
// and a step with no stamps of its own borrows the longest clock underneath it,
// because "this branch has been at it for 28m" is the fact the reader wanted.
//
// There is no context gauge on either path, and that is a READ GAP rather than a
// decision — see the note on [Graph].
func treeMeta(node store.Node, r roll, money spend, now time.Time) rail.Telemetry {
	var meta rail.Telemetry
	meta.Counts = r.states
	if roll, ok := money.rollup(node.ID); ok {
		if roll.Billed() {
			meta.Cost, meta.HasCost = roll.Cost, true
		}
		if elapsed, ok := roll.Elapsed(now); ok && elapsed > 0 {
			meta.Elapsed, meta.HasElapsed = elapsed, true
		}
		return meta
	}
	if elapsed, ok := nodeWall(node, now); ok && elapsed > 0 {
		meta.Elapsed, meta.HasElapsed = elapsed, true
	} else if !settled(node.Status) && r.longest > 0 {
		meta.Elapsed, meta.HasElapsed = r.longest, true
	}
	return meta
}

// nodeWall is how long one node has been at it, borrowed from
// [store.NodeReceipt] so the snapshot path and the ledger path keep ONE clock
// rule between them: work that never started has no clock, work still running is
// measured against now, and work that ended without a finish stamp is absent
// rather than a number that would run forever.
func nodeWall(node store.Node, now time.Time) (time.Duration, bool) {
	return store.NodeReceipt{
		Status:     node.Status,
		StartedAt:  node.StartedAt,
		FinishedAt: node.FinishedAt,
	}.Elapsed(now)
}

// jobWall is the clock a job's card carries: the rollup's wall when the reader
// has entered the job, and otherwise the longest thing running anywhere inside
// it — which is the only reading the board's snapshot supports, and the one the
// card carried before receipts existed.
func jobWall(root store.Node, sum roll, money spend, now time.Time) (time.Duration, bool) {
	if roll, ok := money.rollup(root.ID); ok {
		if elapsed, ok := roll.Elapsed(now); ok && elapsed > 0 {
			return elapsed, true
		}
	}
	if sum.longest > 0 {
		return sum.longest, true
	}
	return 0, false
}

// workerRows are the per-worker rows a FOCUSED card expands into (5.9). They are
// the leaves of the job — the parts that actually do work — because the steps
// above them are already accounted for by the dots.
//
// Unsettled first, then history, each half in the order it was spliced. It is
// the rule the card list itself uses (maxTaskRows): a cap that drops rows should
// only ever drop what has stopped moving.
func (s *scopeSource) workerRows(root store.Node, byID map[string]store.Node,
	children map[string][]string, waits map[string][]string, asks map[string]int,
	rolls map[string]roll, money spend, now time.Time) []rail.Row {

	if len(children[root.ID]) == 0 {
		return nil
	}
	live := make([]rail.Row, 0, 8)
	var done []rail.Row
	var walk func(id string)
	walk = func(id string) {
		for _, kid := range childrenInOrder(children[id], byID) {
			if len(live)+len(done) >= maxSubtreeRows {
				return
			}
			if len(children[kid]) > 0 {
				walk(kid)
				continue
			}
			node := byID[kid]
			row := s.treeRow(node, rail.RowWorker, waits, asks, rolls, money, now)
			if settled(node.Status) {
				done = append(done, row)
				continue
			}
			live = append(live, row)
		}
	}
	walk(root.ID)
	return append(live, done...)
}

// childrenInOrder is the splice order of one node's parts. Sorting here rather
// than at every call site is what keeps the tree, the dots and the worker rows
// listing the same plan in the same order.
func childrenInOrder(kids []string, byID map[string]store.Node) []string {
	if len(kids) < 2 {
		return kids
	}
	out := append([]string(nil), kids...)
	sort.SliceStable(out, func(i, j int) bool {
		return byID[out[i]].CreatedSeq < byID[out[j]].CreatedSeq
	})
	return out
}

// litLife is 10.5 idea 15: "plan steps lit by live execution — our DAG knows the
// join exactly, so light the plan-step row accent while its worker runs".
//
// The join is the parent edge and nothing else: a row is lit because a node
// UNDER IT is running, never because a name matched. It only ever lifts a row
// out of queued — a settled step stays settled, a failed one stays failed, and a
// held one stays held, because those are facts about the row itself and this is
// a fact about its subtree.
func litLife(node store.Node, rolls map[string]roll) rail.Lifecycle {
	life := lifeOf(node)
	if life == rail.LifeQueued && rolls[node.ID].lit {
		return rail.LifeWorking
	}
	return life
}

// composerFor is 5.11's fork: a job with a plan has a chat, and a job that is
// one hand has a steer line. It is asked of the graph rather than stored on the row so the two
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

// roll is what one node's subtree adds up to. Every number a card or a tree row
// needs about the work under it is here, so no row walks the graph for itself.
type roll struct {
	// parts is how many nodes are under this one. Queued parts count (13.3.3):
	// a job with three admitted, unstarted workers is not an empty job.
	parts int
	// active is this node and its parts that are running, claimed or pending —
	// the reading behind "1 part running".
	active int
	// longest is the longest thing running in here, this node included.
	longest time.Duration
	// lit says a node UNDERNEATH this one is running now (idea 15's join).
	lit bool
	// states is the census the row draws instead of a fraction (§14): how many
	// of the PARTS are queued, running, done and broken.
	//
	// It counts the parts and never the node itself, and that is what makes a
	// one-part job silent about its own shape rather than announcing `1◐` about
	// a job with nothing in it. Each part is counted by its OWN status, not by
	// the lit reading its row draws (litLife): a pending step with a running
	// worker under it is one queued part and one running part, which is two
	// facts and the honest pair.
	states rail.StateCounts
}

// rollups computes a roll for every node in one post-order pass.
//
// Iterative rather than recursive, with a three-state mark: a snapshot is data
// from disk, and a parent cycle in it must leave the rail standing rather than
// blow the stack. A node caught mid-visit is simply not counted twice.
func rollups(nodes []store.Node, byID map[string]store.Node,
	children map[string][]string, now time.Time) map[string]roll {

	const (
		unseen = iota
		visiting
		finished
	)
	out := make(map[string]roll, len(nodes))
	state := make(map[string]uint8, len(nodes))
	stack := make([]string, 0, 16)
	for i := range nodes {
		if state[nodes[i].ID] != unseen {
			continue
		}
		stack = append(stack[:0], nodes[i].ID)
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			switch state[id] {
			case unseen:
				state[id] = visiting
				for _, kid := range children[id] {
					if state[kid] == unseen {
						stack = append(stack, kid)
					}
				}
			case visiting:
				stack = stack[:len(stack)-1]
				state[id] = finished
				out[id] = rollOf(byID[id], children[id], byID, out, now)
			default:
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}

// rollOf folds one node's own state together with the rolls of its parts.
func rollOf(node store.Node, kids []string, byID map[string]store.Node,
	done map[string]roll, now time.Time) roll {

	var r roll
	switch node.Status {
	case store.Running, store.Claimed:
		r.active++
		if !node.StartedAt.IsZero() {
			if elapsed := now.Sub(node.StartedAt); elapsed > r.longest {
				r.longest = elapsed
			}
		}
	case store.Pending:
		r.active++
	}
	for _, kid := range kids {
		part := done[kid]
		r.parts += 1 + part.parts
		r.active += part.active
		countState(&r.states, byID[kid].Status)
		r.states.Add(part.states)
		if part.longest > r.longest {
			r.longest = part.longest
		}
		// Running, not merely claimed: a claim is a worker picking the job up,
		// and a step lit by a claim would say "in flight" a moment before
		// anything is. The state axis may only brighten on work that is moving.
		if byID[kid].Status == store.Running || part.lit {
			r.lit = true
		}
	}
	return r
}

// countState folds one part's status into a census, in the five buckets
// [store.StateCounts] keeps and with its rule for the fifth: a CLAIM folds in
// with pending, because a claim is a worker picking the work up and not the work
// moving, and a reader counting what is running must not be told it has.
func countState(counts *rail.StateCounts, status store.Status) {
	switch status {
	case store.Running:
		counts.Running++
	case store.Done:
		counts.Done++
	case store.Failed:
		counts.Failed++
	case store.Cancelled:
		counts.Cancelled++
	default:
		counts.Queued++
	}
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

// started says a node's own row has begun — it is running now, or it ran and
// stopped. It is the column the scheduler stamps when it claims the node
// (store.Node.StartedAt) plus the states that can only be reached through it,
// never a lifecycle read back: [litLife] lights a QUEUED parent whose child is
// working, which is exactly the case this must answer "no" to.
func started(node store.Node) bool {
	return !node.StartedAt.IsZero() || node.Status == store.Running || settled(node.Status)
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

// subtreeNodes is every node a task room's trail is read from: the root and
// whatever the rail already built beneath it.
//
// It reads the scope this source built rather than walking the graph again, for
// the reason the whole file exists: the walk happened once, at the last journal
// move, and a second one here would be a second opinion about the same subtree
// read at a different time. A root with no scope — a leaf worker, a job with no
// plan — answers with itself, which is exactly its own trail.
func (s *scopeSource) subtreeNodes(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	scope, ok := s.tasks[rowTaskPrefix+root]
	if !ok {
		return []string{root}
	}
	out := make([]string, 0, len(scope.Rows)+1)
	seen := make(map[string]bool, len(scope.Rows)+1)
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(root)
	for i := range scope.Rows {
		add(strings.TrimPrefix(scope.Rows[i].ID, rowTaskPrefix))
	}
	return out
}
