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
	file, err := os.Open(s.LedgerPath(now))
	if err != nil {
		// A day with nothing in it has no file, and that is not a failure: it
		// is a day on which nothing standing has spent anything yet.
		if os.IsNotExist(err) {
			return Spend{}, nil
		}
		return Spend{}, err
	}
	defer file.Close()
	var spend Spend
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(raw, &entry); err != nil {
			// A torn line is one line, not a reason to stop counting.
			continue
		}
		if itemID != "" && entry.ItemID != itemID {
			continue
		}
		spend.USD += entry.USD
		if entry.Kind != entryCheck {
			spend.Fired++
		}
	}
	if err := scanner.Err(); err != nil {
		return Spend{}, err
	}
	return spend, nil
}
