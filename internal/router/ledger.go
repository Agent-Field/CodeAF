package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// Entry is one model's measured ability at one kind of call.
//
// Keyed by class because ability is not one number. The router lab's panel
// ordered differently on structured planning than on reasoning, and a single
// pooled rating would have learned the average of two things it could have known
// separately. Keyed by resolved snapshot rather than by configured slug because
// a floating alias is a moving target: `~vendor/model-latest` is a different set
// of weights this month than last, and pooling them would let a regression hide
// behind the record of the model it replaced.
type Entry struct {
	Model   string             `json:"model"`
	Class   provider.CallClass `json:"class"`
	Rating  float64            `json:"rating"`
	Count   int                `json:"count"`
	Updated time.Time          `json:"updated"`
}

// Ledger is what the harness has learned about its panel, across runs and
// across processes.
//
// Two locks, and the split is the point. mutex guards the maps and is held for
// map operations only — reads take it shared, because Rating, Resolve, Entries
// and Aliases only look. flushing serialises the file work: a lock file with
// retries, a re-read, a re-parse, a re-encode and a rename, which is on the
// order of a second and used to happen with the ranking mutex held. Routing a
// single call reads the ledger once per rung, so every one of those reads was
// queued behind whatever observation happened to be writing to disk.
type Ledger struct {
	path string

	mutex   sync.RWMutex
	entries map[string]Entry
	aliases map[string]string
	pending []observation
	unsaved bool

	flushing sync.Mutex

	// timing guards the debounce timer and nothing else, so scheduling a flush
	// never waits on the map or on the disk.
	timing sync.Mutex
	timer  *time.Timer
	writes atomic.Int64

	// closed makes the file-lock wait abandonable. A run on its way out must
	// not sit through a second of 25ms retries for a lock another process is
	// holding, and a lock wait is the one place this code sleeps.
	closeOnce sync.Once
	closed    chan struct{}
}

// ledgerDebounce is how long a graded observation waits for company.
//
// Every one of them used to take a lock file, a re-read, a re-parse, a re-encode
// and a rename — process-wide serialised, and there are eight to twelve of them
// in a plan build alone, plus one per leaf and one per judge. They arrive in
// bursts, because a plan grades its spine all at once, so the whole burst can
// ride one write.
//
// The window opens at the first unflushed observation rather than resetting at
// each one: a steady stream of them must still reach the file every second,
// which is also the whole of what a crash can cost. Save and Close land the
// queue immediately, so the ordinary end of a run loses nothing at all.
const ledgerDebounce = time.Second

// observation is one graded outcome waiting to reach the file. It is kept
// rather than applied-and-forgotten so that a flush blocked by another process
// costs a moment rather than the evidence.
type observation struct {
	model    string
	class    provider.CallClass
	prior    float64
	positive bool
	weight   float64
}

type ledgerFile struct {
	Entries []Entry           `json:"entries"`
	Aliases map[string]string `json:"aliases,omitempty"`
}

// The update rule, and why every constant in it is a brake.
//
// This is a Rasch (one-parameter) update: one ability per model and class, no
// discrimination term. That is not a simplification made for convenience — the
// router lab fitted both and tested them against each other, and the
// two-parameter model lost on a likelihood ratio of 11.63 on 15 degrees of
// freedom, p = 0.71, AIC 137.4 against 155.8. Fifteen extra parameters bought
// less than chance would give. Routing needs ability and nothing else.
//
// Unpenalized maximum likelihood *breaks* on data of this shape, and that is the
// reason for the rest. A model that passes everything it has been shown has a
// likelihood that rises without bound: the lab's first fit ran one model's
// ability to +42 logits, made the observed information singular, inflated every
// standard error to about 470 and collapsed separation reliability to zero. So
// the step is clamped, the step size decays as evidence accumulates, and a
// diffuse N(0, 3²) prior pulls back toward the middle with a weight that itself
// decays as 1/n. Two prior standard deviations span ±6 logits — success
// probabilities from 0.0025 to 0.9975 — so the prior barely touches a rating the
// evidence identifies, and keeps an extreme one finite.
const (
	priorVariance = 9.0 // sigma = 3 logits
	startingRate  = 0.6
	rateHalfLife  = 12.0
	maxStep       = 0.5
	ratingBound   = 6.0
)

// MinGraded is how much graded evidence a rating needs before the ordering may
// prefer it to the cold-start prior.
//
// Eight, which is profile.MinSamples — the number this codebase already uses for
// exactly this decision, "may a measurement overwrite a default". Deliberately
// the same number rather than a new one, and it is comfortably above what went
// wrong: arm B's collapse was **five** graded observations, all of them budget
// stops on a single oversized task, outvoting a prior and rerouting every leaf
// on the panel. The report's own conclusion is that this gate alone would have
// prevented the whole thing.
//
// The gate is on *graded* observations specifically, and that is the point.
// 103 of 108 leaf outcomes in that arm were unverified successes, which move
// nothing and are correct to move nothing — so a count that looked like plenty
// of experience was five failures wearing a hundred and eight's clothes. Below
// the gate the ordering falls back to the cold-start prior, which is the honest
// statement that nothing is known yet.
const MinGraded = 8

// LoadLedger reads what has been learned, returning an empty ledger when there
// is nothing yet. A corrupt file is treated as an empty one, exactly as the
// profile does: it is an accumulation of observations, not a source of truth, and
// refusing to run over it would trade a small loss for a total one.
func LoadLedger(dir string) (*Ledger, error) {
	path, err := statePath(dir, "router-ledger.json")
	if err != nil {
		return nil, err
	}
	ledger := &Ledger{path: path, entries: map[string]Entry{}, aliases: map[string]string{},
		closed: make(chan struct{})}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ledger, nil
	}
	if err != nil {
		return ledger, err
	}
	ledger.adopt(decodeLedger(data))
	return ledger, nil
}

// Rating reports what is known about a model at one kind of call. The prior is
// used only when nothing is known, which is what makes a new model start
// somewhere sensible instead of at the bottom.
func (l *Ledger) Rating(model string, class provider.CallClass, prior float64) (float64, int) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.ratingLocked(model, class, prior)
}

func (l *Ledger) ratingLocked(model string, class provider.CallClass, prior float64) (float64, int) {
	entry, known := l.entries[key(model, class)]
	if !known {
		return prior, 0
	}
	return entry.Rating, entry.Count
}

// Reading is one panel member's standing at one kind of call: the rating and
// the graded evidence behind it, read through the alias map.
type Reading struct {
	Rating float64
	Count  int
}

// Query is one member to read: the configured slug, and the cold-start prior
// to answer with when nothing is known about it.
type Query struct {
	Slug  string
	Prior float64
}

// Read rates a whole panel in one acquisition.
//
// Ranking used to take the lock twice per rung — resolve the alias, then read
// the rating — which is twenty acquisitions for a five-model panel on every
// ordering, and an ordering happens several times per routed call. Each of them
// could land behind a flush. One snapshot per call is both cheaper and more
// honest: every rung in an ordering is now read from the same instant.
func (l *Ledger) Read(class provider.CallClass, queries []Query) []Reading {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	readings := make([]Reading, len(queries))
	for index, query := range queries {
		model := query.Slug
		if resolved, known := l.aliases[model]; known {
			model = resolved
		}
		rating, count := l.ratingLocked(model, class, query.Prior)
		readings[index] = Reading{Rating: rating, Count: count}
	}
	return readings
}

// Observe folds one outcome in, and only the outcomes that are evidence: a
// provider failure says nothing about a model and an unverified success says
// nothing about an answer.
func (l *Ledger) Observe(model string, class provider.CallClass, prior float64, verdict provider.Verdict) {
	positive, graded := verdict.Graded()
	if !graded || model == "" {
		return
	}
	weight := verdict.Weight()
	l.mutex.Lock()
	l.entries[key(model, class)] = update(l.entryOr(model, class, prior), positive, weight)
	l.pending = append(l.pending, observation{model: model, class: class, prior: prior, positive: positive, weight: weight})
	l.unsaved = true
	l.mutex.Unlock()
	// Flushed as it goes rather than at the end of the run: a run that is
	// interrupted has still learned what it learned. What it does not do is
	// take the file once per observation — the queue is already in memory and
	// correct, so the write can wait a second for the rest of its burst.
	l.schedule()
}

// Alias records which dated snapshot a floating slug actually served. The
// resolved model is the response's own `model` field, so this is the provider
// telling us what it ran rather than us guessing.
func (l *Ledger) Alias(slug, resolved string) {
	if slug == "" || resolved == "" || slug == resolved {
		return
	}
	l.mutex.Lock()
	if l.aliases[slug] == resolved {
		l.mutex.Unlock()
		return
	}
	l.aliases[slug] = resolved
	l.unsaved = true
	l.mutex.Unlock()
	l.schedule()
}

// Resolve maps a configured slug onto the snapshot last seen behind it. Ratings
// are read through it so that selection and recording agree about which model
// they are talking about.
func (l *Ledger) Resolve(slug string) string {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if resolved, known := l.aliases[slug]; known {
		return resolved
	}
	return slug
}

// Entries returns everything known, ordered for reading: strongest first within
// a class, classes alphabetically.
func (l *Ledger) Entries() []Entry {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	entries := make([]Entry, 0, len(l.entries))
	for _, entry := range l.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Class != entries[j].Class {
			return entries[i].Class < entries[j].Class
		}
		if entries[i].Rating != entries[j].Rating {
			return entries[i].Rating > entries[j].Rating
		}
		return entries[i].Model < entries[j].Model
	})
	return entries
}

// Aliases returns the floating-slug to snapshot map, for reporting.
func (l *Ledger) Aliases() map[string]string {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	copied := make(map[string]string, len(l.aliases))
	for slug, resolved := range l.aliases {
		copied[slug] = resolved
	}
	return copied
}

// schedule asks for a flush within the debounce window, joining one already
// asked for rather than adding a second.
func (l *Ledger) schedule() {
	if l.abandoned() {
		// A closed ledger has already had its Save and will not get another,
		// so a late observation carries itself to the file. That is cheap by
		// construction: a closed ledger does not wait out the lock file.
		_ = l.flush()
		return
	}
	l.timing.Lock()
	defer l.timing.Unlock()
	if l.timer != nil {
		return
	}
	l.timer = time.AfterFunc(ledgerDebounce, func() {
		// Cleared before the flush, not after: an observation that arrives
		// while this one is on the disk must be able to ask for the next
		// window rather than be swallowed by this one.
		l.timing.Lock()
		l.timer = nil
		l.timing.Unlock()
		_ = l.flush()
	})
}

// unschedule cancels a pending window, for the callers that are about to write
// the file themselves.
func (l *Ledger) unschedule() {
	l.timing.Lock()
	defer l.timing.Unlock()
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

// abandoned reports whether the ledger has been closed.
func (l *Ledger) abandoned() bool {
	if l.closed == nil {
		return false
	}
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

// Save flushes anything still queued, now rather than at the end of the window.
// It is called on the way out; the ordinary path flushes as it goes, so this is
// only ever picking up a debounced burst or a file lock that was busy at the
// time.
func (l *Ledger) Save() error {
	l.unschedule()
	return l.flush()
}

// Close saves and then stops waiting for anything. An observation still trying
// to reach the file after this abandons its lock wait rather than holding the
// process open for it.
func (l *Ledger) Close() error {
	err := l.Save()
	l.closeOnce.Do(func() {
		if l.closed != nil {
			close(l.closed)
		}
	})
	// An observation that landed while the save was in the air has asked for a
	// window nothing is going to wait for now. Take it here instead, which is
	// quick either way: past this point the lock file is no longer waited on.
	l.unschedule()
	_ = l.flush()
	return err
}

// flush merges this process's queued observations into the file.
//
// The merge is the point. Several aforge processes may be running at once, each
// doing read-modify-write on the same small file, and a plain write would let
// the last one out overwrite everyone else's evidence. So the file is re-read
// under an exclusive lock, the queued observations are replayed onto whatever is
// there *now*, and the result becomes both the file and this process's own view
// — which is also how a long run picks up what a concurrent run has learned.
//
// None of that happens under the map lock. The queue is taken out under it, the
// file work runs holding only flushing, and the result is folded back in under
// it — so a routed call reading a rating waits for a map operation, never for a
// lock file, a re-parse and a rename. What that costs is a window in which an
// observation can arrive mid-flush, and the fold below is where it is paid: the
// file's merged view comes back first, then whatever queued while it was in the
// air is replayed on top, so no observation is ever both unflushed and unseen.
func (l *Ledger) flush() error {
	l.flushing.Lock()
	defer l.flushing.Unlock()

	l.mutex.Lock()
	if !l.unsaved {
		l.mutex.Unlock()
		return nil
	}
	pending := append([]observation(nil), l.pending...)
	aliases := make(map[string]string, len(l.aliases))
	for slug, resolved := range l.aliases {
		aliases[slug] = resolved
	}
	l.pending, l.unsaved = nil, false
	l.mutex.Unlock()

	merged, err := l.write(pending, aliases)
	if err != nil {
		// A flush blocked by another process costs a moment rather than the
		// evidence: the queue goes back, ahead of anything observed since.
		l.mutex.Lock()
		l.pending = append(pending, l.pending...)
		l.unsaved = true
		l.mutex.Unlock()
		return err
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, item := range l.pending {
		merged.entries[key(item.model, item.class)] = update(
			merged.entryOr(item.model, item.class, item.prior), item.positive, item.weight)
	}
	for slug, resolved := range l.aliases {
		merged.aliases[slug] = resolved
	}
	l.entries, l.aliases = merged.entries, merged.aliases
	return nil
}

// write is the file half of a flush: everything that touches the disk, and
// nothing that touches the live maps.
func (l *Ledger) write(pending []observation, aliases map[string]string) (*Ledger, error) {
	// Counted because the debounce is only worth having if it holds: the number
	// of times the file is taken is the thing under test, and it is not
	// otherwise visible from outside.
	l.writes.Add(1)
	unlock, err := l.lockFile()
	if err != nil {
		return nil, err
	}
	defer unlock()

	merged := &Ledger{entries: map[string]Entry{}, aliases: map[string]string{}}
	if data, err := os.ReadFile(l.path); err == nil {
		merged.adopt(decodeLedger(data))
	}
	for slug, resolved := range aliases {
		merged.aliases[slug] = resolved
	}
	for _, item := range pending {
		merged.entries[key(item.model, item.class)] = update(
			merged.entryOr(item.model, item.class, item.prior), item.positive, item.weight)
	}

	encoded, err := json.MarshalIndent(merged.file(), "", "  ")
	if err != nil {
		return nil, err
	}
	if err := replaceFile(l.path, encoded); err != nil {
		return nil, err
	}
	return merged, nil
}

func (l *Ledger) entryOr(model string, class provider.CallClass, prior float64) Entry {
	if entry, known := l.entries[key(model, class)]; known {
		return entry
	}
	return Entry{Model: model, Class: class, Rating: prior}
}

func (l *Ledger) adopt(file ledgerFile) {
	for _, entry := range file.Entries {
		if entry.Model == "" {
			continue
		}
		l.entries[key(entry.Model, entry.Class)] = entry
	}
	for slug, resolved := range file.Aliases {
		l.aliases[slug] = resolved
	}
}

func (l *Ledger) file() ledgerFile {
	file := ledgerFile{Entries: make([]Entry, 0, len(l.entries)), Aliases: l.aliases}
	for _, entry := range l.entries {
		file.Entries = append(file.Entries, entry)
	}
	sort.Slice(file.Entries, func(i, j int) bool {
		if file.Entries[i].Model != file.Entries[j].Model {
			return file.Entries[i].Model < file.Entries[j].Model
		}
		return file.Entries[i].Class < file.Entries[j].Class
	})
	return file
}

func decodeLedger(data []byte) ledgerFile {
	var file ledgerFile
	if err := json.Unmarshal(data, &file); err != nil {
		return ledgerFile{}
	}
	return file
}

// update applies one graded outcome. See the comment on the constants above for
// why each brake is here, and Verdict.Weight for why the last one is not a
// constant: how far an outcome may move a rating depends on how much of it was
// about the model.
//
// Count is the number of graded observations and is incremented whatever the
// weight, because it meters two things that are not the same question. The step
// size decays with how much has been seen, and the MinGraded gate asks how much
// has been seen — a down-weighted observation is still something the router
// looked at. What the weight buys is a smaller move, not a smaller count.
func update(entry Entry, positive bool, weight float64) Entry {
	if weight <= 0 {
		weight = 1
	}
	expected := 1 / (1 + math.Exp(-entry.Rating))
	observed := 0.0
	if positive {
		observed = 1
	}
	// The prior enters as a gradient term whose weight falls as 1/n, which is
	// what a prior worth one pseudo-observation looks like once it is spread
	// across the evidence that has arrived since.
	pull := entry.Rating / (priorVariance * float64(entry.Count+1))
	step := weight * rate(entry.Count) * (observed - expected - pull)
	step = math.Max(-maxStep, math.Min(maxStep, step))

	entry.Rating = math.Max(-ratingBound, math.Min(ratingBound, entry.Rating+step))
	entry.Count++
	entry.Updated = time.Now().UTC()
	return entry
}

// rate is the step size. It decays with evidence so that the first few
// observations move a new model quickly onto the scale and the hundredth barely
// moves it at all.
func rate(count int) float64 {
	return startingRate / (1 + float64(count)/rateHalfLife)
}

// Ability is the rating read as a success probability against an average call
// of its class, which is the form the cascade actually orders on.
func Ability(rating float64) float64 { return 1 / (1 + math.Exp(-rating)) }

func key(model string, class provider.CallClass) string {
	return model + "\x00" + string(class)
}

// lockFile takes exclusive access to a state file.
//
// Exclusive create is the one filesystem operation that is atomic everywhere
// this runs, so the lock is a file that only one process can make. The wait is
// bounded because losing an observation is survivable and blocking a run is not,
// and a lock older than the takeover window is assumed to belong to a process
// that died holding it — the alternative is that one crash disables learning
// permanently.
func (l *Ledger) lockFile() (func(), error) {
	const (
		attempts = 40
		interval = 25 * time.Millisecond
		stale    = 30 * time.Second
	)
	lock := l.path + ".lock"
	for attempt := 0; attempt < attempts; attempt++ {
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			file.Close()
			return func() { os.Remove(lock) }, nil
		}
		// A lock older than the takeover window belongs to a process that died
		// holding it. Removing it earns an immediate retry — but only when the
		// removal actually succeeded, because a `continue` on a failed takeover
		// is a tight spin that burns all forty attempts in microseconds and
		// reports contention that was never waited out.
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > stale {
			if removeErr := os.Remove(lock); removeErr == nil {
				continue
			}
		}
		if err := l.pause(interval); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("router ledger: %s is locked by another process", lock)
}

// pause is the lock retry wait, and it is abandonable. A closed ledger stops
// waiting: the alternative is a shutdown that sits through a second of retries
// for a file another process is holding, to save evidence the next run would
// re-derive anyway.
func (l *Ledger) pause(interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-l.closed:
		return errors.New("router ledger: closed while waiting for the lock")
	case <-timer.C:
		return nil
	}
}
