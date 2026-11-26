package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/cell"
)

func typedAIOutput(contract map[string]any, output any) (any, error) {
	text, ok := output.(string)
	if !ok || !json.Valid([]byte(text)) {
		return nil, errors.New("the model did not return a JSON value")
	}
	shape, err := schema.CompileAIOutput(contract)
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := shape.Validate(value); err != nil {
		return nil, errors.New("the model response did not match the declared output schema")
	}
	return value, nil
}

// checkAI keeps a run that cannot invoke its declared model out of the journal.
func (e *Engine) checkAI(steps []Step) error {
	for _, step := range steps {
		if step.AI != nil {
			if e.cfg.Agent == nil {
				return ErrAIUnavailable
			}
			if e.cfg.CheckAI != nil {
				if err := e.cfg.CheckAI(step.AI); err != nil {
					return err
				}
			}
		}
		if err := e.checkAI(step.Steps); err != nil {
			return err
		}
	}
	return nil
}

// Subject names the immutable record address of a run.
type Subject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Proposal holds the effect and revision seen by the model. The person may
// review it after the run completes, including after a restart.
type Proposal struct {
	Type     string         `json:"type"`
	Record   string         `json:"record"`
	Revision int64          `json:"revision"`
	Before   map[string]any `json:"before,omitempty"`
	Set      map[string]any `json:"set"`
}

func makeProposal(subject string, vars, set map[string]any) (*Proposal, error) {
	value, err := resolve(subject, vars)
	if err != nil {
		return nil, err
	}
	row, _ := value.(map[string]any)
	id, _ := row["id"].(string)
	typ, _ := row["type"].(string)
	rev, _ := strconv.ParseInt(fmt.Sprint(row["_rev"]), 10, 64)
	if id == "" || typ == "" || rev <= 0 || len(set) == 0 {
		return nil, errors.New("the proposal needs a record revision and at least one value")
	}
	before := make(map[string]any, len(set))
	for name := range set {
		if v, ok := row[name]; ok {
			before[name] = v
		}
	}
	return &Proposal{Type: typ, Record: id, Revision: rev, Set: set, Before: before}, nil
}

// StartFanout admits all subjects together and divides one declared budget
// across them before any worker can spend it.
func (e *Engine) StartFanout(app, run, screen string, params map[string]any, subjects []string) (string, error) {
	if len(subjects) == 0 || len(subjects) > 50 {
		return "", errors.New("choose between 1 and 50 records")
	}
	queue, err := cell.NewID(e.cfg.Now())
	if err != nil {
		return "", err
	}
	checkpoints := make([]checkpoint, 0, len(subjects))
	rows := make(map[string]json.RawMessage, len(subjects))
	unique := map[string]bool{}
	for _, subject := range subjects {
		if subject == "" || unique[subject] {
			return "", errors.New("choose each subject once")
		}
		unique[subject] = true
		input := maps.Clone(params)
		if input == nil {
			input = map[string]any{}
		}
		// Resolve the subject parameter through the normal run adapter.
		def, err := e.cfg.Load(app, run)
		if err != nil {
			return "", err
		}
		if len(def.Steps) != 1 || def.Steps[0].AI == nil || def.Steps[0].AI.Propose == "" {
			return "", errors.New("fan-out needs an action that proposes a change")
		}
		param := strings.TrimPrefix(def.Steps[0].AI.Propose, "$params.")
		input[param] = subject
		c, err := e.newCheckpoint(app, run, input, nil, false)
		if err != nil {
			return "", err
		}
		c.Queue, c.Screen = queue, screen
		budget := *c.Definition.Budget
		budget.PerRun /= float64(len(subjects))
		c.Definition.Budget = &budget
		c.UpdatedAt = e.cfg.Now()
		raw, err := json.Marshal(c)
		if err != nil {
			return "", err
		}
		rows[runPrefix+c.ID] = raw
		checkpoints = append(checkpoints, c)
	}
	e.mu.Lock()
	if e.closed || e.closing {
		e.mu.Unlock()
		return "", errors.New("app runs are closed")
	}
	err = e.journal.RecordAppRunCheckpoints(rows)
	if err == nil {
		maps.Copy(e.checkpoints, rows)
		for _, c := range checkpoints {
			e.track(c.Run)
		}
	}
	e.mu.Unlock()
	if err != nil {
		return "", err
	}
	for _, c := range checkpoints {
		e.changed(c.Run)
	}
	return queue, nil
}
