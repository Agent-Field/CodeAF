package lane

import (
	"math"
	"sort"
	"time"
)

// Workload is a completed answer's measured generation, split by whether a
// person could read it while the next operation was waiting. Class identifies
// the caller's work and reasoning setting; it never identifies a provider.
type Workload struct {
	Model   string    `json:"model"`
	Class   string    `json:"class"`
	Visible int       `json:"visible"`
	Hidden  int       `json:"hidden"`
	At      time.Time `json:"at"`
}

// Workloads is the optional forecasting seam. Alternative ledgers need not
// implement it: missing history leaves the caller's request as the evidence.
type Workloads interface {
	NoteWorkload(Workload)
	Workload(model, class string, now time.Time) (visible, hidden int, known bool)
}

// workloadLimit bounds retained request classes, not eligible providers. Old
// classes can be relearned, so eviction never restricts where a call may go.
const workloadLimit = 256

type workloadEstimate struct {
	Model   string    `json:"model"`
	Class   string    `json:"class"`
	Visible float64   `json:"visible"`
	Hidden  float64   `json:"hidden"`
	Weight  float64   `json:"weight"`
	At      time.Time `json:"at"`
}

func workloadKey(model, class string) string { return LedgerModel(model) + "\n" + class }

// NoteWorkload uses the existing journal so two sessions merge observations
// instead of overwriting each other's learned answer lengths.
func (l *ledger) NoteWorkload(seen Workload) {
	seen.Model = LedgerModel(seen.Model)
	if !seen.valid() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	l.foldWorkload(seen)
	l.keep(record{At: seen.At, Work: &seen})
}

func (l *ledger) foldWorkload(seen Workload) {
	if !seen.valid() {
		return
	}
	if l.workloads == nil {
		l.workloads = make(map[string]workloadEstimate)
	}
	key := workloadKey(seen.Model, seen.Class)
	held := l.workloads[key]
	if held.Weight == 0 && len(l.workloads) >= workloadLimit {
		oldest := ""
		for name, row := range l.workloads {
			if oldest == "" || row.At.Before(l.workloads[oldest].At) || row.At.Equal(l.workloads[oldest].At) && name < oldest {
				oldest = name
			}
		}
		delete(l.workloads, oldest)
	}
	// Out-of-order observations receive their age's weight. Ordinary arrivals
	// decay the old evidence instead. Neither can move the clock backwards.
	weight := 1.0
	if seen.At.Before(held.At) {
		weight = math.Exp2(-held.At.Sub(seen.At).Seconds() / HalfLife.Seconds())
	} else {
		held.Weight *= math.Exp2(-seen.At.Sub(held.At).Seconds() / HalfLife.Seconds())
		held.At = seen.At
	}
	total := held.Weight + weight
	if total <= 0 {
		return
	}
	held.Visible = (held.Visible*held.Weight + float64(seen.Visible)*weight) / total
	held.Hidden = (held.Hidden*held.Weight + float64(seen.Hidden)*weight) / total
	held.Model, held.Class, held.Weight = LedgerModel(seen.Model), seen.Class, total
	l.workloads[key] = held
}

// Journal replay validates observations too: an interrupted or old writer must
// not turn missing evidence into a prediction.
func (seen Workload) valid() bool {
	return LedgerModel(seen.Model) != "" && !seen.At.IsZero() && seen.Visible >= 0 && seen.Hidden >= 0 &&
		(seen.Visible > 0 || seen.Hidden > 0)
}

func (held workloadEstimate) valid() bool {
	return LedgerModel(held.Model) != "" && !held.At.IsZero() && held.Weight > 0 &&
		!math.IsInf(held.Weight, 0) && held.Visible >= 0 && held.Hidden >= 0 &&
		held.Visible+held.Hidden < float64(math.MaxInt) && held.Visible+held.Hidden > 0
}

// Workload reads memory only after the ledger's ordinary first-use restore.
// Evidence with less than half an observation's remaining weight defers to the
// current request rather than claiming that an old workload is still typical.
func (l *ledger) Workload(model, class string, now time.Time) (int, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.restore()
	held, ok := l.workloads[workloadKey(model, class)]
	if !ok || held.Weight <= 0 {
		return 0, 0, false
	}
	if now.After(held.At) && held.Weight*math.Exp2(-now.Sub(held.At).Seconds()/HalfLife.Seconds()) < 0.5 {
		return 0, 0, false
	}
	return int(math.Ceil(held.Visible)), int(math.Ceil(held.Hidden)), true
}

func (l *ledger) heldWorkloads() []workloadEstimate {
	rows := make([]workloadEstimate, 0, len(l.workloads))
	for _, row := range l.workloads {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return workloadKey(rows[i].Model, rows[i].Class) < workloadKey(rows[j].Model, rows[j].Class)
	})
	return rows
}
