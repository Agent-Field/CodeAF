// Package shape reads what real tasks spent on each seat out of the usage
// ledger, in the form the crew picker learns a seat's shape from.
//
// It touches one file and imports nothing from the session: the ledger's rows
// are decoded through a private struct that names only the fields a shape is
// read from, so a row this build never wrote still decodes, and a row with no
// seat word or no task id — every row written before those fields existed,
// every errand outside a task — is simply not evidence about a seat.
package shape

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/Agent-Field/codeaf/internal/crewpick"
)

// usageRow is the sliver of a usage ledger line a shape is read from. The
// field names are the ledger's own (internal/session.UsageLine), and nothing
// else on the line is decoded.
type usageRow struct {
	Seat string `json:"seat"`
	Task string `json:"task"`
	In   int64  `json:"in"`
	Out  int64  `json:"out"`
}

// seatWords maps the ledger's seat words to the three seats a crew is picked
// for. The reflex, low, judge and talk seats are billed but not picked, so a
// row on one of them is not evidence about a picked seat's shape.
var seatWords = map[string]crewpick.Seat{
	"worker":     crewpick.Worker,
	"high":       crewpick.High,
	"mastermind": crewpick.Mastermind,
}

// maxLine bounds one ledger line; a line past it is not a row this build
// wrote and is skipped rather than read.
const maxLine = 1 << 16

// Tasks reads the usage ledger at path into one TaskUsage per task id the
// rows name, summing each seat's input and output tokens across the rows the
// task spent on it. A row without a seat word, without a task id, or on a
// seat that is not picked is skipped, and so is a line that does not decode:
// the ledger is append-only and a torn last line is ordinary. A missing file
// is no tasks and no error, because a machine that has never run a task has
// nothing to teach and that is not a fault. The tasks come back in task-id
// order, so the answer is a property of the ledger and not of the read.
func Tasks(path string) ([]crewpick.TaskUsage, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return read(f)
}

// read is Tasks over an open ledger.
func read(r io.Reader) ([]crewpick.TaskUsage, error) {
	byTask := map[string]crewpick.TaskUsage{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		var row usageRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			continue
		}
		seat, ok := seatWords[row.Seat]
		if !ok || row.Task == "" || row.In+row.Out <= 0 {
			continue
		}
		task := byTask[row.Task]
		if task == nil {
			task = crewpick.TaskUsage{}
			byTask[row.Task] = task
		}
		t := task[seat]
		t.In += row.In
		t.Out += row.Out
		task[seat] = t
	}
	if err := sc.Err(); err != nil && !errors.Is(err, bufio.ErrTooLong) {
		return nil, err
	}
	ids := make([]string, 0, len(byTask))
	for id := range byTask {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tasks := make([]crewpick.TaskUsage, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, byTask[id])
	}
	return tasks, nil
}
