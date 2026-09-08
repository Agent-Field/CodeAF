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

// ErrNoSheetHere is what a [Fetcher] returns when the base ANSWERED, and its
// answer was that no endpoints ROUTE lives at that address at all.
//
// IT IS THE ONE ANSWER THAT MAKES A BASE SHEETLESS. A timeout, a severed
// connection, a 500 and a 429 are all a router having an afternoon, and a build
// that read any of those as "this is not a router" would throw away every lane
// behaviour it has for five minutes over one bad packet.
//
// AND A 404 IS NOT THIS ANSWER BY ITS STATUS; IT IS BY ITS BODY. The live router
// answers 404 twice over, and the two mean opposite things about the base
// (measured 2026-09-02):
//
//	GET /api/v1/models/nonexistent/model-xyz/endpoints
//	→ 404, {"error":{"message":"Not Found","code":404}}
//
//	GET /api/v1/nonexistent-route/x/endpoints
//	→ 404, <!DOCTYPE html>…<title>Not Found | OpenRouter</title>…
//
// The first is the router's own error envelope — the route is there and it
// answered about ONE MODEL, which simply has no page this round; it is a quiet
// per-model error and says nothing about the base. Only the second, a 404 whose
// body is not that envelope, is "no such route", and only that one wraps this
// error. Which bodies mean which is the transport's decision because only the
// transport can see a status and a body (internal/provider's sheetFetcher, and
// its sheetNotFound); everything here only asks whether the error it was handed
// wraps this one.
var ErrNoSheetHere = errors.New("lane: the base publishes no endpoints page")

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

// ── HOW THIS BUILD LEARNS THAT A BASE HAS LANES ─────────────────────────────
//
// A ROUTER IS RECOGNISABLE BY WHAT IT ANSWERS AND NEVER BY A SUBSTRING OF WHERE
// IT LIVES. This build used to decide whether to fetch an endpoints page at all
// by testing the base URL for `openrouter.ai`, so a binary driven through
// AFORGE_BASE_URL at a proxy, a mirror, a self-hosted router or a router
// reached by its IP silently got no sheet, an empty frontier and no lane
// behaviour whatever — nothing errored, nothing logged a refusal, the feature
// was simply absent (issue #373).
//
// So the question is put to the base instead, ONCE, and the answer is
// remembered here beside the rows it is about.
//
// THE PROBE IS NOT A SECOND REQUEST. It is the reading of the endpoints fetch
// [Refresh] already makes, which is why a base that does serve a sheet pays
// nothing at all for the law: the first refresh both fetches and answers the
// question. A base that says there is no such page is not asked again until its
// answer goes stale, and staleness is [sheetTTL] — the same five minutes the
// beat runs on and the same five minutes a cached sheet is good for, because a
// second number here is a number that would drift.
type sheetAnswer int

const (
	// answerUnasked is a base nobody has put the question to yet, which is the
	// state every wiring starts in except the one that carries a hint.
	answerUnasked sheetAnswer = iota
	// answerServes is a base that has handed back an endpoints page. It is
	// STICKY for the life of the wiring: a router asked about a model it does
	// not happen to serve answers 404 about THAT MODEL, and reading that as
	// "this is not a router" would cost every other model its lanes.
	answerServes
	// answerSheetless is a base that answered, and said there is no endpoints
	// page here. It suppresses fetching until [sheetTTL] has passed, and then
	// the question is asked again exactly once more.
	answerSheetless
)

// ── AND WHETHER IT CARRIES A ROUTING PREFERENCE ─────────────────────────────
//
// The same law one layer up, and it is the same law for the same reason. This
// build used to decide whether a `provider` object went on the wire at all by
// testing the base URL for `openrouter.ai` or the model id for the prefix
// `openrouter/` (internal/provider's isOpenRouter). So on a proxy, a mirror, a
// self-hosted router or the router reached by its IP, a person could rank
// lanes, pin one, and watch the settings row go on reading `pinned: X` while
// the preference was never sent: a silent substitution, which is the thing this
// repository's law forbids (issue #433).
//
// WHETHER A BASE HONOURS A PREFERENCE IS LEARNED FROM THE BASE, NEVER FROM ITS
// HOSTNAME. Two things teach it, and both are readings of a request that was
// going out anyway:
//
//   - A BASE THAT SERVED AN ENDPOINTS PAGE CARRIES THEM. That is the router's
//     own contract — a machine that publishes which lanes serve a model is a
//     machine that takes an instruction about which one to use — so [answerServes]
//     answers this question too and the shipped router pays nothing for the law.
//   - EVERY OTHER BASE IS ASKED ONCE, by a real request carrying a real
//     preference, and answers with what comes back: an answer that NAMES the
//     lane that served it carries them; a refusal that names the `provider`
//     field does not; and a 200 that carries no lane information at all is
//     read as "does not", which is the safe reading under the law — the person
//     is told, and the request still goes out.
//
// ONLY AN ANSWER TO A REQUEST THAT REALLY CARRIED ONE TEACHES ANYTHING. The
// widened retry that follows a refusal carries no preference, and it is
// answered — often perfectly, naming its lane — by the very base that had just
// refused the field. Read as evidence it would put the base straight back to
// [prefCarries] and the next turn would pay the identical refusal.
//
// WHAT THAT SILENT CASE CANNOT TELL APART, said plainly because a reader will
// otherwise assume it can: a base that honoured the preference silently and a
// base that dropped it on the floor look identical from here. Nothing in an
// OpenAI-compatible answer says which lane served it, so there is no evidence
// to separate them, and this build takes the reading that produces a sentence
// rather than the one that produces a silence.
//
// THE FIRST DEFINITE ANSWER STANDS, and only one of the three is indefinite. A
// base that served a sheet or named its lane has SHOWN it takes an instruction;
// a base that refused the field has SAID it does not; and either of those is the
// end of the question, because a build where the beat's next sheet could talk a
// refusal round would re-pay that refusal every five minutes for as long as the
// window lived, and a build where one refusal could overrule the shipped
// router's own contract would retire the feature on the machine it works on.
// Silence is the indefinite one: it proved nothing, so either definite answer
// arriving later replaces it. What clears any of them is the base MOVING, which
// is [wire]'s law and is the only one there is.
type prefAnswer int

const (
	// prefUnasked is a base nobody has put a preference to yet. IT SENDS: the
	// asking IS the sending, and there is no other way to learn.
	prefUnasked prefAnswer = iota
	// prefCarries is a base that served an endpoints page, or that named the
	// lane which served a request carrying a preference.
	prefCarries
	// prefSilent is a base that answered such a request with no lane
	// information at all. It is the indefinite one: nothing was proved, and
	// either definite answer arriving later replaces it.
	prefSilent
	// prefRefused is a base that refused the `provider` field in words.
	prefRefused
)

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
	// answered is what the base above said the last time it was asked for an
	// endpoints page, and askedAt is when it said it. Together they are the
	// whole of the probe, cached per base: they are cleared when [wire] points
	// this sheet somewhere else, because what one router answered is not
	// evidence about another.
	answered sheetAnswer
	askedAt  time.Time
	// carries is what the base above has said about honouring a routing
	// preference, under the rules written at [prefAnswer]. It is cleared beside
	// `answered` when [wire] points this sheet somewhere else, for the same
	// reason: what one router carried is not evidence about another.
	carries prefAnswer
	// now is the clock the probe ages its answer on. Nil is the wall clock,
	// which is what every wiring outside a test gets; a test that has to watch
	// [sheetTTL] pass sets it, because five minutes of real waiting is not a
	// test anybody runs. It is the prober's own arrangement (probe.go's now).
	now func() time.Time
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
//
// `known` is A HINT AND NEVER A REFUSAL. A caller that already knows this base
// publishes an endpoints page — the shipped router, recognised by its hostname
// in internal/provider's LaneSheetCertain — passes true, and the sheet skips
// straight to [answerServes] so the existing path is unchanged in behaviour and
// costs not one extra round trip. FALSE MEANS "ASK IT", never "it has none":
// every other base is wired exactly the same way and learns what it is from
// what it answers.
func WireSheet(base, key string, fetch Fetcher, known bool) bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return false
	}
	own.wire(base, key, fetch, known)
	return true
}

// wire points a sheet at a router.
func (s *sheet) wire(base, key string, fetch Fetcher, known bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	base = trimBase(base)
	// A BASE THAT MOVED FORGETS WHAT THE OLD ONE ANSWERED. The probe is cached
	// per base, and carrying one router's answer over to the next address would
	// be exactly the mistake this whole file is here to stop.
	if base != s.base {
		s.answered, s.askedAt, s.carries = answerUnasked, time.Time{}, prefUnasked
	}
	s.base, s.key, s.fetch = base, strings.TrimSpace(key), fetch
	if known {
		// A BASE KNOWN TO PUBLISH A SHEET IS KNOWN TO TAKE A PREFERENCE, by the
		// router's own contract ([prefAnswer]). Setting it here rather than
		// waiting for the first fetch is what keeps the shipped path at exactly
		// the requests it made before this law existed.
		s.answered, s.askedAt, s.carries = answerServes, time.Time{}, prefCarries
	}
}

// clock is the moment the probe reads. It is the one clock this file keeps
// besides the fetched-at stamps, and it is read only to age an answer: nothing
// about a choice depends on it, which is what lets it be a wall clock by
// default and a test's own when one is handed in.
func (s *sheet) clock() time.Time {
	s.mu.RLock()
	now := s.now
	s.mu.RUnlock()
	if now != nil {
		return now()
	}
	return time.Now()
}

// askable reports whether this base may be asked for an endpoints page now.
//
// Only a base that has SAID there is no page is ever held back, and only until
// its answer is [sheetTTL] old. Everything else — never asked, asked and served
// — goes to the network exactly as it always did.
func (s *sheet) askable(now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.answered != answerSheetless {
		return true
	}
	return now.Sub(s.askedAt) >= sheetTTL
}

// heard files what the base just answered about endpoints pages.
//
// A SHEET THAT ARRIVED IS THE END OF THE QUESTION: the base is a router, it is
// remembered as one, and no later 404 about some model it does not serve can
// take that back. Anything that is not [ErrNoSheetHere] leaves the answer where
// it was, so a bad afternoon is a bad afternoon and not a verdict about the
// address.
func (s *sheet) heard(base string, err error, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heardLocked(base, err, now)
}

// heardLocked files an answer only while the sheet still names the base that
// was asked. AN ANSWER IS FILED UNDER THE BASE IT WAS ASKED OF, AND UNDER NO
// OTHER.
func (s *sheet) heardLocked(base string, err error, now time.Time) bool {
	if s.base != base {
		return false
	}
	switch {
	case err == nil:
		// AND A SERVED SHEET IS ALSO THE ANSWER TO THE PREFERENCE QUESTION. It
		// is the router's contract rather than a second reading: a base that
		// publishes which lanes serve a model takes an instruction about which
		// one to use, so this costs no extra request ([prefAnswer]).
		//
		// IT DOES NOT OVERRULE A REFUSAL IN WORDS. A base that has said it does
		// not know the `provider` field has answered the question directly, and
		// a build where the beat could talk it round would re-pay that refusal
		// every five minutes for as long as the window lived.
		s.answered, s.askedAt = answerServes, now
		if s.carries != prefRefused {
			s.carries = prefCarries
		}
	case errors.Is(err, ErrNoSheetHere) && s.answered != answerServes:
		s.answered, s.askedAt = answerSheetless, now
	}
	return true
}

// PrefsCarried reports whether base will carry a routing preference.
//
// UNKNOWN ANSWERS TRUE, and that is the whole shape of the law: the only way to
// learn is to ask, and asking is sending. Only a base that has ANSWERED — with
// silence where a lane name belonged, or with a refusal naming the field — is
// left off ([prefAnswer] states what teaches which).
//
// The answer is read under the base it was filed against, so a sheet that has
// since been pointed somewhere else answers about the new base and never about
// the old one.
func PrefsCarried(base string) bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return true
	}
	return own.prefsCarried(trimBase(base))
}

// PrefsProven reports whether base has SHOWN it carries a routing preference —
// it served an endpoints page, or it named the lane that answered one.
//
// IT IS THE OTHER HALF OF [PrefsCarried] AND THE TWO ARE BOTH NEEDED. Carried
// is "may this go out", and an unasked base answers yes because the asking is
// the sending. Proven is "has this base earned the default knobs" — the sort
// word, the fallback flag, the parameter filter — which nobody asked for and
// which no answer is owed about, and an unasked base answers NO. What goes out
// on an unasked base is only ever something a PERSON asked for
// (internal/provider's providerPreferences says it in full).
func PrefsProven(base string) bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return false
	}
	base = trimBase(base)
	if base == "" {
		return false
	}
	own.mu.RLock()
	defer own.mu.RUnlock()
	return own.base == base && own.carries == prefCarries
}

// SheetServes reports whether base has HANDED BACK an endpoints page, which is
// a different and narrower question from [PrefsCarried].
//
// IT IS FALSE UNTIL THE BASE HAS SHOWN ONE, where PrefsCarried is true until a
// base has refused one, and the difference is which way the safe reading points
// for what is being asked. "Send the preference" cannot be learned without
// sending it, so an unasked base sends. "This model is served by several
// machines here" is a claim about the base's shape that costs nothing to be
// wrong about in the cautious direction: a build that assumed it would offer a
// person a lane list for a base that has one endpoint, and would read a plain
// endpoint's 404 as a routing layer emptying a set it does not have.
//
// The shipped router answers true with no request at all, through the `known`
// hint [WireSheet] takes.
func SheetServes(base string) bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return false
	}
	base = trimBase(base)
	if base == "" {
		return false
	}
	own.mu.RLock()
	defer own.mu.RUnlock()
	return own.base == base && own.answered == answerServes
}

// PrefsCarriedHere is [PrefsCarried] about whichever base the live sheet is
// wired to. It is what a SURFACE asks — a settings row saying whether the lane
// somebody pinned can be asked for at all — because a panel holds no client and
// so has no base URL of its own to name.
func PrefsCarriedHere() bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return true
	}
	own.mu.RLock()
	defer own.mu.RUnlock()
	return own.carries == prefUnasked || own.carries == prefCarries
}

// HeardPrefsCarried, HeardPrefsSilent and HeardPrefsRefused are the three
// answers a base can give, each with its own door.
//
// THREE DOORS AND NOT ONE FLAG, because the three are different strengths and a
// caller that handed a boolean across would have to know the ranking to get it
// right — which is precisely the knowledge that belongs here. Each files under
// the base it was asked of and under no other, exactly as [sheet.heardLocked]
// does, and each reports whether it was filed at all: false is a sheet that has
// moved on, and the caller has learnt nothing about where it points now.
func HeardPrefsCarried(base string) bool { return fileAnswer(base, prefCarries) }

// HeardPrefsSilent files the weaker no: a request that carried a preference was
// answered with no lane information at all.
func HeardPrefsSilent(base string) bool { return fileAnswer(base, prefSilent) }

// HeardPrefsRefused files the terminal no: the base named the `provider` field
// in a refusal.
func HeardPrefsRefused(base string) bool { return fileAnswer(base, prefRefused) }

// fileAnswer is the one body the three doors share.
func fileAnswer(base string, answer prefAnswer) bool {
	own, ok := Default().Sheet().(*sheet)
	if !ok {
		return false
	}
	return own.heardPrefs(trimBase(base), answer)
}

// trimBase is how a base URL is spelled in this file: trimmed, with no trailing
// slash. It is written once because [wire] files under this spelling and every
// reader has to ask under the same one.
func trimBase(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

// prefsCarried is [PrefsCarried] on one sheet.
func (s *sheet) prefsCarried(base string) bool {
	if base == "" {
		// AN EMPTY BASE IS NOT A BASE. Nothing was ever asked of it and nothing
		// can be, so it carries nothing — which is also what this build did
		// before the question was asked at all.
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.base != base {
		return true
	}
	return s.carries == prefUnasked || s.carries == prefCarries
}

// heardPrefs files one base's answer about routing preferences, under the
// ranking [prefAnswer] states: the first DEFINITE answer stands, and silence is
// the one that is not definite.
func (s *sheet) heardPrefs(base string, answer prefAnswer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if base == "" || s.base != base {
		return false
	}
	if s.carries == prefCarries || s.carries == prefRefused {
		// THE FIRST DEFINITE ANSWER STANDS. Both of those are one, and the
		// reasons they cannot be overturned are opposite and both stated at
		// [prefAnswer].
		return true
	}
	if answer == prefSilent && s.carries == prefSilent {
		return true
	}
	s.carries = answer
	return true
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

// ── THE NEGATIVE HALF OF THE SERVING SET ────────────────────────────────────
//
// A sheet says which machines the router PUBLISHES for a model. It does not say
// which machines the router will actually SERVE it from, and on 2026-09-01
// those were two different sets: three of the five tool-capable lanes on the
// sheet were not in the serving set the completion resolved against, so every
// request that pinned one of them came back
// `…but your request's provider.only preference permits only: coreweave`, six
// times in one run, and the lane was chosen again three separate times because
// nothing anywhere wrote the refusal down (issue #266).
//
// THE FIX IS NOT A BETTER SHEET. A sheet is fetched every few minutes and a
// wire refusal is a fact about this second; the honest shape is a positive half
// that is fetched and a NEGATIVE half that is learned, with the gate reading
// both. [RefuseServing] is the write and [Serves] the read, and [capable] in
// frontier.go is the one reader that matters: a lane the wire has refused
// leaves the candidate set, so the next pin cannot be chosen from it.
//
// IT IS FILED UNDER THE LEDGER KEY and never under the spelling that happened
// to be on the wire ([LedgerModel]). The whole defect had the bare id and the
// dated slug disagreeing about who serves a model; a refusal written under one
// of them and read under the other would reproduce that disagreement inside
// this file.

// servingRefusalHold is how long a wire refusal keeps a lane out of a model's
// candidate set.
//
// LONG ENOUGH THAT NO RUN ASKS TWICE, SHORT ENOUGH THAT NOTHING IS LOST FOR
// GOOD. A router's serving set really does change — a model is rolled out to a
// new endpoint, an account's policy is edited — and a permanent ban earned by
// one 404 would mean a process that is wrong once is wrong until it is
// restarted. Half an hour outlives any single conversation and is a fraction of
// the life of a session that stays open all day.
const servingRefusalHold = 30 * time.Minute

// refusedLanes is every lane the wire has refused, by ledger key, with the
// moment its refusal stops counting.
//
// It is package state rather than a field on [sheet] because a refusal is a
// fact about the ROUTER and not about whichever sheet implementation a build
// installed: a bench that swaps the sheet for its own must not thereby lose the
// refusals the transport has already collected.
var refusedLanes struct {
	sync.RWMutex
	until map[string]map[string]time.Time
}

// RefuseServing writes one lane OUT of the set known to serve a model, because
// the router said on the wire that it does not.
//
// It is called by the layer that saw the refusal (internal/provider's refusal
// classifier) and by nothing else. An unnamed model or lane writes nothing: a
// refusal credited to nobody would take a machine away from every model at
// once.
func RefuseServing(model, lane string) {
	key, name := LedgerModel(model), strings.ToLower(strings.TrimSpace(lane))
	if key == "" || name == "" {
		return
	}
	refusedLanes.Lock()
	defer refusedLanes.Unlock()
	if refusedLanes.until == nil {
		refusedLanes.until = map[string]map[string]time.Time{}
	}
	if refusedLanes.until[key] == nil {
		refusedLanes.until[key] = map[string]time.Time{}
	}
	refusedLanes.until[key][name] = time.Now().Add(servingRefusalHold)
}

// Serves reports whether a lane is still believed to serve a model on the wire.
//
// UNKNOWN IS YES, which is the same reading [capable] takes of every other
// field it gates on: this half of the serving set holds refusals and nothing
// else, so a lane nobody has been refused by has said nothing and passes. Only
// a refusal this process actually collected can take a machine away.
func Serves(model, lane string) bool {
	name := strings.ToLower(strings.TrimSpace(lane))
	if name == "" {
		return true
	}
	refusedLanes.RLock()
	until, refused := refusedLanes.until[LedgerModel(model)][name]
	refusedLanes.RUnlock()
	return !refused || !time.Now().Before(until)
}

// ForgetRefusals empties the negative half. It is for tests, which must not
// inherit another test's refusals.
func ForgetRefusals() {
	refusedLanes.Lock()
	defer refusedLanes.Unlock()
	refusedLanes.until = nil
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
	// THE PROBE IS THIS FETCH AND NOT A SECOND ONE. A base that has already
	// told us there is no endpoints route here is not asked again until that
	// answer is stale, so a session pointed at something that is not a router
	// spends one request every [sheetTTL] rather than one per beat per model —
	// and a base that has never answered is asked, which is the whole law.
	// What counts as "told us" is [ErrNoSheetHere] and nothing looser: the
	// router's own envelope 404 about a model it does not publish is that
	// model's business and holds nothing back, so a base whose first model in
	// the round is unknown to it still gets the second model's sheet at once.
	// Two beats could in principle pass this gate at once; there is one beat
	// per session by construction, and the cost of the race is one duplicate
	// request rather than a wrong answer.
	if !s.askable(s.clock()) {
		return ErrNoSheetHere
	}
	body, err := fetch.Fetch(ctx, base+"/models/"+model+"/endpoints", key)
	if err != nil {
		s.heard(base, err, s.clock())
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
	// The answer and its rows are one reading of one router. AN ANSWER IS FILED
	// UNDER THE BASE IT WAS ASKED OF, AND UNDER NO OTHER.
	if !s.heardLocked(base, nil, at) {
		s.mu.Unlock()
		return errSheetMoved
	}
	s.rows[model], s.at[model], s.looked[model] = rows, at, true
	for id, tag := range tags {
		s.tags[id] = tag
	}
	s.mu.Unlock()
	return writeCache(cachePathIn(dir, model), model, rows, tags, at)
}

// errSheetEmpty is a sheet that decoded to no lanes at all.
var errSheetEmpty = errors.New("lane: the sheet named no lanes")

// errSheetMoved is a reading whose base changed while it was in flight.
var errSheetMoved = errors.New("lane: the sheet moved while it was being read")

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
	// ONE BEAT PER SHEET, HOWEVER MANY DOORS ASK FOR ONE. Two of them do now —
	// the process seats a beat at its one measuring seam and a session still
	// starts its own — and a second fetch loop over one sheet would pay the
	// router twice for the same half-hour aggregate and tick on a clock the
	// cache disagrees with. So the second caller HANDS ITS MODELS OVER through
	// the queue that already exists for exactly this — a model somebody is about
	// to send to, arriving from outside the round — and returns at once.
	release, first := claimTheBeat(queue)
	if !first {
		handOver(s, models)
		return
	}
	defer release()
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
			//
			// A MODEL THIS ROUND ALREADY HOLDS IS NOT FETCHED AGAIN. It arrives
			// here from a door that joined this beat rather than running one, and
			// the sheet it would fetch is the one this beat read a moment ago.
			joined := withModel(models, model)
			if len(joined) == len(models) {
				continue
			}
			models = joined
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

// beating is the sheets a beat is running on, KEYED ON THE QUEUE ITSELF.
//
// The queue is the right key for two reasons. It is the identity of the one
// thing a second caller needs — the channel it hands its models to — so a sheet
// that offers none cannot be joined and is not tracked at all, which leaves
// every bench fixture behaving exactly as it did. And a channel is comparable
// whatever a [Sheet] implementation turns out to be, so this map cannot be made
// to panic by a sheet somebody writes later.
var beating = struct {
	sync.Mutex
	on map[<-chan string]bool
}{on: map[<-chan string]bool{}}

// claimTheBeat records that a beat is running on this queue and hands back the
// release, or reports that one already is. A sheet with no queue is never
// claimed: nothing could join it, so a second beat over it is the caller's own
// business exactly as it was before.
func claimTheBeat(queue <-chan string) (release func(), first bool) {
	if queue == nil {
		return func() {}, true
	}
	beating.Lock()
	defer beating.Unlock()
	if beating.on[queue] {
		return nil, false
	}
	beating.on[queue] = true
	return func() {
		beating.Lock()
		defer beating.Unlock()
		delete(beating.on, queue)
	}, true
}

// handOver gives one caller's models to the beat that is already running. It
// never waits: [sheet.Wants] is a non-blocking send that drops a name a full
// queue has no room for, and a dropped name is one the next ask makes again.
func handOver(s Sheet, models []string) {
	wanter, ok := s.(Wanter)
	if !ok {
		return
	}
	for _, model := range models {
		wanter.Wants(model)
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
//
// The fallback resolves through [stateFile], so a test binary that was handed a
// home rather than choosing one gets no cache at all rather than the sheet of
// whoever started the run: a chooser built with a fake ledger used to be
// answered out of a person's own cached lanes (#475). "" is already "there is
// nothing there" to both readers of this path. See undertest.go.
func cachePathIn(dir, model string) string {
	if strings.TrimSpace(dir) == "" {
		if dir = stateFile("v3", "lanes"); dir == "" {
			return ""
		}
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
	// A sheet with nowhere to sleep is not a failed refresh: the rows are in
	// memory and this process will use them. Only a test binary reaches this,
	// and only for the cache it was never entitled to write (undertest.go).
	if path == "" {
		return nil
	}
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
