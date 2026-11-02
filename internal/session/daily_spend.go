package session

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"sync"
	"time"
)

// DailySpendReading retains coverage gaps separately from the recorded figure.
// Legacy malformed rows have no trustworthy date and are never assigned one.
type DailySpendReading struct {
	USD                  float64
	Malformed, Oversized int
}

// DailySpendUSD is the existing daily rail's recorded figure, not proof of a
// complete bill. Legacy malformed/oversized rows remain skipped; actual I/O,
// nonregular files and invalid current-day figures fail. Shared transport
// admission owns stricter uncertainty and durable unfinished-call coverage.
func DailySpendUSD(path string, now time.Time) (float64, error) {
	reading, err := ReadDailySpend(path, now)
	return reading.USD, err
}

// ReadDailySpend reads one bounded line at a time and preserves degraded facts.
//
// THE LEDGER ONLY GROWS, AND EVERY TURN'S START ASKS THIS: a read that walked
// all of it each time made a new chat wait longer the more the machine had
// been used. What was read is kept as a tally ([spendTally]), and the next
// read on the same day scans only the lines appended since. A ledger that is
// another file, shorter, or no longer ends where the tally did is read from
// its start, so the answer is always the whole file's.
func ReadDailySpend(path string, now time.Time) (DailySpendReading, error) {
	file, err := openRecordFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DailySpendReading{}, nil
	}
	if err != nil {
		return DailySpendReading{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return DailySpendReading{}, err
	}
	if !info.Mode().IsRegular() {
		return DailySpendReading{}, fmt.Errorf("spending ledger is not a regular file")
	}
	spendTallies.Lock()
	defer spendTallies.Unlock()
	day := now.Format(usageDayLayout)
	tally := spendTallies.of[path]
	if !tally.continues(file, info, day) {
		tally = spendTally{day: day}
	}
	if err := tally.read(file, info); err != nil {
		delete(spendTallies.of, path)
		return DailySpendReading{}, err
	}
	spendTallies.of[path] = tally
	return tally.reading, nil
}

// spendTally is one ledger's day as far as it has been read: the day summed,
// the file read, how many bytes of whole lines, and the last of those bytes,
// which prove the file still holds what was read.
type spendTally struct {
	day     string
	file    os.FileInfo
	offset  int64
	tail    []byte
	reading DailySpendReading
}

// spendTallyTail is how many bytes before the tally's end are compared.
const spendTallyTail = 256

var spendTallies = struct {
	sync.Mutex
	of map[string]spendTally
}{of: map[string]spendTally{}}

// continues reports whether file is the one t was read from, on t's day, and
// still holds what t read.
func (t spendTally) continues(file *os.File, info os.FileInfo, day string) bool {
	if t.file == nil || t.day != day || !os.SameFile(t.file, info) || info.Size() < t.offset {
		return false
	}
	return bytes.Equal(ledgerTail(file, t.offset), t.tail)
}

// read adds the whole lines between t's end and the file's size to t.
func (t *spendTally) read(file *os.File, info os.FileInfo) error {
	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}
	usd := t.reading.USD
	stats, err := scanUsageLines(file, info.Size()-t.offset, func(_ int64, line UsageLine) error {
		if UsageLineDay(line).Format(usageDayLayout) != t.day {
			return nil
		}
		if math.IsNaN(line.USD) || math.IsInf(line.USD, 0) || line.USD < 0 {
			return fmt.Errorf("spending ledger contains an invalid charge")
		}
		usd += line.USD
		if math.IsInf(usd, 0) {
			return fmt.Errorf("spending ledger total is not finite")
		}
		return nil
	})
	if err != nil {
		return err
	}
	t.file, t.offset = info, t.offset+stats.Bytes
	t.tail = ledgerTail(file, t.offset)
	t.reading = DailySpendReading{USD: usd, Malformed: t.reading.Malformed + stats.Malformed, Oversized: t.reading.Oversized + stats.Oversized}
	return nil
}

// ledgerTail is the up to [spendTallyTail] bytes of file that end at end.
func ledgerTail(file *os.File, end int64) []byte {
	start := max(end-spendTallyTail, 0)
	tail := make([]byte, end-start)
	n, _ := file.ReadAt(tail, start)
	return tail[:n]
}

func spentTodayOnLedgerChecked() (float64, error) {
	return DailySpendUSD(UsageLedgerPath(), time.Now())
}
