package lane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── THE SHEET: A PRIOR NOBODY HAD TO PAY FOR ────────────────────────────────
//
// OpenRouter publishes, per model, one row per lane: first-token latency and
// throughput at the 50th, 75th, 90th and 99th percentiles over the last half
// hour, plus uptime, tariff, quantization, context and output ceilings, and
// whether the lane takes tool calls. That is a free prior for every lane of
// every model, which is why nothing in this design is blind on the first call
// of a process.
//
// ── THE TWO HALVES, AND WHY THEY ARE NOT ONE METHOD ─────────────────────────
//
// [Sheet.Rows] reads a map and returns. It never opens a connection, never
// touches a file after the first question about a model, and never blocks on
// anything but a read lock, because it is called from the encoder that runs
// immediately before a send. [Sheet.Refresh] goes to the network and belongs
// to [Beat] and to nothing else. A missing sheet is "no prior, use the belief
// alone"; it is never a reason to make somebody wait.
//
// ── WHY THE ROWS ARE DECODED ONE AT A TIME ──────────────────────────────────
//
// This is a third party's schema and it drifts. Decoded whole, one lane that
// grew a field of a shape this struct does not expect would fail the whole
// decode and the sheet would degrade from seventeen lanes to none — which the
// catalog reader in this repo learned the hard way and solved the same way
// (internal/catalog/catalog.go). A row that cannot be read is skipped and every
// row that can be read still arrives.
//
// ── WHY THERE IS NO net/http HERE ───────────────────────────────────────────
//
// A structural test in this directory fails the build when this package so much
// as imports a transport, and it is right to: a package that could open a
// connection is a package where somebody will eventually open one on the send
// path. So the one thing this file cannot own is injected as [Fetcher], and the
// transport package hands it in at session open through [WireSheet].

// ErrNoSheet is what a refresh returns while no sheet client is wired in. It is
// an error rather than a silent success because a beat that thinks it fetched
// is a beat nobody will ever notice is dead.
var ErrNoSheet = errors.New("lane: no sheet client")

// Fetcher is the connection this package may not open for itself.
//
// It is spelled in terms of a URL and a bearer key rather than in terms of a
// request and a response, because naming those types here would mean importing
// the transport this package is forbidden to know about. The implementation
// lives beside the router client, applies the same timeout the catalog reader
// uses, and turns a status that is not a success into an error — everything
// below this line only ever sees bytes or a reason there are none.
type Fetcher interface {
	Fetch(ctx context.Context, url, bearer string) (io.ReadCloser, error)
}

// maxSheetBytes is the ceiling on one sheet. Seventeen lanes of a large model
// come to about forty kilobytes, so a megabyte is two orders of magnitude of
// headroom and still a bound: an unbounded read of a third party's body is how
// one bad response becomes this process's memory problem.
const maxSheetBytes = 1 << 20

// sheetTTL is how old a cached sheet may be before [Beat] fetches at open. It
// is the same five minutes the beat runs on, so a session that opens a minute
// after the last one closed starts from that session's reading rather than
// paying for the same aggregate twice.
const sheetTTL = 5 * time.Minute

// Wanter is the optional half of a [Sheet]: one that can be ASKED about a model
// without being made to fetch on the spot.
//
// It is a SECOND interface rather than a third method on [Sheet] for the reason
// [Prober] is one: queueing is a thing the live sheet has and a fixture in a
// bench does not, and a sheet that does not offer one makes the capability
// ABSENT rather than present and failing. Nothing here waits, ever.
type Wanter interface {
	// Wants queues one model for the next beat and returns at once.
	Wants(model string)
}

// Queue is the other end of that channel, which is [Beat]'s alone.
type Queue interface {
	// Wanted is the models that have been asked for and not yet fetched.
	Wanted() <-chan string
}

// Roster is the optional half of a [Sheet] that can name every lane it has ever
// seen, across every model. It is what gives a never-seen model somewhere to
// borrow a provider-level belief from.
type Roster interface {
	Roster() []string
}

// wantedDepth is how many one-shot refreshes may be waiting at once.
//
// It is small deliberately. The queue holds MODELS A SESSION IS REALLY TALKING
// TO — the two config slots, plus whatever a person picks — and a build that
// had eight of those waiting at once has a beat that is not running rather than
// a queue that is too short. A full queue drops the name and forgets the claim,
// so the next request asks again; nothing waits, and nothing is lost for good.
const wantedDepth = 8

// sheet holds one map of rows per model and the means to refill it.
//
// The lock is a read-write one for the reason the whole file exists: Rows is on
// the send path and Refresh is not, and a beat that is decoding forty
// kilobytes must never be something a request waits behind.
type sheet struct {
	mu sync.RWMutex
	// base is the router's URL with its `/api/v1` already on it, key the
	// bearer, and fetch the thing that can open a connection. All three are
	// empty until [WireSheet], and a sheet without them fetches nothing and
	// says so.
	base  string
	key   string
	fetch Fetcher
	// rows, tags and at are what is known, by model. tags carries the router's
	// own slug for a lane, which [Row] has nowhere to put and which the cache
	// keeps anyway so that a later reader of the file loses nothing.
	rows map[string][]Row
	tags map[ID]string
	at   map[string]time.Time
	// looked records the models whose cache file has already been read, so
	// that a cold miss costs one stat and not one per question.
	looked map[string]bool
	// want is the ONE-SHOT QUEUE and asked is what has already been put on it.
	//
	// A model nobody has a sheet for is discovered on the send path — the
	// chooser asks for rows and gets none — and the send path may not fetch.
	// So it leaves a name here and answers from the hierarchy meanwhile, and
	// [Beat] picks the name up on the other side of the channel. The claim in
	// `asked` is what makes it one-shot: a cold model costs one fetch and not
	// one per keystroke.
	want  chan string
	asked map[string]bool
	// dir overrides where the cache lives, for a test that must not write into
	// the person's own state root. Empty is the real place.
	dir string
}

// newSheet builds the sheet the process starts with: it holds nothing, it can
// fetch nothing until it is wired, and it says so. It is called from the
// registry and nowhere else.
func newSheet() *sheet {
	return &sheet{
		rows:   map[string][]Row{},
		tags:   map[ID]string{},
		at:     map[string]time.Time{},
		looked: map[string]bool{},
		want:   make(chan string, wantedDepth),
		asked:  map[string]bool{},
	}
}

// WireSheet hands the live sheet the two facts it cannot know and the one thing
// this package may not own: where the router is, who we are, and something that
// can open a connection. It is called once, at session open, before the beat
// starts, and it reports whether the live sheet was one this package built —
// a bench that installed a sheet of its own is left alone.
func WireSheet(base, key string, fetch Fetcher) bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return false
	}
	own.wire(base, key, fetch)
	return true
}

// wire points a sheet at a router.
func (s *sheet) wire(base, key string, fetch Fetcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base, s.key, s.fetch = strings.TrimSuffix(strings.TrimSpace(base), "/"), strings.TrimSpace(key), fetch
}

// cacheIn moves this sheet's cache directory. It exists for tests, which must
// never write into the state root of whoever is running them.
func (s *sheet) cacheIn(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir = dir
}

// Rows is what is known about model's lanes right now.
//
// The first question about a model reads its cache file, which is a disk read
// and happens once; every question after it is a map lookup under a read lock.
// That one read is what makes a cold process useful before its first beat has
// finished, and it is deliberately not a fetch.
func (s *sheet) Rows(model string) []Row {
	// A TIER IS NOT A DEPLOYMENT: `model:high` and `model` are one endpoints
	// page, and the router serves it under the bare id. See [BareModel].
	model = BareModel(model)
	if model == "" {
		return nil
	}
	s.mu.RLock()
	rows, looked := s.rows[model], s.looked[model]
	s.mu.RUnlock()
	if !looked {
		rows = s.warm(model)
	}
	if len(rows) == 0 {
		return nil
	}
	return append([]Row(nil), rows...)
}

// warm reads model's cache file once and remembers that it did, whether or not
// there was one. A machine that has never fetched this model must not stat its
// cache on every request that mentions it.
func (s *sheet) warm(model string) []Row {
	cached, tags, at, err := readCache(s.cachePath(model))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.looked[model] = true
	if err != nil || len(cached) == 0 {
		return s.rows[model]
	}
	// A refresh that landed while the file was being read is the fresher
	// account of the two, and it wins.
	if _, held := s.rows[model]; !held {
		s.rows[model], s.at[model] = cached, at
		for id, tag := range tags {
			s.tags[id] = tag
		}
	}
	return s.rows[model]
}

// freshness is how long ago model's rows were fetched, false when this sheet
// has no rows for it. [Beat] asks so that a session opening a minute after the
// last one closed does not pay for the same half-hour aggregate twice.
func (s *sheet) freshness(model string, now time.Time) (time.Duration, bool) {
	if s.Rows(model) == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	at, ok := s.at[model]
	if !ok || at.IsZero() {
		return 0, false
	}
	return now.Sub(at), true
}

// ── ASKING FOR A SHEET WITHOUT WAITING FOR ONE ──────────────────────────────
//
// TWO MOMENTS DISCOVER A MODEL NOBODY HAS A SHEET FOR, and neither of them may
// fetch. The chooser asks for rows immediately before a send and gets none; a
// person picks a model in the picker, which is a keystroke. Both leave the name
// here and carry on — the chooser answers from the hierarchy, the picker
// returns — and [Beat] does the fetching on the other side of the channel,
// where fetching has always belonged.
//
// NOTHING EVER WAITS FOR ONE. The send is a non-blocking send on a buffered
// channel: a full queue drops the name and forgets the claim so a later ask can
// make it again, which is the same bargain the prober strikes with its own rate
// limit (probe.go). A build with no beat running simply never fetches, exactly
// as it never did.

// Wants queues one model for the beat to fetch once, and returns at once.
//
// IT IS THE LEDGER'S NAME FOR THE MODEL AND NOT THE PICKER'S. A person picks a
// spelling, and a floating alias has no endpoints page of its own — the router
// publishes one under the id it currently resolves to. Queued as typed, the
// beat fetches a page that does not exist and the session pays for a refusal;
// queued folded, the sheet lands under the key the sighting side is already
// filing beliefs on. See [LedgerModel].
func (s *sheet) Wants(model string) {
	model = LedgerModel(model)
	if model == "" {
		return
	}
	s.mu.Lock()
	claimed := s.asked[model]
	if !claimed {
		s.asked[model] = true
	}
	s.mu.Unlock()
	if claimed {
		return
	}
	select {
	case s.want <- model:
	default:
		// The queue is full, so the claim is given back: a name dropped here is
		// a name the next request may ask for again, and a claim kept over a
		// name nobody enqueued would be a model that is never fetched at all.
		s.mu.Lock()
		delete(s.asked, model)
		s.mu.Unlock()
	}
}

// Wanted is the queue [Beat] drains.
func (s *sheet) Wanted() <-chan string { return s.want }

// Roster is every lane this sheet has ever named, across every model, in a
// stable order.
//
// IT IS THE PROVIDER LEVEL OF THE HIERARCHY, SPELLED AS NAMES. A model nobody
// has measured is still served by machines this process has seen serving
// something else, and `a[lane]` is exactly the belief that a machine which is
// quick for one model is usually quick for another. Without a roster that
// belief has nowhere to attach: the chooser would know how long CoreWeave takes
// and not that CoreWeave exists.
//
// It reads what is already in memory and never opens a file, because it is
// asked on the one path that may not: the cold start of a choice.
func (s *sheet) Roster() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	names := make([]string, 0, len(s.rows))
	for _, rows := range s.rows {
		for _, row := range rows {
			if row.ID.Lane == "" || seen[row.ID.Lane] {
				continue
			}
			seen[row.ID.Lane] = true
			names = append(names, row.ID.Lane)
		}
	}
	sort.Strings(names)
	return names
}

// WantSheet asks the beat to fetch one model's sheet once, at once. It returns
// before anything is sent.
//
// IT IS THE DOOR [Agent.SetModel] KNOCKS ON. The beat's model list is settled
// when a session opens, from the two config slots, and a person who picks
// another model afterwards used to get a session that never fetched a sheet for
// it again — so cold start was the steady state for exactly the models people
// choose deliberately. A sheet installed by a bench that is not this package's
// own has no queue and is left alone.
func WantSheet(model string) {
	if pages, ok := Default().Sheet().(Wanter); ok {
		pages.Wants(model)
	}
}

// Tag is the router's own slug for a lane — "deep-infra" for "DeepInfra" — as
// the sheet spelled it, empty when this sheet never saw the lane.
//
// It is here rather than on [Row] because a [Row] is what the belief and the
// gate are made of, and the slug is neither: it is a spelling, wanted by a
// surface that filters on `@deepinfra` and by nothing else.
func (s *sheet) Tag(id ID) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tags[id.bare()]
}

// Refresh fetches model's sheet and replaces what Rows returns.
//
// IT IS CALLED FROM THE BEAT AND FROM NOTHING ELSE. A failed refresh leaves the
// rows that are already there: a lane sheet that is half an hour old is a
// better prior than no prior, and the belief is what corrects it anyway.
func (s *sheet) Refresh(ctx context.Context, model string) error {
	// THE PAGE IS PUBLISHED UNDER THE ID THE ROUTER SERVES, never under the
	// alias that resolves to it, so the fold happens before the URL is built as
	// well as before the rows are filed. See [LedgerModel].
	model = LedgerModel(model)
	if model == "" {
		return ErrNoSheet
	}
	s.mu.RLock()
	base, key, fetch, dir := s.base, s.key, s.fetch, s.dir
	s.mu.RUnlock()
	if fetch == nil || base == "" {
		return ErrNoSheet
	}
	body, err := fetch.Fetch(ctx, base+"/models/"+model+"/endpoints", key)
	if err != nil {
		return err
	}
	defer body.Close()
	rows, tags, err := decodeSheet(model, io.LimitReader(body, maxSheetBytes))
	if err != nil {
		return err
	}
	// A sheet with no readable row at all is a schema that has moved, and
	// replacing good rows with nothing on the strength of it would be this
	// process forgetting what it knows because somebody shipped a field.
	if len(rows) == 0 {
		return errSheetEmpty
	}
	at := time.Now()
	// Every row carries the moment it was read, so that the ledger can age a
	// belief to it before folding it in ([Row.At]).
	for i := range rows {
		rows[i].At = at
	}
	s.mu.Lock()
	s.rows[model], s.at[model], s.looked[model] = rows, at, true
	for id, tag := range tags {
		s.tags[id] = tag
	}
	s.mu.Unlock()
	return writeCache(cachePathIn(dir, model), model, rows, tags, at)
}

// errSheetEmpty is a sheet that decoded to no lanes at all.
var errSheetEmpty = errors.New("lane: the sheet named no lanes")

// ── THE BEAT ────────────────────────────────────────────────────────────────

// Beat refreshes models' sheets every `every` until ctx is done, and primes the
// ledger from every reading it gets.
//
// IT STARTS NOTHING. Nothing in this package ever runs a goroutine of its own:
// a session that wants a beat runs this in one it owns and can stop, which is
// what keeps "who is fetching, and when" a question with an answer in the
// session's own code rather than in a package nobody thought was running.
//
// THE REFRESH AND THE PRIMING ARE ONE ACT, and that is a correction rather than
// a convenience. A sheet fetched into [Sheet.Rows] and never folded into the
// ledger is a prior nothing reads: the chooser asks the LEDGER, so a build that
// refreshed on a beat and primed somewhere else would work exactly until the
// two drifted, and then be blind on the first call of every process with no
// symptom but slowness. Priming here means a fresh sheet always reaches the
// belief, in the one place a sheet is ever fresh.
//
// The first pass skips a model whose cached sheet is younger than the interval,
// so opening a session a minute after closing one costs nothing — and it primes
// from that cached reading anyway, because a prior read off the disk is worth
// exactly as much as one off the wire.
func Beat(ctx context.Context, s Sheet, models []string, every time.Duration) {
	if s == nil {
		return
	}
	var queue <-chan string
	if asked, ok := s.(Queue); ok {
		queue = asked.Wanted()
	}
	// A beat with no models and no queue has nothing it could ever do. A beat
	// with a queue and no models is a real state — a session whose only model
	// arrives from the picker — and it waits on the channel.
	if len(models) == 0 && queue == nil {
		return
	}
	if every <= 0 {
		every = sheetTTL
	}
	fresh, canAge := s.(interface {
		freshness(string, time.Time) (time.Duration, bool)
	})
	for _, model := range models {
		if canAge {
			if age, ok := fresh.freshness(model, time.Now()); ok && age < every {
				primeFrom(s, model)
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		refreshAndPrime(ctx, s, model)
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case model := <-queue:
			// A MODEL THAT ARRIVES HERE JOINS THE ROUND AND IS FETCHED AT ONCE.
			// Both halves are the point: at once, because somebody is about to
			// send to it, and joined, because the next half hour of its sheet is
			// worth as much as the first minute. A nil queue is a channel that
			// never fires, which is what a bench's own sheet gets.
			models = withModel(models, model)
			refreshAndPrime(ctx, s, model)
		case <-ticker.C:
			for _, model := range models {
				if ctx.Err() != nil {
					return
				}
				refreshAndPrime(ctx, s, model)
			}
		}
	}
}

// refreshAndPrime is one model's round: fetch, and fold what came back into the
// belief. The two are one act for the reason [Beat] states — a sheet nothing
// primed from is a prior nothing reads.
func refreshAndPrime(ctx context.Context, s Sheet, model string) {
	if ctx.Err() != nil {
		return
	}
	if err := s.Refresh(ctx, model); err == nil {
		primeFrom(s, model)
	}
}

// withModel adds a model to the beat's round, once.
func withModel(models []string, model string) []string {
	model = BareModel(model)
	if model == "" {
		return models
	}
	for _, held := range models {
		if BareModel(held) == model {
			return models
		}
	}
	return append(models, model)
}

// primeFrom folds one model's rows into the live ledger at [SheetWeight].
//
// It is the only caller of [Ledger.Prime] in this package that is not a test,
// and it asks the registry rather than holding a ledger so that a bench which
// swapped one in is primed too.
func primeFrom(s Sheet, model string) {
	rows := s.Rows(model)
	if len(rows) == 0 {
		return
	}
	beliefs := Default().Ledger()
	// AND THE FILE IS READ BACK FIRST. Another aforge may have been running the
	// whole time this one was, learning about models this session has never
	// mentioned; priming over a ledger that has not looked since it opened is
	// how a save deletes them (store.go, "two processes, one file"). The beat is
	// the right moment for it: it is already a round of bookkeeping, it is
	// nowhere near a send, and it is immediately before the one act that
	// rewrites the whole file.
	if reader, ok := beliefs.(interface{ reload() }); ok {
		reader.reload()
	}
	for _, row := range rows {
		beliefs.Prime(row, SheetWeight)
	}
}

// ── THE ROUTER'S OWN FIELD NAMES ────────────────────────────────────────────

// wirePercentiles is the router's percentile block: milliseconds for latency,
// tokens per second for throughput.
type wirePercentiles struct {
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P99 float64 `json:"p99"`
}

// wireEndpoint is one lane's row in the router's field names exactly. Prices
// are STRINGS on the wire — dollars per token, with more precision than a JSON
// float survives being read back by everybody's decoder.
type wireEndpoint struct {
	ProviderName        string `json:"provider_name"`
	Tag                 string `json:"tag"`
	Quantization        string `json:"quantization"`
	ContextLength       int    `json:"context_length"`
	MaxCompletionTokens int    `json:"max_completion_tokens"`
	Pricing             struct {
		Prompt         string `json:"prompt"`
		Completion     string `json:"completion"`
		InputCacheRead string `json:"input_cache_read"`
	} `json:"pricing"`
	SupportsToolChoice struct {
		Function bool `json:"function"`
	} `json:"supports_tool_choice"`
	// Status is the router's own health word for the endpoint: zero is healthy,
	// and a negative figure is a lane it has derated. See [Facts.Status].
	Status                int             `json:"status"`
	UptimeLast5m          float64         `json:"uptime_last_5m"`
	SupportsImplicitCache bool            `json:"supports_implicit_caching"`
	LatencyLast30m        wirePercentiles `json:"latency_last_30m"`
	ThroughputLast30m     wirePercentiles `json:"throughput_last_30m"`
}

// decodeSheet reads the endpoints body one row at a time. The error it returns
// is about the envelope; a row it could not read is skipped in silence, which
// is the whole point of decoding this way.
func decodeSheet(model string, body io.Reader) ([]Row, map[ID]string, error) {
	var envelope struct {
		Data struct {
			Endpoints []json.RawMessage `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return nil, nil, err
	}
	rows := make([]Row, 0, len(envelope.Data.Endpoints))
	tags := map[ID]string{}
	for _, raw := range envelope.Data.Endpoints {
		var item wireEndpoint
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		lane := strings.TrimSpace(item.ProviderName)
		if lane == "" {
			// A row that cannot say who served it names no machine, and a
			// belief keyed on nothing is a belief about everybody.
			continue
		}
		id := ID{Model: model, Lane: lane}
		rows = append(rows, Row{
			ID: id,
			Facts: Facts{
				Tools:      item.SupportsToolChoice.Function,
				Quant:      strings.TrimSpace(item.Quantization),
				MaxOut:     item.MaxCompletionTokens,
				Context:    item.ContextLength,
				Uptime5m:   item.UptimeLast5m,
				PriceIn:    price(item.Pricing.Prompt),
				PriceOut:   price(item.Pricing.Completion),
				PriceCache: price(item.Pricing.InputCacheRead),
				Caches:     item.SupportsImplicitCache,
				Status:     item.Status,
			},
			TTFTp50: item.LatencyLast30m.P50,
			TTFTp75: item.LatencyLast30m.P75,
			TTFTp90: item.LatencyLast30m.P90,
			TTFTp99: item.LatencyLast30m.P99,
			Ratep50: item.ThroughputLast30m.P50,
			Ratep75: item.ThroughputLast30m.P75,
			Ratep90: item.ThroughputLast30m.P90,
			Ratep99: item.ThroughputLast30m.P99,
		})
		if tag := strings.TrimSpace(item.Tag); tag != "" {
			tags[id] = tag
		}
	}
	return rows, tags, nil
}

// price reads one of the router's money strings as dollars per token. An
// unreadable or absent figure is zero, which under the emptiness law reads as
// "the sheet did not say" rather than as "free".
func price(text string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// ── THE CACHE ───────────────────────────────────────────────────────────────

// cachedSheet is what one model's sheet looks like on disk: the rows, the slugs
// [Row] has nowhere to keep, and the moment they were true. The stamp is the
// point of the file — a cached sheet with no fetched-at is a prior nobody can
// decide whether to trust.
type cachedSheet struct {
	Model string      `json:"model"`
	At    time.Time   `json:"fetched_at"`
	Lanes []cachedRow `json:"lanes"`
}

type cachedRow struct {
	Row Row    `json:"row"`
	Tag string `json:"tag,omitempty"`
}

// cachePath is where model's sheet sleeps.
func (s *sheet) cachePath(model string) string {
	s.mu.RLock()
	dir := s.dir
	s.mu.RUnlock()
	return cachePathIn(dir, model)
}

// cachePathIn names one model's cache file, `~/.aforge/v3/lanes/{model}.json`
// under the home this process was pointed at, with the slash in a model id
// escaped so that "deepseek/deepseek-v4-flash" is one file and not a directory
// nobody meant to make.
// It is keyed on the ledger's name for the model, so that a sheet fetched under
// the alias and a sheet fetched under the served id are one file rather than
// two accounts of one endpoints page.
func cachePathIn(dir, model string) string {
	if strings.TrimSpace(dir) == "" {
		dir = home.Join("v3", "lanes")
	}
	return filepath.Join(dir, url.PathEscape(LedgerModel(model))+".json")
}

// readCache reads one model's cached sheet. A file that is not there is not an
// error: it is a machine that has not fetched this model yet, which is the
// normal state of a new one.
func readCache(path string) ([]Row, map[ID]string, time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var cached cachedSheet
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, nil, time.Time{}, err
	}
	rows := make([]Row, 0, len(cached.Lanes))
	tags := map[ID]string{}
	for _, lane := range cached.Lanes {
		if lane.Row.ID.Zero() {
			continue
		}
		rows = append(rows, lane.Row)
		if lane.Tag != "" {
			tags[lane.Row.ID] = lane.Tag
		}
	}
	return rows, tags, cached.At, nil
}

// writeCache writes one model's sheet, atomically. A cache that can be read
// half-written is worse than no cache: the reader of it is a cold process
// deciding where to send its first request.
func writeCache(path, model string, rows []Row, tags map[ID]string, at time.Time) error {
	cached := cachedSheet{Model: model, At: at, Lanes: make([]cachedRow, 0, len(rows))}
	for _, row := range rows {
		cached.Lanes = append(cached.Lanes, cachedRow{Row: row, Tag: tags[row.ID]})
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data)
}
