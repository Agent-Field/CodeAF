package session

// ── THE LEDGER, ADDED UP BY DAY AND MODEL: a generic report, never a guess ───
//
// [QueryUsageLedger] answers rows (runs and calls) and [UsageBySubject] answers
// one grouping; a calendar, a model flow and a sparkline all want the same
// thing a third way: what was spent on each LOCAL DAY, on each MODEL, on each
// KIND OF THING. This file is that one aggregate and nothing more. It is
// GENERIC — it names no screen — and the screen folds, ranks and colours it.
//
// THE CELLS ARE ONLY WHAT THE LEDGER WROTE. A cell is (day, model, kind,
// subject) and, for a task, the conversation it ran in, because a task id alone
// restarts with every conversation and would merge two different tasks. The day
// is the line's STORED [UsageLine.Day] — the day the writer's own clock gave it
// — and only a line with no valid stored day is placed by its instant in the
// query's zone (counted in [UsageReport.DaysDerived]). A call that named no
// model has an empty model, and an empty model is "nobody said", never a named
// "Unknown". The ledger does not record the provider that BILLED a call, the
// outcome of the task it served, or the crew that owned it; nothing here joins
// those from today's membership or today's task states, because a crew or an
// outcome as it is NOW is not what it was when the money was spent.
//
// NOTHING IS FOLDED AND NOTHING IS CUT. There is no "top three" and no "Other"
// here. A range that would need more cells than [UsageReportMaxCells], or more
// estimated bytes than [UsageReportMaxBytes], is REFUSED with
// [ErrUsageReportTooLarge] asking for a narrower filter, rather than answered
// with fewer cells than the range holds. The bound is checked BEFORE a cell is
// inserted and before its key strings are copied, so a ledger line with an
// absurd id costs the check and not the copy. The byte figure is an ESTIMATE of
// the cells' weight used to refuse early; it is not a promise about resident
// memory, and the scan is NOT constant-time: it is one linear pass over the
// ledger's first byte to the end of the prefix it fixed at the start.
//
// ONE PASS, ONE PREFIX, NO CACHE. The file's length is read once, the scan
// reads no further than that (a line appended meanwhile is the next report's),
// and the report is built in a map as it goes. Nothing is remembered between
// calls, so there is nothing to go stale.
//
// THE TOTALS ARE ADDED UP BY THE SAME CHECKED HELPER as the ledger's
// ([UsageLedgerTotals.count]): a negative count or a sum past what a number
// holds refuses with [ErrUsageLedgerFigures]. The cells' figures are parts of
// those totals, so they cannot overflow where the totals did not.

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The most a report may hold.
const (
	// UsageReportMaxCells is the most cells one report may hold.
	UsageReportMaxCells = 8192
	// UsageReportMaxBytes is the estimate of the cells' weight past which a
	// report is refused.
	UsageReportMaxBytes = 4 << 20
	// UsageReportMaxDays is the longest range, in calendar days, a report is
	// asked for. A report needs a range: there is no "all history".
	UsageReportMaxDays = 366
)

// usageReportCellBytes is the estimate of one cell's own weight, before its
// strings: the struct, its map slot and its slice slot.
const usageReportCellBytes = 256

// ErrUsageReportTooLarge is a range holding more cells, or heavier ones, than
// one report may. It is refused rather than answered with fewer.
var ErrUsageReportTooLarge = errors.New("this range holds too many day, model and subject groups to report at once; ask for a shorter date range or a narrower filter")

// UsageReportBudget bounds what one report may hold. A zero field is the
// default; a larger one than the default is not honoured.
type UsageReportBudget struct {
	MaxCells int
	MaxBytes int64
}

func (b UsageReportBudget) bounded() UsageReportBudget {
	if b.MaxCells <= 0 || b.MaxCells > UsageReportMaxCells {
		b.MaxCells = UsageReportMaxCells
	}
	if b.MaxBytes <= 0 || b.MaxBytes > UsageReportMaxBytes {
		b.MaxBytes = UsageReportMaxBytes
	}
	return b
}

// UsageReportCell is everything the ledger wrote for one day, model and
// subject, added up.
type UsageReportCell struct {
	// Day is the stored local calendar day, "2006-01-02".
	Day string
	// Model is the provider's model id, and empty where nobody said.
	Model string
	// Kind and Subject are [UsageBySubject]'s classification, and both empty
	// for a line that belonged to nothing. Root is the conversation a TASK ran
	// in, set only on a task cell: a task id means nothing without it.
	Kind, Subject, Root string
	// Records is ledger lines; Calls is provider requests, counted apart from
	// the lines because an older line may cover many.
	Records, Calls int
	Input, Output  int64
	// USD is the sum of the priced lines; Unpriced counts the lines with no
	// figure and Unbilled those marked as a missing receipt (also unpriced).
	USD                float64
	Unpriced, Unbilled int
}

// Priced reports whether any line in the cell carried a figure; a cell with
// none has no cost, which is not a cost of nothing.
func (c UsageReportCell) Priced() bool { return c.Records > c.Unpriced }

// UsageReport is one question's answer: the whole range's totals, exactly, and
// every cell that makes them up.
type UsageReport struct {
	Totals UsageLedgerTotals
	// Cells are sorted by day, model, kind, subject, then root.
	Cells []UsageReportCell
	// Models are the distinct named models in the cells, sorted.
	Models []string
	// DaysDerived is lines with no valid stored day, placed by their instant.
	DaysDerived int
	// Stats is what the reader passed over; Stats.Bytes is the length of the
	// ledger prefix the report is of.
	Stats UsageScanStats
}

type usageReportKey struct{ day, model, kind, subject, root string }

// checkReportRange refuses a query that is not a bounded range.
func (q UsageQuery) checkReportRange() error {
	if q.From.IsZero() || q.To.IsZero() {
		return fmt.Errorf("%w: a report needs both a from and a to day", ErrUsageQuery)
	}
	if q.To.After(q.From.AddDate(0, 0, UsageReportMaxDays)) {
		return fmt.Errorf("%w: a report covers at most %d days", ErrUsageQuery, UsageReportMaxDays)
	}
	return nil
}

// validStoredDay reports whether day is a calendar day as the ledger writes it.
func validStoredDay(day string) bool {
	if len(day) != len(usageDayLayout) {
		return false
	}
	_, err := time.Parse(usageDayLayout, day)
	return err == nil
}

// QueryUsageReport adds up every ledger line the query matches into day, model
// and subject cells. The query must name both ends of a range of at most
// [UsageReportMaxDays] days; loc places a line that has no stored day.
func QueryUsageReport(path string, q UsageQuery, loc *time.Location, budget UsageReportBudget) (UsageReport, error) {
	q, err := q.normalized()
	if err != nil {
		return UsageReport{}, err
	}
	if err := q.checkReportRange(); err != nil {
		return UsageReport{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	budget = budget.bounded()
	file, done, err := openUsageScan(path)
	if err != nil {
		return UsageReport{}, err
	}
	defer done()

	var report UsageReport
	if file == nil {
		return report, nil
	}
	info, err := file.Stat()
	if err != nil {
		return UsageReport{}, err
	}
	cells := map[usageReportKey]*UsageReportCell{}
	var weight int64
	lastDay, lastDayValid := "", false
	report.Stats, err = scanUsageLines(file, info.Size(), func(_ int64, line UsageLine) error {
		line.Run = strings.TrimSpace(line.Run)
		if !q.matches(line) {
			return nil
		}
		if err := report.Totals.count(line); err != nil {
			return err
		}
		day := line.Day
		if day != lastDay {
			lastDay, lastDayValid = day, validStoredDay(day)
		}
		if !lastDayValid {
			day = line.At.In(loc).Format(usageDayLayout)
			report.DaysDerived++
		}
		kind, subject := usageSubject(line)
		root := ""
		if kind == SubjectTask {
			root = strings.TrimSpace(line.Root)
		}
		key := usageReportKey{day: day, model: strings.TrimSpace(line.Model), kind: kind, subject: subject, root: root}
		cell := cells[key]
		if cell == nil {
			cost := int64(usageReportCellBytes + len(key.day) + len(key.model) + len(key.kind) + len(key.subject) + len(key.root))
			if len(cells) >= budget.MaxCells || weight+cost > budget.MaxBytes {
				return fmt.Errorf("%w (more than %d groups or %d estimated bytes)", ErrUsageReportTooLarge, budget.MaxCells, budget.MaxBytes)
			}
			// Only now are the strings copied, so a line's large id is never
			// retained by a map it was refused from.
			key = usageReportKey{strings.Clone(key.day), strings.Clone(key.model), key.kind, strings.Clone(key.subject), strings.Clone(key.root)}
			cell = &UsageReportCell{Day: key.day, Model: key.model, Kind: key.kind, Subject: key.subject, Root: key.root}
			cells[key] = cell
			weight += cost
		}
		cell.Records++
		cell.Calls += max(line.Calls, 1)
		cell.Input += int64(line.Input)
		cell.Output += int64(line.Output)
		if line.USD > 0 {
			cell.USD += line.USD
		} else {
			cell.Unpriced++
		}
		if line.Unbilled {
			cell.Unbilled++
		}
		return nil
	})
	if err != nil {
		return UsageReport{}, err
	}
	report.Cells = make([]UsageReportCell, 0, len(cells))
	seen := map[string]struct{}{}
	for _, cell := range cells {
		report.Cells = append(report.Cells, *cell)
		if cell.Model != "" {
			if _, ok := seen[cell.Model]; !ok {
				seen[cell.Model] = struct{}{}
				report.Models = append(report.Models, cell.Model)
			}
		}
	}
	slices.SortFunc(report.Cells, func(a, b UsageReportCell) int {
		return cmp.Or(strings.Compare(a.Day, b.Day), strings.Compare(a.Model, b.Model), strings.Compare(a.Kind, b.Kind),
			strings.Compare(a.Subject, b.Subject), strings.Compare(a.Root, b.Root))
	})
	slices.Sort(report.Models)
	return report, nil
}
