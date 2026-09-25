package crewroute

import (
	_ "embed"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// WHAT A SEAT IS WORTH, BEFORE THIS INSTALL HAS RUN ANYTHING.
//
// The router needs, for every class of work, every seat and every model it
// might sit there, two numbers: how much that model in that seat adds to the
// work's quality, and what it costs. Both are read off the model's own
// catalog row through fitted weights (prior.json, embedded). No model has a
// row of its own there: every model, whoever makes it, is scored the same way.
//
//  1. ABILITY FROM METADATA. A model's ability is a latent number predicted
//     from the fields its catalog row carries: log prompt and completion
//     prices, log context length, release date, open weights, the published
//     intelligence, coding and agentic indexes, and the design-arena Elo. The
//     weights are a joint Gaussian over the ability and those fields
//     ([weights.Mean], [weights.Cov]); a row is scored by conditioning on the
//     fields it HAS. A missing field is not guessed at a fixed value: it is
//     integrated out, and the ability's variance is wider for it. A model's
//     product family ([familyOf]) then moves the mean by the family's fitted
//     offset and narrows the variance.
//
//  2. ABILITY TO A SEAT. The ability is mapped to an expected solve rate u in
//     (0, 1) ([weights.Ability]), and each (class, seat) reads u through its
//     own linear link: quality = level + slope·(u − u_ref), on the 0–10 crew
//     scale ([seatLink]). The slope is what the seat pays for ability in that
//     class of work; its standard deviation is how sure the fit is of it.
//
//  3. THIS INSTALL'S OUTCOMES. What a person kept or redid, per class, seat
//     and model, moves the quality by a bounded amount ([Request.Learned]).
//
// A model the weights cannot say enough about — its variance barely below the
// prior's, because its row carries almost nothing ([weights.EvidenceMaxRatio])
// — sits no seat unless a person pins it. Nor does a model whose ability's
// upper bound falls under the weakest ability the weights credit with doing
// the work ([weights.UFloor]).
//
// The cost of a seat is the seat's token shape — how much it reads fresh, how
// much it reads back from a warm cache, how much it writes on an ordinary task
// — priced at the route's published per-token prices and scaled per class and
// seat ([weights.CostScale]).

//go:embed prior.json
var priorJSON []byte

// shape is one seat's tokens on an ordinary task.
type shape struct {
	Prompt     float64 `json:"prompt"`
	Cached     float64 `json:"cached"`
	Completion float64 `json:"completion"`
}

// seatLink is one (class, seat)'s link from ability to quality.
type seatLink struct {
	Level   float64 `json:"level"`
	Slope   float64 `json:"slope"`
	SlopeSD float64 `json:"slope_sd"`
}

// classLink is one class's links, around the ability u_ref they are centred on.
type classLink struct {
	URef  float64           `json:"u_ref"`
	Seats map[Seat]seatLink `json:"seats"`
}

// weights is prior.json read.
type weights struct {
	Knee float64 `json:"knee_per_usd"`
	// Features names the metadata columns after the ability, in the order of
	// Loc, Scale, Mean and Cov (whose index 0 is the ability).
	Features []string    `json:"features"`
	Loc      []float64   `json:"loc"`
	Scale    []float64   `json:"scale"`
	Mean     []float64   `json:"mean"`
	Cov      [][]float64 `json:"cov"`
	// VarScale calibrates the conditional variance.
	VarScale float64 `json:"var_scale"`
	// FamilyRho is the share of residual variance a family explains, and
	// Families each family's offset (in residual standard deviations) and the
	// variance left in that offset.
	FamilyRho float64               `json:"family_rho"`
	Families  map[string][2]float64 `json:"families"`
	// Ability maps the latent ability to an expected solve rate:
	// u = σ(A·θ + B).
	Ability struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	} `json:"ability"`
	// UFloor is the least solve rate a model's upper bound must reach to sit
	// a seat.
	UFloor float64 `json:"u_floor"`
	// EvidenceMaxRatio is the most the ability's variance may keep of the
	// prior's for a model to be scored at all.
	EvidenceMaxRatio float64                    `json:"evidence_max_ratio"`
	Link             map[Class]classLink        `json:"link"`
	Shapes           map[Seat]shape             `json:"shapes"`
	CostScale        map[Class]map[Seat]float64 `json:"cost_scale"`
	index            map[string]int             // feature name → column
	cols             [len(featureNames)]int     // featureNames' columns, zero for one the weights lack
	priorVar         float64                    // the ability's own variance, standardised
}

// table is one decision's view of the weights: the weights, the abilities
// already read this decision, this install's learned moves, and the cost floor
// of each seat over the candidates in front of it.
type table struct {
	*weights
	learned map[string]float64
	floors  map[Seat]float64
	// priced is [table.pricedLadder], built from candidates on first use.
	priced     map[Seat][]pricePoint
	candidates []Candidate
	cache      map[string]ability
	// rescue is a table asked for a seat's last rungs ([Request.Rescue]).
	rescue bool
}

var (
	loaded   *weights
	loadOnce sync.Once
)

// load reads the weights once. Weights that do not parse are a build that
// must not ship, so it panics at first use the way a malformed embedded asset
// does everywhere else in this tree, and a test reads it on every run.
func load() *weights {
	loadOnce.Do(func() {
		var w weights
		if err := json.Unmarshal(priorJSON, &w); err != nil {
			panic("crewroute: prior.json: " + err.Error())
		}
		n := len(w.Features) + 1
		if len(w.Loc) != n || len(w.Scale) != n || len(w.Mean) != n || len(w.Cov) != n {
			panic("crewroute: prior.json: the ability model's dimensions disagree")
		}
		w.index = map[string]int{}
		for i, f := range w.Features {
			w.index[f] = i + 1
		}
		for i, f := range featureNames {
			w.cols[i] = w.index[f]
		}
		w.priorVar = w.Cov[0][0]
		if w.VarScale <= 0 {
			w.VarScale = 1
		}
		loaded = &w
	})
	return loaded
}

// prior is a fresh decision's table: nothing learned, no candidates seen.
func prior() *table {
	w := load()
	return &table{weights: w, cache: map[string]ability{}}
}

// forDecision is the table one decision reads: this install's learned moves
// and each seat's cost floor over the candidates.
func forDecision(candidates []Candidate, learned map[string]float64) *table {
	t := prior()
	t.cache = make(map[string]ability, len(candidates))
	t.learned = learned
	t.floors = t.floorsOf(candidates)
	return t
}

// ability is what the weights say about one model: the solve rate u, its
// variance, the latent ability's variance, and whether the row carried
// enough to be scored at all — with the model's lineage and whether it is a
// quantised copy, read once.
type ability struct {
	U, VarU  float64
	VarTheta float64
	Known    bool
	lineage  string
	quant    bool
}

// abilityOf reads one model's ability, once per decision.
func (t *table) abilityOf(m Model) ability {
	if a, ok := t.cache[m.ID]; ok {
		return a
	}
	a := remembered(t.weights, m)
	if t.cache != nil {
		t.cache[m.ID] = a
	}
	return a
}

// The abilities of the rows seen lately, keyed by the whole row: a row that
// changes — a new price, an index published — is a new key and is read
// again. The memo is dropped whole when it grows past memoRows.
var (
	memoMu sync.Mutex
	memo   = map[Model]ability{}
)

const memoRows = 8192

// remembered is [weights.abilityOf] through the memo.
func remembered(w *weights, m Model) ability {
	memoMu.Lock()
	a, ok := memo[m]
	memoMu.Unlock()
	if ok {
		return a
	}
	a = w.abilityOf(m)
	memoMu.Lock()
	if len(memo) >= memoRows {
		memo = map[Model]ability{}
	}
	memo[m] = a
	memoMu.Unlock()
	return a
}

// featureNames are the metadata fields the router can read off a model, in
// the order [weights.features] reads them.
var featureNames = [...]string{"lp_in", "lp_out", "lctx", "date", "open", "aa_int", "aa_cod", "aa_ag", "elo"}

// features is a model's metadata in the weights' columns, NaN where the row
// does not carry the field.
func (w *weights) features(m Model) []float64 {
	x := make([]float64, len(w.Features))
	for i := range x {
		x[i] = math.NaN()
	}
	open := 0.0
	if m.Open {
		open = 1
	}
	values := [len(featureNames)]float64{
		math.Log10(m.PromptPrice * 1e6), math.Log10(m.CompletionPrice * 1e6), math.Log2(float64(m.Context)),
		yearsSince2024(m.Released), open, m.Intelligence, m.Coding, m.Agentic, m.ArenaElo,
	}
	has := [len(featureNames)]bool{
		m.PromptPrice > 0, m.CompletionPrice > 0, m.Context > 0, !m.Released.IsZero(), true,
		m.Intelligence > 0, m.Coding > 0, m.Agentic > 0, m.ArenaElo > 0,
	}
	for i, col := range w.cols {
		if col > 0 && has[i] {
			x[col-1] = values[i]
		}
	}
	return x
}

// yearsSince2024 is a release date as the weights read it.
func yearsSince2024(at time.Time) float64 {
	return at.Sub(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)).Hours() / 24 / 365.25
}

// abilityOf conditions the joint Gaussian on the fields a row carries, then
// adds the family's offset and maps the ability to a solve rate.
func (w *weights) abilityOf(m Model) ability {
	x := w.features(m)
	var obs [maxFeatures]int
	var z, so0 [maxFeatures]float64
	var soo [maxFeatures][maxFeatures]float64
	k := 0
	for i, v := range x {
		if !math.IsNaN(v) && k < maxFeatures {
			col := i + 1
			obs[k] = col
			z[k] = (v-w.Loc[col])/w.Scale[col] - w.Mean[col]
			k++
		}
	}
	for i := 0; i < k; i++ {
		for j := 0; j < k; j++ {
			soo[i][j] = w.Cov[obs[i]][obs[j]]
		}
		so0[i] = w.Cov[obs[i]][0]
	}
	mean, variance := w.Mean[0], w.priorVar
	// K = S_0o S_oo⁻¹: solve S_oo k = S_o0.
	if gain, ok := solveSPD(&soo, &so0, k); ok {
		for i := 0; i < k; i++ {
			mean += gain[i] * z[i]
			variance -= gain[i] * so0[i]
		}
	}
	variance = math.Max(variance, 1e-6)
	known := variance/w.priorVar <= w.EvidenceMaxRatio+1e-9
	canon := CanonicalOf(m.ID)
	theta := mean*w.Scale[0] + w.Loc[0]
	v := variance * w.Scale[0] * w.Scale[0]
	if off, ok := w.Families[familyKey(canon.ID)]; ok {
		theta += math.Sqrt(v) * off[0]
		v *= 1 - w.FamilyRho + off[1]
	}
	v *= w.VarScale
	a, b := w.Ability.A, w.Ability.B
	u := sigmoid((a*theta + b) / math.Sqrt(1+math.Pi*a*a*v/8))
	d := a * u * (1 - u)
	return ability{U: u, VarU: d * d * v, VarTheta: v, Known: known, lineage: canon.String(), quant: canon.Variant != ""}
}

// maxFeatures bounds the metadata columns the ability is read from.
const maxFeatures = 16

// solveSPD solves A x = b for the leading n×n block of a small symmetric
// positive-definite A by Cholesky, false when it is not.
func solveSPD(a *[maxFeatures][maxFeatures]float64, b *[maxFeatures]float64, n int) ([maxFeatures]float64, bool) {
	var l [maxFeatures][maxFeatures]float64
	var y, x [maxFeatures]float64
	if n == 0 {
		return x, false
	}
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			sum := a[i][j]
			for k := 0; k < j; k++ {
				sum -= l[i][k] * l[j][k]
			}
			if i == j {
				if sum <= 0 {
					return x, false
				}
				l[i][i] = math.Sqrt(sum)
			} else {
				l[i][j] = sum / l[j][j]
			}
		}
	}
	for i := 0; i < n; i++ {
		sum := b[i]
		for k := 0; k < i; k++ {
			sum -= l[i][k] * y[k]
		}
		y[i] = sum / l[i][i]
	}
	for i := n - 1; i >= 0; i-- {
		sum := y[i]
		for k := i + 1; k < n; k++ {
			sum -= l[k][i] * x[k]
		}
		x[i] = sum / l[i][i]
	}
	return x, true
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// familyOf is a model's product family: its vendor and its name with the
// version numbers taken out — `z-ai/glm-5.3-flash` is `z-ai/glm-flash`, and a
// token such as `v4`, `k3` or `30b` that is mostly a number is dropped whole.
// A new version of a family starts from what the family's earlier versions
// were fitted to.
func familyOf(id string) string { return familyKey(CanonicalOf(id).ID) }

// familyKey is [familyOf] on an id already read by [CanonicalOf].
func familyKey(canon string) string {
	vendor, name := "", canon
	if at := strings.LastIndex(canon, "/"); at >= 0 {
		vendor, name = canon[:at+1], canon[at+1:]
	}
	var keep []string
	for _, tok := range strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '.' }) {
		stripped := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return -1
			}
			return r
		}, tok)
		if stripped != tok && len(stripped) <= 2 {
			continue
		}
		if stripped != "" {
			keep = append(keep, stripped)
		}
	}
	return vendor + strings.Join(keep, "-")
}

// linkOf is a class's link, the average of the others for a class the weights
// do not carry.
func (t *table) linkOf(class Class) classLink {
	if l, ok := t.Link[class]; ok {
		return l
	}
	return t.Link[Other]
}

// quality is what one model adds in one seat for one class of work — the
// link's mean at the model's ability, plus what this install learned — and
// its standard deviation.
func (t *table) quality(class Class, seat Seat, m Model) (q, sd float64) {
	return t.qualityOf(class, seat, t.abilityOf(m))
}

// qualityOf is [table.quality] on an ability already read.
func (t *table) qualityOf(class Class, seat Seat, a ability) (q, sd float64) {
	link := t.linkOf(class)
	s := link.Seats[seat]
	du := a.U - link.URef
	q = s.Level + s.Slope*du
	sd = math.Sqrt(s.Slope*s.Slope*a.VarU + du*du*s.SlopeSD*s.SlopeSD)
	if a.quant {
		// A QUANTISED LOCAL COPY is credited with a share of its model's
		// quality: the weights read the model's row, and the copy is squeezed.
		q *= quantDiscount
	}
	if len(t.learned) > 0 {
		q += t.learned[learnKey(class, seat, a.lineage)]
	}
	return math.Max(0, math.Min(10, q)), sd
}

// credible is whether a model may sit a seat: its row says enough to score
// it, and the upper bound of its ability reaches the weakest ability the
// weights credit with doing the work.
func (t *table) credible(m Model) bool { return t.credibleAt(t.abilityOf(m)) }

// credibleAt is [table.credible] on an ability already read.
func (t *table) credibleAt(a ability) bool {
	return a.Known && a.U+2*math.Sqrt(a.VarU) >= t.UFloor
}

// pricePoint is one ability on a seat's price ladder and the least a
// credible priced model at least that able costs there.
type pricePoint struct {
	U, Cost float64
}

// floorsOf is each seat's cost floor over the candidates: the cheapest flat
// cost of a credible model with a published price. It keeps the candidates
// for the price ladder [table.priceFor] builds when an unpriced model asks.
func (t *table) floorsOf(candidates []Candidate) map[Seat]float64 {
	floors := map[Seat]float64{}
	t.candidates = candidates
	for _, c := range candidates {
		if c.Model.PromptPrice <= 0 && c.Model.CompletionPrice <= 0 {
			continue
		}
		if !t.credibleAt(t.abilityOf(c.Model)) {
			continue
		}
		for _, seat := range Seats {
			if !seatable(seat, c) {
				continue
			}
			cost := t.seatCost(seat, c.Model)
			if cost <= 0 {
				continue
			}
			if held, ok := floors[seat]; !ok || cost < held {
				floors[seat] = cost
			}
		}
	}
	return floors
}

// pricedLadder is, per seat, the credible priced candidates by ability, best
// first, each with the least cost of any at least that able.
func (t *table) pricedLadder() map[Seat][]pricePoint {
	if t.priced != nil {
		return t.priced
	}
	t.priced = map[Seat][]pricePoint{}
	for _, c := range t.candidates {
		if c.Model.PromptPrice <= 0 && c.Model.CompletionPrice <= 0 {
			continue
		}
		a := t.abilityOf(c.Model)
		if !t.credibleAt(a) {
			continue
		}
		for _, seat := range Seats {
			if cost := t.seatCost(seat, c.Model); cost > 0 && seatable(seat, c) {
				t.priced[seat] = append(t.priced[seat], pricePoint{U: a.U, Cost: cost})
			}
		}
	}
	for seat, points := range t.priced {
		sort.Slice(points, func(i, j int) bool { return points[i].U > points[j].U })
		for i := 1; i < len(points); i++ {
			points[i].Cost = math.Min(points[i].Cost, points[i-1].Cost)
		}
		t.priced[seat] = points
	}
	return t.priced
}

// costFloor is the least flat cost of a credible priced model in a seat.
func (t *table) costFloor(seat Seat) float64 { return t.floors[seat] }

// priceFor is what a model that publishes no price is weighed at in a seat:
// A PRICE OF ZERO IS NOT EVIDENCE OF VALUE, so it is the least any credible
// priced model at least as able costs there — or, when none is as able, the
// dearest of them.
func (t *table) priceFor(seat Seat, u float64) float64 {
	points := t.pricedLadder()[seat]
	if len(points) == 0 {
		return 0
	}
	// points run best first; the last one at least as able holds the least
	// cost among all at least as able.
	at := sort.Search(len(points), func(i int) bool { return points[i].U < u })
	if at == 0 {
		dearest := 0.0
		for _, p := range points {
			dearest = math.Max(dearest, p.Cost)
		}
		return dearest
	}
	return points[at-1].Cost
}

// seatCost is what one model is expected to cost in one seat on an ordinary
// task at the prices it publishes, scaled for the seat. A route that bills
// nothing per token (a subscription, a local model) is priced by the caller,
// not here. Classes share the flat shape; [table.classCost] scales it.
func (t *table) seatCost(seat Seat, m Model) float64 {
	s := t.Shapes[seat]
	cached := m.CacheReadPrice
	if cached <= 0 {
		// A provider that publishes no cache-read price is paid the prompt
		// price on what it reads back — the dearer reading, never a free one.
		cached = m.PromptPrice
	}
	return s.Prompt*m.PromptPrice + s.Cached*cached + s.Completion*m.CompletionPrice
}

// classCost is a seat's expected dollars for one task of a class: the flat
// shape's cost times the class and seat's fitted scale.
func (t *table) classCost(class Class, seat Seat, m Model) float64 {
	return t.seatCost(seat, m) * t.costScale(class, seat)
}

// costScale is a class and seat's fitted ratio of dollars to the flat shape,
// one when the weights carry none.
func (t *table) costScale(class Class, seat Seat) float64 {
	if byseat, ok := t.CostScale[class]; ok {
		if s := byseat[seat]; s > 0 {
			return s
		}
	}
	if s := t.CostScale[Other][seat]; s > 0 {
		return s
	}
	return 1
}

// LearnKey is the key a learned quality move is kept under: the class, the
// seat and the model's lineage.
func LearnKey(class Class, seat Seat, model string) string {
	return learnKey(class, seat, Lineage(model))
}

// learnKey is [LearnKey] on a lineage already read.
func learnKey(class Class, seat Seat, lineage string) string {
	return string(class) + "\x00" + string(seat) + "\x00" + lineage
}

// learnedOf is the learned move for one model in one seat, zero for none.
func (t *table) learnedOf(class Class, seat Seat, a ability) float64 {
	if len(t.learned) == 0 {
		return 0
	}
	return t.learned[learnKey(class, seat, a.lineage)]
}

// Lineage is the id a model's quality is kept under: its canonical identity
// across every provider's spelling ([CanonicalOf]) — lowercase, a provider's
// namespace and a route suffix (`:free`, `:nitro`) taken off, a thinking
// level, a floating alias's `~` and `-latest`, and a dated snapshot suffix
// taken off. `deepseek/deepseek-v4-flash-0731` is the same lineage as
// `deepseek/deepseek-v4-flash`, and so is its free pool: a route is not a
// model. A quantised local copy keeps its variant after `@`, so it is never
// mistaken for the model it was squeezed from.
func Lineage(id string) string { return CanonicalOf(id).String() }

// lineageTail is [Lineage]'s own rules on an id already read by
// [CanonicalOf]: `-latest` and a dated snapshot suffix taken off.
func lineageTail(id string) string {
	id = strings.TrimSuffix(id, "-latest")
	if at := strings.LastIndex(id, "-"); at > 0 {
		if tail := id[at+1:]; (len(tail) == 4 || len(tail) == 8) && allDigits(tail) {
			id = id[:at]
		}
	}
	return id
}

// allDigits is whether a word is made of ASCII digits only.
func allDigits(word string) bool {
	for i := 0; i < len(word); i++ {
		if word[i] < '0' || word[i] > '9' {
			return false
		}
	}
	return word != ""
}

// Scorable is whether the weights can score a model at all from what its row
// carries — enough metadata for a finite-variance ability — for a caller that
// offers models and wants to say which the router may pick unpinned.
func Scorable(m Model) bool { return load().abilityOf(m).Known }

// Knee is the default price of a quality point, in points per dollar — see
// [Route] for why this is the knee of the quality-cost front.
func Knee() float64 { return load().Knee }
