package lane

import "sync"

// ── ONE PLACE THE PARTS ARE PLUGGED TOGETHER ────────────────────────────────
//
// Five seams, five implementations, and exactly one file that knows which
// concrete type answers which interface. Everything else in this build — the
// transport that needs a preference, the surface that draws a belief, the beat
// that refreshes a sheet — asks the registry and never names a struct.
//
// WHY A REGISTRY AND NOT A SWITCH, or a package-level variable per seam, or a
// constructor threaded through six call sites: because the alternative is what
// the response boundary already taught this codebase (docs/ARCHITECTURE.md,
// Decision 8). Sites that each decide for themselves start the same and drift
// the first time one of them is fixed. Here a bench swaps a sheet for a file
// reader, a test swaps the chooser for a pin, and no caller notices.
//
// A structural test in this directory fails the build when a concrete
// implementation is constructed anywhere but here.

// Registry holds the one live set of implementations.
//
// Every accessor returns something usable — never nil. A seam nobody has filled
// in yet answers with the honest empty implementation below it: no rows, no
// belief, no opinion. A caller must never have to check.
type Registry struct {
	mu      sync.RWMutex
	sheet   Sheet
	ledger  Ledger
	chooser Chooser
	prober  Prober
	store   Store
}

// defaultRegistry is the process's own set. It is a variable rather than a
// once-built value because tests replace parts of it and put them back.
var defaultRegistry = newRegistry()

// Default is the registry this process routes through.
func Default() *Registry { return defaultRegistry }

// newRegistry builds the set every seam starts at. The seams that have been
// built answer for real from the first compile — the sheet with nothing fetched
// yet, the ledger with nothing measured yet — and the ones that have not
// believe nothing and say so.
func newRegistry() *Registry {
	// The ledger writes every observation through the store, so the two are
	// introduced to each other here: this file is the only one entitled to know
	// which concrete type answers which seam, and a ledger that went looking
	// for a store by itself would be the reach-around this registry exists to
	// prevent.
	keeper, beliefs, pages := newStore(), newLedger(), newSheet()
	beliefs.keepIn(keeper)
	// AND THE LEDGER IS SHOWN THE SHEET. A lane arrives at the ledger two ways
	// — primed from a row, or merely SEEN in an answer — and the second of them
	// carries no facts at all. The sheet is already in memory and [Sheet.Rows]
	// is a map lookup, so the ledger dresses a newly seen lane from it at the
	// moment it lands rather than leaving a factless belief in the file until
	// some later beat happens to prime it (belief.go's dress).
	beliefs.readFrom(pages)
	return &Registry{
		sheet:   pages,
		ledger:  beliefs,
		chooser: newChooser(),
		prober:  newProber(),
		store:   keeper,
	}
}

// Sheet is the live sheet.
func (r *Registry) Sheet() Sheet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sheet
}

// Ledger is the live ledger.
func (r *Registry) Ledger() Ledger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ledger
}

// Chooser is the live chooser.
func (r *Registry) Chooser() Chooser {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.chooser
}

// Prober is the live prober.
func (r *Registry) Prober() Prober {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.prober
}

// Store is where beliefs sleep.
func (r *Registry) Store() Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store
}

// SetSheet installs a sheet. A nil argument restores the empty one rather than
// leaving a hole for a caller to trip over.
func (r *Registry) SetSheet(sheet Sheet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sheet == nil {
		sheet = newSheet()
	}
	r.sheet = sheet
	// A sheet swapped underneath a ledger this package built is the sheet that
	// ledger must then read — a bench that installed a sheet of its own and
	// found beliefs still being dressed from the empty one would have been
	// given a seam that only half moves. It is the same reasoning as
	// [Registry.SetStore] below, and for the same reason it is here.
	if own, ok := r.ledger.(*ledger); ok {
		own.readFrom(sheet)
	}
}

// SetLedger installs a ledger.
func (r *Registry) SetLedger(beliefs Ledger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if beliefs == nil {
		beliefs = newLedger()
	}
	r.ledger = beliefs
	// A ledger this package built is introduced to the store and the sheet it
	// is expected to work through, for the reason [Registry.SetStore] does the
	// same in the other direction: a seam that only half moves is a seam whose
	// two halves disagree the first time one of them is swapped.
	if own, ok := beliefs.(*ledger); ok {
		own.keepIn(r.store)
		own.readFrom(r.sheet)
	}
}

// SetChooser installs a chooser.
func (r *Registry) SetChooser(chooser Chooser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if chooser == nil {
		chooser = newChooser()
	}
	r.chooser = chooser
}

// SetProber installs a prober.
func (r *Registry) SetProber(prober Prober) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prober == nil {
		prober = newProber()
	}
	r.prober = prober
}

// SetStore installs a store.
func (r *Registry) SetStore(store Store) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if store == nil {
		store = newStore()
	}
	r.store = store
	// A store swapped underneath a ledger this package built is a store that
	// ledger must then write through — a bench that pointed the beliefs at a
	// temporary file and found them still landing in the person's own home
	// would have been given a seam that only half moves.
	if own, ok := r.ledger.(*ledger); ok {
		own.keepIn(store)
	}
}

// Reset puts every seam back to the empty implementation. It is for tests, and
// a test that installs anything must defer it.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	fresh := newRegistry()
	r.sheet, r.ledger, r.chooser, r.prober, r.store =
		fresh.sheet, fresh.ledger, fresh.chooser, fresh.prober, fresh.store
}
