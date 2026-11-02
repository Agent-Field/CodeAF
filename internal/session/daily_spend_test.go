package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestDailySpendReadErrorsNeverGrantAPaidGuardCall(t *testing.T) {
	now := time.Now()
	for _, bad := range []string{"directory", "invalid charge"} {
		t.Run(bad, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "usage.jsonl")
			switch bad {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				row, _ := json.Marshal(UsageLine{At: now, Day: now.Format(usageDayLayout), USD: -1})
				if err := os.WriteFile(path, append(row, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			guard := &SpendGuard{Cap: 20, Price: kimiPrice, Day: newCheckedLedgerSpendDay(func() (float64, error) { return DailySpendUSD(path, now) }, func() time.Time { return now })}
			calls := &spendingCompleter{usd: 0.1}
			_, err := guard.Wrap("m", calls).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "hi")}, ai.WithMaxTokens(10))
			var stopped ErrSpendStopped
			if !errors.As(err, &stopped) || stopped.Action != spendAccountingUnread || calls.calls != 0 {
				t.Fatalf("%v, calls%d", err, calls.calls)
			}
			row, _ := json.Marshal(UsageLine{At: now, Day: now.Format(usageDayLayout), USD: 2})
			if bad == "directory" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, append(row, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = guard.Wrap("m", calls).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "hi")}, ai.WithMaxTokens(10)); err != nil || calls.calls != 1 {
				t.Fatalf("repair: %v calls%d", err, calls.calls)
			}
		})
	}
}

func TestDailySpendKeepsSettlementAcrossUnreadableRollover(t *testing.T) {
	now := time.Date(2026, 10, 16, 23, 59, 0, 0, time.Local)
	fail := false
	day := newCheckedLedgerSpendDay(func() (float64, error) {
		if fail {
			return 0, errors.New("unreadable")
		}
		return 0, nil
	}, func() time.Time { return now })
	held, fits, err := day.holdChecked("m", 1, 20)
	if err != nil || !fits {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	fail = true
	day.settle("m", held, 5)
	if _, _, err = day.holdChecked("m", 1, 20); err == nil {
		t.Fatal("read failure admitted work")
	}
	fail = false
	total, err := day.totalChecked()
	if err != nil || total != 5 || day.held != 0 {
		t.Fatalf("%v,%v held%v", total, err, day.held)
	}
}

func TestDailySpendRetainsLegacyGapsWithoutInventingTheirDates(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	old, _ := json.Marshal(UsageLine{At: now.AddDate(0, 0, -1), Day: now.AddDate(0, 0, -1).Format(usageDayLayout), USD: 8})
	today, _ := json.Marshal(UsageLine{At: now, Day: now.Format(usageDayLayout), USD: 3})
	body := append(old, '\n')
	body = append(body, []byte("broken\n")...)
	body = append(body, today...)
	body = append(body, '\n')
	body = append(body, []byte(strings.Repeat("x", usageLineBytes+1)+"\n")...)
	body = append(body, []byte("{\"at\":")...)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	reading, err := ReadDailySpend(path, now)
	if err != nil || reading.USD != 3 || reading.Malformed != 1 || reading.Oversized != 1 {
		t.Fatalf("%+v,%v", reading, err)
	}
	later, err := ReadDailySpend(path, now.AddDate(0, 0, 1))
	if err != nil || later.USD != 0 || later.Malformed != 1 || later.Oversized != 1 {
		t.Fatalf("next period locked: %+v,%v", later, err)
	}
}

func TestUnverifiedDailySpendingIsNotClassifiedAsAReachedCap(t *testing.T) {
	err := spendUnverified{said: spendAccountingUnread}
	if !errors.Is(err, ErrSpendUnverified) || errors.Is(err, ErrSpendRail) {
		t.Fatal("unverified and reached are conflated")
	}
	kind, _, _ := ClassifyTurnError(err)
	if kind != TurnErrorOther {
		t.Fatalf("unverified kind=%s", kind)
	}
}

// writeSpendRows appends one ledger row per figure, all on now's day.
func writeSpendRows(t *testing.T, path string, now time.Time, usd ...float64) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, v := range usd {
		row, _ := json.Marshal(UsageLine{At: now, Day: now.Format(usageDayLayout), Model: "m", USD: v})
		if _, err := file.Write(append(row, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// A TURN'S START READS THE DAY'S SPEND, and the ledger holds every call this
// machine ever made: a read that walked all of it again on every check made a
// new chat wait longer the more the person had used it. A second read of the
// same ledger reads only what was appended since the first.
func TestDailySpendReadsOnlyWhatWasAppended(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	old := make([]float64, 2000)
	writeSpendRows(t, path, now.AddDate(0, 0, -1), old...)
	writeSpendRows(t, path, now, 1, 2)
	first, err := ReadDailySpend(path, now)
	if err != nil || first.USD != 3 {
		t.Fatalf("first read: %+v, %v", first, err)
	}
	before, _ := os.Stat(path)
	writeSpendRows(t, path, now, 4)
	after, _ := os.Stat(path)
	scanned := usageBytesScanned.Load()
	second, err := ReadDailySpend(path, now)
	if err != nil || second.USD != 7 {
		t.Fatalf("second read: %+v, %v", second, err)
	}
	if read, appended := usageBytesScanned.Load()-scanned, after.Size()-before.Size(); read != appended {
		t.Fatalf("second read scanned %d bytes; only %d were appended", read, appended)
	}
}

// What the tally remembers never outlives what it was read from: a new day,
// a line finished after it was half written, and a ledger replaced or
// rewritten are each read again.
func TestDailySpendTallyFollowsTheLedger(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	writeSpendRows(t, path, now, 1)
	if got, err := DailySpendUSD(path, now); err != nil || got != 1 {
		t.Fatalf("%v, %v", got, err)
	}
	// Half a line is not counted until it is whole.
	row, _ := json.Marshal(UsageLine{At: now, Day: now.Format(usageDayLayout), USD: 2})
	file, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	_, _ = file.Write(row[:10])
	if got, err := DailySpendUSD(path, now); err != nil || got != 1 {
		t.Fatalf("partial: %v, %v", got, err)
	}
	_, _ = file.Write(append(row[10:], '\n'))
	file.Close()
	if got, err := DailySpendUSD(path, now); err != nil || got != 3 {
		t.Fatalf("finished: %v, %v", got, err)
	}
	// Another day is its own sum.
	if got, err := DailySpendUSD(path, now.AddDate(0, 0, 1)); err != nil || got != 0 {
		t.Fatalf("next day: %v, %v", got, err)
	}
	// A ledger replaced by another file is read from its start.
	next := filepath.Join(dir, "next.jsonl")
	writeSpendRows(t, next, now, 5)
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	if got, err := DailySpendUSD(path, now); err != nil || got != 5 {
		t.Fatalf("replaced: %v, %v", got, err)
	}
	// So is one rewritten in place, even to a longer one.
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	writeSpendRows(t, path, now, 6, 7)
	if got, err := DailySpendUSD(path, now); err != nil || got != 13 {
		t.Fatalf("rewritten: %v, %v", got, err)
	}
}
