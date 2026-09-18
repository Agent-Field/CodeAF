// Package record turns a judge's seat scores into the two records an install
// keeps of them: additive cells in a tally sheet on disk, and one row per
// score in an outbox.
//
// THE MECHANISM. [Recorder.Record] takes the scores [judge.Judge] answered and
// does two things with them. Every valid one is observed into the sheet the
// recorder holds, under the role_quality metric the crew picker reads seat
// quality from, and one row is appended to the recorder's outbox when it holds
// one. A score whose role is not one of the judged seats, or that is not on
// the 0-100 scale, is skipped and named in the error; the others are recorded
// however the round went, and the sheet is observed whatever the outbox does.
// A nil outbox records locally only.
//
// The sheet an install keeps of its own scores is its own evidence: it is
// saved under the pool directory ([OwnSheetName]) and read back as the cells
// the crew picker's prior blends in beside an index's ([Cells]).
package record

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/tally"
)

// Metric is the sheet metric a judged seat score is observed under: a model's
// 0-100 opinion of a seat's work. It is kept as its own evidence and is NOT
// what the crew picker reads a measured seat quality from; that is Acceptable.
const Metric = "role_quality"

// Acceptable is the sheet metric a graded task is observed under, one
// observation per seat the crew held: 100 when the harness's own model-free
// grade of the landing passed, 0 when it did not. Its mean is the share of
// graded tasks accepted, on the 0-100 scale the crew picker scores a seat on,
// and it is the one metric the picker learns from. The cell carries one dim,
// SourceDim, naming which grader said so — internal/pool/grade's source words
// for this install's own grades, and the seeded reviewer for the index's
// seed — so the relay can fit a reliability per source and never blends two
// scales in one cell (docs/design/model-pool/pareto-crewing.tex, the reward
// section).
const Acceptable = "acceptable"

// SourceDim is the dim label an Acceptable cell carries its grader under. The
// label's value is the source's own word — `grader`, `grader-build`,
// `reviewer` — which is the wire's judge id with its `codeaf/` vendor taken
// off, because a sheet label may not carry a slash and a wire judge must.
const SourceDim = "source"

// sourceWord is the sheet's spelling of a wire source: the text after the
// last slash, or the whole id when there is none.
func sourceWord(source string) string {
	if i := strings.LastIndex(source, "/"); i >= 0 {
		return source[i+1:]
	}
	return source
}

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

// Cells reads the sheet's own Acceptable cells as the cells a prior reads:
// the seat, the model, the share accepted and the count behind it, one cell
// per source, sorted by seat then model then source, so the answer is a
// property of the sheet and never of the order it was observed in. The
// picker's PriorFromCells folds the sources of one seat and model into one
// rating. A nil sheet answers no cells.
func Cells(s *tally.Sheet) []crewpick.Cell {
	if s == nil {
		return nil
	}
	var cells []crewpick.Cell
	s.Each(Acceptable, func(role, model string, dims map[string]string, c tally.Cell) {
		cells = append(cells, crewpick.Cell{Role: role, Model: model, Mean: c.Mean(), N: int(c.N)})
	})
	return cells
}

// RecordGrade observes one graded task into the sheet, one Acceptable
// observation per seat the crew held — 100 when the grade passed, 0 when it
// did not — under the source that graded it, and appends one row per seat to
// the outbox when the recorder holds one, with the source in the row's judge
// column, which is the column the relay fits a reliability per grader on.
// THE SHEET IS OBSERVED WHATEVER THE OUTBOX DOES, exactly as Record keeps it.
// A seat whose role is not a judged seat, or whose model is empty, is skipped
// and named in the error beside the others that were recorded.
func (r *Recorder) RecordGrade(seats map[judge.Role]string, source string, pass bool, door, size, day string) error {
	if r.Sheet == nil {
		return errors.New("record: no sheet to observe into")
	}
	score := 0.0
	if pass {
		score = 100
	}
	roles := make([]judge.Role, 0, len(seats))
	for role := range seats {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	var errs []error
	var appendErr error
	for _, role := range roles {
		model := seats[role]
		if !judgedRoles[role] {
			errs = append(errs, fmt.Errorf("record: the %s seat is not a judged seat and was skipped", role))
			continue
		}
		if model == "" {
			errs = append(errs, fmt.Errorf("record: the %s seat names no model and was skipped", role))
			continue
		}
		r.Sheet.Observe(Acceptable, string(role), model, map[string]string{SourceDim: sourceWord(source)}, score)
		if r.Outbox == nil {
			continue
		}
		line, err := json.Marshal(Row{
			Schema: 1,
			Metric: Acceptable,
			Role:   string(role),
			Model:  model,
			Score:  score,
			Judge:  source,
			Door:   door,
			Size:   size,
			Day:    day,
		})
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
