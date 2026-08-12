package head

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The two holes Part 0.4 names, and the ceiling that cut every answer that got
// through: nothing served a running step's own progress to the conversation,
// nothing said what the system as a whole was doing, and every read that did
// work stopped at four kilobytes without a word. These are the assertions that
// each of the three is closed.

// lensRun is one lens call the way the loop makes it.
func lensRun(t *testing.T, graph *store.Store, session string) *beltRun {
	t.Helper()
	return &beltRun{head: New(&beltClient{}, graph), user: postUser(t, graph, session, "how are things?")}
}

// seedRecallMemory is one of each kind of thing memory is made of, all about
// the same subject, so a blended read has something to blend.
func seedRecallMemory(t *testing.T, graph *store.Store) store.Fact {
	t.Helper()

	// Something said and never turned into work — the half of memory that had
	// no index at all until messages_fts.
	if _, err := graph.PostMessage(store.Message{SessionID: "lens", Role: store.RoleUser,
		Body: "we decided the pricing model should stay per-seat until the enterprise tier lands"}); err != nil {
		t.Fatal(err)
	}
	// Work that finished, with a real finding in it.
	spliceSurgeryJob(t, graph, "pricing-study", "Pricing study", "study the pricing model")
	completeNodeWith(t, graph, "pricing-study",
		"Per-seat pricing beats usage-based at every cohort size we measured; the crossover is above 4,000 seats.")
	// Work still queued, so the job kind has something that is not a result.
	spliceSurgeryJob(t, graph, "pricing-deck", "Pricing deck", "build the pricing deck")
	// A belief.
	belief, err := graph.RecordFactFrom(store.FactWriterHead, store.RootID, "user",
		store.FactPreference, "pricing numbers are always quoted excluding VAT")
	if err != nil {
		t.Fatal(err)
	}
	// A standing rule.
	charter, charterErr := store.NewCharter("pricing-watch", "Pricing pages never disagree with the pricing model.",
		store.WatchSpec{Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "check the pricing page", Cadence: time.Hour}},
		"Did the pricing page change?", store.CharterAction{Template: "Reconcile the pricing page"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "lens", Evidence: "yes"})
	if charterErr != nil {
		t.Fatal(charterErr)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	// A service.
	headServiceFixture(t, graph, "svc-pricing", "pricing-api", "pricing-leaf", 71)
	return belief
}

// One question, every memory. The five FTS surfaces and the settled rows were
// reachable through three tools that each knew about some of them, so the
// answer depended on which tool the model happened to pick.
func TestRecallBlendsEveryKindOfMemoryInOneRead(t *testing.T) {
	graph := openHeadStore(t)
	seedRecallMemory(t, graph)
	run := lensRun(t, graph, "lens")

	read, failed := run.execute(beltToolRecall, mustJSON(map[string]any{"q": "pricing"}))
	if failed {
		t.Fatalf("recall failed: %s", read)
	}
	for kind, why := range map[string]string{
		lensKindMessage: "what was merely said",
		lensKindResult:  "what finished work concluded",
		lensKindJob:     "work that has not concluded anything yet",
		lensKindBelief:  "the notebook",
		lensKindRule:    "the standing rules",
		lensKindService: "the services",
	} {
		if !strings.Contains(read, "- "+kind+" | ") {
			t.Fatalf("recall never reached %s (kind %q):\n%s", why, kind, read)
		}
	}
	// Real content, off the thing itself. A hit whose snippet was a summary
	// teaches the loop to trust a sentence nobody wrote.
	if !strings.Contains(read, "per-seat until the enterprise tier lands") {
		t.Fatalf("the message hit carried no actual bytes:\n%s", read)
	}
	if !strings.Contains(read, "the crossover is above 4,000 seats") {
		t.Fatalf("the result hit carried no actual finding:\n%s", read)
	}
	// Every hit carries an id, and the ids are the ones open takes.
	if !strings.Contains(read, "| pricing-study |") || !strings.Contains(read, "| pricing-watch |") {
		t.Fatalf("recall handed back hits with no openable id:\n%s", read)
	}
	// Nothing was clipped, so nothing claims it was.
	if strings.Contains(read, "more via recall") {
		t.Fatalf("an unclipped recall advertised a clip:\n%s", read)
	}
}

// The kind filter is the person's vocabulary rather than the store's, and it
// must actually exclude — a filter that only reorders is a filter that lies.
func TestRecallRespectsItsFilters(t *testing.T) {
	graph := openHeadStore(t)
	seedRecallMemory(t, graph)
	run := lensRun(t, graph, "lens")

	only, failed := run.execute(beltToolRecall, mustJSON(map[string]any{"q": "pricing", "kind": lensKindBelief}))
	if failed {
		t.Fatalf("filtered recall failed: %s", only)
	}
	if !strings.Contains(only, "excluding VAT") {
		t.Fatalf("the belief filter lost the belief:\n%s", only)
	}
	for _, excluded := range []string{"- " + lensKindMessage + " |", "- " + lensKindResult + " |", "- " + lensKindService + " |"} {
		if strings.Contains(only, excluded) {
			t.Fatalf("kind=belief still returned %q:\n%s", excluded, only)
		}
	}

	// A window that ends before anything happened is an empty window, and the
	// honest answer to it is that it is empty.
	empty, failed := run.execute(beltToolRecall, mustJSON(map[string]any{
		"q": "pricing", "since": "2001-01-01", "until": "2001-01-02"}))
	if failed {
		t.Fatalf("windowed recall failed: %s", empty)
	}
	if !strings.Contains(empty, "nothing remembered matches those words") {
		t.Fatalf("a window with nothing in it invented something:\n%s", empty)
	}

	// A backwards window is a mistake in the call rather than an empty answer.
	if backwards, failed := run.execute(beltToolRecall, mustJSON(map[string]any{
		"q": "pricing", "since": "2026-08-10", "until": "2026-08-01"})); !failed ||
		!strings.Contains(backwards, "until is before since") {
		t.Fatalf("a backwards window was accepted: %q failed=%t", backwards, failed)
	}
	// An unknown kind is named back with the ones that exist.
	if bad, failed := run.execute(beltToolRecall, mustJSON(map[string]any{
		"q": "pricing", "kind": "invoice"})); !failed || !strings.Contains(bad, "message, job, result") {
		t.Fatalf("an unknown kind was accepted: %q failed=%t", bad, failed)
	}
	// And q is the one thing it cannot do without.
	if bare, failed := run.execute(beltToolRecall, mustJSON(map[string]any{})); !failed ||
		!strings.Contains(bare, "q must say what to look for") {
		t.Fatalf("recall ran with nothing to look for: %q failed=%t", bare, failed)
	}
}

// A recall with more than a page of hits says so, says how many it dropped, and
// says the call that gets at them. Silence there is the failure the whole read
// exists to prevent.
func TestRecallSaysWhenItClippedAndHowToGetTheRest(t *testing.T) {
	graph := openHeadStore(t)
	for index := 0; index < lensHitCap+6; index++ {
		if _, err := graph.PostMessage(store.Message{SessionID: "lens", Role: store.RoleUser,
			Body: "another line about the migration, number " + string(rune('a'+index))}); err != nil {
			t.Fatal(err)
		}
	}
	run := lensRun(t, graph, "lens")
	read, failed := run.execute(beltToolRecall, mustJSON(map[string]any{"q": "migration"}))
	if failed {
		t.Fatalf("recall failed: %s", read)
	}
	if !strings.Contains(read, "more via recall(q, kind:") {
		t.Fatalf("a clipped recall never said it was clipped:\n%s", read)
	}
	if !strings.Contains(read, "further hits not shown") {
		t.Fatalf("a clipped recall never said how much it dropped:\n%s", read)
	}
	if lines := strings.Count(read, "\n- "); lines > lensHitCap {
		t.Fatalf("recall rendered %d hits past its own cap of %d", lines, lensHitCap)
	}
}

// Opening running work is the read that never existed. The head could see that
// three steps were running and never what any of them was doing — only the TUI
// saw the feed — so "how is it going?" was answered with a pair of counts.
func TestOpenOnLiveWorkShowsTheePlanTheFeedAndTheSpend(t *testing.T) {
	graph := openHeadStore(t)
	seedPlannedMarket(t, graph)
	// The workers talking, which is what a live job's own progress IS: messages
	// anchored to the nodes doing the work, never to the root.
	for _, said := range []string{
		"Reading the third filing now — the vendor concentration is higher than expected.",
		"Two vendor interviews booked for tomorrow morning.",
	} {
		if _, err := graph.PostMessage(store.Message{SessionID: "market", Role: store.RoleSystem,
			Body: said, NodeID: "market-n2"}); err != nil {
			t.Fatal(err)
		}
	}
	run := lensRun(t, graph, "market")

	read, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "market"}))
	if failed {
		t.Fatalf("open failed: %s", read)
	}
	// The plan, with per-step state read off the durable rows.
	if !strings.Contains(read, "- Read the filings | done") ||
		!strings.Contains(read, "- Interview the vendors | working") ||
		!strings.Contains(read, "Write it up | waiting | after Read the filings, Interview the vendors") {
		t.Fatalf("open on live work carried no per-step plan state:\n%s", read)
	}
	// The feed the TUI has always had and the head never did.
	if !strings.Contains(read, "progress, most recent last:") {
		t.Fatalf("open on live work carried no progress feed:\n%s", read)
	}
	if !strings.Contains(read, "vendor concentration is higher than expected") ||
		!strings.Contains(read, "Two vendor interviews booked") {
		t.Fatalf("the progress feed missed what the workers actually said:\n%s", read)
	}
	if !strings.Contains(read, "spend so far: $") {
		t.Fatalf("open on live work never said what it has cost:\n%s", read)
	}
	// A live job is not reported as if it had concluded.
	if strings.Contains(read, "how its parts ended") {
		t.Fatalf("live work was rendered as settled work:\n%s", read)
	}
}

// The settled branch is the same read from the other side: the whole result,
// with none of the 4KB clip the belt's result tool keeps.
func TestOpenOnSettledWorkReturnsTheWholeResultWithNoClip(t *testing.T) {
	graph := openHeadStore(t)
	long := strings.Repeat("A finding sentence that says something real about the ledger. ", 200)
	spliceSurgeryJob(t, graph, "ledger", "Ledger close", "close the ledger")
	completeNodeWith(t, graph, "ledger", long+"\nThe verdict is on the last line: the ledger balances.")
	run := lensRun(t, graph, "ledger")

	// The old read stops at its own budget, and keeps it.
	clipped, failed := run.execute(beltToolResult, mustJSON(map[string]any{"id": "ledger"}))
	if failed {
		t.Fatalf("result failed: %s", clipped)
	}
	if strings.Contains(clipped, "the ledger balances") {
		t.Fatalf("the old result read stopped clipping — this wave must not have touched it:\n%s", clipped[:200])
	}

	// The new one pages, and the last page carries the verdict.
	first, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "ledger"}))
	if failed {
		t.Fatalf("open failed: %s", first)
	}
	if !strings.Contains(first, "part 1 of ") {
		t.Fatalf("a paged open never said which part it was:\n%s", first[len(first)-400:])
	}
	if !strings.Contains(first, `open("ledger", part:2) for the next`) {
		t.Fatalf("a paged open never said how to read on:\n%s", first[len(first)-400:])
	}
	parts := 0
	for part := 1; part < 20; part++ {
		page, pageFailed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "ledger", "part": part}))
		if pageFailed {
			t.Fatalf("part %d failed: %s", part, page)
		}
		parts++
		if strings.Contains(page, "that is the end of it") {
			if !strings.Contains(page, "the ledger balances") {
				t.Fatalf("the last page did not carry the verdict:\n%s", page)
			}
			break
		}
		if part == 19 {
			t.Fatalf("open never reached an end after %d parts", parts)
		}
	}
	if parts < 2 {
		t.Fatalf("a result four times the old ceiling came back in %d part(s)", parts)
	}
	// And a part past the end says so rather than handing back nothing.
	if beyond, _ := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "ledger", "part": 99})); !strings.Contains(beyond, "there is no part 99") {
		t.Fatalf("a part past the end came back silently: %q", beyond)
	}
}

// A file opens as its actual bytes, through artifact.go's boundary, and pages
// off the disk rather than through a buffer — so the middle of a long document
// is reachable instead of elided.
func TestOpenReadsAFileWholeAndPagesIt(t *testing.T) {
	graph := openHeadStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	body := strings.Repeat("x", lensPageBytes) + "\nTHE CONCLUSION IS HERE\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "report-job", "Report", "write the report")
	completeNodeWith(t, graph, "report-job", "Wrote it up. "+path)
	run := lensRun(t, graph, "report")

	first, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": path}))
	if failed {
		t.Fatalf("open on a recorded file failed: %s", first)
	}
	if !strings.Contains(first, "part 1 of 2") {
		t.Fatalf("a file past one page never said so:\n%s", first[len(first)-300:])
	}
	second, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": path, "part": 2}))
	if failed {
		t.Fatalf("part 2 failed: %s", second)
	}
	if !strings.Contains(second, "THE CONCLUSION IS HERE") {
		t.Fatalf("the second page did not carry the end of the file:\n%s", second)
	}
	if !strings.Contains(second, "that is the end of it") {
		t.Fatalf("the last page never said it was the last:\n%s", second)
	}
	// The boundary is unchanged: only a path the graph recorded opens.
	if invented, failed := run.execute(beltToolOpen, mustJSON(map[string]any{
		"id": "/etc/hosts", "job": "report-job"})); !failed || strings.Contains(invented, "localhost") {
		t.Fatalf("an unrecorded path was opened: %q failed=%t", invented, failed)
	}
}

// raw is the all-details mode: the journal's own rows, unredacted, for people
// who are asking what actually happened rather than what it amounts to.
func TestOpenRawHandsBackTheJournalRows(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audit", "Audit", "audit the ledger")
	completeNodeWith(t, graph, "audit", "Everything reconciles.")
	if _, err := graph.PostMessage(store.Message{SessionID: "raw", Role: store.RoleSystem,
		Body: "reconciled the third account", NodeID: "audit"}); err != nil {
		t.Fatal(err)
	}
	run := lensRun(t, graph, "raw")

	read, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "audit", "raw": true}))
	if failed {
		t.Fatalf("raw open failed: %s", read)
	}
	if !strings.Contains(read, "raw audit |") {
		t.Fatalf("raw mode did not announce itself:\n%s", read)
	}
	if !strings.Contains(read, "events, oldest first:") {
		t.Fatalf("raw mode returned no event rows:\n%s", read)
	}
	// The rows are the journal's own: a seq, a kind, and the payload as written.
	if !strings.Contains(read, string(store.EventNodeCompleted)) {
		t.Fatalf("raw mode never showed the completion event:\n%s", read)
	}
	if !strings.Contains(read, "messages anchored to it") ||
		!strings.Contains(read, "reconciled the third account") {
		t.Fatalf("raw mode dropped the node-anchored messages:\n%s", read)
	}
	// The composed read of the same job says none of that, which is the point of
	// having both.
	composed, _ := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "audit"}))
	if strings.Contains(composed, "events, oldest first:") {
		t.Fatalf("the composed read leaked the journal rows:\n%s", composed)
	}
}

// The other things a person owns open as their full record, by the ids and
// names they were shown rather than by a second naming scheme.
func TestOpenReachesRulesServicesAndNotebookLines(t *testing.T) {
	graph := openHeadStore(t)
	belief := seedRecallMemory(t, graph)
	run := lensRun(t, graph, "lens")

	rule, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "pricing-watch"}))
	if failed {
		t.Fatalf("open on a standing rule failed: %s", rule)
	}
	if !strings.Contains(rule, "watches for: Pricing pages never disagree") ||
		!strings.Contains(rule, "how often:") {
		t.Fatalf("a standing rule opened without its record:\n%s", rule)
	}

	service, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "pricing-api"}))
	if failed {
		t.Fatalf("open on a service failed: %s", service)
	}
	if !strings.Contains(service, "runs: npm run dev") || !strings.Contains(service, "auto-restart:") {
		t.Fatalf("a service opened without its record:\n%s", service)
	}

	// The three spellings a notebook number arrives in all reach the same line:
	// the "#12" the notebook prints, the bare number a person types, and the
	// "fact-12" a recall id could carry.
	for _, spelling := range []string{lensFactID(belief.Seq),
		strconv.FormatInt(belief.Seq, 10), "fact-" + strconv.FormatInt(belief.Seq, 10)} {
		opened, openFailed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": spelling}))
		if openFailed {
			t.Fatalf("open(%q) on a notebook line failed: %s", spelling, opened)
		}
		if !strings.Contains(opened, "excluding VAT") {
			t.Fatalf("open(%q) opened a notebook line without its body:\n%s", spelling, opened)
		}
	}

	// And a name nothing answers to comes back as a sentence the loop can act
	// on, naming what open does take.
	missing, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "the-blue-one"}))
	if !failed || !strings.Contains(missing, "ids come from a board or recall read") {
		t.Fatalf("an unknown id was not refused readably: %q failed=%t", missing, failed)
	}
}

// One page for the whole system. It was split across three tools and a pane
// nobody in the conversation could see.
func TestStatusRendersTheWholeSystemOnOnePage(t *testing.T) {
	graph := openHeadStore(t)
	seedRecallMemory(t, graph)
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("live", "", "Live work", "do the live work"),
		spec("live-n1", "live", "Step one", "step one"))
	startNode(t, graph, "live-n1")

	head := New(&beltClient{}, graph).
		WithCompetenceMap(func() string { return "strong on research, weak on long-running builds" }).
		WithStandingWatch(func() string { return "checks continue with no terminal open; last wake 2h ago" }).
		WithDailyBudgetUSD(5)
	run := &beltRun{head: head, user: postUser(t, graph, "lens", "how are things?")}

	read, failed := run.execute(beltToolStatus, "")
	if failed {
		t.Fatalf("status failed: %s", read)
	}
	for _, section := range []string{"WORK", "MONEY", "WATCH", "SERVICES", "WAYS OF WORKING", "COMPETENCE", "HEALTH"} {
		if !strings.Contains(read, section+"\n") {
			t.Fatalf("status is missing its %s section:\n%s", section, read)
		}
	}
	if !strings.Contains(read, "1 running,") {
		t.Fatalf("status never counted the live work:\n%s", read)
	}
	if !strings.Contains(read, "daily rail") {
		t.Fatalf("status never said what today cost against the rail:\n%s", read)
	}
	if !strings.Contains(read, "checks continue with no terminal open") {
		t.Fatalf("status never read the standing watch:\n%s", read)
	}
	if !strings.Contains(read, "pricing-watch") || !strings.Contains(read, "next check") {
		t.Fatalf("status never said when the next check is:\n%s", read)
	}
	if !strings.Contains(read, "pricing-api") {
		t.Fatalf("status never named the services:\n%s", read)
	}
	if !strings.Contains(read, "weak on long-running builds") {
		t.Fatalf("status never read the competence map:\n%s", read)
	}
}

// A dependency this surface never registered is reported as absent rather than
// omitted — silence would let the model infer that nothing has been measured,
// which is a different and untrue thing.
func TestStatusSaysWhatIsNotWiredRatherThanLeavingItOut(t *testing.T) {
	graph := openHeadStore(t)
	run := lensRun(t, graph, "bare")

	read, failed := run.execute(beltToolStatus, "")
	if failed {
		t.Fatalf("status failed on a bare surface: %s", read)
	}
	if !strings.Contains(read, "nothing is running, queued or failed") {
		t.Fatalf("an empty board was not said plainly:\n%s", read)
	}
	if !strings.Contains(read, "no daily rail is configured on this surface") {
		t.Fatalf("an unset rail was passed off as a rail:\n%s", read)
	}
	if !strings.Contains(read, "standing-watch status is not wired into this surface") {
		t.Fatalf("an unwired watch was left out rather than named:\n%s", read)
	}
	if !strings.Contains(read, "no competence measurement is wired into this surface") {
		t.Fatalf("an unwired competence map was left out rather than named:\n%s", read)
	}
	if !strings.Contains(read, "not reachable from this surface") {
		t.Fatalf("the craft shelf and the lease were left out rather than named:\n%s", read)
	}
	// A surface with no graph at all refuses readably rather than panicking —
	// the belt's one guard for every tool, which these three inherit.
	headless := &beltRun{head: New(nil, nil), user: store.Message{}}
	for _, tool := range []string{beltToolStatus, beltToolRecall, beltToolOpen} {
		answer, refused := headless.execute(tool, "")
		if !refused || !strings.Contains(answer, "no graph behind it") {
			t.Fatalf("%s on a graphless surface = %q refused=%t", tool, answer, refused)
		}
	}
}

// The three reads are reads: they journal nothing, they record nothing on the
// run, and a message that only looked carries no command seq.
func TestTheLensChangesNothing(t *testing.T) {
	graph := openHeadStore(t)
	seedRecallMemory(t, graph)
	run := lensRun(t, graph, "lens")

	for _, call := range []struct {
		tool string
		args string
	}{
		{beltToolRecall, mustJSON(map[string]any{"q": "pricing"})},
		{beltToolOpen, mustJSON(map[string]any{"id": "pricing-study"})},
		{beltToolStatus, ""},
	} {
		if _, failed := run.execute(call.tool, call.args); failed {
			t.Fatalf("%s failed", call.tool)
		}
		if !beltReadOnly(call.tool) {
			t.Fatalf("%s is not classified as a read, so the turn budgets it as an act", call.tool)
		}
	}
	if run.acted || run.commandSeq != 0 || len(run.issued) != 0 {
		t.Fatalf("the lens recorded an act: acted=%t seq=%d issued=%v",
			run.acted, run.commandSeq, run.issued)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("the lens journaled %d commands", len(commands))
	}
}

// The plan renderer is shared rather than copied, and borrowing it must not
// have moved the plan tool's own bounds.
func TestThePlanToolKeepsItsOwnBoundsWhileTheLensBorrowsTheRenderer(t *testing.T) {
	graph := openHeadStore(t)
	seedPlannedMarket(t, graph)
	node, found, err := graph.Node("market")
	if err != nil || !found {
		t.Fatalf("node market found=%t err=%v", found, err)
	}
	head := New(&beltClient{}, graph)
	capped, failed := head.renderPlan(node)
	if failed {
		t.Fatalf("renderPlan failed: %s", capped)
	}
	uncapped, failed := head.renderPlanWithin(node, 0, 0)
	if failed {
		t.Fatalf("renderPlanWithin failed: %s", uncapped)
	}
	// Same plan, same words: a three-step plan is under both bounds, so the two
	// readers must agree exactly.
	if capped != uncapped {
		t.Fatalf("the shared renderer disagrees with itself:\n%q\nvs\n%q", capped, uncapped)
	}
	if !strings.Contains(capped, "plan for Northern market") {
		t.Fatalf("the plan read lost its heading:\n%s", capped)
	}
}
