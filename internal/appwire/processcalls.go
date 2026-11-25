package appwire

import (
	"encoding/json"
	"reflect"
	"strconv"
)

const EventWorkItemsChanged = "work_items.changed"

type WorkItemsChanged struct {
	ID string `json:"id,omitempty"`
}

// ExpectedRevision accepts the engine's numeric revision and the desk's
// decimal string representation of that revision.
type ExpectedRevision int64

func (r *ExpectedRevision) UnmarshalJSON(raw []byte) error {
	var rev int64
	if len(raw) > 0 && raw[0] == '"' {
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err != nil {
			return err
		}
		value, err := strconv.ParseInt(quoted, 10, 64)
		if err != nil {
			return err
		}
		rev = value
	} else if err := json.Unmarshal(raw, &rev); err != nil {
		return err
	}
	*r = ExpectedRevision(rev)
	return nil
}

type ProcessParams struct {
	DataScope
	Record DataRecordRef `json:"record"`
}

type TransitionSelection struct {
	Record      DataRecordRef    `json:"record"`
	ExpectedRev ExpectedRevision `json:"expected_rev"`
}

type TransitionBatchParams struct {
	DataScope
	Transition string                `json:"transition"`
	Records    []TransitionSelection `json:"records"`
	Reason     string                `json:"reason,omitempty"`
	Unfreeze   string                `json:"unfreeze,omitempty"`
}

type ChecklistSetParams struct {
	ProcessParams
	Checklist   string             `json:"checklist"`
	Step        string             `json:"step"`
	Done        *bool              `json:"done"`
	ExpectedRev ExpectedRevision   `json:"expected_rev"`
	Evidence    *ChecklistEvidence `json:"evidence,omitempty"`
}

type ChecklistEvidence struct {
	Kind   string         `json:"kind"`
	Text   string         `json:"text,omitempty"`
	File   *ChecklistFile `json:"file,omitempty"`
	Record *DataRecordRef `json:"record,omitempty"`
}

type ChecklistFile struct {
	Blob    string `json:"blob"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Content string `json:"content,omitempty"`
}

type DataProcessResult struct {
	State       string                 `json:"state,omitempty"`
	Rev         int64                  `json:"_rev"`
	Transitions []ProcessTransition    `json:"transitions"`
	Checklist   *ProcessChecklist      `json:"checklist,omitempty"`
	Checklists  []ProcessChecklist     `json:"checklists"`
	WorkItems   []DataRow              `json:"work_items"`
	Timers      []DataRow              `json:"timers"`
	Signoffs    []DataRow              `json:"signoffs"`
	Timeline    []ProcessTimelineEntry `json:"timeline"`
}

type ProcessTransition struct {
	DataTransitionState
	Blocked string `json:"blocked,omitempty"`
}

type ProcessChecklist struct {
	ID    string                 `json:"id"`
	Steps []ProcessChecklistStep `json:"steps"`
}

type ProcessChecklistStep struct {
	ID       string             `json:"id"`
	Label    string             `json:"label"`
	Done     bool               `json:"done"`
	Evidence string             `json:"evidence,omitempty"`
	Attached *ChecklistEvidence `json:"attached,omitempty"`
}

type ProcessTimelineEntry struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Who  string `json:"who"`
	Why  string `json:"why"`
	At   string `json:"at"`
}

type WorkCompleteParams struct {
	DataIDParams
	Values map[string]any `json:"values,omitempty"`
}

type WorkReassignParams struct {
	DataIDParams
	Assignee *string `json:"assignee"`
}

// ProcessCalls is the engine's process projection and writer commands. The
// same typed method registry serves these calls to clients of codeaf app.
type ProcessCalls interface {
	DataProcessSnapshot(ProcessParams) (*DataProcessResult, error)
	DataTransitionBatch(TransitionBatchParams) (*DataWriteResult, error)
	DataChecklistSet(ChecklistSetParams) (*DataProcessResult, error)
	WorkItemsList(DataScope) (*DataItemsResult, error)
	WorkItemsClaim(DataIDParams) (*DataItemResult, error)
	WorkItemsComplete(WorkCompleteParams) (*DataItemResult, error)
	WorkItemsReassign(WorkReassignParams) (*DataItemResult, error)
}

func processCall[P, R any](name string, call func(ProcessCalls, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		d, ok := e.(ProcessCalls)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		return call(d, p)
	})
}

func init() {
	eventShapes[EventWorkItemsChanged] = reflect.TypeFor[WorkItemsChanged]()
	methods["data.process"] = processCall("data.process", func(d ProcessCalls, p ProcessParams) (*DataProcessResult, error) {
		if err := needRecord(p.Record.Type, p.Record.ID); err != nil {
			return nil, err
		}
		return d.DataProcessSnapshot(p)
	})
	methods["data.transition_batch"] = processCall("data.transition_batch", func(d ProcessCalls, p TransitionBatchParams) (*DataWriteResult, error) {
		if _, err := required("transition", p.Transition); err != nil {
			return nil, err
		}
		if len(p.Records) == 0 {
			return nil, Errorf(CodeBadParams, "records must contain at least one record")
		}
		for _, selection := range p.Records {
			if err := needRecord(selection.Record.Type, selection.Record.ID); err != nil {
				return nil, err
			}
			if selection.ExpectedRev <= 0 {
				return nil, Errorf(CodeBadParams, "expected_rev is required for record %s", selection.Record.ID)
			}
		}
		return d.DataTransitionBatch(p)
	})
	methods["data.checklist_set"] = processCall("data.checklist_set", func(d ProcessCalls, p ChecklistSetParams) (*DataProcessResult, error) {
		if err := needRecord(p.Record.Type, p.Record.ID); err != nil {
			return nil, err
		}
		if _, err := required("checklist", p.Checklist); err != nil {
			return nil, err
		}
		if _, err := required("step", p.Step); err != nil {
			return nil, err
		}
		if p.Done == nil || p.ExpectedRev <= 0 {
			return nil, Errorf(CodeBadParams, "done and expected_rev are required")
		}
		return d.DataChecklistSet(p)
	})
	methods["work_items.list"] = processCall("work_items.list", ProcessCalls.WorkItemsList)
	methods["work_items.claim"] = processCall("work_items.claim", func(d ProcessCalls, p DataIDParams) (*DataItemResult, error) {
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		return d.WorkItemsClaim(p)
	})
	methods["work_items.complete"] = processCall("work_items.complete", func(d ProcessCalls, p WorkCompleteParams) (*DataItemResult, error) {
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		return d.WorkItemsComplete(p)
	})
	methods["work_items.reassign"] = processCall("work_items.reassign", func(d ProcessCalls, p WorkReassignParams) (*DataItemResult, error) {
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		if p.Assignee == nil {
			return nil, Errorf(CodeBadParams, "assignee is required")
		}
		return d.WorkItemsReassign(p)
	})
}
