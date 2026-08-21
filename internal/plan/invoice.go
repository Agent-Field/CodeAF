package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/profile"
)

// The invoice is what a split costs, in the only currency anybody can check:
// what work of this kind has already cost on this machine.
//
// Every prompt in this package that judges division states a burden of proof —
// a split has to be bought on wall time, cost and quality together — and then
// hands the model no prices with which to do the arithmetic. So the burden is
// discharged on plausibility: a division that sounds parallel sounds bought. The
// measured lines that would settle it exist, are already collected per (model,
// subharness), and reach exactly one reader today as decoration under a menu.
//
// This is that measurement rendered as evidence for the three decisions that
// actually divide work. It adds no rule and replaces no principle: the prompts
// keep their one burden of proof and gain a short table of numbers underneath
// it. Where there is no measurement the block is absent — never a placeholder,
// never a prior dressed as an observation — because a fabricated price is worse
// than no price at all: it is an argument the model cannot check and will not
// question.
//
// Cache shape (L7): an invoice moves every time a leaf finishes, so it is never
// part of a shared prefix. Every injection site below appends it to the tail of
// the last user message of its own call, behind everything stable — the same
// position, and for the same reason, that the worker menu takes in the retry
// judgements (internal/revision/judge.go) and the measured line takes in the
// compiler.

// Invoice is one worker's measured price list.
//
// It is deliberately not a model of anything. Each field is a quantile or an
// extreme of observations that were journaled as they happened; nothing here is
// fitted, extrapolated or smoothed, so every number in the rendered block is a
// thing that actually happened to a real leaf.
type Invoice struct {
	// Worker names whose price list this is. Prices are per (model, subharness)
	// because capacity is a property of the executor: a coding pipeline and a
	// lookup share no envelope, and one average over both describes neither.
	Worker string
	// Shapes are the per-size rows, in the order sizes are worth reading.
	Shapes []InvoiceShape
	// Floor is what the cheapest leaf this worker has ever run cost, and it is
	// the closest thing to a measured fixed price of a child there is: whatever
	// a piece costs before it does any work of its own — being told the subject,
	// being told the working method, claiming the node and saying it is done.
	// A split of N ways pays it N times whatever else it buys.
	Floor int
	// FloorKnown separates a measured floor from an unmeasured one. A zero
	// floor is not a free child; it is a worker nobody has watched yet.
	FloorKnown bool
	// JoinTokens is the fan-in tax: what a leaf that consumed earlier results
	// has actually cost, over the runs that recorded how many results they
	// consumed. JoinInputs is the median number of results those runs read.
	// A join re-reads every branch's digest, so this is the price the width
	// itself adds at the far end and it is the one cost a fan-out prompt never
	// sees.
	JoinTokens int
	JoinInputs int
	JoinRuns   int
}

// InvoiceShape is one size bucket's measured price.
type InvoiceShape struct {
	Shape  string
	Runs   int
	Turns  int
	Tokens int
	Cost   float64
}

// CapacityEvidence is the measured failure rate that rides beside prices.
// It belongs in the invoice because it answers the same economic question:
// what happened here when work was kept as one worker's job.
type CapacityEvidence struct {
	Worker   string
	Runs     int
	Overruns int
	Rate     float64
}

// Priced reports whether this invoice says anything at all. An invoice with no
// shape rows is one nobody has enough evidence for, and it renders to nothing.
func (i Invoice) Priced() bool { return len(i.Shapes) > 0 }

// invoiceShapeOrder is the reading order of the size buckets: the judged ruler
// ascending, then the node nobody judged. The two rows a division is actually
// weighed against — what one piece costs, and what the whole thing costs
// undivided — are the ends of this list, which is the comparison the block
// exists to make.
//
// The whole bucket is last and was missing entirely. Undivided goals, single-leaf
// remainders and post-planning splices are the commonest shape the product runs
// and they carried no size at all, so they landed in a bucket with no name and no
// reader: the one row a planner most needs — what happens if you do not split
// this — was the one row the price list could never show.
var invoiceShapeOrder = []string{
	profile.BucketDirect, string(SizeAtomic), string(SizeBorderline), string(SizeOversized),
	profile.BucketWhole,
}

// InvoiceFor prices one worker from its journaled records.
//
// The evidence floor is profile.MinSamples per row, the same gate the ruler and
// the measured menu line are held to. A shape below it is not rendered at a
// lower confidence — it is not rendered, because the whole value of this block
// is that a number in it can be trusted without qualification.
//
// Reflex micro-leaves are excluded outright. They are a different population
// with an envelope that was never allowed to be large, and a price list that
// averaged them in would tell a planner that a piece of work costs a fraction of
// what a piece of work costs.
func InvoiceFor(worker string, records []profile.Record) (Invoice, bool) {
	invoice := Invoice{Worker: strings.TrimSpace(worker)}
	if invoice.Worker == "" {
		invoice.Worker = LinearSubharness
	}
	byShape := map[string][]profile.Record{}
	for _, record := range records {
		if record.Size == profile.BucketReflex || record.Tokens <= 0 {
			continue
		}
		byShape[record.Size] = append(byShape[record.Size], record)
		// The floor is taken across every NAMED shape rather than within one.
		// What it measures is the cost of existing as a leaf at all, and the
		// cheapest leaf ever seen is the cheapest leaf ever seen whatever the
		// planner had guessed its size would be — but only where somebody said
		// what was being guessed at.
		//
		// The unlabelled rows are excluded because they are not a shape. They
		// are what every surface that did not say wrote, including a
		// one-turn no-op that fabricated a result and spent 7,215 tokens doing
		// it, and that number was then advertised to every planning call as
		// "what a piece costs before it does any work" — a floor read off a leaf
		// that did no work. The shapes are named now (profile.BucketWhole), so
		// an unlabelled record is a legacy row and nothing else.
		if !record.Labelled() {
			continue
		}
		if !invoice.FloorKnown || record.Tokens < invoice.Floor {
			invoice.Floor, invoice.FloorKnown = record.Tokens, true
		}
	}
	for _, shape := range invoiceShapeOrder {
		priced, ok := priceShape(shape, byShape[shape])
		if !ok {
			continue
		}
		invoice.Shapes = append(invoice.Shapes, priced)
	}
	invoice.JoinTokens, invoice.JoinInputs, invoice.JoinRuns = priceJoin(records)
	if !invoice.Priced() {
		// Without a single priced shape there is no price list, and the floor
		// alone would be a number with nothing to compare it against.
		return Invoice{}, false
	}
	return invoice, true
}

// priceShape is one bucket's medians, or nothing when the bucket is too thin.
func priceShape(shape string, records []profile.Record) (InvoiceShape, bool) {
	if len(records) < profile.MinSamples {
		return InvoiceShape{}, false
	}
	turns := make([]int, 0, len(records))
	tokens := make([]int, 0, len(records))
	costs := make([]float64, 0, len(records))
	for _, record := range records {
		turns = append(turns, record.Turns)
		tokens = append(tokens, record.Tokens)
		costs = append(costs, record.Cost)
	}
	sort.Ints(turns)
	sort.Ints(tokens)
	sort.Float64s(costs)
	return InvoiceShape{
		Shape:  shape,
		Runs:   len(records),
		Turns:  turns[len(turns)/2],
		Tokens: tokens[len(tokens)/2],
		Cost:   costs[len(costs)/2],
	}, true
}

// priceJoin measures what reassembly has cost, from the records that recorded
// how many earlier results actually landed in them.
//
// It selects on the measured fan-in and never on the source count, and that
// distinction is the whole of the fix. Sources is the touch-list the fan-out
// prompt collects — the pages, datasets and files a part must visit — and for as
// long as this function read it, "a leaf that read earlier results" meant "a leaf
// that named two things", which nearly every leaf does. So the join row was
// computed over the ordinary population and came back at the ordinary price, and
// the one number a fan-out prompt cannot see said reassembly was free.
//
// Only records that carry a counted fan-in participate, which is the honesty
// here: a record written before fan-in was measured, or by a surface that does
// not know the number, is indistinguishable from a leaf that consumed nothing,
// and counting it as zero would price a join from leaves that never made one.
// Below the evidence floor the fan-in tax is simply unknown and the line is
// absent.
func priceJoin(records []profile.Record) (tokens, inputs, runs int) {
	var joinTokens, joinInputs []int
	for _, record := range records {
		if record.Size == profile.BucketReflex || record.Tokens <= 0 {
			continue
		}
		fanIn, counted := record.FanInCount()
		if !counted || fanIn < 2 {
			continue
		}
		joinTokens = append(joinTokens, record.Tokens)
		joinInputs = append(joinInputs, fanIn)
	}
	if len(joinTokens) < profile.MinSamples {
		return 0, 0, 0
	}
	sort.Ints(joinTokens)
	sort.Ints(joinInputs)
	return joinTokens[len(joinTokens)/2], joinInputs[len(joinInputs)/2], len(joinTokens)
}

// invoicePreamble is the only sentence in the block that is not a number, and
// it is framing rather than instruction: it says what the figures are and what
// they are not. The prompts above it already carry the one principle; this adds
// no second one, and in particular it never says which way the numbers point.
const invoicePreamble = `MEASURED HERE — what work of this kind has really cost on this machine, as it
happened. Evidence, not a target and not a limit. A figure that is absent is one
nobody has measured yet; do not supply it.`

// RenderInvoice lays out the price lists that have prices, or returns the empty
// string when none of them do.
//
// The empty string is the whole compatibility story: a fresh machine, a fresh
// worker, or a profile below the evidence floor renders nothing, every prompt
// below sends exactly the bytes it has always sent, and no call anywhere costs a
// token more than it did.
func RenderInvoice(invoices ...Invoice) string {
	var block strings.Builder
	for _, invoice := range invoices {
		if !invoice.Priced() {
			continue
		}
		if block.Len() > 0 {
			block.WriteString("\n")
		}
		for _, shape := range invoice.Shapes {
			fmt.Fprintf(&block, "%s, %s: %d runs, median %d turns, %s tokens, $%.4f each\n",
				invoice.Worker, shape.Shape, shape.Runs, shape.Turns,
				thousands(shape.Tokens), shape.Cost)
		}
		if invoice.FloorKnown {
			fmt.Fprintf(&block, "%s: the cheapest leaf ever recorded cost %s tokens — what a piece "+
				"costs before it does any work of its own, paid once per piece\n",
				invoice.Worker, thousands(invoice.Floor))
		}
		if invoice.JoinRuns > 0 {
			fmt.Fprintf(&block, "%s: a leaf that read earlier results cost %s tokens over %d runs, "+
				"reading a median of %d results each\n",
				invoice.Worker, thousands(invoice.JoinTokens), invoice.JoinRuns, invoice.JoinInputs)
		}
	}
	if block.Len() == 0 {
		return ""
	}
	return invoicePreamble + "\n\n" + strings.TrimRight(block.String(), "\n")
}

// withInvoice appends a rendered invoice to a message that is already the tail
// of its call. It is the one place the block is attached, so that no injection
// site can accidentally put it anywhere but last.
func withInvoice(tail, invoice string) string {
	if strings.TrimSpace(invoice) == "" {
		return tail
	}
	if strings.TrimSpace(tail) == "" {
		return invoice
	}
	return strings.TrimRight(tail, "\n") + "\n\n" + invoice
}

// AppendCapacityEvidence extends an already-rendered invoice with measured
// capacity, or starts the same measured block when prices have not accumulated
// yet. An empty observation returns the input byte for byte, which keeps cold
// start and non-swarm prompts unchanged.
func AppendCapacityEvidence(invoice string, evidence CapacityEvidence) string {
	if evidence.Runs <= 0 {
		return invoice
	}
	worker := strings.TrimSpace(evidence.Worker)
	if worker == "" {
		worker = LinearSubharness
	}
	line := fmt.Sprintf("%s: on this machine, %d settled leaves ran and %.0f%% overran",
		worker, evidence.Runs, evidence.Rate*100)
	if strings.TrimSpace(invoice) == "" {
		return invoicePreamble + "\n\n" + line
	}
	return strings.TrimRight(invoice, "\n") + "\n" + line
}

// thousands groups a token count so a reader can tell 40,000 from 400,000 at a
// glance. The block's whole job is a comparison between magnitudes, and four
// undelimited digits beside six is the one way to lose it.
func thousands(value int) string {
	digits := fmt.Sprintf("%d", value)
	if value < 0 {
		return digits
	}
	var grouped strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return grouped.String()
}
