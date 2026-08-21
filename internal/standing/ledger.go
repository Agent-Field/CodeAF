package standing

// ledger.go is how the rails are told the truth. One append-only file per local
// day, one line per firing and per sentinel call that cost money, and the two
// questions the ticker asks — how many times has this item fired today, and how
// much has everything standing spent today — are sums over that day's file.
//
// APPEND-ONLY IS WHY IT IS SAFE. Three processes may fire in the same second;
// a short O_APPEND write on a local filesystem is atomic, so the worst race is
// two firings passing the rail together, an overspend bounded by one firing's
// own cap. A read-modify-write of a running total would have no such floor.

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// entryCheck names a ledger line that is a probe and its sentinel rather than a
// firing. It costs money, so it counts against the daily rail; it did not fire,
// so it does not count against MaxPerDay.
const entryCheck = "check"

// Append writes one entry to today's ledger with O_APPEND.
func (s *Store) Append(entry Entry) error {
	if entry.At.IsZero() {
		entry.At = s.now()
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.LedgerPath(entry.At), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return err
	}
	return file.Close()
}

// Today sums today's ledger. An empty itemID sums everything, which is what the
// daily rail reads.
func (s *Store) Today(itemID string, now time.Time) (Spend, error) {
	if now.IsZero() {
		now = s.now()
	}
	var spend Spend
	err := readLedgerDay(s.LedgerPath(now), func(entry Entry) {
		if itemID != "" && entry.ItemID != itemID {
			return
		}
		spend.count(entry)
	})
	if err != nil {
		return Spend{}, err
	}
	return spend, nil
}

// ledgerReach is how far back [Store.RunsSince] will walk, in days. It is a
// bound on the WORK and not on the question: a caller asking for a week opens
// eight files, and a caller asking for the beginning of time opens this many
// and answers about them. Without it a stray zero moment would open one file
// per day since 1970 on a screen's own refresh.
//
// A month is the widest window any surface asks about today (a card says `this
// week`), with room for one that asks about a longer one.
const ledgerReach = 31

// RunsSince sums the ledger from a moment until now, PER ITEM: how many times
// each thing fired, and what it spent doing so. It is what a card means by
// `3 runs this week · $0.04`.
//
// ONE WALK ANSWERS EVERY ITEM, and that is the whole reason it answers a map
// rather than one item's figure. The ledger is one file per local day, so a
// surface asking item by item would open the same seven files once per item on
// every card it draws; here they are read once and the caller sums whichever
// ids its subject owns.
//
// A day with no file is a day on which nothing fired, which is not a failure —
// the same reading [Store.Today] takes of the same absence. A day whose file
// cannot be read at all IS reported, because a total silently missing a day is
// a rail quoting a number that is too small.
//
// The moment is inclusive and entries before it are skipped: the day file it
// lands in holds the hours on either side of it.
func (s *Store) RunsSince(from time.Time) (map[string]Spend, error) {
	out := map[string]Spend{}
	if s == nil {
		return out, nil
	}
	now := s.now()
	if from.IsZero() || from.After(now) {
		return out, nil
	}
	day := startOfDay(from)
	if oldest := startOfDay(now).AddDate(0, 0, -(ledgerReach - 1)); day.Before(oldest) {
		day = oldest
	}
	for last := startOfDay(now); !day.After(last); day = day.AddDate(0, 0, 1) {
		if err := readLedgerDay(s.LedgerPath(day), func(entry Entry) {
			if entry.At.Before(from) {
				return
			}
			spend := out[entry.ItemID]
			spend.count(entry)
			out[entry.ItemID] = spend
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// count folds one ledger line into a running sum. IT IS THE ONE PLACE THE TWO
// COLUMNS ARE DEFINED: a check costs money and did not fire, so it counts
// against the money and never against the runs, and a second reader spelling
// that rule again is where the daily rail and a card would come to disagree.
func (s *Spend) count(entry Entry) {
	s.USD += entry.USD
	if entry.Kind != entryCheck {
		s.Fired++
	}
}

// startOfDay is the local midnight a moment belongs to, which is the grain the
// ledger's file names are cut on.
func startOfDay(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

// readLedgerDay hands every readable line of one day's file to fn. A file that
// is not there is a day nothing happened on; a torn line is one line and not a
// reason to stop counting.
func readLedgerDay(path string, fn func(Entry)) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var entry Entry
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		fn(entry)
	}
	return scanner.Err()
}
