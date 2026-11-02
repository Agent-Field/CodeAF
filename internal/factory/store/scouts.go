package store

import (
	"slices"
	"time"
)

// ScoutsKept is how many scouts the floor remembers: enough for a month's
// limit at the daily caps, and for a stable typical price.
const ScoutsKept = 2000

// ScoutNote is one Look closer: its repository, cost and when it ran.
type ScoutNote struct {
	Repo string    `json:"repo"`
	USD  float64   `json:"usd"`
	At   time.Time `json:"at"`
}

// ScoutLedger is what the scouts spent: this month in all, how many ran today
// per repository, and the typical (median) cost of one, which the Look closer
// button quotes. Typical is 0 when none ran.
type ScoutLedger struct {
	Month   float64
	Today   map[string]int
	Typical float64
}

// NoteScout records one scout's cost.
func (st *Store) NoteScout(repo string, usd float64, at time.Time) error {
	return st.changeMeta(func(d *metaDoc) {
		d.Scouts = append(d.Scouts, ScoutNote{Repo: repo, USD: max(usd, 0), At: at})
		if over := len(d.Scouts) - ScoutsKept; over > 0 {
			d.Scouts = d.Scouts[over:]
		}
	})
}

// Scouts is the scout ledger as of now, in now's location.
func (st *Store) Scouts(now time.Time) (ScoutLedger, error) {
	out := ScoutLedger{Today: map[string]int{}}
	d, err := st.readMeta()
	if err != nil {
		return out, err
	}
	y, m, day := now.Date()
	costs := make([]float64, 0, len(d.Scouts))
	for _, n := range d.Scouts {
		ny, nm, nd := n.At.In(now.Location()).Date()
		if ny == y && nm == m {
			out.Month += n.USD
			if nd == day {
				out.Today[n.Repo]++
			}
		}
		costs = append(costs, n.USD)
	}
	if len(costs) > 0 {
		slices.Sort(costs)
		out.Typical = costs[(len(costs)-1)/2]
	}
	return out, nil
}

// ScoutPrice is the typical cost of one scout, 0 when none ran.
func (st *Store) ScoutPrice() (float64, error) {
	l, err := st.Scouts(time.Now())
	return l.Typical, err
}
