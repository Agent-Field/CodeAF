package head

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The lens: three reads that make the journal total.
//
// It lives in internal/head rather than in a package of its own for one reason
// and it is temporary — these reads need the Head's store handle and every
// renderer already written against it (renderPlan, renderResult's shape,
// resultChildren, artifact.go's boundary, jobResult's continuation chase), and
// moving them out before they exist would mean either exporting a dozen
// renderers or writing them twice. The extraction wave named in
// audit-notes/chat-simplify.md Part 3.7 moves this file to internal/lens
// wholesale, with the renderers it leans on; nothing here is meant to stay
// under a package whose name says "head".
//
// What the three reads are FOR is the product rule in Part 0.4: if the store
// knows it, the conversation can surface it. Today's eleven reads leave two
// holes that nothing patches — a running step's own progress reaches the TUI
// and nothing else, and the system's own state is split across three tools and
// a pane — and every one of them cuts its answer at a byte ceiling written when
// truncation was the safety mechanism. So:
//
//   - recall(q, kind?, since?, until?, session?) — one search over everything
//     settled or said, blending the five FTS surfaces and settled history, each
//     hit carrying real bytes and an id that open takes.
//   - open(id, job?, part?, raw?) — one thing whole and state-aware: a running
//     job's plan with live per-step status and its progress feed, a finished
//     job's whole result, a file's actual bytes, a rule, a service, a belief.
//     raw:true hands back the journal rows underneath it, unredacted.
//   - status() — the whole system on one page.
//
// The byte ceilings do not come with them. These three PAGE: a read past one
// transport page says exactly which part of how many it is holding and how to
// ask for the next, so the loop is never handed a fragment it believes is the
// whole. The old reads keep their caps, because those caps are load-bearing for
// prompts that carry a board and a thread beside them; these are reads the loop
// chose, and a read the loop chose is worth its bytes.

const (
	beltToolRecall = "recall"
	beltToolOpen   = "open"
	beltToolStatus = "status"
)

// The kind words recall filters on. They are the person's vocabulary for the
// six things memory is made of, not the store's names for its tables — a person
// asking "only the rules" has never heard of a charter.
const (
	lensKindMessage = "message"
	lensKindJob     = "job"
	lensKindResult  = "result"
	lensKindBelief  = "belief"
	lensKindRule    = "rule"
	lensKindService = "service"
)

const (
	// lensHitCap is how many hits one recall renders. It is a rendering bound
	// and never a silent one: a clipped recall says how many it dropped and how
	// to narrow, which is the difference between a bounded answer and a lie.
	lensHitCap = 20
	// lensSourceCap is how many hits each surface contributes before the blend.
	// It is deliberately larger than lensHitCap: a question whose whole answer
	// lives in one surface — "what did we say about the migration?" is entirely
	// conversation — must still be able to fill the page and then say honestly
	// that there is more behind it. A per-surface cap under the page cap would
	// make the clip marker unreachable for exactly those questions.
	lensSourceCap = 24
	// lensSnippetBytes is the real content each hit carries. It is bytes off the
	// thing itself, never a summary of it: a search result whose snippet was
	// composed rather than quoted teaches the loop to trust a sentence nobody
	// wrote.
	lensSnippetBytes = 200
	// lensPageBytes is one transport page. It is a page, not a ceiling: past it
	// the read continues at part+1 and says so in the same breath.
	lensPageBytes = 8 << 10
	// lensProgressTail is how much of a live job's feed one open shows. The tail
	// rather than the head, because the question behind opening running work is
	// always "where is it now".
	lensProgressTail = 12
	// lensRawEvents and lensRawMessages bound one raw read's two halves.
	lensRawEvents   = 40
	lensRawMessages = 20
	// lensRawScanSeqs is how far back a raw read walks the journal looking for
	// one node's events. The events table is not indexed by node, so this is a
	// scan, and a scan with no bound is a read that gets slower every day the
	// resident lives. A raw read that hit the bound says where it stopped.
	lensRawScanSeqs = 20000
	// lensFileCandidateNodes bounds the sweep that resolves a bare file name to
	// the job that recorded it. Past this the answer is "name the job".
	lensFileCandidateNodes = 400
	// lensNodeMessagePage is one node's share of a progress read.
	lensNodeMessagePage = 200
	// lensServiceCap and lensStatusListCap bound the list sections of status.
	lensStatusListCap = 8
)

// ---------------------------------------------------------------------------
// recall
// ---------------------------------------------------------------------------

// lensHit is one thing memory turned up, in the shape the answer needs it:
// which kind of thing it is, the id that opens it, when it happened, and real
// bytes off it.
//
// rank and score are what the blend orders on. rank is the hit's position
// inside its OWN surface — every surface here already ranks its own results,
// four of them by BM25 — and score is the real number where a surface exposes
// one. Nothing compares a score across surfaces, because those numbers are not
// comparable and pretending otherwise would quietly let whichever index happens
// to score generously own the whole page.
type lensHit struct {
	kind    string
	id      string
	at      time.Time
	snippet string
	rank    int
	score   float64
}

// recall is the one search over everything settled or said.
//
// The five FTS surfaces have been in the store for months and were reachable
// through three separate tools that each knew about some of them, so the answer
// to "what do we know about pricing?" depended on which tool the model happened
// to pick. Here they are one question: the conversation, the notebook, the
// graph, the standing rules, the services, and the settled rows a territory has
// packed away. A person asking about the past cannot know which of those holds
// their answer and has never had to.
func (run *beltRun) recall(args map[string]any) (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "recall is not available on this surface.", false
	}
	query := strings.TrimSpace(beltString(args, "q"))
	if query == "" {
		return "q must say what to look for, in the user's own words", true
	}
	kind := strings.ToLower(strings.TrimSpace(beltString(args, "kind")))
	if kind != "" && !lensKnownKind(kind) {
		return "kind must be one of message, job, result, belief, rule, service", true
	}
	since, ok := parseHistoryBound(beltString(args, "since"), false)
	if !ok {
		return `since must be a local date or time, "2026-08-06" or "2026-08-06T09:00"`, true
	}
	until, ok := parseHistoryBound(beltString(args, "until"), true)
	if !ok {
		return `until must be a local date or time, "2026-08-06" or "2026-08-06T09:00"`, true
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return "until is before since — the window is empty as written", true
	}
	hits := run.head.lensGather(query, kind, strings.TrimSpace(beltString(args, "session")), since, until)
	if len(hits) == 0 {
		return "nothing remembered matches those words — not in the conversation, not in the notebook, not in work, not in the standing rules or services. Say that plainly rather than reconstructing it.", false
	}
	return lensRenderHits(query, kind, hits), false
}

func lensKnownKind(kind string) bool {
	switch kind {
	case lensKindMessage, lensKindJob, lensKindResult, lensKindBelief, lensKindRule, lensKindService:
		return true
	}
	return false
}

// lensGather asks every surface the kind filter admits and blends what comes
// back. Each surface is asked for lensSourceCap and failures are misses: an
// index that cannot answer must never turn a read into an error, because the
// other five still have something true to say.
func (h *Head) lensGather(query, kind, session string, since, until time.Time) []lensHit {
	now := time.Now()
	wants := func(candidate string) bool { return kind == "" || kind == candidate }
	hits := make([]lensHit, 0, lensSourceCap*4)

	if wants(lensKindMessage) {
		hits = append(hits, h.lensMessageHits(query, session)...)
	}
	if wants(lensKindJob) || wants(lensKindResult) {
		hits = append(hits, h.lensNodeHits(query, since, until, now)...)
	}
	if wants(lensKindBelief) {
		hits = append(hits, h.lensBeliefHits(query)...)
	}
	if wants(lensKindRule) {
		hits = append(hits, h.lensRuleHits(query)...)
	}
	if wants(lensKindService) {
		hits = append(hits, h.lensServiceHits(query)...)
	}

	kept := make([]lensHit, 0, len(hits))
	for _, hit := range hits {
		if !wants(hit.kind) {
			continue
		}
		if !lensInWindow(hit.at, since, until) {
			continue
		}
		kept = append(kept, hit)
	}
	// The blend: best-of-each-surface first, then second-of-each, and so on.
	// Recency decides inside a tier, because two surfaces both offering their
	// best guess have given the caller no other reason to prefer one.
	sort.SliceStable(kept, func(first, second int) bool {
		if kept[first].rank != kept[second].rank {
			return kept[first].rank < kept[second].rank
		}
		if kept[first].score != kept[second].score {
			return kept[first].score > kept[second].score
		}
		return kept[first].at.After(kept[second].at)
	})
	return kept
}

// lensInWindow is the time filter. A hit whose time is unknown survives an
// unbounded read and is dropped by a bounded one: the caller who named a window
// asked a question about time, and answering it with something undated is
// answering a different question.
func lensInWindow(at, since, until time.Time) bool {
	if since.IsZero() && until.IsZero() {
		return true
	}
	if at.IsZero() {
		return false
	}
	if !since.IsZero() && at.Before(since) {
		return false
	}
	if !until.IsZero() && at.After(until) {
		return false
	}
	return true
}

func (h *Head) lensMessageHits(query, session string) []lensHit {
	found, err := h.store.SearchMessages(query, session, lensSourceCap)
	if err != nil || len(found) == 0 {
		return nil
	}
	hits := make([]lensHit, 0, len(found))
	for index, hit := range found {
		who := "you"
		if hit.Role == store.RoleUser {
			who = "they"
		}
		hits = append(hits, lensHit{
			kind: lensKindMessage, id: lensMessageID(hit.Seq), at: hit.Time, rank: index,
			snippet: who + " said: " + lensSnippet(hit.Body),
		})
	}
	return hits
}

// lensNodeHits is work, from all three places work is findable: the ranked
// graph search, the fold index over packed history, and the settled rows a time
// window selects. They overlap and the overlap is deduplicated by id, keeping
// whichever source ranked the node highest.
//
// The job/result split is what the node has to say for itself. A node with a
// recorded finding is a result — that is what the person means when they ask
// for results — and a node without one is work, whether it is running, queued
// or failed. So a kind filter of "result" cannot hand back an empty row, and a
// filter of "job" cannot bury live work under last month's conclusions.
func (h *Head) lensNodeHits(query string, since, until time.Time, now time.Time) []lensHit {
	type candidate struct {
		node  store.Node
		rank  int
		score float64
	}
	best := make(map[string]candidate, lensSourceCap*2)
	consider := func(node store.Node, rank int, score float64) {
		if node.ID == store.RootID || !beltAddressable(node) {
			return
		}
		if existing, seen := best[node.ID]; seen && existing.rank <= rank {
			return
		}
		best[node.ID] = candidate{node: node, rank: rank, score: score}
	}

	if targets, err := h.store.SearchSurgeryTargets(query, false); err == nil {
		for index, target := range targets {
			if index == lensSourceCap {
				break
			}
			consider(target.Node, index, target.Score)
		}
	}
	if recalled, err := h.store.Recall(query, resident.ExtractCues(query), lensSourceCap); err == nil {
		for index, hit := range recalled {
			node, found, readErr := h.store.Node(hit.NodeID)
			if readErr != nil || !found {
				continue
			}
			consider(node, index, hit.Score)
		}
	}
	// The settled rows, which is the only read that is ordered by WHEN rather
	// than by relevance — so a window narrows it and the words then filter it,
	// rather than the other way round.
	if settled, err := h.store.SettledHistory(since, until, store.SettledHistoryCap); err == nil {
		terms := lensTerms(query)
		matched := 0
		for _, node := range settled {
			if matched == lensSourceCap {
				break
			}
			if !lensMatchesTerms(terms, surgeryTargetLabel(node), node.ID, h.jobResult(node)) {
				continue
			}
			consider(node, matched, 0)
			matched++
		}
	}

	hits := make([]lensHit, 0, len(best))
	for _, found := range best {
		finding := h.jobResult(found.node)
		kind := lensKindJob
		snippet := strings.TrimSpace(found.node.Brief)
		if strings.TrimSpace(finding) != "" {
			kind = lensKindResult
			snippet = finding
		}
		if strings.TrimSpace(snippet) == "" {
			snippet = surgeryTargetLabel(found.node)
		}
		hits = append(hits, lensHit{
			kind: kind, id: found.node.ID, at: lensNodeTime(found.node), rank: found.rank, score: found.score,
			snippet: fmt.Sprintf("%s | %s | %s", surgeryTargetLabel(found.node),
				lensNodeStateWord(found.node, now), lensSnippet(snippet)),
		})
	}
	// A map has no order and the blend sorts on rank, so equal ranks would come
	// out in whatever order the runtime felt like. Settle them here.
	sort.SliceStable(hits, func(first, second int) bool {
		if hits[first].rank != hits[second].rank {
			return hits[first].rank < hits[second].rank
		}
		return hits[first].id < hits[second].id
	})
	return hits
}

func lensNodeTime(node store.Node) time.Time {
	if !node.FinishedAt.IsZero() {
		return node.FinishedAt
	}
	return node.StartedAt
}

func lensNodeStateWord(node store.Node, now time.Time) string {
	word := surgeryStatusWord(node.Status)
	if node.Held {
		word = "paused"
	}
	if age := store.AgeLabel(lensNodeTime(node), now); age != "" {
		word += " " + age
	}
	return word
}

func (h *Head) lensBeliefHits(query string) []lensHit {
	facts, err := h.store.SearchFacts(store.FactQuery{
		Cues: resident.ExtractCues(query), Terms: query, Limit: lensSourceCap,
	})
	if err != nil || len(facts) == 0 {
		return nil
	}
	hits := make([]lensHit, 0, len(facts))
	for index, fact := range facts {
		hits = append(hits, lensHit{
			kind: lensKindBelief, id: lensFactID(fact.Seq), at: fact.Time, rank: index,
			snippet: "[" + fact.Scope + "] " + lensSnippet(fact.Body),
		})
	}
	return hits
}

func (h *Head) lensRuleHits(query string) []lensHit {
	charters, err := h.store.SearchActiveCharters(query)
	if err != nil || len(charters) == 0 {
		return nil
	}
	hits := make([]lensHit, 0, len(charters))
	for index, charter := range charters {
		if index == lensSourceCap {
			break
		}
		hits = append(hits, lensHit{
			kind: lensKindRule, id: charter.ID, at: charter.CreatedAt, rank: index,
			snippet: lensSnippet(charter.Invariant) + " | " + lensWatchPhrase(charter.Watch),
		})
	}
	return hits
}

func (h *Head) lensServiceHits(query string) []lensHit {
	services, err := h.store.SearchServices(query)
	if err != nil || len(services) == 0 {
		return nil
	}
	hits := make([]lensHit, 0, len(services))
	for index, service := range services {
		if index == lensSourceCap {
			break
		}
		hits = append(hits, lensHit{
			kind: lensKindService, id: service.Name, at: service.StartedAt, rank: index,
			snippet: string(service.Status) + " | " + lensSnippet(service.Command),
		})
	}
	return hits
}

// lensRenderHits is the page recall hands back. Every line carries the kind, an
// id open takes, when it happened, and bytes off the thing itself.
//
// The clip marker is written only when there is something behind it, and it
// names the remedy in the tool's own arguments — a loop told "there is more"
// with no way to ask for it learns to guess at what it cannot see.
func lensRenderHits(query, kind string, hits []lensHit) string {
	clipped := 0
	if len(hits) > lensHitCap {
		clipped = len(hits) - lensHitCap
		hits = hits[:lensHitCap]
	}
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "recall %q", query)
	if kind != "" {
		rendered.WriteString(" | kind " + kind)
	}
	fmt.Fprintf(&rendered, " | %d %s\n", len(hits), pluralWord(len(hits), "hit", "hits"))
	for _, hit := range hits {
		when := "when unknown"
		if !hit.at.IsZero() {
			when = hit.at.Local().Format(nowLineLayout)
		}
		fmt.Fprintf(&rendered, "- %s | %s | %s | %s\n", hit.kind, hit.id, when, hit.snippet)
	}
	if clipped > 0 {
		fmt.Fprintf(&rendered, "%d further %s not shown — more via recall(q, kind:message|job|result|belief|rule|service) or a since/until window\n",
			clipped, pluralWord(clipped, "hit", "hits"))
	}
	return strings.TrimSpace(rendered.String())
}

// lensSnippet is real content, cut to one readable line. Newlines collapse
// rather than being dropped at the first one: a result whose first line is a
// heading would otherwise contribute a heading and nothing else.
func lensSnippet(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	return truncateBytes(strings.Join(fields, " "), lensSnippetBytes)
}

// lensTerms and lensMatchesTerms are the term filter the settled-history source
// needs and nothing else does — that read is ordered by time, so the words have
// to be applied here. It is containment rather than an index because the corpus
// is already bounded to one window's worth of rows.
func lensTerms(query string) []string {
	terms := make([]string, 0, 8)
	for _, field := range strings.Fields(strings.ToLower(query)) {
		field = strings.Trim(field, `"'(),;:.?!`)
		if len(field) < 3 {
			continue
		}
		terms = append(terms, field)
	}
	return terms
}

func lensMatchesTerms(terms []string, corpus ...string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join(corpus, " "))
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			return true
		}
	}
	return false
}

// lensMessageID and lensFactID are the two ids that are not already names. A
// recall hit promises that its id opens, so the two surfaces whose rows are
// numbered get a spelling open can recognise and a person can read.
func lensMessageID(seq int64) string { return "msg-" + strconv.FormatInt(seq, 10) }
func lensFactID(seq int64) string    { return "#" + strconv.FormatInt(seq, 10) }

// ---------------------------------------------------------------------------
// open
// ---------------------------------------------------------------------------

// open is one thing whole.
//
// It is state-aware because "open that" means two different things depending on
// whether the thing is still happening: a running job's whole truth is its plan
// with live per-step status and the feed its workers are writing right now,
// while a finished job's whole truth is what it concluded and what it wrote. The
// first of those reached the TUI and nothing else — the head could see that
// three steps were running and never what any of them was doing — which is the
// hole this read exists to close.
//
// Nothing here truncates. A body past one transport page is paged, and the page
// says which part of how many it is, so a loop is never handed a fragment it
// believes is the whole.
func (run *beltRun) open(args map[string]any) (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "open is not available on this surface.", false
	}
	id := strings.TrimSpace(beltString(args, "id"))
	if id == "" {
		return "id must name one thing — a job id from a board or recall read, a file, a rule, a service, or a #belief number", true
	}
	part := int(beltInt(args, "part"))
	if part <= 0 {
		part = 1
	}
	rendered, err := run.head.lensOpen(id, strings.TrimSpace(beltString(args, "job")),
		beltBool(args, "raw"), part)
	if err != nil {
		return err.Error(), true
	}
	return rendered, false
}

// lensOpen resolves what the id names and renders it whole, then pages.
//
// The resolution order is the order the ids cannot collide in: an explicit job
// argument makes the id a file inside that job and settles it outright; a live
// or settled node is the commonest thing anyone opens and is checked next; the
// two numbered surfaces carry spellings of their own; names come last, because
// a name is the only kind of id that can be ambiguous and the specific things
// have already had their turn.
func (h *Head) lensOpen(id, job string, raw bool, part int) (string, error) {
	if job != "" {
		node, err := h.beltRecordedJob(job, "job")
		if err != nil {
			return "", err
		}
		return h.lensJobFile(node, id, part)
	}
	if node, found, err := h.store.Node(id); err == nil && found &&
		node.ID != store.RootID && beltAddressable(node) {
		if raw {
			body, rawErr := h.lensRawNode(node)
			if rawErr != nil {
				return "", rawErr
			}
			return lensPage(id, body, part), nil
		}
		body, jobErr := h.lensJob(node)
		if jobErr != nil {
			return "", jobErr
		}
		return lensPage(id, body, part), nil
	}
	if seq, ok := lensMessageSeq(id); ok {
		body, err := h.lensMessage(seq)
		if err != nil {
			return "", err
		}
		return lensPage(id, body, part), nil
	}
	if seq, ok := lensFactSeq(id); ok {
		body, err := h.lensFact(seq)
		if err != nil {
			return "", err
		}
		return lensPage(id, body, part), nil
	}
	if charter, found, err := h.store.Charter(id); err == nil && found {
		return lensPage(id, lensCharterRecord(charter), part), nil
	}
	if service, found, err := h.store.ServiceByName(id); err == nil && found {
		return lensPage(id, lensServiceRecord(service), part), nil
	}
	if service, found, err := h.store.Service(id); err == nil && found {
		return lensPage(id, lensServiceRecord(service), part), nil
	}
	if rendered, err, resolved := h.lensAnyFile(id, part); resolved {
		return rendered, err
	}
	return "", fmt.Errorf("nothing here is called %q — ids come from a board or recall read: a job id, a file a job wrote (pass job: as well when two jobs wrote the same name), a standing rule's id, a service's name, or a #number from the notebook. A learned way of working is not reachable from this conversation", id)
}

// lensJob is the state-aware half. The line above both branches is the same,
// because "what is this and what has it cost" is the same question either way;
// what differs underneath is whether the honest answer is a plan and a feed or
// a conclusion and a file.
func (h *Head) lensJob(node store.Node) (string, error) {
	now := time.Now()
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | %s | %s", node.ID, node.Status, surgeryTargetLabel(node))
	spend := 0.0
	if impact, err := h.store.Impact(node.ID, now); err == nil {
		spend = impact.Cost
	}
	fmt.Fprintf(&rendered, " | $%.2f", spend)
	if age := store.AgeLabel(lensNodeTime(node), now); age != "" {
		rendered.WriteString(" | " + age)
	}
	rendered.WriteString("\n")

	if h.lensLive(node) {
		// The plan renderer, uncapped: this read is a page of its own and the
		// question behind opening running work is exactly which step is where.
		plan, failed := h.renderPlanWithin(node, 0, 0)
		if failed {
			rendered.WriteString("plan: " + plan + "\n")
		} else {
			rendered.WriteString(plan + "\n")
		}
		feed := h.lensProgressFeed(node.ID, lensProgressTail)
		if len(feed) == 0 {
			rendered.WriteString("progress: nothing has been said on this job yet.\n")
		} else {
			rendered.WriteString("progress, most recent last:\n" + strings.Join(feed, "\n") + "\n")
		}
		if files := h.lensFiles(node); len(files) > 0 {
			rendered.WriteString("files so far: " + strings.Join(files, ", ") + "\n")
		}
		fmt.Fprintf(&rendered, "spend so far: $%.2f\n", spend)
		return strings.TrimSpace(rendered.String()), nil
	}

	// Settled. The whole result, with no clip: this is the read that replaces
	// the 4KB peephole the head could not compose a real answer through.
	body := strings.TrimSpace(h.jobResult(node))
	if body == "" {
		rendered.WriteString("result: nothing recorded — this job settled without saying anything.\n")
	} else {
		rendered.WriteString("result:\n" + body + "\n")
	}
	if files := h.lensFiles(node); len(files) > 0 {
		rendered.WriteString("files: " + strings.Join(files, ", ") + "\n")
	}
	fmt.Fprintf(&rendered, "spend: $%.2f\n", spend)
	if children := h.resultChildren(node.ID); len(children) > 0 {
		rendered.WriteString("how its parts ended:\n" + strings.Join(children, "\n") + "\n")
	}
	return strings.TrimSpace(rendered.String()), nil
}

// lensLive reads liveness off the whole job rather than off its root row, for
// the reason boardRowMatches gives: a planned job's root usually carries no
// status of its own worth reading, and the work is in its parts.
func (h *Head) lensLive(node store.Node) bool {
	if classOpen(node.Status) {
		return true
	}
	nodes, err := h.store.SubtreeNodes(node.ID)
	if err != nil {
		return false
	}
	for _, member := range nodes {
		if classOpen(member.Status) {
			return true
		}
	}
	return false
}

// lensProgressFeed is the running job's own voice, and it reads the SUBTREE for
// the reason the task room does: a job root usually says nothing at all while
// its workers do the talking, so a feed filtered to the root draws a job with
// three people working on it as a job with nothing happening in it.
func (h *Head) lensProgressFeed(root string, tail int) []string {
	nodes, err := h.store.SubtreeNodes(root)
	if err != nil {
		return nil
	}
	merged := make([]store.Message, 0, lensNodeMessagePage)
	for _, node := range nodes {
		messages, readErr := h.store.NodeMessages(node.ID, 0, lensNodeMessagePage)
		if readErr != nil {
			continue
		}
		merged = append(merged, messages...)
	}
	sort.SliceStable(merged, func(first, second int) bool { return merged[first].Seq < merged[second].Seq })
	if tail > 0 && len(merged) > tail {
		merged = merged[len(merged)-tail:]
	}
	lines := make([]string, 0, len(merged))
	for _, message := range merged {
		body := strings.TrimSpace(message.Body)
		if body == "" && message.Progress != nil {
			body = fmt.Sprintf("%s %d/%d %s", message.Progress.Phase,
				message.Progress.Done, message.Progress.Total, message.Progress.Latest)
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | %s",
			message.Time.Local().Format(nowLineLayout), message.NodeID, message.Role, lensSnippet(body)))
	}
	return lines
}

// lensFiles is everything a job and its parts have recorded, live or settled.
// It is the whole recorded set rather than the set minus what the result quotes
// — this read is about the thing itself, and a path stated twice costs a line
// while a path missing costs the answer.
func (h *Head) lensFiles(node store.Node) []string {
	nodes, err := h.store.SubtreeNodes(node.ID)
	if err != nil {
		nodes = []store.Node{node}
	}
	seen := make(map[string]bool, beltArtifactCap)
	files := make([]string, 0, beltArtifactCap)
	for _, member := range nodes {
		for _, path := range collectResultFiles(member, 0) {
			if seen[path] || len(files) == beltArtifactCap {
				continue
			}
			seen[path] = true
			files = append(files, path)
		}
	}
	return files
}

// ---------------------------------------------------------------------------
// open: files
// ---------------------------------------------------------------------------

// lensJobFile opens one file a named job recorded. Every byte of the boundary
// is artifact.go's: the argument only ever CHOOSES among paths the graph
// already recorded, and the chosen path must still resolve inside the directory
// it was recorded in. What is new is only that the read is paged rather than
// elided, so the middle of a long document is reachable instead of lost.
func (h *Head) lensJobFile(node store.Node, name string, part int) (string, error) {
	paths := h.artifactSet(node)
	if len(paths) == 0 {
		return "", fmt.Errorf("%s recorded no files — what it came back with is all there is",
			surgeryTargetLabel(node))
	}
	picked, chosen := artifactPick(paths, name)
	if !chosen {
		if strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("%s wrote more than one file — name one of: %s",
				surgeryTargetLabel(node), strings.Join(paths, ", "))
		}
		return "", fmt.Errorf("%q is not a file that job wrote; it wrote: %s",
			name, strings.Join(paths, ", "))
	}
	return lensFilePage(picked, name, part)
}

// lensAnyFile is open with no job named. It tries the files this conversation
// wrote first — a head repairing its own document is the commonest case and the
// one with the smallest recorded set — and then sweeps the addressable graph
// for a job that recorded that name. Two jobs with the same file name is the one
// genuine ambiguity, and it comes back as a question rather than as a guess.
//
// The third return says whether the id was recognised as a file at all, so the
// caller can fall through to its own not-found sentence rather than replacing it
// with a file-shaped one.
func (h *Head) lensAnyFile(name string, part int) (string, error, bool) {
	if written := h.writtenArtifacts(); len(written) > 0 {
		sort.Strings(written)
		if picked, chosen := artifactPick(written, name); chosen {
			rendered, err := lensFilePage(picked, name, part)
			return rendered, err, true
		}
	}
	nodes, err := h.store.AddressableNodes()
	if err != nil {
		return "", nil, false
	}
	owners := make([]store.Node, 0, 2)
	picks := make([]string, 0, 2)
	scanned := 0
	for _, node := range nodes {
		if scanned == lensFileCandidateNodes {
			break
		}
		if node.ID == store.RootID || !beltAddressable(node) {
			continue
		}
		scanned++
		paths := collectResultFiles(node, 0)
		if len(paths) == 0 {
			continue
		}
		picked, chosen := artifactPick(paths, name)
		if !chosen {
			continue
		}
		owners = append(owners, node)
		picks = append(picks, picked)
	}
	switch len(picks) {
	case 0:
		return "", nil, false
	case 1:
		rendered, fileErr := lensFilePage(picks[0], name, part)
		return rendered, fileErr, true
	}
	names := make([]string, 0, len(owners))
	for _, owner := range owners {
		names = append(names, owner.ID+" ("+surgeryTargetLabel(owner)+")")
	}
	return "", fmt.Errorf("more than one job recorded a file called %q — say which with open(%q, job:<id>): %s",
		name, name, strings.Join(names, ", ")), true
}

// lensFilePage reads one page of one recorded file, straight off the disk at
// the page's own offset. Paging rather than buffering is not an optimisation: a
// read that had to hold the whole file to page it would have a size past which
// it silently stopped being total, and totality is the entire point.
func lensFilePage(picked, spoken string, part int) (string, error) {
	real, err := artifactRealPath(picked)
	if err != nil {
		return "", err
	}
	file, err := os.Open(real)
	if err != nil {
		return "", fmt.Errorf("%s is recorded but could not be opened: %w", picked, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", picked, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a document", picked)
	}
	size := info.Size()
	header := picked + " (" + artifactSize(size) + ")"
	if size == 0 {
		return header + "\nthe file is empty", nil
	}
	pages := int((size + lensPageBytes - 1) / lensPageBytes)
	if part > pages {
		return "", fmt.Errorf("%s has %d %s; there is no part %d",
			picked, pages, pluralWord(pages, "part", "parts"), part)
	}
	offset := int64(part-1) * lensPageBytes
	length := lensPageBytes
	if remaining := size - offset; remaining < int64(length) {
		length = int(remaining)
	}
	window := make([]byte, length)
	if _, err := file.ReadAt(window, offset); err != nil {
		return "", fmt.Errorf("%s could not be read: %w", picked, err)
	}
	// Both ends of a page can land mid-rune, and only the ends that have a
	// neighbouring page can: page one opens at byte zero and the last page ends
	// at the file's end, so trimming those would eat real characters.
	body := string(window)
	if part > 1 {
		body = artifactWholeRunes([]byte(body), true)
	}
	if part < pages {
		body = artifactWholeRunes([]byte(body), false)
	}
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		spoken = picked
	}
	return header + "\n" + body + lensPageFooter(spoken, part, pages), nil
}

// ---------------------------------------------------------------------------
// open: the numbered and named records
// ---------------------------------------------------------------------------

func lensMessageSeq(id string) (int64, bool) {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "msg-")
	if trimmed == strings.ToLower(strings.TrimSpace(id)) {
		return 0, false
	}
	seq, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || seq <= 0 {
		return 0, false
	}
	return seq, true
}

// lensFactSeq takes the three spellings a notebook number arrives in: the "#12"
// the notebook itself prints beside every line, the "fact-12" a recall id could
// have been given, and the bare number a person types.
func lensFactSeq(id string) (int64, bool) {
	trimmed := strings.TrimSpace(id)
	trimmed = strings.TrimPrefix(trimmed, "#")
	trimmed = strings.TrimPrefix(strings.ToLower(trimmed), "fact-")
	seq, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || seq <= 0 {
		return 0, false
	}
	return seq, true
}

func (h *Head) lensMessage(seq int64) (string, error) {
	messages, err := h.store.Messages("", seq-1, 1)
	if err != nil {
		return "", fmt.Errorf("that message could not be read: %w", err)
	}
	if len(messages) == 0 || messages[0].Seq != seq {
		return "", fmt.Errorf("there is no message numbered %d", seq)
	}
	message := messages[0]
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | %s | %s | %s\n", lensMessageID(message.Seq),
		message.Time.Local().Format(nowLineLayout), message.Role, message.SessionID)
	if node := strings.TrimSpace(message.NodeID); node != "" {
		rendered.WriteString("about: " + node + "\n")
	}
	rendered.WriteString(strings.TrimSpace(message.Body))
	return strings.TrimSpace(rendered.String()), nil
}

func (h *Head) lensFact(seq int64) (string, error) {
	fact, found, err := h.store.Fact(seq)
	if err != nil {
		return "", fmt.Errorf("that notebook line could not be read: %w", err)
	}
	if !found {
		return "", fmt.Errorf("there is no notebook line numbered %d", seq)
	}
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | %s | %s | %s\n", lensFactID(fact.Seq), fact.Scope, fact.Kind, fact.Status)
	if !fact.Time.IsZero() {
		rendered.WriteString("written: " + fact.Time.Local().Format(nowLineLayout) + "\n")
	}
	if node := strings.TrimSpace(fact.NodeID); node != "" && node != store.RootID {
		rendered.WriteString("learned from: " + node + "\n")
	}
	rendered.WriteString(strings.TrimSpace(fact.Body) + "\n")
	if note := strings.TrimSpace(fact.StatusNote); note != "" {
		rendered.WriteString("status note: " + note + "\n")
	}
	if artifact := strings.TrimSpace(fact.Artifact); artifact != "" {
		rendered.WriteString("artifact: " + artifact + "\n")
	}
	return strings.TrimSpace(rendered.String()), nil
}

// lensCharterRecord is a standing rule whole: what it watches for, how often it
// looks, what it does when it fires, and — the half that is usually the actual
// question — whether it has been looking at all.
func lensCharterRecord(charter store.Charter) string {
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | standing rule | %s | %s\n", charter.ID, charter.Status, charter.Autonomy)
	rendered.WriteString("watches for: " + strings.TrimSpace(charter.Invariant) + "\n")
	rendered.WriteString("how often: " + lensWatchPhrase(charter.Watch) + "\n")
	if action := strings.TrimSpace(charter.Action.Template); action != "" {
		word := "does"
		if charter.Action.SayOnly {
			word = "says"
		}
		rendered.WriteString(word + ": " + action + "\n")
	}
	if hint := strings.TrimSpace(charter.SentinelHint); hint != "" {
		rendered.WriteString("what to look at: " + hint + "\n")
	}
	if !charter.CreatedAt.IsZero() {
		rendered.WriteString("agreed: " + charter.CreatedAt.Local().Format(nowLineLayout) + "\n")
	}
	if !charter.LastChecked.IsZero() {
		rendered.WriteString("last looked: " + charter.LastChecked.Local().Format(nowLineLayout) + "\n")
	}
	if line := strings.TrimSpace(charter.LastCheckLine); line != "" {
		rendered.WriteString("and found: " + line + "\n")
	}
	if !charter.LastWake.IsZero() {
		rendered.WriteString("last fired: " + charter.LastWake.Local().Format(nowLineLayout) + "\n")
	}
	if !charter.NextDue.IsZero() {
		rendered.WriteString("next check: " + charter.NextDue.Local().Format(nowLineLayout) + "\n")
	}
	fmt.Fprintf(&rendered, "fired cleanly %d %s, stood down %d %s\n",
		charter.GreenFirings, pluralWord(charter.GreenFirings, "time", "times"),
		charter.Demotions, pluralWord(charter.Demotions, "time", "times"))
	return strings.TrimSpace(rendered.String())
}

// lensWatchPhrase says a watch's rhythm in words. A guessed cadence is marked
// as a guess, because a rule that says "about every 2 minutes" in the same
// voice a person's own "every Monday" is said in is a rule nobody can audit.
func lensWatchPhrase(watch store.WatchSpec) string {
	phrase := string(watch.Kind)
	if cadence := strings.TrimSpace(watch.Cadence); cadence != "" {
		phrase += " " + cadence
		if watch.CadenceGuessed {
			phrase += " (a guess — nobody said a rhythm)"
		}
	}
	return strings.TrimSpace(phrase)
}

func lensServiceRecord(service store.Service) string {
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | service | %s\n", service.Name, service.Status)
	rendered.WriteString("runs: " + strings.TrimSpace(service.Command) + "\n")
	if dir := strings.TrimSpace(service.Dir); dir != "" {
		rendered.WriteString("in: " + dir + "\n")
	}
	if service.PID > 0 {
		fmt.Fprintf(&rendered, "process: %d\n", service.PID)
	}
	if !service.StartedAt.IsZero() {
		rendered.WriteString("started: " + service.StartedAt.Local().Format(nowLineLayout) + "\n")
	}
	restart := "off"
	if service.AutoRestart {
		restart = "on"
	}
	fmt.Fprintf(&rendered, "auto-restart: %s, restarted %d %s\n",
		restart, service.RestartCount, pluralWord(service.RestartCount, "time", "times"))
	if log := strings.TrimSpace(service.LogPath); log != "" {
		rendered.WriteString("log: " + log + "\n")
	}
	return strings.TrimSpace(rendered.String())
}

// ---------------------------------------------------------------------------
// open: raw
// ---------------------------------------------------------------------------

// lensRawNode is the all-details mode: the journal rows underneath one job,
// unredacted. It is what the raw policy leaves reachable on explicit ask (Part
// 2.6) — the composed answer is what the person gets, and this is what backs it
// when they want to see the machine's own record instead of a reading of it.
//
// The events table is not indexed by node, so the scan is bounded and says so
// when it hits the bound. An answer that quietly covered only the recent
// journal would be the exact failure this read exists to prevent.
func (h *Head) lensRawNode(node store.Node) (string, error) {
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "raw %s | %s | %s\n", node.ID, node.Status, surgeryTargetLabel(node))

	members := map[string]bool{node.ID: true}
	if nodes, err := h.store.SubtreeNodes(node.ID); err == nil {
		for _, member := range nodes {
			members[member.ID] = true
		}
	}

	events, from, err := h.lensNodeEvents(members, lensRawEvents)
	if err != nil {
		return "", fmt.Errorf("the journal could not be read: %w", err)
	}
	if from > 0 {
		fmt.Fprintf(&rendered, "journal scanned back to seq %d; anything older than that is not in this read\n", from)
	}
	if len(events) == 0 {
		rendered.WriteString("events: none in the scanned window\n")
	} else {
		rendered.WriteString("events, oldest first:\n")
		for _, event := range events {
			fmt.Fprintf(&rendered, "- %d | %s | %s | %s | %s\n", event.Seq,
				event.Time.Local().Format(nowLineLayout), event.NodeID, event.Kind,
				strings.TrimSpace(string(event.Payload)))
		}
	}

	messages := make([]store.Message, 0, lensRawMessages)
	for member := range members {
		anchored, readErr := h.store.NodeMessages(member, 0, lensNodeMessagePage)
		if readErr != nil {
			continue
		}
		messages = append(messages, anchored...)
	}
	sort.SliceStable(messages, func(first, second int) bool { return messages[first].Seq < messages[second].Seq })
	if len(messages) > lensRawMessages {
		messages = messages[len(messages)-lensRawMessages:]
	}
	if len(messages) == 0 {
		rendered.WriteString("messages anchored to it: none\n")
	} else {
		rendered.WriteString("messages anchored to it, oldest first:\n")
		for _, message := range messages {
			fmt.Fprintf(&rendered, "- %d | %s | %s | %s | %s\n", message.Seq,
				message.Time.Local().Format(nowLineLayout), message.NodeID, message.Role,
				strings.TrimSpace(message.Body))
		}
	}
	return strings.TrimSpace(rendered.String()), nil
}

// lensNodeEvents walks one bounded window of the journal and keeps the rows
// belonging to the ids asked for. It returns the seq the window opened at so
// the caller can say what it did not look at.
func (h *Head) lensNodeEvents(members map[string]bool, limit int) ([]store.Event, int64, error) {
	latest, err := h.store.LatestEventSeq()
	if err != nil {
		return nil, 0, err
	}
	from := latest - lensRawScanSeqs
	if from < 0 {
		from = 0
	}
	events, err := h.store.EventsThrough(from, latest)
	if err != nil {
		return nil, 0, err
	}
	kept := make([]store.Event, 0, limit)
	for _, event := range events {
		if !members[event.NodeID] {
			continue
		}
		kept = append(kept, event)
	}
	if limit > 0 && len(kept) > limit {
		kept = kept[len(kept)-limit:]
	}
	return kept, from, nil
}

// ---------------------------------------------------------------------------
// paging
// ---------------------------------------------------------------------------

// lensPage cuts an assembled body into transport pages. It prefers a line
// boundary when there is one near the end of the page, because a page that
// stops mid-sentence reads as damage while a page that stops at a line reads as
// a page.
func lensPage(id, body string, part int) string {
	pages := lensPageBounds(body)
	if len(pages) <= 1 {
		return body
	}
	if part > len(pages) {
		return fmt.Sprintf("%s has %d %s; there is no part %d — open(%q, part:1) starts again at the beginning",
			id, len(pages), pluralWord(len(pages), "part", "parts"), part, id)
	}
	return pages[part-1] + lensPageFooter(id, part, len(pages))
}

// lensPageBounds is the split itself, done once so the part count and the part
// contents can never disagree about how many there are.
func lensPageBounds(body string) []string {
	if len(body) <= lensPageBytes {
		return []string{body}
	}
	pages := make([]string, 0, len(body)/lensPageBytes+1)
	for len(body) > lensPageBytes {
		cut := lensPageBytes
		// Back off to a line boundary, but only inside the last quarter of the
		// page: further back than that and the page loses more than the ragged
		// edge cost it.
		if newline := strings.LastIndexByte(body[:cut], '\n'); newline > cut-lensPageBytes/4 {
			cut = newline + 1
		}
		for cut > 0 && !utf8.ValidString(body[:cut]) {
			cut--
		}
		if cut == 0 {
			cut = lensPageBytes
		}
		pages = append(pages, body[:cut])
		body = body[cut:]
	}
	if body != "" {
		pages = append(pages, body)
	}
	return pages
}

// lensPageFooter is the sentence that keeps a paged read honest. It names the
// exact next call, because a loop that is told there is more and not how to ask
// for it will answer from the part it has.
func lensPageFooter(id string, part, pages int) string {
	if pages <= 1 {
		return ""
	}
	if part < pages {
		return fmt.Sprintf("\n\npart %d of %d — open(%q, part:%d) for the next", part, pages, id, part+1)
	}
	return fmt.Sprintf("\n\npart %d of %d — that is the end of it", part, pages)
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

// status is the whole system on one page.
//
// It exists because the answer to "how are things?" was split across three
// tools and a pane nobody in the conversation could see, so the honest answer
// cost three calls and still left out the services and the health. Every
// section here is read from something that was already journaled and already
// rendered somewhere else; what is new is that they arrive together, which is
// the only form in which they answer the question that was asked.
//
// A dependency this surface never registered is reported as absent rather than
// omitted. A chat with no competence measurement is a fact about the chat, and
// one the model may say out loud — silence there would let it infer that
// nothing has been measured, which is a different and untrue thing.
func (run *beltRun) status() (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "status is not available on this surface.", false
	}
	return run.head.lensStatus(run.user.SessionID), false
}

func (h *Head) lensStatus(sessionID string) string {
	now := time.Now()
	var rendered strings.Builder
	rendered.WriteString(nowLine(now) + "\n\n")

	rendered.WriteString("WORK\n" + h.lensWorkCounts(sessionID, now) + "\n\n")
	rendered.WriteString("MONEY\n" + h.lensMoney() + "\n\n")
	rendered.WriteString("WATCH\n" + h.lensWatch() + "\n\n")
	rendered.WriteString("SERVICES\n" + h.lensServices() + "\n\n")
	rendered.WriteString("WAYS OF WORKING\n" + h.lensWays() + "\n\n")
	rendered.WriteString("COMPETENCE\n" + h.lensCompetence() + "\n\n")
	rendered.WriteString("HEALTH\n" + h.lensHealth() + "\n")
	return strings.TrimSpace(rendered.String())
}

// lensWorkCounts counts over the addressable graph rather than over a board
// read, and the difference matters: the board is capped at a dozen rows because
// a board is read to choose a target, while a count that stopped at twelve
// would be a wrong number rather than a short list.
func (h *Head) lensWorkCounts(sessionID string, now time.Time) string {
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return "the board could not be read: " + err.Error()
	}
	running, queued, failed, jobs := 0, 0, 0, 0
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	for _, node := range nodes {
		if node.ID == store.RootID || node.Folded || !beltAddressable(node) {
			continue
		}
		switch node.Status {
		case store.Running, store.Claimed:
			running++
		case store.Pending:
			queued++
		case store.Failed:
			failed++
		default:
			continue
		}
		if boardJobRoot(node, byID) {
			jobs++
		}
	}
	line := fmt.Sprintf("%d running, %d queued, %d failed | %d %s of work open",
		running, queued, failed, jobs, pluralWord(jobs, "piece", "pieces"))
	if running+queued+failed == 0 {
		line = "nothing is running, queued or failed — the board is clear"
	}
	// The top of the board itself, because a count with no names in it cannot be
	// spoken about, and this is the one place both belong on the same page.
	rows, err := h.boardRowsAt(sessionID, "", "all", "", now)
	if err != nil || len(rows) == 0 {
		return line
	}
	if len(rows) > lensStatusListCap {
		rows = rows[:lensStatusListCap]
	}
	return line + "\n" + renderBoard(rows)
}

func (h *Head) lensMoney() string {
	var rendered strings.Builder
	if h.dailyRailSet {
		if rail, err := h.store.DailyRailToday(h.dailyBudgetUSD); err == nil {
			if rail.Unlimited {
				fmt.Fprintf(&rendered, "today: $%.2f spent; daily rail unlimited\n", rail.Spend)
			} else {
				fmt.Fprintf(&rendered, "today: $%.2f spent of a $%.2f daily rail", rail.Spend, rail.Ceiling)
				if rail.Reached {
					rendered.WriteString(" — the rail is reached")
				}
				rendered.WriteString("\n")
			}
		}
	} else {
		// No rail was configured on this surface, so today's total is still true
		// and the ceiling is simply not a fact here.
		if spend, err := h.store.SpendToday(); err == nil {
			fmt.Fprintf(&rendered, "today: $%.2f spent; no daily rail is configured on this surface\n", spend)
		}
	}
	if self, err := h.store.SelfSpendToday(); err == nil {
		fmt.Fprintf(&rendered, "your own upkeep today: $%.2f\n", self)
	}
	if rendered.Len() == 0 {
		return "no spend has been recorded."
	}
	return strings.TrimSpace(rendered.String())
}

func (h *Head) lensWatch() string {
	var rendered strings.Builder
	if h.standingWatch == nil {
		rendered.WriteString("standing-watch status is not wired into this surface.\n")
	} else if status := strings.TrimSpace(h.standingWatch()); status == "" {
		rendered.WriteString("nothing is on watch and no standing check is arranged.\n")
	} else {
		rendered.WriteString(status + "\n")
	}
	// The next checks come off the charters directly, because "what happens
	// overnight" is a question about times and the watch block is a question
	// about whether anything is watching at all.
	charters, err := h.store.ActiveCharters()
	if err != nil || len(charters) == 0 {
		return strings.TrimSpace(rendered.String())
	}
	sort.SliceStable(charters, func(first, second int) bool {
		return charters[first].NextDue.Before(charters[second].NextDue)
	})
	if len(charters) > lensStatusListCap {
		charters = charters[:lensStatusListCap]
	}
	rendered.WriteString("standing rules and their next checks:\n")
	for _, charter := range charters {
		next := "no next check arranged"
		if !charter.NextDue.IsZero() {
			next = "next " + charter.NextDue.Local().Format(nowLineLayout)
		}
		fmt.Fprintf(&rendered, "- %s | %s | %s | %s\n", charter.ID,
			lensSnippet(charter.Invariant), lensWatchPhrase(charter.Watch), next)
	}
	return strings.TrimSpace(rendered.String())
}

func (h *Head) lensServices() string {
	services, err := h.store.ActiveServices()
	if err != nil {
		return "the services could not be read: " + err.Error()
	}
	if len(services) == 0 {
		return "nothing is running as a service."
	}
	lines := make([]string, 0, len(services))
	for index, service := range services {
		if index == lensStatusListCap {
			lines = append(lines, fmt.Sprintf("- and %d more — open one by name for its whole record",
				len(services)-lensStatusListCap))
			break
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", service.Name, service.Status,
			lensSnippet(service.Command)))
	}
	return strings.Join(lines, "\n")
}

// lensWays is the learned-workflow shelf, and it is honestly absent. The craft
// repository is a git repo on disk rather than a table in this store, and no
// wiring hands it to the head — the craft TOOL passes a name straight through to
// the executor for exactly that reason. Head.WithCraftShelf(func() int, error)
// is the one line that would make this a number.
func (h *Head) lensWays() string {
	return "the shelf of learned ways of working is not reachable from this surface — the craft repository lives on disk beside the journal, and nothing hands it to the conversation. Naming one still works; counting them does not."
}

func (h *Head) lensCompetence() string {
	if h.competence == nil {
		return "no competence measurement is wired into this surface."
	}
	measured := strings.TrimSpace(h.competence())
	if measured == "" {
		return "nothing has been measured yet — not enough work has settled to say where you are strong or weak."
	}
	return measured
}

// lensHealth is what the process can cheaply say about its own liveness. The
// resident lease is the fact a person actually means by "is it alive?", and it
// is NOT here: lease.ProbeResident is keyed by the journal's path on disk and
// the head is handed an open handle rather than a path.
// Head.WithResidentProbe(func() (string, error)) is the one line that would put
// it on this page.
func (h *Head) lensHealth() string {
	var rendered strings.Builder
	rendered.WriteString("the resident's lease is not reachable from this surface — it is keyed by the journal's path on disk and the conversation is handed an open journal instead.\n")
	if seq, err := h.store.LatestEventSeq(); err == nil {
		fmt.Fprintf(&rendered, "journal: %d events recorded\n", seq)
	}
	if questions := strings.TrimSpace(h.renderOpenQuestions("")); questions != "" {
		rendered.WriteString("questions waiting on the person:\n" + questions + "\n")
	} else {
		rendered.WriteString("nothing is waiting on an answer from the person.\n")
	}
	return strings.TrimSpace(rendered.String())
}
