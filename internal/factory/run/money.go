package run

import (
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// LedgerName is the role the floor's runs are recorded under on the spend
// ledger, beside `factory triage`. The func handed to [NewMoney] writes the line
// (the wiring owns the session ledger; this package imports none of the
// session engine) and is given the item's id and the dollars.
const LedgerName = "factory run"

// ActivityHours is how many hourly buckets Stream.Activity keeps: the last 24
// hours, oldest first. The head draws the newest few of them as its sparkline.
const ActivityHours = 24

// activityTop is the tallest a bucket stands (the sparkline's range is 0..7).
const activityTop = 7

// Money is the one pool: the item's cap, the day's rail, and the dollars
// every round reports. It writes the item's Spent, the ledger line and the
// activity; it decides nothing about what to do when a cap is reached.
//
// THE CAP CONTRACT. The loop calls [Money.CapReached] after EVERY round, and
// when it is true it parks the item as needing the person with
// [Money.CapQuestion] as Item.Question and QKind `cap` (a stored key; the sentence says `budget`). Money never stops a
// round itself: a round already paid for is kept.
//
// MONEY IS WRITTEN THROUGH Annotate. A model's cost is not the item moving, so
// the row's age is left alone.
type Money struct {
	st     *store.Store
	ledger func(item int, usd float64)
	clock  func() time.Time

	mu    sync.Mutex
	today map[int]float64 // dollars each item spent today, by this process
	day   [3]int          // the day `today` is for
	hour  map[int]int64   // the hour (unix hours) of each item's newest bucket
}

// NewMoney makes the money. A nil ledger records nothing and a nil clock is
// time.Now.
func NewMoney(st *store.Store, ledger func(item int, usd float64), clock func() time.Time) *Money {
	if clock == nil {
		clock = time.Now
	}
	return &Money{st: st, ledger: ledger, clock: clock, today: map[int]float64{}, hour: map[int]int64{}}
}

// Spend is the hook for Job.Spend of item id. A cost of nothing, or less, is
// not recorded.
func (m *Money) Spend(id int) func(usd float64) {
	return func(usd float64) {
		if m == nil || usd <= 0 {
			return
		}
		now := m.clock()
		slide := m.note(id, usd, now)
		if m.st != nil {
			_ = m.st.Annotate(id, func(it *factory.Item) error {
				if it.Stream == nil {
					it.Stream = &factory.Stream{}
				}
				it.Stream.Spent += usd
				it.Stream.Activity = bump(it.Stream.Activity, slide)
				return nil
			})
		}
		if m.ledger != nil {
			m.ledger(id, usd)
		}
	}
}

// note books usd to today and answers how many hours the item's activity must
// slide before the newest bucket takes this call: -1 for a first call, whose
// newest bucket is taken to be this hour.
func (m *Money) note(id int, usd float64, now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roll(now)
	m.today[id] += usd
	h := now.Unix() / 3600
	last, seen := m.hour[id]
	m.hour[id] = h
	if !seen {
		return 0
	}
	return int(h - last)
}

// roll forgets yesterday's spend when the day turns. Callers hold mu.
func (m *Money) roll(now time.Time) {
	y, mo, d := now.Date()
	if cur := [3]int{y, int(mo), d}; cur != m.day {
		m.day = cur
		m.today = map[int]float64{}
	}
}

// bump slides the 24 buckets forward by hours and adds one to the newest, to
// at most the sparkline's top. An activity of another length is made 24 first.
func bump(a []int, hours int) []int {
	out := make([]int, ActivityHours)
	if len(a) >= ActivityHours {
		copy(out, a[len(a)-ActivityHours:])
	} else {
		copy(out[ActivityHours-len(a):], a)
	}
	if hours > 0 {
		if hours >= ActivityHours {
			out = make([]int, ActivityHours)
		} else {
			copy(out, out[hours:])
			for i := ActivityHours - hours; i < ActivityHours; i++ {
				out[i] = 0
			}
		}
	}
	if out[ActivityHours-1] < activityTop {
		out[ActivityHours-1]++
	}
	return out
}

// CapReached is whether the item has spent what its cap allows. No cap (0) is
// never reached.
func (m *Money) CapReached(it factory.Item) bool {
	return it.Cap > 0 && it.Stream != nil && it.Stream.Spent >= it.Cap
}

// CapQuestion is the sentence the item waits on, and its QKind.
func (m *Money) CapQuestion(it factory.Item) (q string, kind string) {
	c := dollars(it.Cap)
	return "budget of " + c + " reached · " + c + " more, or stop?", "cap"
}

func dollars(usd float64) string {
	if usd == float64(int64(usd)) {
		return "$" + strconv.FormatInt(int64(usd), 10)
	}
	return "$" + strconv.FormatFloat(usd, 'f', 2, 64)
}

// SpentToday is what the floor has spent today, for the rail. An item this
// process paid for counts what it paid today; any other counts its whole
// spend when it moved or started today. Triage reads are not added: they are
// kept as a running total, not by day.
func (m *Money) SpentToday() float64 {
	if m == nil || m.st == nil {
		return 0
	}
	items, err := m.st.List()
	if err != nil {
		return 0
	}
	now := m.clock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roll(now)
	y, mo, d := now.Date()
	isToday := func(t time.Time) bool {
		if t.IsZero() {
			return false
		}
		ty, tm, td := t.In(now.Location()).Date()
		return ty == y && tm == mo && td == d
	}
	var sum float64
	for _, it := range items {
		if it.Stream == nil {
			continue
		}
		if usd, ok := m.today[it.ID]; ok {
			sum += usd
		} else if isToday(it.Changed) || isToday(it.Stream.Started) || isToday(it.Stream.Ended) {
			sum += it.Stream.Spent
		}
	}
	return sum
}

// Rail is the day's rail in dollars, 0 for none.
func (m *Money) Rail() float64 {
	if m == nil || m.st == nil {
		return 0
	}
	r, _ := m.st.Rail()
	return r
}
