package session

// memory_consolidate.go is the dreaming pass over what is remembered: ONE call,
// made while nobody is here, over the fifty lines most recently touched, whose
// whole instruction is "merge the duplicates and retire what another line has
// replaced". internal/roles named this file before it existed
// ([roles.RoleConsolidate]); this is the file it named.
//
// ── WHY IT EXISTS, AND WHAT THE EVIDENCE ACTUALLY SAYS ──
//
// Nothing in the per-turn path ever revisits an old memory. internal/reflex's
// Decide settles ONE new candidate against the three lines nearest it and then
// never looks at any of them again, so a store drifts: two sessions state the
// same preference in different words, a project's state moves on, and the
// router ends up choosing between near-synonyms — which is the retrieval
// failure that costs the most (Cuconasu et al., SIGIR 2024, measure one
// top-retrieved NON-ANSWER at −18 to −20% relative, while random documents were
// harmless).
//
// Revisiting old notes is the one memory operation with a clean ablation behind
// it. A-MEM (arXiv 2502.12110, NeurIPS 2025) turns its "memory evolution" off
// and on: multi-hop F1 21.35 → 27.02, temporal 31.24 → 45.85, from rewriting
// old notes alone. SECOM (arXiv 2502.05589, ICLR 2025) measures the compression
// half by itself at +9.46. And doing it off the turn is Sleep-time Compute
// (arXiv 2504.13171): a context queried by every future message is the regime
// where precomputing pays, with the paper's own caveat that a context queried
// once is a net loss — which is why this runs over the STORE and never over one
// conversation.
//
// ── AND WHAT IT DELIBERATELY DOES NOT DO ──
//
// THERE IS NO DECAY BY AGE. Not a half-life, not an expiry, not a floor on use
// count. No paper demonstrates that time-based forgetting improves anything:
// MemoryBank (arXiv 2305.10250, AAAI 2024) proposed the Ebbinghaus curve and
// never ablated it, and FadeMem (arXiv 2601.18642) — the most decay-committed
// paper in the literature — attributes its own gains to fusion and conflict
// resolution and ships no "without decay" row. What IS supported is retiring
// what has been SUPERSEDED (Memora, arXiv 2604.20006: the staleness penalty
// grows 18.2 → 29.5 from weekly to quarterly horizons). So the pass retires
// contradicted lines and lets old ones alone.
//
// IT NEVER DELETES. Every write it makes is one of the store's existing events
// — memory_update to rewrite a line, memory_supersede to retire one — and both
// leave the row readable ([store.Store.MemoryRecord]). Nothing here forgets,
// because a forget is the tombstone that records THE PERSON asking for one.
//
// AND IT NEVER RETIRES SOMEBODY'S OWN WORDS. See [consolidateMayRetire].

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func init() {
	// THE TIER IS THE ROLE'S WHOLE POINT. This is the archetypal cheap call:
	// made while nobody is waiting, over a short list, answering in a few lines
	// of JSON, and passed over again by the next idle hour if it got nothing.
	roles.Register(roles.RoleConsolidate, roles.TierLow,
		"tidies what is remembered while nobody is here")
}

const (
	// consolidateEvery is the floor between two passes. Six hours is chosen
	// against what the pass can possibly be worth: what it reads is a few dozen
	// lines that change a handful of times a day, so a pass an hour would be
	// five calls buying nothing, and a pass a week would let a fortnight of
	// drift accumulate before anything looked at it. It is a MINIMUM and never
	// a schedule — a machine nobody is idle on runs it less often than this,
	// and never more often.
	consolidateEvery = 6 * time.Hour

	// consolidateQuiet is how long nobody may have said anything anywhere
	// before the pass is allowed to run, handed to [StandingIdle]. Fifteen
	// minutes is "they have gone to do something else" rather than "they are
	// reading the answer" — the pass writes into the same store the very next
	// message routes against, and doing that under somebody's hands is the one
	// way an off-path pass can be felt.
	consolidateQuiet = 15 * time.Minute

	// consolidateBatch is how many remembered lines one call reads, newest
	// touched first. Fifty is internal/roles' own figure for this call and it is
	// a bound on the PROMPT rather than on the store: what changed recently is
	// what has had no chance to be tidied, and the rest was read by an earlier
	// pass.
	consolidateBatch = 50

	// consolidateFloor is how many of those must have moved since the last pass
	// for this one to be worth making. Below two there is nothing to merge with
	// anything — one new line has already been settled against its neighbours on
	// the turn that wrote it (internal/reflex's Decide) — so the call would be
	// paid for to be told what is already known.
	consolidateFloor = 2

	// consolidateOps bounds what one plan may change. It is a rail on the BLAST
	// RADIUS of a single bad answer: a pass that rewrote forty lines while
	// nobody was watching is not a tidy, it is a rewrite of somebody's memory
	// that they would have to notice to undo.
	consolidateOps = 8

	// consolidateInputRunes bounds the listing the call reads. Fifty lines at
	// the store's own text ceiling would be 25,000 runes; this is the point past
	// which the oldest-touched rows are simply left out of this pass and read by
	// the next one. NOTHING IS EVER CLIPPED MID-LINE — a model asked to merge
	// two lines out of half of one of them would write a memory that says less
	// than the row it replaced.
	consolidateInputRunes = 20000

	// consolidateTokens is the ceiling on the plan. Eight operations carrying a
	// title and a line of text each is roughly twelve hundred tokens of JSON,
	// and this is that with room to finish the object.
	consolidateTokens = 2000

	// consolidateWindow bounds the whole pass. It sits inside the tick's own
	// [standing.TickWindow], so a provider that never answers costs one pass and
	// not the reminders the pass had not reached yet.
	consolidateWindow = 90 * time.Second
)

// consolidatePrompt is the whole instruction. It is short for internal/reflex's
// reason — the answer is a few lines of JSON and the model is not being asked to
// think — and it is explicit about the ONE direction that is dangerous, because
// a plan the code refuses is a call that was paid for and threw its answer away.
const consolidatePrompt = `You tidy a person's remembered notes while they are away. You are given every note, grouped by how far its truth reaches, as: id · type · title — text.

Answer with JSON and nothing else:

{"ops":[{"op":"refine","id":"<id>","title":"<new title>","text":"<new text>"}]}

Three operations, and no others:

- "refine" — rewrite ONE note so it says one clear thing. This is how two notes that overlap become one: fold what the weaker one adds into the stronger one and refine that. Keep every fact both of them carried.
- "supersede" — retire ONE note that another note has REPLACED, giving the title and text of the note that says what is true now. Use it when a note has stopped being true, never merely because it is old.
- "skip" — nothing is worth changing. Answer {"ops":[]} and that is a good answer.

Rules:

- Never retire a preference, a decision or a correction. Those are the person's own words about how they want things; you may refine their wording, never decide they have been replaced.
- Never invent a fact that is in neither note. A merged note says what the two notes said and nothing more.
- Change nothing that is already clear. Most passes should answer {"ops":[]}.
- A title is one short line, under eighty characters. Text is one line of substance.
- Touch each id at most once, and answer with at most eight operations.`

// consolidatePlan is the answer, and [consolidateOp] is one line of it.
type consolidatePlan struct {
	Ops []consolidateOp `json:"ops"`
}

// consolidateOp is one operation, and the three ops it may name are exactly the
// store's existing events plus doing nothing. There is deliberately no new
// event kind here and no delete: what this pass can do is what a person could
// already do by hand in /memory.
type consolidateOp struct {
	// Op is "refine", "supersede" or "skip".
	Op string `json:"op"`
	// ID names the row the operation is about.
	ID string `json:"id"`
	// Title and Text are what the row — or the row replacing it — says
	// afterwards.
	Title string `json:"title"`
	Text  string `json:"text"`
}

const (
	consolidateRefine    = "refine"
	consolidateSupersede = "supersede"
	consolidateSkip      = "skip"
)

// consolidateSource is the name a write from this pass is journaled under, so
// that /memory's provenance line can tell a line the person wrote from a line
// the idle pass rewrote. It is not a session id and is not meant to look like
// one — no conversation asked for this.
const consolidateSource = "memory-tidy"

// ── the seam the tick fills ─────────────────────────────────────────────────

// NewMemoryTidy is the seam a door fills [standing.Ticker.Tidy] with.
//
// brainPath is the store's file and NOT an open store, which is the whole of
// how this stays cheap: a ticker is rebuilt every five minutes, and a handle
// opened per ticker would be a database connection per five minutes for the
// life of a window. The pass opens the store only once it has decided it is
// going to run — which is a few times a day — and closes it before it returns.
//
// A BLANK brainPath IS MEMORY OFF and answers a nil seam, so the tick has
// nothing to call. That is the same absence [Config.Memory] being nil is on the
// turn path: the door opens no store when the memory row is off, and this reads
// the same switch through the same door.
func NewMemoryTidy(parent Config, brainPath, root string, idle standing.Idle) standing.Tidy {
	if strings.TrimSpace(brainPath) == "" || strings.TrimSpace(root) == "" {
		return nil
	}
	var (
		once   sync.Once
		client Completer
		model  string
		built  error
	)
	return func(ctx context.Context) (standing.Tidied, error) {
		// THE TWO FREE GATES FIRST. Nobody is here, and it has been long enough
		// since the last pass. Neither opens a database or a connection, so an
		// ordinary tick — one every five minutes, forever — pays a stat and a
		// world read for this and nothing else.
		if idle == nil || !idle(consolidateQuiet) {
			return standing.Tidied{}, nil
		}
		mark, _ := readConsolidateMark(root)
		if !mark.At.IsZero() && time.Since(mark.At) < consolidateEvery {
			return standing.Tidied{}, nil
		}
		brain, err := store.Open(brainPath)
		if err != nil {
			return standing.Tidied{}, err
		}
		defer brain.Close()

		once.Do(func() {
			model, built = roles.Resolve(roles.Source(parent.RolesSource), roles.RoleConsolidate, parent.Model)
			if built != nil {
				return
			}
			client, built = provider.NewClient(provider.Config{
				APIKey:  parent.APIKey,
				BaseURL: parent.BaseURL,
				Model:   model,
				Timeout: providerTimeout,
				Routing: provider.StaticRouting(parent.Routing),
			})
		})
		if built != nil {
			return standing.Tidied{}, built
		}
		return tidyPass{brain: brain, client: client, model: model, root: root, mark: mark}.run(ctx)
	}
}

// tidyPass is one pass with everything it needs already resolved: the store
// open, the client built, and the watermark read.
//
// It is a type rather than six arguments because the SEAM above owns the two
// things a test cannot have — a database on the person's disk and a connection
// to a provider — and this owns everything else. Splitting them there is what
// lets the gate, the plan and every refusal be tested without either.
type tidyPass struct {
	brain  *store.Store
	client Completer
	model  string
	root   string
	mark   consolidateMark
}

// run is the pass proper: read what is remembered, decide whether it is worth a
// call, make it, apply what came back, stamp the clock and say one line.
func (p tidyPass) run(ctx context.Context) (standing.Tidied, error) {
	batch, err := p.brain.ListMemories("", consolidateBatch)
	if err != nil {
		return standing.Tidied{}, err
	}
	if consolidateChanged(batch, p.mark.Seq) < consolidateFloor {
		// NOT ENOUGH HAS MOVED, and the watermark is deliberately NOT advanced:
		// the one line that did change is still owed a look, and stamping the
		// clock here would mean a store that changes a line every seven hours is
		// never tidied at all.
		return standing.Tidied{}, nil
	}
	bounded, cancel := context.WithTimeout(ctx, consolidateWindow)
	defer cancel()

	plan, usd, err := consolidateAsk(bounded, p.client, p.model, batch)
	if err != nil {
		return standing.Tidied{USD: usd}, err
	}
	tidied := applyConsolidatePlan(p.brain, batch, plan)
	tidied.USD = usd

	// THE WATERMARK IS TAKEN AFTER THE WRITES, from the store rather than from
	// the batch, so the pass's own updates are not what the next pass reads as
	// "something changed". A pass that re-triggered itself would tidy the same
	// fifty lines every six hours forever.
	_ = writeConsolidateMark(p.root, consolidateMark{
		At: time.Now(), Seq: consolidateHighWater(p.brain, batch),
		Merged: tidied.Merged, Superseded: tidied.Superseded, USD: usd,
	})

	if line := consolidateNoticeLine(tidied); line != "" {
		noticeLiveWindow(line)
	}
	return tidied, nil
}

// consolidateChanged counts how many of the rows read have moved since the
// watermark. A zero watermark is a machine that has never run a pass, on which
// every row counts — the first idle window after this ships tidies whatever is
// already there.
func consolidateChanged(batch []store.Memory, since int64) int {
	changed := 0
	for _, memory := range batch {
		if memory.UpdatedSeq > since {
			changed++
		}
	}
	return changed
}

// consolidateHighWater is the newest sequence the store holds after a pass. It
// falls back to the batch's own highest when the read fails, which is the safe
// direction: a watermark that is too NEW skips a pass, where one that is too old
// pays for one that changes nothing.
func consolidateHighWater(brain *store.Store, batch []store.Memory) int64 {
	high := int64(0)
	for _, memory := range batch {
		if memory.UpdatedSeq > high {
			high = memory.UpdatedSeq
		}
	}
	newest, err := brain.ListMemories("", 1)
	if err == nil && len(newest) == 1 && newest[0].UpdatedSeq > high {
		high = newest[0].UpdatedSeq
	}
	return high
}

// ── the call ────────────────────────────────────────────────────────────────

// consolidateAsk makes the one call and reads the plan out of it.
//
// It is ONE call and never two. internal/reflex retries an unusable answer once
// because a turn is waiting on it; nothing is waiting on this, the next pass is
// six hours away, and a second full-price call to re-ask a question nobody
// asked is the bill this whole file is shaped to avoid.
func consolidateAsk(ctx context.Context, client Completer, model string, batch []store.Memory) (consolidatePlan, float64, error) {
	if client == nil {
		return consolidatePlan{}, 0, errors.New("session: there is nothing in this build to tidy with")
	}
	response, err := client.CompleteWithMessages(
		// WithoutStream for the sentinel's reason: nobody is watching this, and
		// a stream would be typing JSON into a room that is not open.
		// IntentBackground is the same fact aimed at the router: a pass that runs
		// six hours from now is not waiting on the fastest endpoint, it is
		// waiting on the cheapest one.
		// And the role, which is the same fact said where the table can price it:
		// a consolidation is the memory reflex's slow half, unattended and
		// judged on nothing but whether its answer is usable (internal/lane's
		// roles.go).
		provider.WithRole(
			provider.WithRoutingIntent(provider.WithoutStream(ctx), provider.IntentBackground),
			lane.RoleMemory),
		[]ai.Message{
			textMessage("system", consolidatePrompt),
			textMessage("user", consolidateListing(batch)),
		},
		ai.WithModel(model),
		ai.WithMaxTokens(consolidateTokens),
		ai.WithTemperature(0))
	if err != nil {
		return consolidatePlan{}, 0, err
	}
	if response == nil {
		return consolidatePlan{}, 0, errors.New("session: the tidy answered nothing")
	}
	usd := 0.0
	if response.Usage != nil && response.Usage.Cost != nil {
		usd = *response.Usage.Cost
	}
	var plan consolidatePlan
	if err := provider.DecodeJSONObject(response.Text(), &plan); err != nil {
		return consolidatePlan{}, usd, err
	}
	return plan, usd, nil
}

// consolidateListing is what the call reads: every row grouped by how far its
// truth reaches, because that is the only grouping under which two lines can
// honestly be merged — something true about the person everywhere and something
// true only in one repository are not duplicates however alike they read.
func consolidateListing(batch []store.Memory) string {
	byScope := map[string][]store.Memory{}
	for _, memory := range batch {
		byScope[memory.Scope] = append(byScope[memory.Scope], memory)
	}
	var out strings.Builder
	used := 0
	for _, scope := range []string{store.MemoryScopeUser, store.MemoryScopeProject, store.MemoryScopeEnv} {
		rows := byScope[scope]
		if len(rows) == 0 {
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].UpdatedSeq > rows[j].UpdatedSeq })
		header := consolidateScopeWord(scope) + "\n"
		for _, memory := range rows {
			line := "  " + memory.ID + " · " + memory.Type + " · " + memory.Title + " — " + memory.Text + "\n"
			cost := utf8.RuneCountInString(line)
			if header != "" {
				cost += utf8.RuneCountInString(header)
			}
			if used+cost > consolidateInputRunes {
				// THE TAIL GOES WHOLE OR NOT AT ALL. What is dropped is the
				// least recently touched, which is what an earlier pass has
				// already read.
				break
			}
			if header != "" {
				out.WriteString(header)
				header = ""
			}
			out.WriteString(line)
			used += cost
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

// consolidateScopeWord is the scope in the words the manual uses, because the
// listing is the only place a model is told what a scope means.
func consolidateScopeWord(scope string) string {
	switch scope {
	case store.MemoryScopeProject:
		return "true only inside one project"
	case store.MemoryScopeEnv:
		return "true only on this machine"
	}
	return "true about them everywhere"
}

// ── applying the plan ───────────────────────────────────────────────────────

// applyConsolidatePlan writes the plan through the store's existing events and
// answers what actually landed.
//
// EVERY REFUSAL IS SILENT AND LOCAL. An operation naming a row that is not in
// the batch, a row already touched by this plan, an empty title or a retirement
// the pass is not allowed to make changes nothing and stops nothing else in the
// plan — the model answered about fifty lines and got one of them wrong, which
// is not a reason to throw away the other seven.
func applyConsolidatePlan(brain *store.Store, batch []store.Memory, plan consolidatePlan) standing.Tidied {
	known := map[string]store.Memory{}
	for _, memory := range batch {
		known[memory.ID] = memory
	}
	touched := map[string]bool{}
	var tidied standing.Tidied
	for index, op := range plan.Ops {
		if index >= consolidateOps {
			break
		}
		id := strings.TrimSpace(op.ID)
		row, found := known[id]
		if !found || touched[id] {
			continue
		}
		title, text := consolidateBody(op, row)
		if title == "" || text == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case consolidateRefine:
			if title == row.Title && text == row.Text {
				// A rewrite that rewrites nothing is a skip that spelled itself
				// differently, and counting it would put a line on the person's
				// screen about a store that did not move.
				continue
			}
			if err := brain.UpdateMemoryFromSession(id, title, text, row.Tags, consolidateSource); err != nil {
				continue
			}
			touched[id] = true
			tidied.Merged++
		case consolidateSupersede:
			if !consolidateMayRetire(row) {
				continue
			}
			fresh := store.Memory{
				Type: row.Type, Scope: row.Scope,
				Title: title, Text: text, Tags: row.Tags,
				SourceSession: consolidateSource,
			}
			// THE TYPE AND THE SCOPE ARE THE RETIRED ROW'S. A replacement is the
			// same memory said better; one that changed how far its truth
			// reaches would be a different memory wearing the old one's place.
			if _, err := brain.SupersedeMemory(id, fresh); err != nil {
				continue
			}
			touched[id] = true
			tidied.Superseded++
		case consolidateSkip, "":
			continue
		}
	}
	return tidied
}

// consolidateBody is the title and text an operation leaves behind, trimmed and
// bounded to what the store will take. A row the plan named without saying
// anything new keeps what it had, which is what makes a plan carrying only an
// id and an op a no-op rather than a write of two empty strings.
func consolidateBody(op consolidateOp, row store.Memory) (string, string) {
	title := strings.TrimSpace(op.Title)
	if title == "" {
		title = row.Title
	}
	text := strings.TrimSpace(op.Text)
	if text == "" {
		text = row.Text
	}
	return consolidateClip(title, store.MemoryTitleRunes), consolidateClip(text, store.MemoryTextRunes)
}

// consolidateClip keeps a field inside the store's own ceiling. The store
// refuses anything longer, and a refusal here would throw away a good plan over
// a model that wrote one word too many.
func consolidateClip(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:limit]))
}

// consolidateMayRetire is the one refusal this pass makes in code rather than
// in the prompt, and it is the most important line in the file.
//
// A SUPERSEDE IS A MODEL DECIDING THAT SOMETHING SOMEBODY SAID IS NO LONGER
// TRUE, with nobody in the room and nothing to appeal to. Two measurements say
// not to let it near a person's own words: BEAM (arXiv 2510.27246, ICLR 2026)
// scores contradiction resolution at 0.000–0.053 for EVERY model and method
// tested — retiring a fact correctly is the thing nothing in this literature can
// do — and MINJA (arXiv 2503.03704) shows a memory store being rewritten
// through ordinary conversation alone at 98.2% injection success, which is
// exactly the material this pass reads.
//
// So it may only retire the two types that go stale on their own: a `fact` the
// world has moved past, and `project_state`, which the store's own comment says
// "goes stale in a way a preference never does". A preference, a decision and a
// correction are somebody stating how they want things — a correction is
// literally them saying aforge had it wrong — and the pass may sharpen their
// wording with a refine, never decide they have been replaced.
//
// The store keeps no "the person typed this" flag, so this is a rule about
// TYPES and it over-protects: a `preference` the extractor wrote from an
// exchange is shielded exactly as one typed into /remember is. That is the safe
// direction. The unsafe one is a pass that retires the sentence somebody wrote
// by hand in favour of one a small model inferred.
func consolidateMayRetire(row store.Memory) bool {
	switch row.Type {
	case store.MemoryFact, store.MemoryProjectState:
		return true
	}
	return false
}

// ── what the person is told ─────────────────────────────────────────────────

// consolidateNoticeLine is the one dim line a tidy ever says, and it is said
// only when something actually moved. THE EMPTINESS LAW: a half that is zero is
// absent rather than printed as a zero, and a pass that changed nothing says
// nothing at all — which is most passes.
func consolidateNoticeLine(tidied standing.Tidied) string {
	parts := make([]string, 0, 2)
	if tidied.Merged > 0 {
		parts = append(parts, fmt.Sprintf("%d merged", tidied.Merged))
	}
	if tidied.Superseded > 0 {
		parts = append(parts, fmt.Sprintf("%d superseded", tidied.Superseded))
	}
	if len(parts) == 0 {
		return ""
	}
	return "memory tidied · " + strings.Join(parts, " · ")
}

// noticeLiveWindow puts one line into the conversation the person most recently
// touched in this process, and into no other.
//
// IT GOES THROUGH [Agent.sayMemory], which is the path `remembered ·`,
// `forgot ·` and `superseded ·` already travel: the turn's own stream when a
// turn is open, and held until the next one when there is not. Holding is the
// ordinary case here — the pass runs precisely because nobody is typing — and
// it is what keeps the line from being written into a room with no reader.
//
// ONE WINDOW AND NOT ALL OF THEM. The store is one brain shared by every
// conversation on the machine, so a line per open window would be the same
// piece of news said four times — [standingRunner.deliver]'s law, applied to a
// pass that has no origin conversation to prefer.
//
// AND NO WINDOW AT ALL IS THE ORDINARY CASE, because most passes happen inside
// `aforge tick` with nothing open anywhere. Then nothing is said, and /memory is
// where the change is seen.
func noticeLiveWindow(text string) {
	liveSessionsMu.Lock()
	windows := make([]liveWindow, 0, len(liveSessions))
	for _, window := range liveSessions {
		windows = append(windows, window)
	}
	liveSessionsMu.Unlock()

	var best *Agent
	var bestAt time.Time
	for _, window := range windows {
		if window.agent == nil || !window.agent.remembers() {
			continue
		}
		at := liveSessionTouched(window)
		if best == nil || at.After(bestAt) {
			best, bestAt = window.agent, at
		}
	}
	if best == nil {
		return
	}
	best.sayMemory(text)
}

// ── the watermark ───────────────────────────────────────────────────────────

// consolidateMark is when the last pass ran, how far the store had got by then,
// and what that pass came to. It is ONE small file rewritten per pass rather
// than a log, because it is a watermark and not an audit: what the pass actually
// changed is in the store's own event journal, under [consolidateSource], and
// every line of it is readable in /memory.
type consolidateMark struct {
	At         time.Time `json:"at"`
	Seq        int64     `json:"seq"`
	Merged     int       `json:"merged,omitempty"`
	Superseded int       `json:"superseded,omitempty"`
	USD        float64   `json:"usd,omitempty"`
}

// consolidateMarkName is the file, beside everything else standing keeps.
const consolidateMarkName = "memory-tidy.json"

func consolidateMarkPath(root string) string {
	return filepath.Join(root, consolidateMarkName)
}

// readConsolidateMark answers the last pass, and false for a machine that has
// never run one. A file that cannot be read is the same answer as no file: the
// worst it costs is one pass made earlier than it had to be.
func readConsolidateMark(root string) (consolidateMark, bool) {
	raw, err := os.ReadFile(consolidateMarkPath(root))
	if err != nil {
		return consolidateMark{}, false
	}
	var mark consolidateMark
	if json.Unmarshal(raw, &mark) != nil {
		return consolidateMark{}, false
	}
	return mark, true
}

// writeConsolidateMark stamps the clock. It is written temp-and-rename for the
// reason every other file in the ambient side is: a machine that lost power
// mid-write would otherwise come back with a half-written watermark, and the
// honest reading of that file is "no pass has ever run".
func writeConsolidateMark(root string, mark consolidateMark) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(mark)
	if err != nil {
		return err
	}
	temp := consolidateMarkPath(root) + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, consolidateMarkPath(root))
}
