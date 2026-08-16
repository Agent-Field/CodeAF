// Package consent is the price before the purchase.
//
// Every number in this system used to be retrospective. Cost was computed after
// spending, the rail fired after crossing, and the first quantity a user ever
// saw about a fifty-leaf job was a step count emitted well after the planning
// that produced it had been paid for. There was no moment at which a person
// could look at what they had asked for and decline it.
//
// This is that moment, and it is deliberately small: one question, quoting a
// count and a price, with two options, on the last tick at which not starting
// is free — the claim. Below the threshold nothing is asked at all, because a
// resident that asks permission for two dollars of work is not a resident.
//
// It is a package rather than a corner of the chat window because the desk has
// to be reachable from wherever work is admitted. A window-side desk answers
// only for the process holding the terminal; a resident-side orchestrator —
// the chat-rebuild doc's 4.6 v2 — admits work with no window attached at all,
// and until this moved it had no way to price anything (Part 9.10).
package consent

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

const (
	// Approve and Hold are the two answers. The gate reads the recorded
	// resolution rather than decoding an option value, so the labels are the
	// contract and are matched exactly.
	Approve = "yes, start it"
	Hold    = "hold it — I'll trim it first"

	// pollEvery is how often an outstanding question is re-read. It runs only
	// while at least one job is waiting, so an idle resident pays nothing for
	// it.
	pollEvery = 2 * time.Second

	// holdReason is what the hold records. It appears wherever a held node
	// explains itself.
	holdReason = "waiting for your go-ahead on the estimate"
)

// Category names this ask for the empirical gate the way every other durable
// question is named. It is its own class because its answer is the one thing no
// default may be assumed for: the whole point is that a person said yes before
// the money moved.
const Category = store.QuestionCategory("plan-consent")

// Estimate is the honest arithmetic behind the question: how many steps, and
// what a step has cost on this machine. Both halves are measurements. When
// there is no measurement there is no estimate and no question — a forecast the
// model made up would be worse than the silence it replaced.
type Estimate struct {
	Leaves  int
	PerLeaf float64
	Dollars float64
}

// EstimateJob prices a spliced subtree. The unit cost is the median of what a
// run has actually cost here, preferring the executor's own profile records
// (which know the shape of the work) and falling back to the journal's own
// median (which knows this machine). A job with no measured history anywhere
// returns nothing at all.
func EstimateJob(graph *store.Store, measured *profile.Profile, root string) (Estimate, bool) {
	nodes, err := graph.SubtreeNodes(root)
	if err != nil || len(nodes) == 0 {
		return Estimate{}, false
	}
	hasChild := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node.Parent != "" {
			hasChild[node.Parent] = true
		}
	}
	estimate := Estimate{}
	for _, node := range nodes {
		if !hasChild[node.ID] {
			estimate.Leaves++
		}
	}
	if estimate.Leaves == 0 {
		return Estimate{}, false
	}
	estimate.PerLeaf = MedianLeafCost(measured)
	if estimate.PerLeaf <= 0 {
		if journaled, err := graph.MeasuredCostPerRun(); err == nil {
			estimate.PerLeaf = journaled
		}
	}
	if estimate.PerLeaf <= 0 {
		return Estimate{}, false
	}
	estimate.Dollars = float64(estimate.Leaves) * estimate.PerLeaf
	return estimate, true
}

// MedianLeafCost is what one leaf of ordinary planned work has cost this model.
// The median rather than the mean because leaf costs are long-tailed and one
// runaway must not set the price of the next fifty.
func MedianLeafCost(measured *profile.Profile) float64 {
	if measured == nil {
		return 0
	}
	costs := make([]float64, 0, len(measured.Records))
	for _, record := range measured.Records {
		if record.Cost > 0 && record.Size != profile.BucketReflex {
			costs = append(costs, record.Cost)
		}
	}
	if len(costs) == 0 {
		return 0
	}
	sort.Float64s(costs)
	return costs[len(costs)/2]
}

// Desk holds the jobs waiting on an answer and releases them when one arrives.
// It is in-memory because it is a watch loop, not a record: the question and
// the hold are both durable, so a restart rebuilds the watch from the graph
// rather than from anything kept here.
type Desk struct {
	graph *store.Store
	// headless is the answer given by a desk with nobody standing at it. A
	// durable question asked where no one can read it is a job that waits
	// forever, so a one-shot run decides at this exact point instead — approve
	// and carry on, or refuse with the price it refused — and the question is
	// never asked. Nil is the ordinary desk.
	headless func(store.Node, Estimate) bool
	mu       sync.Mutex
	waiting  map[string]int64
	wake     chan struct{}
}

// NewDesk builds a desk over one durable graph.
func NewDesk(graph *store.Store) *Desk {
	return &Desk{graph: graph, waiting: make(map[string]int64), wake: make(chan struct{}, 1)}
}

// WithHeadless installs the answer a desk with nobody at it gives. Nil — the
// default — is the ordinary desk: it asks, and the job waits.
func (d *Desk) WithHeadless(answer func(store.Node, Estimate) bool) *Desk {
	if d == nil {
		return d
	}
	d.headless = answer
	return d
}

// consentQuestion finds the one consent question this job has ever been asked.
// One is the whole design: a job is priced once, and a person who said "hold"
// is not asked again every time a leaf comes up for claim.
func (d *Desk) consentQuestion(root string) (store.AgentQuestion, bool) {
	questions, err := d.graph.QuestionsForNode(root, 20)
	if err != nil {
		return store.AgentQuestion{}, false
	}
	for _, question := range questions {
		if question.Category == Category {
			return question, true
		}
	}
	return store.AgentQuestion{}, false
}

// Gate is the last free moment. It returns true when the caller must not run.
//
// The hold is what makes this work without a new mechanism: a held node is not
// ready, the runner's landing path already treats a held claim as a release
// rather than a failure, and the leaf that got this far has spent nothing yet
// because this is the first thing its executor does.
func (d *Desk) Gate(settings config.Config, measured *profile.Profile, node store.Node) bool {
	if d == nil || settings.PlanConsentUSD <= 0 || node.Group == resident.ReflexGroup ||
		node.Provenance.Origin != store.OriginUser {
		return false
	}
	root, ok := JobRootOf(d.graph, node)
	if !ok {
		return false
	}
	if question, found := d.consentQuestion(root.ID); found {
		if question.Status == store.QuestionAnswered || question.Status == store.QuestionExpired {
			// Asked and settled. Whatever the answer was, the decision has
			// been made once and by a person; re-holding here would be the
			// system overruling them, and would deadlock a job they released
			// by hand.
			return false
		}
		d.hold(root)
		d.watch(root.ID, question.Seq)
		return true
	}
	estimate, priced := EstimateJob(d.graph, measured, root.ID)
	if !priced || estimate.Dollars < settings.PlanConsentUSD {
		return false
	}
	if d.headless != nil {
		if d.headless(root, estimate) {
			return false
		}
		// Refused. The hold is what makes the refusal cost nothing: the claim
		// is released, no worker has said a word, and the driver above reports
		// the price rather than paying it.
		d.hold(root)
		return true
	}
	seq, err := d.ask(root, estimate)
	if err != nil {
		// A question that could not be asked must not become a job that never
		// runs. Silence here costs money; a wedge costs the errand.
		log.Printf("note: could not ask for spending consent on %s: %v", root.ID, err)
		return false
	}
	d.hold(root)
	d.watch(root.ID, seq)
	return true
}

func (d *Desk) ask(root store.Node, estimate Estimate) (int64, error) {
	label := clipUTF8Bytes(firstLine(nodeDisplay(root)), 60)
	prompt := fmt.Sprintf("%s comes to %d %s, about $%.2f at what work like this has cost here. Start it, or trim it first?",
		label, estimate.Leaves, plural(estimate.Leaves, "step"), estimate.Dollars)
	options := []store.QuestionOption{
		{Label: Approve},
		{Label: Hold, Hint: "the plan stays as it is; cancel the parts you don't want, then say go"},
	}
	allowFree := false
	body := store.QuestionMessageBody(prompt, options, store.QuestionConfig{
		Kind: store.QuestionConfirm, Category: Category,
		Default: "1", AllowFree: &allowFree,
	})
	question, err := d.graph.AskQuestion(store.AgentQuestion{
		SessionID: root.Provenance.SessionID, Text: body, OriginNodeID: root.ID,
		Urgency: store.QuestionBlocking, Category: Category,
		DefaultAnswer: "1", Options: options,
	})
	if err != nil {
		return 0, err
	}
	if _, err := d.graph.SurfaceQuestion(question.Seq); err != nil {
		return 0, err
	}
	return question.Seq, nil
}

func (d *Desk) hold(root store.Node) {
	d.setHold(root, true)
}

func (d *Desk) setHold(root store.Node, held bool) {
	nodes, err := d.graph.SubtreeNodes(root.ID)
	if err != nil {
		return
	}
	for _, node := range nodes {
		if node.Status == store.Done || node.Status == store.Failed || node.Status == store.Cancelled {
			continue
		}
		if node.Held == held {
			continue
		}
		if err := d.graph.SetNodeHold(node.ID, held, holdReason); err != nil {
			log.Printf("note: could not %s %s for consent: %v", holdVerb(held), node.ID, err)
		}
	}
}

func holdVerb(held bool) string {
	if held {
		return "hold"
	}
	return "release"
}

func (d *Desk) watch(root string, seq int64) {
	if !d.enqueue(root, seq) {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Desk) enqueue(root string, seq int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, already := d.waiting[root]; already {
		return false
	}
	d.waiting[root] = seq
	return true
}

func (d *Desk) outstanding() map[string]int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	pending := make(map[string]int64, len(d.waiting))
	for root, seq := range d.waiting {
		pending[root] = seq
	}
	return pending
}

func (d *Desk) forget(root string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.waiting, root)
}

// Rehydrate rebuilds the watch after a restart. Held nodes and unanswered
// questions are both durable, so a resident that died between the question and
// the answer comes back knowing exactly what it was waiting for — and a job
// approved while it was down is released on the way up rather than sitting
// held forever.
func (d *Desk) Rehydrate() {
	nodes, err := d.graph.ActiveNodes()
	if err != nil {
		return
	}
	for _, node := range nodes {
		if node.Parent != store.RootID || !node.Held {
			continue
		}
		question, found := d.consentQuestion(node.ID)
		if !found {
			continue
		}
		switch question.Status {
		case store.QuestionAnswered, store.QuestionExpired:
			d.settle(node.ID, question)
		default:
			d.watch(node.ID, question.Seq)
		}
	}
}

// Serve is the release loop. It sleeps on a channel while nothing is waiting,
// which is the difference between a feature and a tax on every idle resident.
func (d *Desk) Serve(ctx context.Context) {
	for {
		if len(d.outstanding()) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-d.wake:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollEvery):
		}
		for root, seq := range d.outstanding() {
			question, found, err := d.graph.AgentQuestionBySeq(seq)
			if err != nil || !found {
				continue
			}
			if question.Status != store.QuestionAnswered && question.Status != store.QuestionExpired {
				continue
			}
			d.settle(root, question)
		}
	}
}

// settle acts on an answered question exactly once. An approval releases the
// hold and says so; anything else leaves the plan standing and held, which is
// what "trim it first" means — the pending nodes are all still there to cancel.
func (d *Desk) settle(root string, question store.AgentQuestion) {
	d.forget(root)
	node, found, err := d.graph.Node(root)
	if err != nil || !found {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(question.Resolution), Approve) {
		return
	}
	d.setHold(node, false)
	_, _ = thread.Post(d.graph, store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		NodeID:    node.ID,
		Body:      "starting — " + clipUTF8Bytes(firstLine(nodeDisplay(node)), 60),
	})
}

// JobRootOf walks to the top-level node an errand hangs from. It is the unit a
// price is quoted for, because it is the unit a person asked for.
func JobRootOf(graph *store.Store, node store.Node) (store.Node, bool) {
	current := node
	for depth := 0; depth < 32; depth++ {
		if current.Parent == store.RootID {
			return current, true
		}
		if strings.TrimSpace(current.Parent) == "" {
			return store.Node{}, false
		}
		parent, found, err := graph.Node(current.Parent)
		if err != nil || !found {
			return store.Node{}, false
		}
		current = parent
	}
	return store.Node{}, false
}

func nodeDisplay(node store.Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	return firstLine(node.Brief)
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func clipUTF8Bytes(value string, limit int) string {
	if limit <= 3 || len(value) <= limit {
		return value
	}
	cut := limit - 3
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
}

func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}
