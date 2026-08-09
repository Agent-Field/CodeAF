// Package auditorgate ports src/session/auditor-gate.ts:1-2744 from the
// frozen swe-pro commit 3b25a1a. This file defines the auditor verdict wire
// contract and preserves JavaScript object insertion order for spread-based
// verdict transformations.
package auditorgate

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/auditconvergence"
)

type VerdictStatus string

const (
	VerdictPass VerdictStatus = "pass"
	VerdictFail VerdictStatus = "fail"
)

type ClauseCoverage struct {
	Clause   string `json:"clause"`
	Evidence string `json:"evidence"`
}

type AcceptanceRow struct {
	Criterion string  `json:"criterion"`
	Claimed   *string `json:"claimed,omitempty"`
	Verified  *string `json:"verified,omitempty"`
	Evidence  *string `json:"evidence,omitempty"`
}

type Step2Signal struct {
	Reproduced          *bool `json:"reproduced,omitempty"`
	Commands            []any `json:"commands,omitempty"`
	SpecExamplesMatched any   `json:"spec_examples_matched,omitempty"`
	Notes               *string
	raw                 json.RawMessage
}

func (s *Step2Signal) UnmarshalJSON(data []byte) error {
	type wire struct {
		Reproduced          *bool   `json:"reproduced,omitempty"`
		Commands            []any   `json:"commands,omitempty"`
		SpecExamplesMatched any     `json:"spec_examples_matched,omitempty"`
		Notes               *string `json:"notes,omitempty"`
	}
	var w wire
	if err := decodeUseNumber(data, &w); err != nil {
		return err
	}
	s.Reproduced = w.Reproduced
	s.Commands = w.Commands
	s.SpecExamplesMatched = w.SpecExamplesMatched
	s.Notes = w.Notes
	s.raw = cloneBytes(data)
	return nil
}

func (s Step2Signal) MarshalJSON() ([]byte, error) {
	if len(s.raw) > 0 {
		return cloneBytes(s.raw), nil
	}
	obj := newOrderedObject()
	if s.Reproduced != nil {
		obj.setValue("reproduced", *s.Reproduced)
	}
	if s.Commands != nil {
		obj.setValue("commands", s.Commands)
	}
	if s.SpecExamplesMatched != nil {
		obj.setValue("spec_examples_matched", s.SpecExamplesMatched)
	}
	if s.Notes != nil {
		obj.setValue("notes", *s.Notes)
	}
	return obj.bytes(), nil
}

type Step3Scope struct {
	CallersChecked []string `json:"callers_checked,omitempty"`
	MissingSites   []string `json:"missing_sites,omitempty"`
	Regressions    []string `json:"regressions,omitempty"`
}

type Step4Structural struct {
	ShapeMatchesSpec *bool    `json:"shape_matches_spec,omitempty"`
	Concerns         []string `json:"concerns,omitempty"`
}

type Blocker struct {
	File     *string                           `json:"file,omitempty"`
	Line     *float64                          `json:"line,omitempty"`
	Step     *float64                          `json:"step,omitempty"`
	Detail   string                            `json:"detail"`
	Severity *auditconvergence.BlockerSeverity `json:"severity,omitempty"`
	raw      json.RawMessage
}

func (b *Blocker) UnmarshalJSON(data []byte) error {
	type wire struct {
		File     *string                           `json:"file,omitempty"`
		Line     *float64                          `json:"line,omitempty"`
		Step     *float64                          `json:"step,omitempty"`
		Detail   string                            `json:"detail"`
		Severity *auditconvergence.BlockerSeverity `json:"severity,omitempty"`
	}
	var w wire
	if err := decodeUseNumber(data, &w); err != nil {
		return err
	}
	b.File, b.Line, b.Step = w.File, w.Line, w.Step
	b.Detail, b.Severity = w.Detail, w.Severity
	b.raw = cloneBytes(data)
	return nil
}

func (b Blocker) MarshalJSON() ([]byte, error) {
	if len(b.raw) > 0 {
		return cloneBytes(b.raw), nil
	}
	obj := newOrderedObject()
	if b.File != nil {
		obj.setValue("file", *b.File)
	}
	if b.Line != nil {
		obj.setValue("line", *b.Line)
	}
	if b.Step != nil {
		obj.setValue("step", *b.Step)
	}
	obj.setValue("detail", b.Detail)
	if b.Severity != nil {
		obj.setValue("severity", *b.Severity)
	}
	return obj.bytes(), nil
}

func newBlockerOrdered(fields ...orderedField) Blocker {
	obj := newOrderedObject()
	for _, field := range fields {
		obj.setValue(field.name, field.value)
	}
	raw := obj.bytes()
	var blocker Blocker
	_ = json.Unmarshal(raw, &blocker)
	return blocker
}

func (b Blocker) withSeverity(severity auditconvergence.BlockerSeverity) Blocker {
	if b.Severity != nil {
		return b
	}
	obj, err := parseOrderedObject(b.raw)
	if err != nil {
		obj = newOrderedObject()
		if b.File != nil {
			obj.setValue("file", *b.File)
		}
		if b.Line != nil {
			obj.setValue("line", *b.Line)
		}
		if b.Step != nil {
			obj.setValue("step", *b.Step)
		}
		obj.setValue("detail", b.Detail)
	}
	obj.setValue("severity", severity)
	raw := obj.bytes()
	var out Blocker
	_ = json.Unmarshal(raw, &out)
	return out
}

type AuditorVerdict struct {
	Verdict          VerdictStatus    `json:"verdict"`
	Commands         []any            `json:"commands,omitempty"`
	Notes            *string          `json:"notes,omitempty"`
	Step1Goal        *string          `json:"step1_goal,omitempty"`
	Step2Signal      *Step2Signal     `json:"step2_signal,omitempty"`
	Step3Scope       *Step3Scope      `json:"step3_scope,omitempty"`
	Step4Structural  *Step4Structural `json:"step4_structural,omitempty"`
	Blockers         []Blocker        `json:"blockers,omitempty"`
	RepairHints      []string         `json:"repair_hints,omitempty"`
	ClauseCoverage   []ClauseCoverage `json:"clause_coverage,omitempty"`
	Step2CAcceptance []AcceptanceRow  `json:"step2c_acceptance,omitempty"`
	raw              json.RawMessage
}

func (v *AuditorVerdict) UnmarshalJSON(data []byte) error {
	type wire struct {
		Verdict          VerdictStatus    `json:"verdict"`
		Commands         []any            `json:"commands,omitempty"`
		Notes            *string          `json:"notes,omitempty"`
		Step1Goal        *string          `json:"step1_goal,omitempty"`
		Step2Signal      *Step2Signal     `json:"step2_signal,omitempty"`
		Step3Scope       *Step3Scope      `json:"step3_scope,omitempty"`
		Step4Structural  *Step4Structural `json:"step4_structural,omitempty"`
		Blockers         []Blocker        `json:"blockers,omitempty"`
		RepairHints      []string         `json:"repair_hints,omitempty"`
		ClauseCoverage   []ClauseCoverage `json:"clause_coverage,omitempty"`
		Step2CAcceptance []AcceptanceRow  `json:"step2c_acceptance,omitempty"`
	}
	var w wire
	if err := decodeUseNumber(data, &w); err != nil {
		return err
	}
	v.Verdict, v.Commands, v.Notes, v.Step1Goal = w.Verdict, w.Commands, w.Notes, w.Step1Goal
	v.Step2Signal, v.Step3Scope, v.Step4Structural = w.Step2Signal, w.Step3Scope, w.Step4Structural
	v.Blockers, v.RepairHints = w.Blockers, w.RepairHints
	v.ClauseCoverage, v.Step2CAcceptance = w.ClauseCoverage, w.Step2CAcceptance
	v.raw = cloneBytes(data)
	return nil
}

func (v AuditorVerdict) MarshalJSON() ([]byte, error) {
	if len(v.raw) > 0 {
		return cloneBytes(v.raw), nil
	}
	obj := newOrderedObject()
	obj.setValue("verdict", v.Verdict)
	if v.Commands != nil {
		obj.setValue("commands", v.Commands)
	}
	if v.Notes != nil {
		obj.setValue("notes", *v.Notes)
	}
	if v.Step1Goal != nil {
		obj.setValue("step1_goal", *v.Step1Goal)
	}
	if v.Step2Signal != nil {
		obj.setValue("step2_signal", v.Step2Signal)
	}
	if v.Step3Scope != nil {
		obj.setValue("step3_scope", v.Step3Scope)
	}
	if v.Step4Structural != nil {
		obj.setValue("step4_structural", v.Step4Structural)
	}
	if v.Blockers != nil {
		obj.setValue("blockers", v.Blockers)
	}
	if v.RepairHints != nil {
		obj.setValue("repair_hints", v.RepairHints)
	}
	if v.ClauseCoverage != nil {
		obj.setValue("clause_coverage", v.ClauseCoverage)
	}
	if v.Step2CAcceptance != nil {
		obj.setValue("step2c_acceptance", v.Step2CAcceptance)
	}
	return obj.bytes(), nil
}

func (v AuditorVerdict) cloneSet(fields ...orderedField) AuditorVerdict {
	obj, err := parseOrderedObject(v.raw)
	if err != nil {
		raw, _ := v.MarshalJSON()
		obj, _ = parseOrderedObject(raw)
	}
	for _, field := range fields {
		obj.setValue(field.name, field.value)
	}
	raw := obj.bytes()
	var out AuditorVerdict
	_ = json.Unmarshal(raw, &out)
	return out
}

func newVerdictOrdered(fields ...orderedField) AuditorVerdict {
	obj := newOrderedObject()
	for _, field := range fields {
		obj.setValue(field.name, field.value)
	}
	var verdict AuditorVerdict
	_ = json.Unmarshal(obj.bytes(), &verdict)
	return verdict
}

func validateAuditorVerdict(v AuditorVerdict) error {
	if v.Verdict != VerdictPass && v.Verdict != VerdictFail {
		return errors.New("verdict must be pass or fail")
	}
	for _, blocker := range v.Blockers {
		if blocker.Detail == "" {
			return errors.New("blocker detail must be a string")
		}
		if blocker.Severity != nil &&
			*blocker.Severity != auditconvergence.SeverityCorrectness &&
			*blocker.Severity != auditconvergence.SeverityHygiene &&
			*blocker.Severity != auditconvergence.SeverityPolish {
			return errors.New("invalid blocker severity")
		}
	}
	return nil
}

type orderedField struct {
	name  string
	value any
}

func field(name string, value any) orderedField { return orderedField{name: name, value: value} }

type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func newOrderedObject() *orderedObject {
	return &orderedObject{values: map[string]json.RawMessage{}}
}

func parseOrderedObject(data []byte) (*orderedObject, error) {
	if len(data) == 0 {
		return nil, errors.New("empty object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("not an object")
	}
	obj := newOrderedObject()
	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("non-string object key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		obj.keys = append(obj.keys, key)
		obj.values[key] = cloneBytes(raw)
	}
	_, err = dec.Token()
	return obj, err
}

func (o *orderedObject) setRaw(name string, raw []byte) {
	if _, exists := o.values[name]; !exists {
		o.keys = append(o.keys, name)
	}
	o.values[name] = cloneBytes(raw)
}

func (o *orderedObject) setValue(name string, value any) {
	raw, err := jscompat.Stringify(value)
	if err != nil {
		raw = []byte("null")
	}
	o.setRaw(name, raw)
}

func (o *orderedObject) bytes() []byte {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			out.WriteByte(',')
		}
		keyJSON, _ := jscompat.Stringify(key)
		out.Write(keyJSON)
		out.WriteByte(':')
		out.Write(o.values[key])
	}
	out.WriteByte('}')
	return out.Bytes()
}

func decodeUseNumber(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(out)
}

func cloneBytes(data []byte) []byte {
	return append([]byte(nil), data...)
}
