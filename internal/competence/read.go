package competence

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// THE READ IS BOUNDED AND IT IS ONE READ.
//
// Every question — the map, a page of evidence — reads the whole ledger once,
// folds it, and answers. Nothing is cached between calls and nothing polls: a
// surface asks when it opens and when the person asks again.
//
// A CLASS IS A CLAIM ABOUT EVERY RESULT IN ITS WINDOW, so a read that did not
// see every result answers NOTHING rather than a class. A ledger larger than
// [DefaultMaxBytes] is [ErrHistoryLimit], and a ledger holding a row that
// cannot be read is [ErrUnreadableHistory]: either way an old failed check may
// be exactly what was missed, and a Strong drawn without it would be false.
// Nothing is skipped, cut, guessed at or rewritten to get an answer; an empty
// or absent ledger is the one honest "no evidence yet".

// DefaultMaxBytes is the largest ledger one read answers for. A row is a few
// hundred bytes, so this is on the order of a hundred thousand results; past
// it the read is [ErrHistoryLimit], never a partial answer.
const DefaultMaxBytes int64 = 32 << 20

// The evidence page bounds.
const (
	DefaultEvidenceLimit = 7
	MaxEvidenceLimit     = 100
)

// ErrUnknownScope is a filter or scope id the map does not hold for this model.
var ErrUnknownScope = errors.New("no such scope for this model")

// ErrHistoryLimit is a ledger larger than one bounded read takes. Classifying
// only its newest bytes could call a model strong while an older failed check
// still inside the window went unseen, so no map or evidence is returned.
var ErrHistoryLimit = errors.New("checked-result history exceeds this bounded read; competence unavailable")

// ErrUnreadableHistory is a ledger holding a row this reader cannot take as a
// checked result: not JSON, over-long, of a format or kind it does not know,
// or missing what a result names. The row may have been a failed check, so no
// map or evidence is returned. The ledger is left exactly as it is.
var ErrUnreadableHistory = errors.New("checked-result history has an unreadable row; competence unavailable")

// ErrBadCursor is a cursor that is not this package's, or not for this
// model and scope.
var ErrBadCursor = errors.New("the cursor is not one this list gave out")

// Reader answers questions about one ledger.
type Reader struct {
	// Path is the ledger; empty is [LedgerPath].
	Path string
	// Policy is the grouping; the zero value is [DefaultPolicy].
	Policy Policy
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// MaxBytes bounds the read; zero is [DefaultMaxBytes].
	MaxBytes int64
	// PlaceAdded says whether codeaf still works in a folder. Nil answers yes
	// for every folder, so nothing is called removed that nobody checked.
	PlaceAdded func(path string) bool
	// SourceGone says what of a result's source was deleted: "task",
	// "conversation", or "" for nothing or unknown. Nil is always "".
	SourceGone func(Source) string
	// SourceTitle is the owning conversation's title as it stands now, read
	// for the page being answered and never stored: a title is renamed, a
	// result is not. Nil, or "", is no title.
	SourceTitle func(Source) string
}

// Place is a folder results were earned in.
type Place struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Kind    string `json:"place_kind"`
	Removed bool   `json:"removed"`
}

// Work is one kind of work, pooled.
type Work struct {
	ID    string `json:"id"`
	Words string `json:"words"`
}

// Scope is one model's results in one place at one kind of work.
type Scope struct {
	ID      string    `json:"id"`
	Place   Place     `json:"place"`
	Work    Work      `json:"work"`
	Class   Class     `json:"class"`
	Results int       `json:"results"`
	Passed  int       `json:"passed"`
	Failed  int       `json:"failed"`
	Undone  int       `json:"undone"`
	LastAt  time.Time `json:"last_at"`
	// Since is the start of the counted window, and zero on a stale scope,
	// whose counts are everything it has.
	Since time.Time `json:"since,omitzero"`
}

// ModelSummary is one model that has results.
type ModelSummary struct {
	ID      string    `json:"id"`
	Results int       `json:"results"`
	LastAt  time.Time `json:"last_at"`
}

// Filter is one entry of the scope menu: a place or a kind of work.
type Filter struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	PlaceKind string `json:"place_kind,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	Words     string `json:"words,omitempty"`
}

// The filter kinds.
const (
	FilterPlace = "place"
	FilterWork  = "work"
)

// Group is one class's scopes.
type Group struct {
	Class  Class   `json:"class"`
	Scopes []Scope `json:"scopes"`
}

// Totals is what the groups shown hold.
type Totals struct {
	Scopes  int `json:"scopes"`
	Results int `json:"results"`
}

// PolicyView is the policy as a reader is told it.
type PolicyView struct {
	StaleAfterDays int     `json:"stale_after_days"`
	StrongAtLeast  float64 `json:"strong_at_least"`
	WeakAtMost     float64 `json:"weak_at_most"`
	MinResults     int     `json:"min_results"`
}

// Map is one model's competence, grouped.
type Map struct {
	Model   string         `json:"model"`
	Models  []ModelSummary `json:"models"`
	Scope   string         `json:"scope"`
	Filters []Filter       `json:"filters"`
	Groups  []Group        `json:"groups"`
	Totals  Totals         `json:"totals"`
	Policy  PolicyView     `json:"policy"`
	ReadAt  time.Time      `json:"read_at"`
	// Truncated is always false on an answer, kept for the wire's shape: a
	// ledger past one read is [ErrHistoryLimit], never a map of its newest
	// part. Omitted says what this answer left out for size.
	Truncated bool    `json:"truncated"`
	Omitted   Omitted `json:"omitted"`
}

// Counts is how a list of evidence came out.
type Counts struct {
	All    int `json:"all"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Undone int `json:"undone"`
}

// Evidence is one result as the drawer shows it.
type Evidence struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	At            time.Time `json:"at"`
	Outcome       Outcome   `json:"outcome"`
	Source        Source    `json:"source"`
	SourceMissing string    `json:"source_missing"`
	// SourceTitle is the owning conversation's current title, when it has one.
	SourceTitle string     `json:"source_title,omitempty"`
	Check       *CheckRun  `json:"check,omitempty"`
	Checks      []CheckRun `json:"checks"`
	UndoneAt    time.Time  `json:"undone_at,omitzero"`
}

// EvidencePage is one page of a scope's evidence.
type EvidencePage struct {
	Model      string     `json:"model"`
	Scope      string     `json:"scope"`
	Counts     Counts     `json:"counts"`
	Items      []Evidence `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// item is one result folded out of the ledger.
type item struct {
	id        string
	model     string
	place     string
	placeKind string
	work      string
	title     string
	source    Source
	at        time.Time
	// result is the whole outcome the row recorded, decided over every check
	// that ran — never re-derived from the bounded list beside it.
	result   Outcome
	checks   []CheckRun
	undoneAt time.Time
}

func (i *item) outcome() Outcome {
	if !i.undoneAt.IsZero() {
		return Undone
	}
	return i.result
}

// folded is one read of the ledger.
type folded struct {
	byModel   map[string][]*item
	truncated bool // always false: a ledger past the bound never folds
	now       time.Time
	policy    Policy
}

func (r Reader) read(ctx context.Context) (*folded, error) {
	policy := r.Policy
	if policy == (Policy{}) {
		policy = DefaultPolicy()
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	path := r.Path
	if path == "" {
		path = LedgerPath()
	}
	limit := r.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	out := &folded{byModel: map[string][]*item{}, now: now().UTC(), policy: policy}
	// ONE PASS, ROW BY ROW. Each row is folded into its result as it is read,
	// so what is held is one entry per result and never the file's rows.
	results := map[string]*item{}
	undos := map[string]time.Time{}
	err := eachRow(ctx, path, limit, func(record Record) {
		key := record.Model + "\x00" + record.ID
		switch record.Kind {
		case KindResult:
			// THE LATEST RESULT STANDS. A task reopened and settled again is a
			// later fact about the same work.
			if it := results[key]; it != nil && record.At.Before(it.at) {
				return
			}
			it := &item{
				id: record.ID, model: record.Model, place: record.Place, placeKind: record.PlaceKind,
				work: record.Work, title: record.Title, source: record.Source, at: record.At,
				result: record.Outcome, checks: record.Checks,
			}
			if it.result != Passed && it.result != Failed {
				it.result = Passed
				for _, check := range record.Checks {
					if check.Outcome == Failed {
						it.result = Failed
					}
				}
			}
			results[key] = it
		case KindUndo:
			if record.At.After(undos[key]) {
				undos[key] = record.At
			}
		}
	})
	if err != nil {
		return nil, err
	}
	// AN UNDO MARKS A RESULT; IT IS NEVER ONE. Work that finished with no check
	// never counts, and taking it back does not make it count: an undo with no
	// checked result beside it is dropped. An undo older than the result
	// standing is of earlier work, done again since.
	seen := 0
	for key, it := range results {
		if seen++; seen%4096 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if at, undone := undos[key]; undone && !at.Before(it.at) {
			it.undoneAt = at
		}
		out.byModel[it.model] = append(out.byModel[it.model], it)
	}
	return out, nil
}

// maxRowBytes is the longest line read as a row. The writer's own bounds keep
// every row far under it; a longer line is not one of this ledger's rows.
const maxRowBytes = 64 << 10

// eachRow hands every row of the ledger to fold, oldest first, and answers
// for the WHOLE ledger or for none of it.
//
// A ledger over limit bytes is [ErrHistoryLimit] before a byte of it is
// folded. A line that is over-long, is not JSON, is of a format or kind this
// reader does not know, or lacks what a result names is
// [ErrUnreadableHistory]: skipping it would cost a result silently, and a
// skipped failure flips a class. Only a blank line is ignored, and nothing is
// deleted, repaired or recovered by guessing.
func eachRow(ctx context.Context, path string, limit int64, fold func(Record)) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > limit {
		return ErrHistoryLimit
	}
	// The reader is bounded too: a ledger that grew since the stat is read as
	// it stood, and rows appended after are a later question's.
	reader := bufio.NewReaderSize(io.LimitReader(file, limit), 64<<10)
	var line []byte
	overlong := false
	for count := 0; ; count++ {
		if count%1024 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		chunk, prefix, err := reader.ReadLine()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(line)+len(chunk) <= maxRowBytes {
			line = append(line, chunk...)
		} else {
			overlong = true
		}
		if prefix {
			continue
		}
		whole := bytes.TrimSpace(line)
		line = line[:0]
		if overlong {
			return ErrUnreadableHistory
		}
		if len(whole) == 0 {
			continue
		}
		var record Record
		if json.Unmarshal(whole, &record) != nil || record.V != recordVersion {
			return ErrUnreadableHistory
		}
		if record.Model == "" || record.ID == "" || record.Place == "" || record.Work == "" || record.At.IsZero() {
			return ErrUnreadableHistory
		}
		switch record.Kind {
		case KindResult:
			if len(record.Checks) == 0 {
				return ErrUnreadableHistory
			}
		case KindUndo:
		default:
			return ErrUnreadableHistory
		}
		fold(record)
	}
}

// shortID is a stable id for one string.
func shortID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + ":" + hex.EncodeToString(sum[:8])
}

// scopeOf is one item's scope id and its parts' ids.
//
// A SCOPE IS A PLACE AND THE ENGINE'S EXACT KIND OF WORK ([WorkOf]), never a
// pooled one. Pooling by overlap would name a scope after whichever kind
// arrived first, so the scope's id would move the day that result left the
// read window; an exact key is the same id for as long as the work exists, and
// grouping by it is one pass.
func scopeOf(it *item) (scope, place, work string) {
	return shortID("scope", it.place, it.work), shortID("place", it.place), shortID("work", it.work)
}

// scopeAcc gathers one scope's items.
type scopeAcc struct {
	scope Scope
	items []*item
}

// scopes builds every scope of one model, each with its class and the items
// it counts, newest first.
//
// IT STOPS WHEN ITS CALLER DOES. The deadline a read was given is asked as the
// groups are built, not only while the file is scanned, so a timed-out read
// leaves no work running behind the answer.
func (f *folded) scopes(ctx context.Context, model string) ([]*scopeAcc, error) {
	byKey := map[string]*scopeAcc{}
	var order []string
	for index, it := range f.byModel[model] {
		if index%4096 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		key := it.place + "\x00" + it.work
		acc := byKey[key]
		if acc == nil {
			id, placeID, workID := scopeOf(it)
			acc = &scopeAcc{scope: Scope{
				ID:    id,
				Place: Place{ID: placeID, Path: it.place, Kind: it.placeKind},
				Work:  Work{ID: workID, Words: it.work},
			}}
			byKey[key] = acc
			order = append(order, key)
		}
		acc.items = append(acc.items, it)
		if it.at.After(acc.scope.LastAt) {
			acc.scope.LastAt = it.at
		}
	}
	since := f.now.Add(-f.policy.StaleAfter)
	out := make([]*scopeAcc, 0, len(order))
	for index, key := range order {
		if index%1024 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		acc := byKey[key]
		sort.Slice(acc.items, func(a, b int) bool { return newer(acc.items[a], acc.items[b]) })
		if acc.scope.LastAt.Before(since) {
			acc.scope.Class = Stale
		} else {
			acc.scope.Since = since
			kept := acc.items[:0]
			for _, it := range acc.items {
				if !it.at.Before(since) {
					kept = append(kept, it)
				}
			}
			acc.items = kept
		}
		for _, it := range acc.items {
			acc.scope.Results++
			switch it.outcome() {
			case Passed:
				acc.scope.Passed++
			case Failed:
				acc.scope.Failed++
			case Undone:
				acc.scope.Undone++
			}
		}
		if acc.scope.Class != Stale {
			acc.scope.Class = f.policy.classify(acc.scope.Results, acc.scope.Passed)
		}
		out = append(out, acc)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].scope.LastAt.Equal(out[b].scope.LastAt) {
			return out[a].scope.LastAt.After(out[b].scope.LastAt)
		}
		return out[a].scope.ID < out[b].scope.ID
	})
	return out, ctx.Err()
}

func newer(a, b *item) bool {
	if !a.at.Equal(b.at) {
		return a.at.After(b.at)
	}
	return a.id > b.id
}

// The answer bounds. A map is drawn whole, so what it may carry is bounded
// as surely as what is read; past a bound the newest are kept, and Omitted
// says how many were left out so an absence is never mistaken for none.
const (
	MaxMapScopes  = 300
	MaxMapFilters = 200
	MaxMapModels  = 50
)

// Omitted is how many of each list a map left out for size. Totals still count
// everything that matched.
type Omitted struct {
	Scopes  int `json:"scopes"`
	Filters int `json:"filters"`
	Models  int `json:"models"`
}

// Map answers one model's map, filtered to one scope id, place id or work id
// when scope is not empty. With no model it answers the model whose evidence
// is newest, and with no evidence at all it answers four empty groups.
func (r Reader) Map(ctx context.Context, model, scope string) (Map, error) {
	f, err := r.read(ctx)
	if err != nil {
		return Map{}, err
	}
	out := Map{
		Scope:     strings.TrimSpace(scope),
		Models:    f.models(),
		Filters:   []Filter{},
		ReadAt:    f.now,
		Truncated: f.truncated,
		Policy: PolicyView{
			StaleAfterDays: int(f.policy.StaleAfter / (24 * time.Hour)),
			StrongAtLeast:  f.policy.StrongAtLeast,
			WeakAtMost:     f.policy.WeakAtMost,
			MinResults:     f.policy.MinResults,
		},
	}
	out.Model = strings.TrimSpace(model)
	if out.Model == "" && len(out.Models) > 0 {
		out.Model = out.Models[0].ID
	}
	if len(out.Models) > MaxMapModels {
		out.Omitted.Models = len(out.Models) - MaxMapModels
		out.Models = out.Models[:MaxMapModels]
	}
	// WHETHER A PLACE IS STILL WORKED IN is asked once per place, and only of
	// the places this answer names.
	removed := map[string]bool{}
	isRemoved := func(path string) bool {
		gone, asked := removed[path]
		if !asked {
			gone = r.PlaceAdded != nil && !r.PlaceAdded(path)
			removed[path] = gone
		}
		return gone
	}
	groups := map[Class][]Scope{}
	seen := map[string]bool{}
	known := out.Scope == ""
	shown := 0
	all, err := f.scopes(ctx, out.Model)
	if err != nil {
		return Map{}, err
	}
	for index, acc := range all {
		if index%1024 == 0 && ctx.Err() != nil {
			return Map{}, ctx.Err()
		}
		place, work := acc.scope.Place, acc.scope.Work
		for _, filter := range []Filter{
			{ID: place.ID, Kind: FilterPlace, Path: place.Path, PlaceKind: place.Kind},
			{ID: work.ID, Kind: FilterWork, Words: work.Words},
		} {
			if seen[filter.ID] {
				continue
			}
			seen[filter.ID] = true
			if len(out.Filters) == MaxMapFilters {
				out.Omitted.Filters++
				continue
			}
			if filter.Kind == FilterPlace {
				filter.Removed = isRemoved(filter.Path)
			}
			out.Filters = append(out.Filters, filter)
		}
		switch out.Scope {
		case "", acc.scope.ID, place.ID, work.ID:
			known = true
		default:
			continue
		}
		out.Totals.Scopes++
		out.Totals.Results += acc.scope.Results
		if shown == MaxMapScopes {
			out.Omitted.Scopes++
			continue
		}
		shown++
		acc.scope.Place.Removed = isRemoved(place.Path)
		groups[acc.scope.Class] = append(groups[acc.scope.Class], acc.scope)
	}
	if !known {
		return Map{}, ErrUnknownScope
	}
	for _, class := range Classes {
		scopes := groups[class]
		if scopes == nil {
			scopes = []Scope{}
		}
		out.Groups = append(out.Groups, Group{Class: class, Scopes: scopes})
	}
	return out, nil
}

// models is every model with a result, newest evidence first.
func (f *folded) models() []ModelSummary {
	out := make([]ModelSummary, 0, len(f.byModel))
	for model, list := range f.byModel {
		summary := ModelSummary{ID: model, Results: len(list)}
		for _, it := range list {
			if it.at.After(summary.LastAt) {
				summary.LastAt = it.at
			}
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].LastAt.Equal(out[b].LastAt) {
			return out[a].LastAt.After(out[b].LastAt)
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// Evidence answers one page of one scope's results, newest first, filtered to
// one outcome when outcome is not empty.
func (r Reader) Evidence(ctx context.Context, model, scope string, outcome Outcome, cursor string, limit int) (EvidencePage, error) {
	model, scope = strings.TrimSpace(model), strings.TrimSpace(scope)
	switch outcome {
	case "", Passed, Failed, Undone:
	default:
		return EvidencePage{}, fmt.Errorf("an outcome is passed, failed or undone, not %q", outcome)
	}
	if limit <= 0 {
		limit = DefaultEvidenceLimit
	}
	if limit > MaxEvidenceLimit {
		limit = MaxEvidenceLimit
	}
	var after *cursorAt
	if cursor != "" {
		decoded, err := decodeCursor(cursor, model, scope)
		if err != nil {
			return EvidencePage{}, err
		}
		after = &decoded
	}
	f, err := r.read(ctx)
	if err != nil {
		return EvidencePage{}, err
	}
	var found *scopeAcc
	all, err := f.scopes(ctx, model)
	if err != nil {
		return EvidencePage{}, err
	}
	for _, acc := range all {
		if acc.scope.ID == scope {
			found = acc
			break
		}
	}
	if found == nil {
		return EvidencePage{}, ErrUnknownScope
	}
	page := EvidencePage{Model: model, Scope: scope, Items: []Evidence{}}
	// gone remembers what was asked about each source on this page, so a page
	// of results from one conversation stats it once.
	gone := map[string]string{}
	for index, it := range found.items {
		if index%1024 == 0 && ctx.Err() != nil {
			return EvidencePage{}, ctx.Err()
		}
		got := it.outcome()
		page.Counts.All++
		switch got {
		case Passed:
			page.Counts.Passed++
		case Failed:
			page.Counts.Failed++
		case Undone:
			page.Counts.Undone++
		}
		if outcome != "" && got != outcome {
			continue
		}
		if after != nil && !after.before(it) {
			continue
		}
		if len(page.Items) == limit {
			if page.NextCursor == "" {
				last := page.Items[len(page.Items)-1]
				page.NextCursor = encodeCursor(model, scope, last.At, last.ID)
			}
			continue
		}
		page.Items = append(page.Items, r.evidence(it, got, gone))
	}
	return page, nil
}

func (r Reader) evidence(it *item, got Outcome, gone map[string]string) Evidence {
	out := Evidence{
		ID: it.id, Title: it.title, At: it.at, Outcome: got,
		Source:   Source{Kind: it.source.Kind, Agent: it.source.Agent, Task: it.source.Task, Turn: it.source.Turn},
		Checks:   append([]CheckRun{}, it.checks...),
		UndoneAt: it.undoneAt,
	}
	if r.SourceGone != nil {
		key := it.source.Transcript + "\x00" + it.source.Task
		missing, asked := gone[key]
		if !asked {
			missing = r.SourceGone(it.source)
			gone[key] = missing
		}
		out.SourceMissing = missing
	}
	if r.SourceTitle != nil && out.SourceMissing != SourceConversation {
		key := "title\x00" + it.source.Transcript
		title, asked := gone[key]
		if !asked {
			title = r.SourceTitle(it.source)
			gone[key] = title
		}
		out.SourceTitle = title
	}
	// THE DECIDING CHECK is the first that failed, or else the last that ran.
	for index := range it.checks {
		if it.checks[index].Outcome == Failed {
			check := it.checks[index]
			out.Check = &check
			break
		}
	}
	if out.Check == nil && len(it.checks) > 0 {
		check := it.checks[len(it.checks)-1]
		out.Check = &check
	}
	return out
}

// cursorAt is where a page ended.
type cursorAt struct {
	at time.Time
	id string
}

// before reports whether it comes after the cursor in newest-first order.
func (c cursorAt) before(it *item) bool {
	if !it.at.Equal(c.at) {
		return it.at.Before(c.at)
	}
	return it.id < c.id
}

func encodeCursor(model, scope string, at time.Time, id string) string {
	raw := strings.Join([]string{"1", model, scope, strconv.FormatInt(at.UnixNano(), 10), id}, "\x00")
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor, model, scope string) (cursorAt, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return cursorAt{}, ErrBadCursor
	}
	parts := strings.SplitN(string(raw), "\x00", 5)
	if len(parts) != 5 || parts[0] != "1" || parts[1] != model || parts[2] != scope {
		return cursorAt{}, ErrBadCursor
	}
	nanos, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return cursorAt{}, ErrBadCursor
	}
	return cursorAt{at: time.Unix(0, nanos).UTC(), id: parts[4]}, nil
}
