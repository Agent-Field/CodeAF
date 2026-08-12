package chat

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// What the work page reads, and when.
//
// EVERY READ HERE IS OPTIONAL AND EVERY FAILURE IS AN EMPTY BAND. That is the
// same discipline [Ledger], [Graph], [Subtrees] and [Models] keep, for the same
// reason stated at each of them: a window driven by a stub in a test, or by a
// backend that is only a message log, must still open and still render — it
// simply has no charters and no services, which is an honest empty page and not
// a broken one. A band that failed loudly would be the least of this surface's
// facts taking the loudest row.
//
// EVERY READ IS STAMPED ON THE JOURNAL AND TAKEN ONLY ON THIS PAGE. [syncBoard]
// is the one door, it runs at most once per journal move, and it runs at all
// only while the work page is the lens. A reader looking at the thread pays
// nothing for the charters, and a reader parked on the board pays once per
// thing that actually happened rather than once per keystroke or once per frame.

// Watching is the standing-charters read behind the `watching` band.
//
// It is three questions rather than one because the store answers them
// separately and materializing them together would be a fourth opinion about
// the same rows: [store.Charter] carries the cadence, the state, the ladder and
// the rails; FiringsToday counts admitted actions against the day; and
// CharterLastFired is the one moment the materialized row does not hold (LastWake
// is when the sentinel LOOKED, which is a different fact and one this band shows
// separately).
type Watching interface {
	Charters(statuses ...store.CharterStatus) ([]store.Charter, error)
	FiringsToday(id string, now time.Time) (int, error)
	CharterLastFired(id string) (time.Time, error)
}

// Firings is the charter detail page's own read: the last few judgments with
// what followed each. It is separate from [Watching] because it is asked once,
// when a reader opens one charter, and never for the band.
type Firings interface {
	RecentSentinelJudgments(id string, limit int) ([]store.SentinelJudgment, error)
}

// Serving is the promoted-process read behind the `services` band.
type Serving interface {
	ActiveServices() ([]store.Service, error)
}

// ServiceLogs is the one read the STORE cannot answer: a service's log lives on
// the filesystem at a path the store merely records, and the engine that
// started the process is what owns the bytes it wrote
// (internal/command/board_reads.go). It hangs off the commander rather than the
// backend for exactly that reason.
type ServiceLogs interface {
	ServiceLogTail(path string, lines, maxBytes int) []string
}

// jobSpend is what one job cost and how many tokens it burned — the two halves
// of §5b's receipt that the rail row does not carry. It is a projection of
// [store.JobUsage] rather than the type itself, so this file states exactly
// which two fields the board depends on.
type jobSpend struct {
	tokens int64
	known  bool
}

// boardCharter is one standing charter as this page reads it: the store's row
// plus the two derived facts the band needs, taken in the same pass so the
// receipt and the detail page cannot disagree about them.
type boardCharter struct {
	store.Charter
	Today     int
	LastFired time.Time
	now       time.Time
}

// boardService is one promoted process as this page reads it.
type boardService struct {
	store.Service
	now time.Time
}

// -- the sync ----------------------------------------------------------------

// syncBoard takes every read the work page needs, at most once per journal move
// and only while the work page is the lens.
//
// THE TRADE, stated because it is a real one. This is the three reads history
// needs — the addressable corpus, the open questions, the per-job spend — plus
// one charter list, one service list, and a small handful of per-charter and
// per-job follow-ups bounded by how many rows the page actually draws. The
// palette pays the first three on the keystroke that opens it (catalogJobs); the
// board pays all of them per JOURNAL MOVE instead, which is the cheaper end of
// the same bargain for a surface a reader leaves open, and it pays nothing at
// all on any other page.
func (a *App) syncBoard() {
	if a.page != pageBoard {
		return
	}
	// The zero journal means "just walked in" (switchRoom resets it), and a
	// stamp that ALSO reads zero would swallow the re-read for the new room —
	// so zero never satisfies the guard.
	if a.board.read && a.journal != 0 && a.board.stamp == a.journal {
		return
	}
	a.board.stamp, a.board.read = a.journal, true
	a.board.history = a.historyRows()
	a.board.usage = a.boardSpend()
	a.board.models = a.boardModelReads()
	a.board.charters = a.boardCharterReads()
	a.board.services = a.boardServiceReads()
	if a.board.detail.target == boardOpensService && a.board.detail.id != "" {
		// An open service page's log ages with the journal like everything else
		// on this surface. It is FILE I/O, so it is emphatically not re-read per
		// frame: the tick that turns a spinner must not open a file.
		a.board.detail.log = a.boardServiceLog(a.board.detail.id)
		a.board.detail.logID = a.board.detail.id
	}
}

// boardSpend is the token half of every job's receipt, by job root id.
//
// It is the SAME READ the money already comes from ([Graph.TopLevelJobUsage],
// via App.jobUsage) rather than a second one: the store answers cost and tokens
// in one query, and the rail happens to keep only the cost. Nothing new is asked
// of the database for §5b's `~NK tok`.
func (a *App) boardSpend() map[string]jobSpend {
	usage := a.jobUsage()
	if len(usage) == 0 {
		return nil
	}
	out := make(map[string]jobSpend, len(usage))
	for id, spent := range usage {
		total := int64(spent.PromptTokens) + int64(spent.CompletionTokens)
		if total <= 0 {
			// Zero tokens is a job that has not billed a run, not a job that
			// billed nothing (§16's EMPTINESS). The cell simply does not appear.
			continue
		}
		out[id] = jobSpend{tokens: total, known: true}
	}
	return out
}

// boardModelReads names WHO DID THE WORK for every job the page draws.
//
// It is bounded by what is ON SCREEN — the live sections only, never history —
// because the answer is a per-node read and a store with four hundred jobs
// behind a closed fold would otherwise pay four hundred queries for words nobody
// asked to see. A job the reader opens the fold onto keeps its money and its
// clock and simply says nothing about its models, which is §16's absence rather
// than a guess.
func (a *App) boardModelReads() map[string][]string {
	if a.source == nil || a.source.models == nil {
		return nil
	}
	working, recent := a.boardJobs()
	rows := append(append([]rail.Row(nil), working...), recent...)
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string][]string, len(rows))
	for _, row := range rows {
		node := strings.TrimPrefix(row.ID, rowTaskPrefix)
		if node == "" {
			continue
		}
		if models := a.source.jobModels(node); len(models) > 0 {
			out[node] = models
		}
	}
	return out
}

// boardCharterReads is the `watching` band's read.
//
// ACTIVE AND PAUSED, and nothing else. A retired charter is a thing that used to
// be true, and a band called `watching` that listed things nobody is watching
// would be a list of the past wearing the present tense. A paused one IS still
// watched-over — the user paused it and can unpause it — so it stays, wearing
// its state word honestly.
func (a *App) boardCharterReads() []boardCharter {
	watch, ok := a.backend.(Watching)
	if !ok {
		return nil
	}
	charters, err := watch.Charters(store.CharterActive, store.CharterPaused)
	if err != nil || len(charters) == 0 {
		return nil
	}
	now := a.now()
	out := make([]boardCharter, 0, len(charters))
	for _, charter := range charters {
		row := boardCharter{Charter: charter, now: now}
		if today, err := watch.FiringsToday(charter.ID, now); err == nil {
			row.Today = today
		}
		if fired, err := watch.CharterLastFired(charter.ID); err == nil {
			row.LastFired = fired
		}
		out = append(out, row)
	}
	return out
}

// boardServiceReads is the `services` band's read.
func (a *App) boardServiceReads() []boardService {
	serving, ok := a.backend.(Serving)
	if !ok {
		return nil
	}
	services, err := serving.ActiveServices()
	if err != nil || len(services) == 0 {
		return nil
	}
	now := a.now()
	out := make([]boardService, 0, len(services))
	for _, service := range services {
		out = append(out, boardService{Service: service, now: now})
	}
	return out
}

// boardCharters and boardServices are the cached bands, as the projection reads
// them. They are separate from the reads above so [boardLines] never triggers a
// store query on a paint.
func (a *App) boardCharters() []boardCharter { return a.board.charters }
func (a *App) boardServices() []boardService { return a.board.services }

// boardCharterAt and boardServiceAt find one cached row by id, for the detail
// pages. A row that has gone since the detail opened answers false, and the page
// closes rather than drawing a card about something that no longer exists.
func (a *App) boardCharterAt(id string) (boardCharter, bool) {
	for _, charter := range a.board.charters {
		if charter.ID == id {
			return charter, true
		}
	}
	return boardCharter{}, false
}

func (a *App) boardServiceAt(id string) (boardService, bool) {
	for _, service := range a.board.services {
		if service.ID == id {
			return service, true
		}
	}
	return boardService{}, false
}

// -- the derived receipts ----------------------------------------------------

// boardElapsed is a job's wall clock, re-derived against the ANIMATION CLOCK
// when the job is still running.
//
// This is what makes the reading tick. [rail.Telemetry.Elapsed] was measured
// when the source last rebuilt, which happens on a journal move — and a worker
// mid-tool-call journals nothing at all, so a row that trusted the cached figure
// would sit on one number for minutes and read as a window that had stopped.
// That was the reported defect. A live row asks the same function the rail asked
// (nodeWall) with the instant this frame was latched at, so the number ages with
// the frame; a settled row keeps the cached figure, because a settled row's wall
// is a fact and not a reading.
func (a *App) boardElapsed(row rail.Row) (time.Duration, bool) {
	if a.source != nil && row.Attention().Live() {
		node, ok := a.source.nodes[strings.TrimPrefix(row.ID, rowTaskPrefix)]
		if ok {
			if elapsed, known := nodeWall(node, a.boardNow()); known && elapsed > 0 {
				return elapsed, true
			}
		}
	}
	if row.Meta.HasElapsed {
		return row.Meta.Elapsed, true
	}
	return 0, false
}

// boardNow is the instant this frame is drawing at — the animation clock's
// latched one, so every ticking cell on the page agrees, and the app's own clock
// before the first latch.
func (a *App) boardNow() time.Time {
	if clock := a.transcript.Clock(); clock != nil {
		if now := clock.Now(); !now.IsZero() {
			return now
		}
	}
	return a.now()
}

// boardTokens is the `~NK tok` cell: what a job's whole subtree burned, marked
// as the estimate it is.
func (a *App) boardTokens(row rail.Row) (int64, bool) {
	spent, ok := a.board.usage[strings.TrimPrefix(row.ID, rowTaskPrefix)]
	if !ok || !spent.known {
		return 0, false
	}
	return spent.tokens, true
}

// boardIsLive says the work page is drawing something that MOVES.
//
// It is what arms the animation clock while this lens is up, and the predicate
// is [rail.Attention.Live] rather than "is the glyph a spinner", because two
// different things on this page move: the braille glyph on a working row, and
// the ELAPSED READING on any row whose clock is still running. A job holding a
// question wears the amber `?` and never spins — but its parts are still going
// and its wall time is still growing, and a window that stopped waking would
// freeze that number at whatever it said when the journal last moved.
//
// A QUEUED ROW IS NOT LIVE (rail.Attention.Live says so and says why), and a
// page of queued work that ticked would be a window burning wakeups to redraw
// bytes nobody can tell apart. Neither is a detail page: it is a document, and
// nothing on it moves.
func (a *App) boardIsLive() bool {
	if a.page != pageBoard || a.board.detail.open() {
		return false
	}
	working, _ := a.boardJobs()
	for _, row := range working {
		if row.Attention().Live() {
			return true
		}
	}
	return false
}

// -- the charter's words -----------------------------------------------------

// receipt is the `watching` band's second line, in notebook-split.md §2's order:
//
//	cadence · state · last-fired · next-due · today's count · cost per run
//
// Each part appears only when it is known. The STATE is two words at most and
// they are different questions: the status (`paused`) says whether the charter
// is armed, the autonomy (`probation` / `tenured`) says how it may fire, and a
// surface that collapsed them would be telling a user their paused watch is
// still on probation OR that their probationary watch is merely paused. An
// active charter draws no status word, because active is the resting state and a
// default never wears a badge.
func (c boardCharter) receipt() []boardSpan {
	out := make([]boardSpan, 0, 12)
	add := func(text string, tier tokens.Token) {
		if text == "" {
			return
		}
		if len(out) > 0 {
			out = append(out, boardSpan{text: boardSeparator, tier: tokens.TextTertiary})
		}
		out = append(out, boardSpan{text: text, tier: tier})
	}
	add(c.Watch.Spoken(), tokens.TextTertiary)
	if c.Status != store.CharterActive {
		add(string(c.Status), tokens.TextTertiary)
	}
	add(string(c.Autonomy), tokens.TextTertiary)
	add(c.firedWord(), tokens.TextTertiary)
	add(c.nextWord(), tokens.TextTertiary)
	if c.Today > 0 {
		add(strconv.Itoa(c.Today)+" today", tokens.TextTertiary)
	}
	if run := c.Rails().EstimatedCostUSD; run > 0 {
		// Marked as an estimate because it IS one — it is what the charter was
		// ratified against, not what its last firing cost — and 10.2.8 says an
		// estimated number wears the ~ that says so.
		add(tokens.GlyphEstimate+tokens.Money(run)+"/run", tokens.TextTertiary)
	}
	return out
}

// firedWord is the last-fired reading, and it keeps DILIGENCE AND DEATH APART.
//
// [store.Charter] spells the requirement out at its own LastChecked field: a
// watch that has checked faithfully every morning and correctly found nothing
// used to render byte-identically to a watch that had never run once. So a
// charter that has never fired says when the sentinel last LOOKED, which is the
// honest thing it has to report, and only a charter that has never even been
// looked at says `never fired`.
func (c boardCharter) firedWord() string {
	if !c.LastFired.IsZero() {
		return "fired " + reltime.Short(c.LastFired, c.now)
	}
	if !c.LastChecked.IsZero() {
		return "checked " + reltime.Short(c.LastChecked, c.now)
	}
	return "never fired"
}

// nextWord is when the watch is due to look again. A due time in the past is a
// wake the reconciler has not reached yet, and `due` is the honest reading of it
// — reltime reads the future as "now" and would otherwise say `next now`, which
// is a sentence about nothing.
func (c boardCharter) nextWord() string {
	if c.NextDue.IsZero() {
		return ""
	}
	if !c.NextDue.After(c.now) {
		return "due"
	}
	return "next in " + reltime.Elapsed(c.NextDue.Sub(c.now))
}

// boardCharterGlyph is a charter's state glyph. The vocabulary is the product's
// and nothing is minted here (§16's GLYPH DISCIPLINE): a wake in flight is
// working, a paused watch is paused, and an armed watch waiting for its moment
// is queued — which is exactly what `○` means everywhere else, "not moving and
// not done".
func boardCharterGlyph(c boardCharter) (string, tokens.Token) {
	switch {
	case c.Status == store.CharterPaused:
		return tokens.GlyphPaused, tokens.TextTertiary
	case c.WakePending:
		return tokens.GlyphWorking, tokens.Cyan
	default:
		return tokens.GlyphQueued, tokens.TextTertiary
	}
}

// -- the service's words -----------------------------------------------------

// receipt is the `services` band's second line: notebook-split.md §2's
// `uptime · restarts`, with the life glyph carried by the line above it.
//
// A service that is not running says its state instead of an uptime, because
// "up 3h" on a stopped process would be the surface reporting the age of a
// corpse. Restarts appear only when there have been some: zero restarts is the
// resting state and §16 renders an absent value as absence.
func (s boardService) receipt() []boardSpan {
	out := make([]boardSpan, 0, 6)
	add := func(text string, tier tokens.Token) {
		if text == "" {
			return
		}
		if len(out) > 0 {
			out = append(out, boardSpan{text: boardSeparator, tier: tokens.TextTertiary})
		}
		out = append(out, boardSpan{text: text, tier: tier})
	}
	add(s.uptimeWord(), tokens.TextTertiary)
	if s.RestartCount == 1 {
		add("1 restart", tokens.TextTertiary)
	} else if s.RestartCount > 1 {
		add(strconv.Itoa(s.RestartCount)+" restarts", tokens.TextTertiary)
	}
	return out
}

// uptimeWord is how long this process has been up, or what it is instead.
func (s boardService) uptimeWord() string {
	if s.Status != store.ServiceRunning {
		return string(s.Status)
	}
	if s.StartedAt.IsZero() || !s.now.After(s.StartedAt) {
		return "up"
	}
	return "up " + reltime.Elapsed(s.now.Sub(s.StartedAt))
}

// boardServiceGlyph is a promoted process's life glyph.
//
// IT NEVER SPINS, and that is a decision rather than an omission. §18 forbids a
// moving glyph on anything durable — "a dancing glyph on a long-lived object is
// a lie about liveness" — and a service is the most durable thing this product
// has: it is meant to still be up tomorrow. A job in `working` spins because it
// is transient work that will end; a service is a fact that persists, and its
// glyph says so by holding still.
func boardServiceGlyph(s boardService) (string, tokens.Token) {
	switch s.Status {
	case store.ServiceRunning:
		return tokens.GlyphWorking, tokens.Cyan
	case store.ServiceFailed:
		return tokens.GlyphFailed, tokens.Coral
	case store.ServiceResting:
		return tokens.GlyphPaused, tokens.TextTertiary
	default:
		return tokens.GlyphQueued, tokens.TextTertiary
	}
}

// -- the history read --------------------------------------------------------

// boardHistoryRows is history as rail rows: every job the store can still name,
// minus the ones the live sections are already showing.
//
// It is the palette's own idiom (overlay.go's [Ledger] and catalogJobs), read
// for the same reason and answered in the same words. The rail's home scope is
// the newest two dozen jobs, which is the right window for a column six lines
// tall and the wrong one for the page whose whole promise is "▸ history (38)" —
// the hundreds answer §6 asks for.
func (a *App) boardHistoryRows(shown map[string]bool) []rail.Row {
	out := make([]rail.Row, 0, len(a.board.history))
	for _, row := range a.board.history {
		if shown[row.ID] {
			continue
		}
		out = append(out, row)
	}
	return out
}

// historyRows reads the ledger and dresses each job as the row the rail would
// have built for it — the same label, the same lifecycle, the same question
// count, the same money and the same clock, through the same functions
// (scope.go's nodeLabelOf, lifeOf, isJobRoot, nodeWall).
//
// A backend with no ledger answers nothing, which is an honest empty history and
// not a broken one — the same degradation every optional read on this surface
// makes.
func (a *App) historyRows() []rail.Row {
	ledger, ok := a.backend.(Ledger)
	if !ok {
		return nil
	}
	nodes, err := ledger.AddressableNodes()
	if err != nil || len(nodes) == 0 {
		return nil
	}
	byID := make(map[string]store.Node, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = nodes[i]
	}
	asks := questionCounts(a.catalogQuestions(), byID)
	usage := a.jobUsage()
	now := a.now()

	// Newest first is a walk BACKWARDS rather than a sort: the corpus comes back
	// in stable admission order, so its reverse is already the order a reader
	// wants (catalogJobs states the same, for the same list).
	out := make([]rail.Row, 0, len(nodes))
	for i := len(nodes) - 1; i >= 0 && len(out) < boardHistoryCap; i-- {
		node := nodes[i]
		if node.ID == store.RootID || node.Group == store.TerritoryGroup {
			// The permanent spine is not a job and a territory is a filing
			// cabinet. Neither is a place a reader can be taken to.
			continue
		}
		if !isJobRoot(node, byID) {
			continue
		}
		row := rail.Row{
			ID:        rowTaskPrefix + node.ID,
			Kind:      rail.RowTask,
			Name:      nodeLabelOf(node),
			Status:    nodeStatusLine(node),
			Life:      lifeOf(node),
			Questions: asks[node.ID],
			Seed:      node.ID,
		}
		if spent, found := usage[node.ID]; found && spent.Cost > 0 {
			row.Meta.Cost, row.Meta.HasCost = spent.Cost, true
		}
		if elapsed, known := nodeWall(node, now); known && elapsed > 0 {
			row.Meta.Elapsed, row.Meta.HasElapsed = elapsed, true
		}
		out = append(out, row)
	}
	return out
}
