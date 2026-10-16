package session

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
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
	var total float64
	day := now.Format(usageDayLayout)
	stats, err := scanUsageLines(file, info.Size(), func(_ int64, line UsageLine) error {
		if UsageLineDay(line).Format(usageDayLayout) == day {
			if math.IsNaN(line.USD) || math.IsInf(line.USD, 0) || line.USD < 0 {
				return fmt.Errorf("spending ledger contains an invalid charge")
			}
			total += line.USD
			if math.IsInf(total, 0) {
				return fmt.Errorf("spending ledger total is not finite")
			}
		}
		return nil
	})
	if err != nil {
		return DailySpendReading{}, err
	}
	return DailySpendReading{USD: total, Malformed: stats.Malformed, Oversized: stats.Oversized}, nil
}

func spentTodayOnLedgerChecked() (float64, error) {
	return DailySpendUSD(UsageLedgerPath(), time.Now())
}
