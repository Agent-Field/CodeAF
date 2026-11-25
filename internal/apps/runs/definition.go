// Package runs executes an app's declarative run definitions against scoped
// adapters, with progress and timers in the engine journal.
package runs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Definition struct {
	Schema string               `json:"$schema,omitempty" yaml:"$schema"`
	Run    string               `json:"run" yaml:"run"`
	Title  string               `json:"title" yaml:"title"`
	Budget Budget               `json:"budget" yaml:"budget"`
	Params map[string]Parameter `json:"params" yaml:"params"`
	Steps  []Step               `json:"steps" yaml:"steps"`
}

type Budget struct {
	PerRun float64 `json:"per_run" yaml:"per_run"`
}

type Parameter struct {
	Type    string `json:"type" yaml:"type"`
	To      string `json:"to" yaml:"to"`
	Default any    `json:"default" yaml:"default"`
}

type Step struct {
	Title     string         `json:"title,omitempty" yaml:"title"`
	Repo      string         `json:"repo,omitempty" yaml:"repo"`
	Branch    string         `json:"branch,omitempty" yaml:"branch"`
	ID        string         `json:"id" yaml:"id"`
	Create    *RecordWrite   `json:"records.create,omitempty" yaml:"records.create"`
	Update    *RecordWrite   `json:"records.update,omitempty" yaml:"records.update"`
	Agent     string         `json:"agent,omitempty" yaml:"agent"`
	Prompt    string         `json:"prompt,omitempty" yaml:"prompt"`
	Tool      string         `json:"tool,omitempty" yaml:"tool"`
	As        string         `json:"as,omitempty" yaml:"as"`
	Args      map[string]any `json:"args,omitempty" yaml:"args"`
	Gate      *Gate          `json:"gate,omitempty" yaml:"gate"`
	Wait      *Wait          `json:"wait,omitempty" yaml:"wait"`
	Loop      *Loop          `json:"loop,omitempty" yaml:"loop"`
	Steps     []Step         `json:"steps,omitempty" yaml:"steps"`
	If        string         `json:"if,omitempty" yaml:"if"`
	Retry     Retry          `json:"retry,omitempty" yaml:"retry"`
	OnError   string         `json:"on_error,omitempty" yaml:"on_error"`
	Workspace string         `json:"workspace,omitempty" yaml:"workspace"`
	Shell     any            `json:"shell,omitempty" yaml:"shell"`
}

type RecordWrite struct {
	Type string         `json:"type" yaml:"type"`
	ID   any            `json:"id,omitempty" yaml:"id"`
	IDs  any            `json:"ids,omitempty" yaml:"ids"`
	Set  map[string]any `json:"set" yaml:"set"`
}

type Wait struct {
	For string `json:"for" yaml:"for"`
}

type Gate struct {
	Approval string `json:"approval" yaml:"approval"`
	Who      string `json:"who" yaml:"who"`
}

type Loop struct {
	Over  any    `json:"over,omitempty" yaml:"over"`
	As    string `json:"as,omitempty" yaml:"as"`
	Until string `json:"until,omitempty" yaml:"until"`
	Max   int    `json:"max" yaml:"max"`
}

type Retry struct {
	Max     int    `json:"max" yaml:"max"`
	Backoff string `json:"backoff" yaml:"backoff"`
}

// Parse checks execution invariants as well as YAML shape, since callers may
// execute sample definitions before they reach the app store.
func Parse(contents []byte) (Definition, error) {
	var def Definition
	dec := yaml.NewDecoder(bytes.NewReader(contents))
	dec.KnownFields(true)
	if err := dec.Decode(&def); err != nil {
		return Definition{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Definition{}, errors.New("a run file must contain one YAML document")
	}
	return def, def.validate()
}

func (d Definition) validate() error {
	if !validID(d.Run) || len(d.Steps) == 0 || d.Budget.PerRun < 0 ||
		math.IsInf(d.Budget.PerRun, 0) || math.IsNaN(d.Budget.PerRun) {
		return errors.New("a run needs an id, steps and a valid per-run budget")
	}
	return validateSteps(d.Steps, make(map[string]bool))
}

func validID(id string) bool {
	return id != "" && strings.Trim(id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
}

func validateSteps(steps []Step, seen map[string]bool) error {
	siblings := make(map[string]bool, len(steps))
	for _, s := range steps {
		siblings[s.ID] = true
	}
	for _, s := range steps {
		if !validID(s.ID) || seen[s.ID] {
			return fmt.Errorf("invalid or duplicate step id %q", s.ID)
		}
		seen[s.ID] = true
		if err := s.validate(siblings); err != nil {
			return fmt.Errorf("step %s: %w", s.ID, err)
		}
		if err := validateSteps(s.Steps, seen); err != nil {
			return err
		}
	}
	return nil
}

const maxRetries = 5

func (s Step) validate(siblings map[string]bool) error {
	if s.Shell != nil || s.Workspace != "" || s.Repo != "" || s.Branch != "" {
		return errors.New("shell and workspace steps are unavailable in app runs")
	}
	kinds := 0
	for _, present := range []bool{s.Create != nil, s.Update != nil, s.Agent != "", s.Tool != "", s.Wait != nil, s.Loop != nil} {
		if present {
			kinds++
		}
	}
	if kinds > 1 || (kinds == 0 && s.Gate == nil) || (s.Loop == nil && len(s.Steps) > 0) {
		return errors.New("a step needs one action or a gate")
	}
	if s.Gate != nil {
		switch s.Gate.Approval {
		case "plan", "proof", "start", "send":
		default:
			return errors.New("gate approval is plan, proof, start or send")
		}
	}
	if s.Loop != nil {
		if s.Loop.Max < 1 || len(s.Steps) == 0 || (s.Loop.Over == nil) == (s.Loop.Until == "") {
			return errors.New("a loop needs steps, a positive max and exactly one of over or until")
		}
		if s.Loop.Over != nil && (!validID(s.Loop.As) || reservedVariable(s.Loop.As)) {
			return errors.New("a for-each loop needs an as variable")
		}
	}
	if s.Wait != nil {
		if _, err := duration(s.Wait.For); err != nil {
			return err
		}
	}
	if s.Retry.Max < 0 || s.Retry.Max > maxRetries {
		return fmt.Errorf("retry max must be between 0 and %d", maxRetries)
	}
	if s.Retry.Backoff != "" {
		if _, err := duration(s.Retry.Backoff); err != nil {
			return err
		}
	}
	switch s.OnError {
	case "", "stop", "continue":
	default:
		id, ok := strings.CutPrefix(s.OnError, "goto ")
		if !ok || !siblings[id] || id == s.ID {
			return errors.New("on_error must be stop, continue or goto another step in this list")
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
