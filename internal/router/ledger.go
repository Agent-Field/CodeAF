package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
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
type Ledger struct {
	path string

	mutex   sync.Mutex
	entries map[string]Entry
	aliases map[string]string
	pending []observation
	unsaved bool
}

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
	ledger := &Ledger{path: path, entries: map[string]Entry{}, aliases: map[string]string{}}
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
	l.mutex.Lock()
	defer l.mutex.Unlock()
	entry, known := l.entries[key(model, class)]
	if !known {
		return prior, 0
	}
	return entry.Rating, entry.Count
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
	defer l.mutex.Unlock()
	l.entries[key(model, class)] = update(l.entryOr(model, class, prior), positive, weight)
	l.pending = append(l.pending, observation{model: model, class: class, prior: prior, positive: positive, weight: weight})
	l.unsaved = true
	// Flushed as it goes rather than at the end of the run: a run that is
	// interrupted has still learned what it learned, and the cost is one locked
	// read-modify-write of a small file after a call that took seconds.
	_ = l.flush()
}

// Alias records which dated snapshot a floating slug actually served. The
// resolved model is the response's own `model` field, so this is the provider
// telling us what it ran rather than us guessing.
func (l *Ledger) Alias(slug, resolved string) {
	if slug == "" || resolved == "" || slug == resolved {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.aliases[slug] == resolved {
		return
	}
	l.aliases[slug] = resolved
	l.unsaved = true
	_ = l.flush()
}

// Resolve maps a configured slug onto the snapshot last seen behind it. Ratings
// are read through it so that selection and recording agree about which model
// they are talking about.
func (l *Ledger) Resolve(slug string) string {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if resolved, known := l.aliases[slug]; known {
		return resolved
	}
	return slug
}

// Entries returns everything known, ordered for reading: strongest first within
// a class, classes alphabetically.
func (l *Ledger) Entries() []Entry {
	l.mutex.Lock()
	defer l.mutex.Unlock()
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
	l.mutex.Lock()
	defer l.mutex.Unlock()
	copied := make(map[string]string, len(l.aliases))
	for slug, resolved := range l.aliases {
		copied[slug] = resolved
	}
	return copied
}

// Save flushes anything still queued. It is called on the way out; the ordinary
// path flushes as it goes, so this is only ever picking up after a file lock
// that was busy at the time.
func (l *Ledger) Save() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.flush()
}

// flush merges this process's queued observations into the file. The caller
// holds the mutex.
//
// The merge is the point. Several aforge processes may be running at once, each
// doing read-modify-write on the same small file, and a plain write would let
// the last one out overwrite everyone else's evidence. So the file is re-read
// under an exclusive lock, the queued observations are replayed onto whatever is
// there *now*, and the result becomes both the file and this process's own view
// — which is also how a long run picks up what a concurrent run has learned.
func (l *Ledger) flush() error {
	if !l.unsaved {
		return nil
	}
	unlock, err := lockFile(l.path)
	if err != nil {
		return err
	}
	defer unlock()

	merged := &Ledger{entries: map[string]Entry{}, aliases: map[string]string{}}
	if data, err := os.ReadFile(l.path); err == nil {
		merged.adopt(decodeLedger(data))
	}
	for slug, resolved := range l.aliases {
		merged.aliases[slug] = resolved
	}
	for _, item := range l.pending {
		merged.entries[key(item.model, item.class)] = update(
			merged.entryOr(item.model, item.class, item.prior), item.positive, item.weight)
	}

	encoded, err := json.MarshalIndent(merged.file(), "", "  ")
	if err != nil {
		return err
	}
	if err := replaceFile(l.path, encoded); err != nil {
		return err
	}
	l.entries, l.aliases, l.pending, l.unsaved = merged.entries, merged.aliases, nil, false
	return nil
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
func lockFile(path string) (func(), error) {
	const (
		attempts = 40
		interval = 25 * time.Millisecond
		stale    = 30 * time.Second
	)
	lock := path + ".lock"
	for attempt := 0; attempt < attempts; attempt++ {
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			file.Close()
			return func() { os.Remove(lock) }, nil
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > stale {
			os.Remove(lock)
			continue
		}
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("router ledger: %s is locked by another process", lock)
}
