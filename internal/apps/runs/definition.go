// Package runs executes an app's declarative run definitions against scoped
// adapters, with progress and timers in the engine journal.
package runs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type Definition = schema.Run
type Parameter = schema.RunParam
type Step = schema.Step

// Parse applies the app's run file shape and checks execution invariants,
// since Check and Preview can execute sample definitions before Keep.
func Parse(contents []byte) (Definition, error) {
	raw, err := schema.RunJSON(contents)
	if err != nil {
		return Definition{}, err
	}
	var document struct {
		schema.Run
		Schema string `json:"$schema,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&document); err != nil {
		return Definition{}, err
	}
	return document.Run, validate(document.Run)
}

func validate(d Definition) error {
	if !validID(d.Run) || len(d.Steps) == 0 || (d.Budget != nil && !validCost(d.Budget.PerRun)) {
		return errors.New("a run needs an id, steps and a valid per-run budget")
	}
	return validateSteps(d.Steps, make(map[string]bool), make(map[string]bool))
}

func validID(id string) bool {
	return id != "" && strings.Trim(id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
}

func validateSteps(steps []Step, seen, enclosing map[string]bool) error {
	targets := maps.Clone(enclosing)
	for _, s := range steps {
		targets[s.ID] = true
	}
	for _, s := range steps {
		if !validID(s.ID) || seen[s.ID] {
			return fmt.Errorf("invalid or duplicate step id %q", s.ID)
		}
		seen[s.ID] = true
		if err := validateStep(s, targets); err != nil {
			return fmt.Errorf("step %s: %w", s.ID, err)
		}
		if err := validateSteps(s.Steps, seen, targets); err != nil {
			return err
		}
	}
	return nil
}

const maxRetries = 5

func validateStep(s Step, targets map[string]bool) error {
	if s.Shell != "" || s.Workspace != "" || s.Repo != "" || s.Branch != "" {
		return errors.New("shell and workspace steps are unavailable in app runs")
	}
	kinds := 0
	for _, present := range []bool{s.RecordsCreate != nil, s.RecordsUpdate != nil, s.Agent != "", s.Tool != "", s.Wait != nil, s.Loop != nil} {
		if present {
			kinds++
		}
	}
	if kinds > 1 || (kinds == 0 && s.Gate == nil) || (s.Loop == nil && len(s.Steps) > 0) {
		return errors.New("a step needs one action or a gate")
	}
	if (s.RecordsCreate != nil && s.RecordsCreate.Set == nil) ||
		(s.RecordsUpdate != nil && len(s.RecordsUpdate.Set) == 0) {
		return errors.New("record writes need a set of fields")
	}
	if s.Gate != nil {
		switch s.Gate.Approval {
		case "plan", "proof", "start", "send":
		default:
			return errors.New("gate approval is plan, proof, start or send")
		}
	}
	if s.Loop != nil {
		if s.Loop.Max < 1 || len(s.Steps) == 0 || (s.Loop.Over == "") == (s.Loop.Until == "") {
			return errors.New("a loop needs steps, a positive max and exactly one of over or until")
		}
		if s.Loop.Over != "" && (!validID(s.Loop.As) || reservedVariable(s.Loop.As)) {
			return errors.New("a for-each loop needs an as variable")
		}
	}
	if s.Wait != nil {
		if s.Wait.Until != "" || s.Wait.Thread != "" || s.Wait.Timeout != "" || s.OnTimeout != "" {
			return errors.New("app runs wait for a duration")
		}
		if _, err := duration(s.Wait.For); err != nil {
			return err
		}
	}
	if s.Retry != nil {
		if s.Retry.Max < 1 || s.Retry.Max > maxRetries {
			return fmt.Errorf("retry max must be between 1 and %d", maxRetries)
		}
		if s.Retry.Backoff != "" {
			if _, err := duration(s.Retry.Backoff); err != nil {
				return err
			}
		}
	}
	switch s.OnError {
	case "", "stop", "continue":
	default:
		id, ok := strings.CutPrefix(s.OnError, "goto ")
		if !ok || !targets[id] || id == s.ID {
			return errors.New("on_error must be stop, continue or goto a step in this or an enclosing list")
		}
	}
	return nil
}

func duration(value string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(value, "d"); ok {
		value = n
		days, err := time.ParseDuration(value + "h")
		if err == nil && days > 0 && days <= time.Duration(math.MaxInt64)/24 {
			return days * 24, nil
		}
		return 0, errors.New("expected a positive duration")
	}
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d, nil
	}
	return 0, errors.New("expected a positive duration")
}
