package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/pool/index"
)

// THE WORD THAT ASKS THE CATALOG TO ANSWER A TIER ROW.
//
// A tier row names a model id, and a model id is a bet the person made one day
// against a catalog that keeps moving under it. `auto` is the row that
// declines to bet: the seat's model is computed from the catalog's own
// published figures — the three capability indexes against the three prices,
// under each seat's call shape (internal/crewpick) — every time the row is
// read, through [autoRow], the one seam both ladders pass through.
//
// NOTHING IS REWRITTEN. The row stays `auto` on disk and the id is derived on
// every read, so a catalog that moves moves the seat with it, and a profile
// never holds a model id this build chose for somebody.

// AutoValue is the bare word a tier row holds to have its seat computed from
// the catalog.
const AutoValue = "auto"

// IsAuto reports whether a tier value is the bare word, case folded with the
// surrounding space ignored. `auto:high` is not it: the rows own the
// `:<level>` notation, and a suffixed word is a model id with a level, handed
// on whole the way every other one is.
func IsAuto(value string) bool {
	return strings.ToLower(strings.TrimSpace(value)) == AutoValue
}

// AutoModels is how the catalog reaches seat resolution: the binary holding
// the catalog sets it ONCE AT START-UP, from its non-blocking read, and never
// a fetch — a tier row that says auto resolves from whatever the catalog
// already holds, the same posture every other catalog reader here keeps. Nil,
// and a func answering with no rows, are ordinary states rather than errors:
// a row that says auto then reads the family's table row ([autoRow]), which
// is the answer it had before this word existed.
var AutoModels func() []catalog.Model

// AutoIndex is how the Model Pool's measurement index reaches seat
// resolution: the binary holding the index sets it ONCE AT START-UP, from
// whatever read it already made, and never a fetch — a tier row that says
// auto resolves from the index already in hand, the same posture AutoModels
// keeps. Nil is an ordinary state, not an error: a row that says auto then
// reads the catalog's own figures alone, which is what it read before this
// seam existed.
var AutoIndex func() *index.Index

// AutoOwnCells is how an install's own judged scores reach seat resolution:
// the binary holding the pool sets it ONCE AT START-UP, from the own sheet it
// read from disk under the pool directory, the same posture AutoIndex keeps.
// Nil is an ordinary state, not an error: the prior then reads the index's
// cells alone, which is what it read before this seam existed. The cells are
// never held to the index's min_installs — an install's own scores are its
// own evidence, one observation of which is worth having.
var AutoOwnCells func() []crewpick.Cell

// PoolQualityMetric names the index metric the picker reads a measured seat
// quality from: a gaussian metric whose mean is on the same 0-100 scale
// crewpick scores a seat on, with one cell per role and model.
const PoolQualityMetric = "role_quality"

// AutoPick is the pick with the measured quality the Model Pool holds ALWAYS
// carried as a prior: [AutoPickWith] with [autoPrior], whatever a profile's
// pick word says. No seat resolves through it any more — the bare `auto` row
// ([autoRow]) and the pick row's words ([pickedModel]) decide the prior
// together in [priorFor] — and it stays for a caller that wants the measured
// computation regardless of the pick word.
func AutoPick(tier, family, preset string, models []catalog.Model) (modelID string, ok bool) {
	return AutoPickWith(tier, family, preset, models, autoPrior(autoIndex()))
}

// AutoPickWith is the pick itself, with the measured quality NAMED: prior is
// carried into the front as a rating seats read on top of the catalog's own
// published scores, and a nil prior leaves every seat on those scores alone —
// the answer the `catalog` pick word carries, where `learn` passes the pool's
// measurements and the person's own judged runs ([autoPrior]).
//
// It is PURE: no disk, no network, the rows are only read, and the same rows
// give the same answer however often it is asked and in whatever order they
// arrive (crewpick breaks its ties by id, not by order). Each row is read as
// a crewpick candidate — the three indexes, the three prices AS PUBLISHED (a
// uniform scale changes no pick on a front sorted by bill), whether the
// provider published a cache-read price at all, the window, the modalities
// and the parameters — and a row whose price the provider did not publish is
// left out, because a model that may cost anything has no place in a pick
// that is about cost.
//
// tier names the seat the pick is read from: worker, high and mastermind have
// answers, reflex and low and any other word do not. family narrows the
// shelf: `open` picks off the open-weight rows, every other word off the
// whole catalog. preset is one of the three crew words, and any other word
// has no answer. No rows, an empty front or an empty id are no answer too;
// no answer is ok false and an empty id, which is the caller's cue to read
// the family's table row instead ([autoRow], [pickedSeat]).
func AutoPickWith(tier, family, preset string, models []catalog.Model, prior crewpick.Prior) (modelID string, ok bool) {
	seat, ok := autoSeat(tier)
	if !ok || len(models) == 0 {
		return "", false
	}
	fam := crewpick.All
	if normalCrewSource(family) == CrewSourceOpen {
		fam = crewpick.Open
	}
	candidates := autoCandidates(models)
	if len(candidates) == 0 {
		return "", false
	}
	frugal, balanced, max := crewpick.Presets(crewpick.FrontWith(candidates, crewpick.DefaultShapes(), fam, prior))
	var pick crewpick.Crew
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case CrewFrugal:
		pick = frugal
	case CrewBalanced:
		pick = balanced
	case CrewMax:
		pick = max
	default:
		return "", false
	}
	var id string
	switch seat {
	case crewpick.Worker:
		id = pick.Worker
	case crewpick.High:
		id = pick.High
	case crewpick.Mastermind:
		id = pick.Mastermind
	}
	return id, id != ""
}

// autoIndex is [AutoIndex] read with its ordinary absence folded into one
// answer.
func autoIndex() *index.Index {
	if AutoIndex == nil {
		return nil
	}
	return AutoIndex()
}

// autoPrior reads the measured quality of the index AND of this install's own
// sheet into a crewpick prior. The index's cells of PoolQualityMetric are kept
// only when the metric is declared gaussian — a mean on any other scale would
// be blended against figures it does not share units with — and dropped by
// PriorFromCells below the index's own min_installs, resolved through the
// index's canonical ids so an alias meets its candidate. The own sheet's cells
// are this install's own evidence: they are read through a second
// PriorFromCells at a floor of one and folded into the index's prior seat by
// seat, the means combined by observation count. Their scores are on the
// 0-100 scale a judge answers on whatever the index's metric says, so they
// are read even beside an index whose role_quality is not gaussian. A nil
// index and a nil seam answer no prior, which leaves every seat on the
// catalog quality.
func autoPrior(idx *index.Index) crewpick.Prior {
	canonical := autoCanonical(idx)
	var indexPrior crewpick.Prior
	if idx != nil {
		if kind, ok := idx.Kind(PoolQualityMetric); ok && kind == "gaussian" {
			cells := idx.Cells(PoolQualityMetric)
			measured := make([]crewpick.Cell, 0, len(cells))
			for _, c := range cells {
				measured = append(measured, crewpick.Cell{Role: c.Role, Model: c.Model, Mean: c.Mean, N: c.N})
			}
			indexPrior = crewpick.PriorFromCells(measured, idx.MinInstalls(), idx.Canonical)
		}
	}
	var ownPrior crewpick.Prior
	if own := autoOwnCells(); len(own) > 0 {
		ownPrior = crewpick.PriorFromCells(own, 1, canonical)
	}
	return crewpick.MergePriors(indexPrior, ownPrior)
}

// autoCanonical is the index's canonical ids, nil when there is no index. The
// own sheet's ids are the ones this install resolved its seats to, and a nil
// canonical leaves them as they stand.
func autoCanonical(idx *index.Index) func(string) string {
	if idx == nil {
		return nil
	}
	return idx.Canonical
}

// autoOwnCells is [AutoOwnCells] read with its ordinary absence folded into
// one answer.
func autoOwnCells() []crewpick.Cell {
	if AutoOwnCells == nil {
		return nil
	}
	return AutoOwnCells()
}

// autoSeat is the tier word that names which of crewpick's three seats the
// pick is read from. The two tiers that never vary — reflex and small work —
// have no answer here, the same law their columns in the shipped tables keep.
func autoSeat(tier string) (crewpick.Seat, bool) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case ModelTierWorker:
		return crewpick.Worker, true
	case ModelTierHigh:
		return crewpick.High, true
	case ModelTierMastermind:
		return crewpick.Mastermind, true
	}
	return 0, false
}

// autoCandidates reads every published row as a candidate. Rows with no
// published price are dropped here, where the reason is nameable, rather than
// being carried into the picker to fall out of its own candidacy law.
func autoCandidates(models []catalog.Model) []crewpick.Candidate {
	candidates := make([]crewpick.Candidate, 0, len(models))
	for _, model := range models {
		if model.PriceUnknown {
			continue
		}
		candidates = append(candidates, crewpick.Candidate{
			ID:              model.ID,
			Open:            model.OpenWeights,
			Intelligence:    model.IntelligenceIndex,
			Coding:          model.CodingIndex,
			Agentic:         model.AgenticIndex,
			PromptPrice:     model.PromptPrice,
			CompletionPrice: model.CompletionPrice,
			CacheReadPrice:  model.CacheReadPrice,
			HasCacheRead:    model.CacheReadPrice > 0,
			Context:         model.ContextLength,
			Images:          listHolds(model.InputModalities, "image"),
			Tools:           listHolds(model.Parameters, "tools"),
		})
	}
	return candidates
}

// listHolds says whether a row's own list carries the word, case folded — the
// catalog cleans its lists, but a reader that folds is one fewer assumption.
func listHolds(words []string, word string) bool {
	for _, held := range words {
		if strings.EqualFold(strings.TrimSpace(held), word) {
			return true
		}
	}
	return false
}

// ── the seam ────────────────────────────────────────────────────────────────

// autoRow is THE ONE SEAM both ladders resolve a tier row that says auto
// through — [tierSeatUnder], which the conversation, the settings sheet and
// the crew word read, and [resolveSeat], which every headless door climbs. A
// second copy in each would be a seat that means one thing in chat and
// another headless, which is the defect this file exists to prevent one
// ladder at a time.
//
// The answer is the computed id, on its own rung, when the catalog can
// compute one; otherwise the family's table row FOR THE PRESET, on the table
// rung — the same id a preset write would have landed, and never `auto` and
// never empty.
func autoRow(profileDir, family, tier string) (model string, source SeatSource, preset string) {
	preset = crewPresetUnder(profileDir, family)
	// THE ANSWER IS THE PICK WORD'S OWN ANSWER, computed the way a
	// pick-computed seat's is ([pickedModel]): the prior is decided once, in
	// [priorFor] — the pool's measurements under `learn`, the published
	// figures alone under every other word — so the two seams cannot disagree
	// about what `catalog` means, and the rung names the word that ran
	// ([computedRung]) so they cannot name one computation two ways either.
	pick := CrewPickAt(profileDir)
	if id, ok := AutoPickWith(tier, family, preset, autoCatalogRows(), priorFor(pick)); ok {
		return id, computedRung(pick), preset
	}
	if row, ok := CrewModelsForSource(family, preset); ok {
		if id := strings.TrimSpace(row[tier]); id != "" {
			return id, SeatTable, preset
		}
	}
	// Unreachable for a tier this build knows — every preset row answers all
	// five — but the never-empty law is carried rather than assumed: the
	// family's default row is the last word.
	return defaultTierModel(family, tier), SeatTable, preset
}

// pickedSeat is THE PICK ROW'S HALF OF THE LADDER: where a seat's model comes
// from when the pick is off the table and the tier row does not name a model
// of its own. Both ladders call it — [tierSeatUnder] for the conversation and
// the settings sheet, [resolveSeat] for every headless door — so a pick
// cannot mean one thing in chat and another headless.
//
// The rule, in the order it is applied:
//
//   - the two seats that read every turn are NOT asked: reflex and small work
//     always read the table, the same law their columns in the shipped tables
//     keep, and the caller decides which tiers reach here;
//   - a row holding a model id that is not the preset's own table value is a
//     person's own model, and it wins — the pick answers for the seats nobody
//     named, not over the names they typed. A row holding the preset's OWN id
//     is the preset answering rather than a pin, and the pick computes it;
//   - every other seat among worker, careful work and mastermind is computed
//     at the crew's preset ([crewPresetUnder]), on the rung the pick names:
//     [SeatComputed] under the `catalog` word, [SeatLearned] under `learn`;
//   - when the catalog cannot compute one — no rows, no pick off the front —
//     the preset's own table row is the answer, on the table rung, the same
//     fallback [autoRow] keeps. Never `auto` and never empty.
//
// ok is false only when the pick has nothing to say — the pick row at its
// default, a hand-typed id, or a tier that always reads the table — and the
// caller answers with the row's own rung instead. The DEFAULT PICK IS THE
// FIRST CHECK: a profile that has never answered the row reads every seat
// exactly as it read before the row existed, which is what a default is for.
func pickedSeat(profileDir, family, tier, model string) (Seat, bool) {
	if CrewPickAt(profileDir) == CrewPickTable {
		return Seat{}, false
	}
	if tier != ModelTierWorker && tier != ModelTierHigh && tier != ModelTierMastermind {
		return Seat{}, false
	}
	preset := crewPresetUnder(profileDir, family)
	if row, ok := CrewModelsForSource(family, preset); ok {
		if model != "" && !strings.EqualFold(strings.TrimSpace(model), row[tier]) {
			return Seat{}, false
		}
	}
	id, source := pickedModel(tier, family, preset, CrewPickAt(profileDir))
	return Seat{Role: tierSeatRole(tier), Model: id, Source: source, Crew: preset}, true
}

// priorFor is the prior a seat computed at the crew's preset carries, read
// from the pick word alone: [autoPrior] under `learn` — the Model Pool's
// measurements and this install's own judged runs — and nil under every other
// word, which leaves each seat on the published figures alone. The bare
// `auto` row ([autoRow]) and the pick word's own seat ([pickedModel]) both
// answer through it, so the two seams cannot disagree again about what
// `catalog` means.
func priorFor(pick string) crewpick.Prior {
	if normalCrewPick(pick) == CrewPickLearn {
		return autoPrior(autoIndex())
	}
	return nil
}

// pickedModel is the pick word's own answer for one tier: the catalog's pick
// at the preset, on the rung the word names; and, when nothing can be
// computed, the preset's own table row on the table rung — the id a preset
// write would have landed, and never `auto` and never empty.
func pickedModel(tier, family, preset, pick string) (modelID string, source SeatSource) {
	pick = normalCrewPick(pick)
	if id, ok := AutoPickWith(tier, family, preset, autoCatalogRows(), priorFor(pick)); ok {
		return id, computedRung(pick)
	}
	if row, ok := CrewModelsForSource(family, preset); ok {
		if id := strings.TrimSpace(row[tier]); id != "" {
			return id, SeatTable
		}
	}
	// Unreachable for a tier this build knows — every preset row answers all
	// five — but the never-empty law is carried rather than assumed, the same
	// last word [autoRow] keeps.
	return defaultTierModel(family, tier), SeatTable
}

// computedRung is the rung a seat computed at the crew's preset answers on,
// read from the pick word alone: [SeatLearned] under `learn`, whose pick
// carries the Model Pool's measurements and this install's own judged runs
// ([autoPrior]), and [SeatComputed] under `catalog` — and under the table word,
// which computes nothing of its own. IT ASKS WHICH PICK RAN, never whether the
// prior moved the id: [SeatLearned]'s own comment says the rung names the
// computation, and a rung that reported an actual move would be a second rule.
// The pick's own seat ([pickedModel]) and a bare `auto` row ([autoRow]) both
// answer through it, so the two seams cannot name one computation two ways.
func computedRung(pick string) SeatSource {
	if normalCrewPick(pick) == CrewPickLearn {
		return SeatLearned
	}
	return SeatComputed
}

// autoCatalogRows is [AutoModels] read with its two ordinary absences folded
// into one answer.
func autoCatalogRows() []catalog.Model {
	if AutoModels == nil {
		return nil
	}
	return AutoModels()
}

// crewPresetUnder is the preset the profile's five STORED rows make, in the
// family given — read from the rows and never through seat resolution, so
// the seam cannot ask the ladder that is asking it.
//
// Each stored tier value is read through the ladder's own row reader
// ([crewRow]), which is what keeps a row reached through the lineage saying
// it the same way. A row the reader cannot answer — cleared, or never held —
// reads the family's default-preset id, which is what that tier runs until
// somebody writes it. A row that says auto matches WHICHEVER preset is being
// compared, because auto is the one row with no opinion of its own: it is
// asking to be computed at the budget the other four rows name.
//
// The answer is the first preset whose every row matches, the default preset
// tried first and then the crew's own order; when none matches, the default —
// the budget an undecided profile runs at is the budget an auto seat runs at.
//
// THE DEFAULT PRESET WINS EVERY TIE, and ties are ordinary rather than rare:
// the presets differ in only a few cells, so an auto row on a seat the two
// presets share leaves the rest of the rows matching both. Max differs from
// balanced only in the worker seat today, which makes a crew with an auto
// worker and the shipped rows elsewhere exactly that tie — it reads balanced.
// Trying the default first is what decides it, so the order above is the rule
// and not an accident of iteration.
func crewPresetUnder(profileDir, family string) string {
	defaults := crewTableFor(family)[DefaultCrew]
	read := make(map[string]string, len(ModelTiers))
	for _, tier := range ModelTiers {
		value, _, source, cleared := crewRow(profileDir, tier)
		switch {
		case cleared || source == "":
			read[tier] = strings.ToLower(strings.TrimSpace(defaults[tier]))
		case IsAuto(value):
			read[tier] = AutoValue
		default:
			read[tier] = strings.ToLower(strings.TrimSpace(value))
		}
	}
	matches := func(preset string) bool {
		row, ok := CrewModelsForSource(family, preset)
		if !ok {
			return false
		}
		for _, tier := range ModelTiers {
			if read[tier] == AutoValue {
				continue
			}
			if read[tier] != strings.ToLower(strings.TrimSpace(row[tier])) {
				return false
			}
		}
		return true
	}
	if matches(DefaultCrew) {
		return DefaultCrew
	}
	for _, preset := range CrewPresets {
		if preset != DefaultCrew && matches(preset) {
			return preset
		}
	}
	return DefaultCrew
}
