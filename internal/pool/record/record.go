// Package record turns a judge's seat scores into the two records an install
// keeps of them: additive cells in a tally sheet on disk, and one row per
// score in an outbox.
//
// THE MECHANISM. [Recorder.Record] takes the scores [judge.Judge] answered and
// does two things with them. Every valid one is observed into the sheet the
// recorder holds, under the role_quality metric, and one row is appended to
// the recorder's outbox when it holds
// one. A score whose role is not one of the judged seats, or that is not on
// the 0-100 scale, is skipped and named in the error; the others are recorded
// however the round went, and the sheet is observed whatever the outbox does.
// A nil outbox records locally only.
//
// The sheet an install keeps of its own scores is its own evidence: it is
// saved under the pool directory ([OwnSheetName]) and read back as cells
// ([Cells]) — which `codeaf pool` lists beside the index's.
package record

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/tally"
)

// Metric is the sheet metric a judged seat score is observed under.
const Metric = "role_quality"

// OwnSheetName is the file the install's own sheet is kept as, under the pool
// directory.
const OwnSheetName = "own.json"

// OwnSheetPath is where the install's own sheet lives for the pool directory
// given.
func OwnSheetPath(poolDir string) string {
	return filepath.Join(poolDir, OwnSheetName)
}

// dirMode is the mode the pool's own directory keeps: private to the person.
const dirMode = 0700

// LoadSheet reads the sheet at path. A path with no file answers an empty
// sheet and no error — an install that has recorded nothing yet is not a
// fault; bytes that are there and do not parse as a sheet document are.
func LoadSheet(path string) (*tally.Sheet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tally.New(), nil
		}
		return nil, err
	}
	var s tally.Sheet
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("record: %s does not hold a sheet document: %w", path, err)
	}
	return &s, nil
}

// SaveSheet writes the sheet to a temporary file in the path's own directory
// and renames it into place, so a reader mid-write never sees half a document.
// The file is mode 0600 and its parent directory is created with mode 0700
// when it is missing.
func SaveSheet(path string, s *tally.Sheet) error {
	doc, err := json.Marshal(s)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, OwnSheetName+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // nothing once the rename has landed
	if _, err := tmp.Write(append(doc, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// rowSchema is the only row schema this package writes.
const rowSchema = 1

// Row is one judged seat score as it leaves the install: what was scored —
// the metric, the seat and the model that held it, the score on the 0-100
// scale — and the facts of the run it was read from, the judge that answered,
// the door the run came in by (task, do, exec or run), the size of the crew,
// and the day.
type Row struct {
	Schema int     `json:"schema"`
	Metric string  `json:"metric"`
	Role   string  `json:"role"`
	Model  string  `json:"model"`
	Score  float64 `json:"score"`
	Judge  string  `json:"judge"`
	Door   string  `json:"door"`
	Size   string  `json:"size"`
	Day    string  `json:"day"`
}

// ExampleRowJSON is one row as the relay would receive it, on the day given,
// with placeholder slugs where a real row carries the model that held the
// seat and the model that judged it. `codeaf telemetry show` prints it so a
// person sees the bytes before any row exists. It is marshalled from [Row],
// so it cannot spell a key a real row would not.
func ExampleRowJSON(now time.Time) string {
	row := Row{
		Schema: rowSchema,
		Metric: Metric,
		Role:   "worker",
		Model:  "<the seat's model slug>",
		Score:  81,
		Judge:  "<the judge's model slug>",
		Door:   "task",
		Size:   "M",
		Day:    now.UTC().Format("2006-01-02"),
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(row); err != nil {
		return ""
	}
	return strings.TrimRight(out.String(), "\n")
}

// RowsOf reads judge scores into rows, one per score. Every row carries
// schema 1 and the role_quality metric; the day is spelled by the caller, the
// way the outbox spells its own.
func RowsOf(scores []judge.Score, judgeModel, door, size, day string) []Row {
	rows := make([]Row, 0, len(scores))
	for _, score := range scores {
		rows = append(rows, Row{
			Schema: rowSchema,
			Metric: Metric,
			Role:   string(score.Role),
			Model:  score.Model,
			Score:  score.Score,
			Judge:  judgeModel,
			Door:   door,
			Size:   size,
			Day:    day,
		})
	}
	return rows
}

// judgedRoles is the whole set of seats a score may name: the three seats the
// judge reads, and nothing else. A role outside it is not a seat the picker
// reads a quality for.
var judgedRoles = map[judge.Role]bool{
	judge.RoleWorker:     true,
	judge.RoleHigh:       true,
	judge.RoleMastermind: true,
}

// Recorder turns judge scores into the two records an install keeps: the
// sheet observed locally and, when one is held, the outbox rows that leave.
type Recorder struct {
	// Sheet holds the install's own tallies; a recorder without one has
	// nowhere to put what it is given, and refuses.
	Sheet *tally.Sheet
	// Outbox is the rows' way out. A nil Outbox keeps the scores locally
	// only.
	Outbox *outbox.Outbox
}

// Record observes every score it is given into the sheet, and appends one row
// per score to the outbox when the recorder holds one. THE SHEET IS OBSERVED
// WHATEVER THE OUTBOX DOES: every score is observed before the first row is
// appended, so an append that fails costs the install nothing of its own
// evidence, and the first append error is returned once every score has been
// observed. A score whose role is not one of the judged seats, or whose score
// is not on the 0-100 scale, is skipped and named in the error beside the
// others that were recorded.
func (r *Recorder) Record(scores []judge.Score, judgeModel, door, size, day string) error {
	if r.Sheet == nil {
		return errors.New("record: no sheet to observe into")
	}
	rows := RowsOf(scores, judgeModel, door, size, day)
	var errs []error
	var appendErr error
	for i, score := range scores {
		if !judgedRoles[score.Role] {
			errs = append(errs, fmt.Errorf("record: the %s seat is not a judged seat and was skipped", score.Role))
			continue
		}
		if score.Score < 0 || score.Score > 100 {
			errs = append(errs, fmt.Errorf("record: the %s seat's score %v is outside 0-100 and was skipped", score.Role, score.Score))
			continue
		}
		r.Sheet.Observe(Metric, string(score.Role), score.Model, nil, score.Score)
		if r.Outbox == nil {
			continue
		}
		line, err := json.Marshal(rows[i])
		if err != nil {
			continue // a struct of scalars never fails to marshal
		}
		if err := r.Outbox.Append(line); err != nil && appendErr == nil {
			appendErr = err
		}
	}
	if appendErr != nil {
		errs = append(errs, appendErr)
	}
	return errors.Join(errs...)
}

// Cell is one seat's measured quality on a sheet: the seat, the model, the
// mean of its 0-100 scores and how many stand behind it.
type Cell struct {
	Role  string
	Model string
	Mean  float64
	N     int
}

// Cells reads the sheet's own role_quality cells — the ones recorded with no
// dim labels — sorted by seat then model, so the answer is a property of the
// sheet and never of the order it was observed in. A nil sheet answers no
// cells.
func Cells(s *tally.Sheet) []Cell {
	if s == nil {
		return nil
	}
	var cells []Cell
	s.Each(Metric, func(role, model string, dims map[string]string, c tally.Cell) {
		if dims != nil {
			return
		}
		cells = append(cells, Cell{Role: role, Model: model, Mean: c.Mean(), N: int(c.N)})
	})
	return cells
}
