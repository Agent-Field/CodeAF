package taxonomy

import "sync"

// Tally is what ONE piece of work remembers about its own failures, and it is
// the thing that makes the taxonomy more than a name for an error.
//
// ── THE DISTINCTION IT KEEPS ────────────────────────────────────────────────
//
// A check that finds gaps is capability evidence ONLY IF the round it judged
// actually got to run. When a round's calls died on the wire there was nothing
// for the check to pass, and the finding it wrote is a transport failure wearing
// a verdict's clothes. Counting it is precisely how four bad responses in a row
// bought a seven-times-dearer model for the remainder of a run.
//
// So a round is a bracket: [Tally.Wire] records what the wire cost inside it,
// [Tally.Refuted] closes it and decides which counter the finding belongs in,
// and [Tally.Round] opens the next one. The semantic count — the one the
// capability policy reads — only ever moves for a finding with a clean round
// under it.
//
// ── AND WHAT THE LIFT HAS COST ──
//
// [Tally.Spend] is the other half: the money a lifted tier has taken off this
// one piece of work, which is what the cap is checked against. It is fed from
// wherever the caller already folds a worker's usage, so nothing has to be
// counted twice.
//
// A nil Tally is a piece of work that is not being tallied, and every method
// tolerates it: the zero evidence it reports classifies exactly as a caller
// with no history would.
type Tally struct {
	mu sync.Mutex
	// round is the transport failures inside the round now open.
	round int
	// wire is the transport failures across the whole piece of work.
	wire int
	// semantic is the findings that had a clean round under them.
	semantic int
	// tainted is the findings that did not. It is kept rather than dropped
	// because "we held instead of escalating, and here is how often" is the one
	// number that says whether this mechanism is doing anything.
	tainted int
	// escalated is whether the work stands on a tier something lifted it onto,
	// and spent is what that tier has cost it.
	escalated bool
	spent     float64
}

// Wire records one transport failure inside the round now open.
func (t *Tally) Wire() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.round++
	t.wire++
}

// Refuted closes the open round with a finding and reports whether it counted as
// SEMANTIC — a finding about the work rather than about the wire.
//
// The round is closed either way: whatever comes next is a fresh attempt and
// starts with a clean wire count, which is what makes "on the same tier" mean
// anything.
func (t *Tally) Refuted() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	clean := t.round == 0
	if clean {
		t.semantic++
	} else {
		t.tainted++
	}
	t.round = 0
	return clean
}

// Round opens a fresh round, discarding the wire failures of the last one. It is
// called when a round ends in anything other than a finding — a pass, a stop, a
// cancel — so the next round is judged on its own.
func (t *Tally) Round() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.round = 0
}

// Passed clears the semantic count and the open round: the work has been read
// and it holds, so nothing that came before it is evidence about the tier any
// more.
func (t *Tally) Passed() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.round, t.semantic = 0, 0
}

// Escalate records that a lift was bought, and Deescalate that it was given
// back. The spend follows the tier: a tier that has been handed back has not
// spent anything on the work that comes next.
func (t *Tally) Escalate() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.escalated = true
}

func (t *Tally) Deescalate() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.escalated, t.spent = false, 0
}

// Escalated reports whether the work stands on a lifted tier.
func (t *Tally) Escalated() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.escalated
}

// Spend adds to what the lifted tier has cost this piece of work. It is ignored
// while nothing is lifted, because a cap on a tier nobody bought is a cap on the
// ordinary price of the work — which is somebody else's rail entirely.
func (t *Tally) Spend(usd float64) {
	if t == nil || usd <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.escalated {
		t.spent += usd
	}
}

// Evidence is the tally as the boundary reads it. The caller fills in whatever
// else it knows — a status, an empty flag — on top of this.
func (t *Tally) Evidence() Evidence {
	if t == nil {
		return Evidence{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return Evidence{
		Refuted:       t.semantic,
		TransportSeen: t.round,
		Escalated:     t.escalated,
		SpentUSD:      t.spent,
	}
}

// Counts is what a test and an autopsy read: the wire failures, the findings
// that counted, and the findings that were held because the wire explained them.
func (t *Tally) Counts() (wire, semantic, tainted int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wire, t.semantic, t.tainted
}
