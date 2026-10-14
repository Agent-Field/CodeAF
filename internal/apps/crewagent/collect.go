package crewagent

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type turnCollector struct {
	service   *Service
	req       svc.CrewAsk
	scope     svc.Scope
	def       *spec.Bundle
	sent      bridge.AgentSendResult
	before    float64
	baseline  []bridge.Record
	changed   map[string]int
	approvals map[string]bool
}

func (t *turnCollector) collect(ctx context.Context, events <-chan svc.Event, answer svc.CrewAnswer) (svc.CrewAnswer, error) {
	for {
		select {
		case <-ctx.Done():
			return answer, ctx.Err()
		case event, open := <-events:
			if !open {
				return answer, bridge.Errorf(bridge.CodeConflict, "the app agent stream ended before its answer")
			}
			if err, ok := event.Data.(*bridge.Error); ok {
				return answer, err
			}
			if strings.HasPrefix(event.Topic, "app/"+t.req.App+"/records/") {
				var diff bridge.RecordDiff
				if err := decodeEvent(event.Data, &diff); err != nil {
					return answer, err
				}
				t.recordChange(ctx, &answer, diff)
				continue
			}
			if event.Topic == bridge.TopicApprovals(t.req.App) {
				var approval bridge.Approval
				if err := decodeEvent(event.Data, &approval); err != nil {
					return answer, err
				}
				if approval.State == bridge.ApprovalOpen && approval.Who == t.req.Asker.Member && approval.Payload["crew"] == t.req.Crew {
					if _, err := t.agentEvent(ctx, &answer, bridge.AgentEvent{Kind: bridge.AgentApproval, Approval: &approval}); err != nil {
						return answer, err
					}
				}
				continue
			}
			if event.Topic != bridge.TopicAgent(t.req.App, t.sent.Thread) {
				continue
			}
			var e bridge.AgentEvent
			if err := decodeEvent(event.Data, &e); err != nil {
				return answer, err
			}
			if e.Turn != t.sent.Turn {
				continue
			}
			done, err := t.agentEvent(ctx, &answer, e)
			if done || err != nil {
				return answer, err
			}
		}
	}
}

func decodeEvent(data any, target any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (t *turnCollector) agentEvent(ctx context.Context, answer *svc.CrewAnswer, e bridge.AgentEvent) (bool, error) {
	switch e.Kind {
	case bridge.AgentDelta:
		answer.Text += e.Text
	case bridge.AgentShapeThis:
		// Shape receives the person's request, never the linked content appended for Use.
		answer.ShapeThis = t.req.Text
		answer.Text += e.Text
	case bridge.AgentApproval:
		if e.Approval == nil || t.approvals[e.Approval.ID] {
			return false, nil
		}
		approval, err := t.saveApproval(ctx, *e.Approval)
		if err != nil {
			return false, err
		}
		t.approvals[approval.ID] = true
		answer.Approvals = append(answer.Approvals, approval)
	case bridge.AgentDone:
		if e.Cost != nil {
			cost, err := t.service.charge(t.req.App, t.sent.Thread, e.Cost.Spent, t.before)
			if err != nil {
				return false, err
			}
			answer.Cost = cost
		}
		return true, nil
	case bridge.AgentError:
		return false, bridge.Errorf(bridge.CodeInternal, e.Text)
	}
	return false, nil
}

func (t *turnCollector) saveApproval(ctx context.Context, approval bridge.Approval) (bridge.Approval, error) {
	if approval.App != "" && approval.App != t.req.App {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeDenied, "the app agent cannot open another app's approval")
	}
	if approval.Who == t.req.Asker.Member && approval.Payload["crew"] == t.req.Crew && approval.Payload["crew_name"] == t.req.CrewName {
		return approval, nil
	}
	if t.service.all.Approvals == nil {
		return bridge.Approval{}, bridge.NotBuilt("approvals")
	}
	payload := map[string]any{}
	for k, v := range approval.Payload {
		payload[k] = v
	}
	payload["crew"], payload["crew_name"] = t.req.Crew, t.req.CrewName
	approval.App, approval.Who, approval.Payload = t.req.App, t.req.Asker.Member, payload
	return t.service.all.Approvals.Open(ctx, t.scope, approval)
}

func (t *turnCollector) recordChange(ctx context.Context, answer *svc.CrewAnswer, diff bridge.RecordDiff) {
	if diff.By != t.req.Asker.Member || diff.ID == "" || diff.Type == "" {
		return
	}
	raw, _ := json.Marshal(bridge.GetRecordParams{Type: diff.Type, ID: diff.ID})
	if t.service.all.Caps.Allow(ctx, t.scope, bridge.MethodRecordsGet, raw) != nil {
		return
	}
	row := bridge.Record{ID: diff.ID, Type: diff.Type}
	if diff.Record != nil {
		row = visibleRecord(t.def, *diff.Record)
	}
	what := "record changed"
	if diff.Op == "delete" {
		what = "record deleted"
	}
	if diff.Op == "upsert" && row.Version == 1 {
		what = "record created"
	}
	for _, previous := range t.baseline {
		if previous.Type == diff.Type && previous.ID == diff.ID {
			if diff.Op == "delete" {
				row = previous
			} else {
				what = changedFields(t.def, previous, row)
			}
			break
		}
	}
	change := svc.ChangedRecord{Type: diff.Type, ID: diff.ID, Label: recordLabel(t.def, row), What: what, Version: t.def.App.Version}
	key := diff.Type + "/" + diff.ID
	if index, ok := t.changed[key]; ok {
		answer.Changed[index] = change
	} else {
		t.changed[key] = len(answer.Changed)
		answer.Changed = append(answer.Changed, change)
	}
}

func changedFields(def *spec.Bundle, before, after bridge.Record) string {
	var fields []string
	for name, value := range after.Fields {
		if reflect.DeepEqual(before.Fields[name], value) {
			continue
		}
		label := strings.ReplaceAll(name, "_", " ")
		if rt := def.App.Records[after.Type]; rt != nil && rt.Fields[name] != nil && rt.Fields[name].Label != "" {
			label = strings.ToLower(rt.Fields[name].Label)
		}
		fields = append(fields, label)
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		return "record changed"
	}
	return strings.Join(fields, ", ") + " changed"
}
