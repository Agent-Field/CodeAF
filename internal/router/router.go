package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Client is the slice of the model adapter everything above the transport
// consumes. Both the single-model adapter and the Router satisfy it, which is
// what lets a panel be a configuration choice instead of a second code path
// through the planner and the executor.
type Client interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
	Model() string
}

// maxRungs bounds a cascade. Three is not a round number: the router lab's
// winning policy fired its second rung on about a third of tasks and averaged
// 1.35 calls per task, so a third rung is already the tail of the tail, and a
// fourth would cost latency on the cases a third did not save.
const maxRungs = 3

// unknownPrice is what a model costs when neither the catalog nor the operator
// said. It is a placeholder, not an estimate: with every price equal the
// cost-effectiveness ordering degenerates to the rating ordering, and with
// nothing rated either it degenerates to the order the operator wrote the panel
// in — which is the right answer when nothing is known.
const unknownPrice = 1.0

// exploreEvery is how often a schema-verified call opens on an under-observed
// panel member instead of the one the ledger would pick. One in ten.
//
// The problem it answers is measured: after nine cells and about a thousand
// calls, three of five panel members had **zero** observations. A router that
// only ever tries what it already believes in can never be wrong about the rest,
// and the four models it was choosing against were the four it knew nothing
// about — which is what made every other defect invisible until the ledger acted
// on them.
//
// Ten per cent is chosen so that the cost is bounded by construction rather than
// by hope. Exploration is confined to calls carrying a JSON schema, where a bad
// answer is caught by the verifier for free and escalates within the same call,
// so a wasted trial costs one cheap request and never a wrong result; it never
// touches the terminal rung, so the cascade's ceiling is untouched; and it never
// touches a leaf, where there is no verifier and a bad trial costs a whole
// conversation. At one in ten, an unmeasured model reaches MinGraded inside a
// single run of the size arm B ran, while nine calls in ten still go where the
// evidence says.
const exploreEvery = 10

// Router sends each call to the cheapest model that can do it, and escalates
// when it turns out one could not.
//
// It is the same shape as the adapter it wraps, which is the whole design: the
// planner and the executor call CompleteWithMessages and never learn that there
// is more than one model behind it.
type Router struct {
	rungs  []*rung
	opener *rung
	ledger *Ledger
	events *Events

	mutex sync.Mutex
	// draws counts routed calls per class, and with the run id it is the whole
	// of the exploration seed. A counter rather than a random source so that a
	// run is reproducible: the same panel, the same ledger and the same sequence
	// of calls explore in the same places, which is what lets an experiment be
	// re-run rather than merely repeated.
	draws map[provider.CallClass]int
}

// rung is one model on the panel, with its own adapter.
type rung struct {
	spec   Spec
	price  float64 // $/M output tokens
	client *provider.Client
}

// New builds a router over a panel. base carries everything the adapter needs
// except the model, which each rung supplies for itself.
func New(panel Panel, base provider.Config, dir string) (*Router, error) {
	return buildRouter(panel, base, dir, "")
}

// NewPinned builds a router whose first attempt honours an explicit model
// choice. The choice joins the panel when it is not already present, so a chat
// picker may name any model without giving up ledger observation or the panel's
// terminal escalation rung.
func NewPinned(panel Panel, base provider.Config, dir, opener string) (*Router, error) {
	opener = strings.TrimSpace(opener)
	if opener == "" {
		return nil, errors.New("router: the pinned opener is empty")
	}
	return buildRouter(panel, base, dir, opener)
}

func buildRouter(panel Panel, base provider.Config, dir, opener string) (*Router, error) {
	if len(panel.Models) == 0 {
		return nil, errors.New("router: the panel is empty")
	}
	models := append([]Spec(nil), panel.Models...)
	if opener != "" {
		found := false
		for _, spec := range models {
			if spec.Slug == opener {
				found = true
				break
			}
		}
		if !found {
			models = append([]Spec{{Slug: opener}}, models...)
		}
	}
	catalog := LoadCatalog(dir, base.BaseURL, base.APIKey, base.HTTPClient)
	ledger, err := LoadLedger(dir)
	if err != nil {
		return nil, err
	}
	events, err := OpenEvents(dir)
	if err != nil {
		// A router that cannot keep a diary still routes.
		events = nil
	}

	router := &Router{ledger: ledger, events: events, draws: map[provider.CallClass]int{}}
	for _, spec := range models {
		price := spec.Price
		if price <= 0 {
			if entry, known := catalog.Entry(spec.Slug); known {
				price = entry.OutputPrice
			}
		}
		if panel.MaxOutputPrice > 0 && price > panel.MaxOutputPrice {
			return nil, fmt.Errorf("router: %s costs $%.2f/M output, over the configured cap of $%.2f",
				spec.Slug, price, panel.MaxOutputPrice)
		}
		if price <= 0 {
			price = unknownPrice
		}
		config := base
		config.Model = spec.Slug
		client, err := provider.NewClient(config)
		if err != nil {
			return nil, fmt.Errorf("router: %s: %w", spec.Slug, err)
		}
		item := &rung{spec: spec, price: price, client: client}
		router.rungs = append(router.rungs, item)
		if opener != "" && spec.Slug == opener && router.opener == nil {
			router.opener = item
		}
	}
	return router, nil
}

// Model reports the configured opener. It is what the harness prints and what
// the profile is keyed on; which model actually served a given call is in the
// events log, where a per-call answer belongs.
func (r *Router) Model() string {
	if r.opener != nil {
		return r.opener.spec.Slug
	}
	return r.rungs[0].spec.Slug
}

// Rungs is how many models the panel holds. The scheduler reads it to decide
// whether re-running a failed leaf could possibly help.
func (r *Router) Rungs() int { return len(r.rungs) }

// Ledger exposes what has been learned, for reporting.
func (r *Router) Ledger() *Ledger { return r.ledger }

// Close flushes what the run learned and releases the diary.
//
// A Router owns a file handle and a queue of observations the run has already
// paid for, so a router that is replaced — a model switched mid-session — has
// to be closed rather than dropped. Close is idempotent: a second call saves
// nothing new and closes nothing twice.
func (r *Router) Close() error {
	return errors.Join(r.ledger.Close(), r.events.Close())
}

// CompleteWithMessages routes one call.
//
// The two shapes of work are routed differently and the difference is not an
// optimisation. A planning call is one request whose answer can be checked the
// moment it arrives, so it cascades: cheap model, verify, escalate on a failure
// the verifier caught. A leaf is a whole conversation whose worth is only known
// at the end, so it is routed once and pinned — escalation for a leaf is the
// scheduler re-running it, not the loop changing model between turns.
func (r *Router) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	call := provider.CallFrom(ctx)
	if call.Class() == provider.ClassExecLeaf {
		// A leaf is a whole conversation running in a room with nobody in it,
		// and a cascade is a side errand of the work rather than the work — so
		// the two shapes this router knows about are also two roles, and it is
		// the last layer that can tell them apart (internal/lane's roles.go).
		return r.leaf(named(ctx, lane.RoleLeafUnattended), call, messages, options)
	}
	return r.cascade(named(ctx, lane.RoleAuxiliary), call, messages, options)
}

// named says who a call is for WHEN NOBODY ABOVE HAS ALREADY SAID.
//
// THE MORE SPECIFIC CLAIM WINS, and a caller's is always the more specific one:
// this router knows only the shape of the request, while the layer that made it
// knows whether a person is reading the answer. A conversation that has named
// itself talk must not be re-labelled a background errand on its way through a
// panel, because the role is what decides who owns the phase clock and a talk
// turn demoted here would hand the status line to a naming errand beside it.
func named(ctx context.Context, role lane.Role) context.Context {
	if provider.RoleFrom(ctx).Known() {
		return ctx
	}
	return provider.WithRole(ctx, role)
}

// StreamComplete streams from the first rung and never cascades. A stream is
// committed the moment its first byte is delivered, so there is nothing to
// escalate to: the answer has already started arriving. Nothing in the harness
// streams today; this exists so that the router is a drop-in for the adapter.
func (r *Router) StreamComplete(ctx context.Context, prompt string, options ...ai.Option) (<-chan ai.StreamChunk, <-chan error) {
	first := r.rungs[0]
	// A STREAM IS A THING SOMEBODY IS READING. That is the only reason to ask
	// for one — an answer nobody watches arrive is cheaper and safer whole — so
	// where the caller named no role this is the one place in this file that
	// reads as talk rather than as an errand.
	return first.client.StreamComplete(first.affinity(named(ctx, lane.RoleTalk)), prompt, options...)
}

// cascade is the policy both labs converged on.
//
// Its economics rest entirely on the verifier, and that is worth stating where
// the code is: the cheap first attempt is only cheap because a failure costs
// nothing but the retry, and the escalation is only rare because most first
// attempts pass. Where nothing can check the answer the cascade collapses into
// "always use the cheapest model", which is why an unverifiable call reports an
// unverified success and moves nothing.
func (r *Router) cascade(ctx context.Context, call *provider.Call, messages []ai.Message, options []ai.Option) (*ai.Response, error) {
	class := call.Class()
	order := r.order(class)
	schema := schemaOf(options)

	// Exploration comes before the attempt window, so a retry never explores:
	// an escalation is already the expensive path and spending it on evidence
	// rather than on the answer would charge the trial to the caller.
	explorer := 0.0
	if call.Attempt() == 0 {
		if opened, propensity := r.explore(ctx, class, order, schema); opened != nil {
			order, explorer = opened, propensity
		}
	}
	if start := call.Attempt(); start > 0 {
		if start >= len(order) {
			start = len(order) - 1
		}
		order = order[start:]
	}
	order = preferDifferent(order, avoidedModelFrom(ctx))
	candidates := slugs(order)

	var tried []string
	var failures []error
	for index, pick := range order {
		attempt := Event{Class: string(class), Candidates: candidates, Rung: index, Escalation: tried,
			Model: pick.spec.Slug}
		if index == 0 && explorer > 0 {
			attempt.Explore, attempt.Propensity = true, explorer
		}
		started := time.Now()
		response, err := pick.client.CompleteWithMessages(pick.affinity(ctx), messages, options...)
		elapsed := time.Since(started)
		if err != nil {
			// Transport, not ability. It moves no rating, and the next rung is
			// tried anyway because the work still has to happen.
			attempt.Verdict = provider.VerdictProviderFailure
			r.record(ctx, attempt, nil, elapsed)
			tried = append(tried, pick.spec.Slug)
			failures = append(failures, fmt.Errorf("%s: %w", pick.spec.Slug, err))
			continue
		}
		if verdict := verify(response, schema); verdict != "" {
			// Final for this rung: it produced something and the something was
			// unusable. That is the observation the cascade exists to collect.
			r.observe(pick, class, resolvedOf(response), verdict)
			attempt.Verdict = verdict
			r.record(ctx, attempt, response, elapsed)
			tried = append(tried, pick.spec.Slug)
			failures = append(failures, fmt.Errorf("%s: %s", pick.spec.Slug, verdict))
			continue
		}
		// It parsed and it carried the fields it promised. Whether it is *right*
		// is the call site's to say, so the verdict is held open until it does —
		// and stays unverified, moving nothing, if it never does.
		attempt.Verdict = provider.VerdictUnverifiedSuccess
		id := r.record(ctx, attempt, response, elapsed)
		resolved, run := resolvedOf(response), provider.CacheKeyFrom(ctx)
		call.Observe(func(final provider.Verdict) {
			r.observe(pick, class, resolved, final)
			r.events.Append(Event{Call: id, Run: run, Class: string(class), Model: pick.spec.Slug,
				Resolved: resolved, Verdict: final, Final: true})
		})
		return response, nil
	}
	return nil, fmt.Errorf("every rung failed: %w", errors.Join(failures...))
}

// explore occasionally opens a call on a panel member nothing has measured.
//
// It answers the defect that hid all the others: only the chosen model
// accumulates evidence, so a model never chosen is never measured, and after a
// thousand calls the ledger was confident about exactly one model and knew
// nothing about the four it was choosing against. A prior that is never
// contradicted is indistinguishable from a fact.
//
// Every condition here is a bound on what a wasted trial can cost:
//
//   - only where a schema is being verified, so a bad trial is caught by verify
//     for free and escalates inside the same call — the caller never sees it;
//   - never the terminal rung, so the cascade's ceiling is exactly what it was;
//   - never a leaf, which cannot reach here at all — a leaf has no verifier, and
//     a trial that goes wrong there costs a whole conversation;
//   - only a member still under the MinGraded gate, because a model that has
//     been measured does not need to be measured again to be trusted;
//   - and only once the *opener* is itself past the gate. While the panel is
//     cold the ordinary cheapest-first cascade is already the exploration — it
//     tries the cheap models and escalates what they get wrong. Forced trials
//     are only needed once the router has stopped generating evidence on its
//     own, which is exactly when it has become confident about one model.
func (r *Router) explore(ctx context.Context, class provider.CallClass, order []*rung, schema json.RawMessage) ([]*rung, float64) {
	if len(schema) == 0 || len(order) < 2 {
		return nil, 0
	}
	scored := r.rank(class)
	if ratingOf(scored, order[0]).count < MinGraded {
		return nil, 0
	}
	terminal := order[len(order)-1]

	var chosen ranked
	for _, item := range scored {
		if item.rung == terminal || item.rung == order[0] || item.count >= MinGraded {
			continue
		}
		// Fewest observations first, so the trials spread over the panel rather
		// than pile onto one model; cheaper first among equals, because while
		// nothing is known price is the only thing that is.
		if chosen.rung == nil || item.count < chosen.count ||
			(item.count == chosen.count && item.rung.price < chosen.rung.price) {
			chosen = item
		}
	}
	if chosen.rung == nil || !r.drawExplore(ctx, class) {
		return nil, 0
	}
	opened := []*rung{chosen.rung}
	for _, item := range order {
		if item != chosen.rung {
			opened = append(opened, item)
		}
	}
	return opened, 1.0 / exploreEvery
}

// drawExplore is the coin, and it is not random. The run id, the class and how
// many calls of that class have already been routed are hashed together, so the
// same run over the same ledger explores in the same places — an experiment that
// cannot be re-run is an anecdote, and the A/B this fixes needs re-running.
func (r *Router) drawExplore(ctx context.Context, class provider.CallClass) bool {
	r.mutex.Lock()
	sequence := r.draws[class]
	r.draws[class] = sequence + 1
	r.mutex.Unlock()

	digest := fnv.New64a()
	fmt.Fprintf(digest, "%s\x00%s\x00%d", provider.CacheKeyFrom(ctx), class, sequence)
	return digest.Sum64()%exploreEvery == 0
}

// leaf routes one whole tool loop.
//
// The model is chosen on the first turn and pinned for the rest, because a leaf
// is one lineage: its prefix cache belongs to one model, its transcript is one
// conversation, and swapping models mid-loop would pay to rewrite the cache
// every turn while handing the new model a conversation it did not have. When a
// leaf fails in a way a better model could fix, the scheduler re-runs the whole
// thing at the next attempt number, and that is where escalation happens.
func (r *Router) leaf(ctx context.Context, call *provider.Call, messages []ai.Message, options []ai.Option) (*ai.Response, error) {
	// What is learned about a leaf is keyed on the shape of leaf it was. See
	// provider.WithCallShape: one global exec.leaf rating let five budget stops
	// on one task's oversized leaves reroute the leaves of every other task.
	class := shaped(provider.ClassExecLeaf, call.Shape())
	ladder := r.leafLadder(class)
	index := min(call.Attempt(), len(ladder)-1)
	pick := r.rungOf(call.Pin(ladder[index].spec.Slug), ladder[index])
	// Where the pinned model actually sits, which is not always where the
	// attempt number pointed: a pin set on turn one survives a ledger update
	// that reordered the panel underneath it, and honouring the pin is the point.
	index = indexOf(ladder, pick)
	// What this leaf has already been through. A retried leaf used to log
	// `rung: 1` with an empty escalation, which reads as a first attempt that
	// happened to start high — the one thing the log must not do, since the whole
	// point of recording candidates is that a decision stays analysable after the
	// fact. The ladder is deterministic, so the rungs below the one now serving
	// are exactly the rungs this leaf has already failed on.
	attempt := Event{Class: string(provider.ClassExecLeaf), Shape: call.Shape(),
		Candidates: slugs(ladder), Rung: index, Escalation: slugs(ladder[:index]),
		Model: pick.spec.Slug}

	started := time.Now()
	response, err := pick.client.CompleteWithMessages(pick.affinity(ctx), messages, options...)
	elapsed := time.Since(started)
	if err != nil {
		attempt.Verdict = provider.VerdictProviderFailure
		r.record(ctx, attempt, nil, elapsed)
		return nil, err
	}
	// One row per turn rather than one per leaf. A leaf's cost is the sum of its
	// turns and only the loop knows when the last one was, so the log records
	// what it can see and the verdict row below closes the account.
	attempt.Verdict = provider.VerdictUnverifiedSuccess
	id := r.record(ctx, attempt, response, elapsed)
	resolved, run := resolvedOf(response), provider.CacheKeyFrom(ctx)
	call.Observe(func(final provider.Verdict) {
		r.observe(pick, class, resolved, final)
		r.events.Append(Event{Call: id, Run: run, Class: string(provider.ClassExecLeaf),
			Shape: call.Shape(), Model: pick.spec.Slug,
			Resolved: resolved, Verdict: final, Final: true})
	})
	return response, nil
}

// shaped is the ledger key for a class that is divided into sub-populations. The
// class stays readable — `exec.leaf/oversized` — because a ledger nobody can
// read is a ledger nobody checks.
func shaped(class provider.CallClass, shape string) provider.CallClass {
	if shape == "" {
		return class
	}
	return class + provider.CallClass("/"+shape)
}

// rank reads the whole panel for one class, in the order the operator wrote it.
//
// The one decision it makes is which number counts as a model's ability, and
// that is the gate. A learned rating is used only once it rests on MinGraded
// graded observations; below that the cold-start prior stands, because a rating
// fitted to five outcomes is not a measurement of a model, it is a measurement
// of five outcomes. Arm B is the whole argument: five budget stops on one task,
// against a hundred and three unverified successes that correctly moved nothing,
// were enough to reorder the panel for every leaf of every other task and
// collapse two of them from working to zero.
// The whole panel is read in one acquisition rather than two per rung. Every
// rung in an ordering is then rated as of the same instant, which is what an
// ordering is supposed to mean, and a routed call stops taking the ledger's
// lock twenty times over.
func (r *Router) rank(class provider.CallClass) []ranked {
	queries := make([]Query, 0, len(r.rungs))
	for _, item := range r.rungs {
		queries = append(queries, Query{Slug: item.spec.Slug, Prior: coldStart(item.spec.Role)})
	}
	readings := r.ledger.Read(class, queries)

	scored := make([]ranked, 0, len(r.rungs))
	for index, item := range r.rungs {
		rating, count := readings[index].Rating, readings[index].Count
		if count < MinGraded {
			rating = queries[index].Prior
		}
		scored = append(scored, ranked{rung: item, rating: rating, count: count,
			score: Ability(rating) / item.price})
	}
	return scored
}

// order ranks the panel for one class.
//
// Cheapest-that-works first, by expected success per dollar — the ordering the
// router lab's winning policy used, and the reason that policy landed within
// $0.000067 a task of an omniscient oracle on cost while matching it exactly on
// success. Ties go to the cheaper model, and with nothing measured the whole
// ordering falls back to price, which is how a cold panel generates its own
// evidence: the cheap models get tried, and what they get wrong escalates.
func (r *Router) order(class provider.CallClass) []*rung {
	return pinOpener(seat(r.rank(class)), r.opener, len(r.rungs))
}

// pinOpener seats an explicit choice first without evicting the terminal rung.
// The middle remains the ordinary value ordering, bounded by the same cascade
// budget as every other call.
func pinOpener(order []*rung, opener *rung, available int) []*rung {
	if opener == nil || len(order) == 0 || order[0] == opener {
		return order
	}
	limit := min(maxRungs, available)
	terminal := order[len(order)-1]
	pinned := make([]*rung, 0, limit)
	pinned = append(pinned, opener)
	reserveTerminal := terminal != opener
	for _, item := range order {
		if item == opener || item == terminal {
			continue
		}
		if len(pinned) >= limit || reserveTerminal && len(pinned) == limit-1 {
			break
		}
		pinned = append(pinned, item)
	}
	if reserveTerminal && len(pinned) < limit {
		pinned = append(pinned, terminal)
	}
	return pinned
}

// seat turns a ranking into a cascade: value decides who opens, ability decides
// who finishes.
func seat(scored []ranked) []*rung {
	terminal := terminalOf(scored)

	byValue := append([]ranked(nil), scored...)
	sort.SliceStable(byValue, func(i, j int) bool {
		if byValue[i].score != byValue[j].score {
			return byValue[i].score > byValue[j].score
		}
		return byValue[i].rung.price < byValue[j].rung.price
	})

	limit := min(maxRungs, len(byValue))
	chosen := make([]*rung, 0, limit)
	for _, item := range byValue[:limit] {
		chosen = append(chosen, item.rung)
	}

	// The last rung is the one that has to be right, so it is the panel's
	// ceiling rather than whichever cheap model happened to rank third. That is
	// what makes the cascade's ceiling the best model's rather than the panel's
	// average, and it is the invariant arm B lost: eight of eleven classes went
	// from ending in the panel's strong model to ending in a cheap one, and both
	// tasks that reordered collapsed.
	//
	// It was lost to *success*, which is what makes it worth stating carefully.
	// The old rule promoted whichever model was rated highest, so as the cheap
	// incumbent accumulated positive planning observations its rating climbed
	// past the strong model's cold-start prior, the strong model stopped being
	// the highest rated, and the promotion quietly stopped firing. The panel lost
	// its escalation target precisely as it became confident about the cheap one.
	// So the terminal seat is decided by terminalOf, where the operator's role
	// hint outranks the measurement — see there for why.
	//
	// Two cases leave the value order alone. If the last rung is already the
	// terminal choice there is nothing to seat. And if the terminal choice is
	// also the opening rung, the cascade has opened on its own ceiling:
	// everything below it is both weaker and worse value, so the rungs behind it
	// are a fallback for a malformed reply rather than an escalation, and
	// reordering them would only mean opening on a model known to be worse in
	// both directions.
	if chosen[len(chosen)-1] == terminal.rung || chosen[0] == terminal.rung {
		return chosen
	}
	kept := make([]*rung, 0, limit)
	for _, item := range chosen {
		if item != terminal.rung {
			kept = append(kept, item)
		}
	}
	if len(kept) >= limit {
		kept = kept[:limit-1]
	}
	return append(kept, terminal.rung)
}

// leafLadder is the escalation path for a leaf, and it has at most two rungs.
//
// Rung 0 is the cascade's opener: cost-effectiveness governs the *first* choice,
// because most work does not need the ceiling and a cheap first attempt is the
// entire economics of the panel. Everything after that is governed by ability
// alone, and it goes straight to the top.
//
// Both halves of that are corrections. Escalation used to be `order[attempt]`,
// which climbs cost-effectiveness — and on a barbell panel the second-best value
// is the *weakest* model, so every escalation arm B made went from a model
// measured 18/20 on the anchor battery to one measured 11/20. Escalating to a
// weaker model cannot help by construction. And the ladder stops at two because
// a leaf has no cheap verifier: a retry is a whole conversation rather than one
// request, the scheduler grants exactly one of them, and spending it on a
// near-peer means the panel's strong model is unreachable from a leaf under any
// circumstances. Recovery gets one shot, so it gets the best shot.
//
// Rung 1 is the terminal seat, and it is there only when it is not already the
// opener. When the opener *is* the ceiling the ladder is one rung and a retry
// re-runs the same model, which is a second draw rather than an escalation and
// is the honest thing to offer: there is nowhere stronger to send it.
func (r *Router) leafLadder(class provider.CallClass) []*rung {
	scored := r.rank(class)
	opener := r.opener
	if opener == nil {
		opener = seat(scored)[0]
	}
	ladder := []*rung{opener}
	if terminal := terminalOf(scored); terminal.rung != opener {
		ladder = append(ladder, terminal.rung)
	}
	return ladder
}

// terminalOf picks the rung the cascade must end on.
//
// The operator's role hint is the primary key and the measurement is the
// tie-break, which is the opposite of what the rating is for everywhere else in
// this file. The reason is that a rating can only demote a model somebody has
// measured, and the failure being fixed here was a model nobody had: the panel's
// strong model served zero calls in the entire experiment, so its rating was
// forever its cold-start prior while the cheap incumbent's climbed past it on
// real evidence. Ordering the terminal seat by rating therefore hands it to
// whichever model has been *used* most successfully, which is precisely the model
// the cascade is supposed to be able to escalate away from.
//
// `role: top` is a claim the operator is making about where the ceiling is, and
// the panel is written so that claim is true — bench/routerlab picked its top
// model as "the terminal cascade rung" on an anchor battery, offline, before any
// of this ran. Honouring it costs nothing when it is right and is correctable by
// measuring the model, which is what the exploration budget is for. Within a
// role the measurement decides, and a panel that declares no roles falls through
// to rating and then to the order it was written in — the last model written,
// because a panel is written in trial order and trial order runs the weakest
// first, so the operator's last line is the operator's ceiling. That is the
// mirror of the value ordering's tie-break, which prefers the earlier line for
// the same reason read the other way round.
func terminalOf(scored []ranked) ranked {
	best := scored[0]
	for _, item := range scored[1:] {
		// Not-strictly-better hands the seat over, which is how a tie resolves to
		// the model written later.
		if !betterCeiling(best, item) {
			best = item
		}
	}
	return best
}

// betterCeiling reports whether held is a strictly better ceiling than
// candidate: higher declared role first, measured ability second.
func betterCeiling(held, candidate ranked) bool {
	if role, other := coldStart(held.rung.spec.Role), coldStart(candidate.rung.spec.Role); role != other {
		return role > other
	}
	return held.rating > candidate.rating
}

// ranked is one rung with what is known about it, for the length of one ordering.
type ranked struct {
	rung   *rung
	rating float64 // gated: the learned rating, or the cold-start prior below MinGraded
	count  int     // graded observations behind the learned rating
	score  float64
}

func ratingOf(scored []ranked, target *rung) ranked {
	for _, item := range scored {
		if item.rung == target {
			return item
		}
	}
	return ranked{rung: target}
}

// observe folds one attempt's verdict into the ledger, against the snapshot the
// provider says actually served it.
func (r *Router) observe(pick *rung, class provider.CallClass, resolved string, verdict provider.Verdict) {
	model := pick.spec.Slug
	if resolved != "" && resolved != model {
		r.ledger.Alias(model, resolved)
		model = resolved
	}
	r.ledger.Observe(model, class, coldStart(pick.spec.Role), verdict)
}

// record fills in what only the router and the response know — the identity, the
// run, the cost — and writes the row. It returns the id later rows join on.
func (r *Router) record(ctx context.Context, event Event, response *ai.Response, elapsed time.Duration) string {
	event.Call = callID()
	event.Run = provider.CacheKeyFrom(ctx)
	event.Resolved = resolvedOf(response)
	event.LatencyMS = elapsed.Milliseconds()
	if response != nil && response.Usage != nil {
		event.PromptTokens = response.Usage.PromptTokens
		event.CompletionTokens = response.Usage.CompletionTokens
		event.CachedTokens = response.Usage.CacheReadTokens()
		if response.Usage.Cost != nil {
			event.Cost = *response.Usage.Cost
		}
	}
	r.events.Append(event)
	return event.Call
}

// rungOf finds the rung for a pinned slug, falling back to the one just chosen.
// The fallback is not defensive padding: a pin set by an earlier turn survives a
// ledger update that reordered the panel underneath it, and honouring the pin is
// the point.
func (r *Router) rungOf(slug string, fallback *rung) *rung {
	for _, item := range r.rungs {
		if item.spec.Slug == slug {
			return item
		}
	}
	return fallback
}

// affinity re-derives the run's cache key for this model.
//
// One run is one cache lineage *per model*, never one shared across them. A
// prefix written by a cheap first rung is not a prefix the escalation target can
// read, and handing both the same key asks the provider to keep two different
// conversations warm on one instance.
func (u *rung) affinity(ctx context.Context) context.Context {
	key := provider.CacheKeyFrom(ctx)
	if key == "" {
		return ctx
	}
	return provider.WithCacheKey(ctx, provider.RunCacheKey(key, u.spec.Slug))
}

// verify is the cascade's whole economics in one function: it decides, for
// nothing, whether the cheap attempt worked.
//
// It checks only what it can check without another model call — that something
// came back, that it parses, and that it carries the fields the schema said were
// required. Whether the parsed answer is *correct* is a different question and
// belongs to the call site, which is the only thing that knows what correct
// means here. An empty reply is called out separately because it is not a
// malformed answer, it is the runaway-reasoning mode: full price, nothing
// delivered, and the probe lab's single largest failure class.
func verify(response *ai.Response, schema json.RawMessage) provider.Verdict {
	text := strings.TrimSpace(response.Text())
	if text == "" {
		return provider.VerdictEmptyResponse
	}
	if len(schema) == 0 {
		return ""
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(unfence(text)), &decoded); err != nil {
		return provider.VerdictFormatFailure
	}
	for _, field := range requiredOf(schema) {
		if _, present := decoded[field]; !present {
			return provider.VerdictFormatFailure
		}
	}
	return ""
}

// schemaOf recovers the schema a call asked for. The options are closures, so
// the only way to see what they do is to let them do it — to a throwaway
// request, which costs nothing and saves the router from keeping its own copy of
// every call site's schema in sync with the call site's.
func schemaOf(options []ai.Option) json.RawMessage {
	request := &ai.Request{}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil
		}
	}
	if request.ResponseFormat == nil || request.ResponseFormat.JSONSchema == nil {
		return nil
	}
	return request.ResponseFormat.JSONSchema.Schema
}

func requiredOf(schema json.RawMessage) []string {
	var decoded struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		return nil
	}
	return decoded.Required
}

// unfence strips a markdown code fence, mirroring what the planner's own decoder
// tolerates. The two must agree: a router that rejected a fenced answer the
// caller would have accepted would escalate a call that was about to succeed.
func unfence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	if start := strings.Index(trimmed, "\n"); start >= 0 {
		trimmed = trimmed[start+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), "```"))
}

// resolvedOf reads which model actually served the call. For a floating alias
// this is a dated snapshot and it is what the ledger keys on: `-latest` is a
// different set of weights every few weeks, and a rating pooled across two of
// them measures neither.
func resolvedOf(response *ai.Response) string {
	if response == nil {
		return ""
	}
	return strings.TrimSpace(response.Model)
}

func indexOf(rungs []*rung, target *rung) int {
	for index, item := range rungs {
		if item == target {
			return index
		}
	}
	return len(rungs) - 1
}

func slugs(rungs []*rung) []string {
	names := make([]string, 0, len(rungs))
	for _, item := range rungs {
		names = append(names, item.spec.Slug)
	}
	return names
}
