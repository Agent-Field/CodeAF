package live

import (
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Changes compares the facts a notification names, not a job's log or cost.
// Each store write records its transition so a fast publish cannot skip pushing.
func Changes(before, after factory.Item, now time.Time) []Event {
	var out []Event
	p, old := after.PublishStatus, before.PublishStatus
	if p != nil && !reflect.DeepEqual(p, old) {
		out = append(out, New("publishing", after.ID, Payload{Job: after.ID, State: p.State}))
		switch p.State {
		case "opened":
			out = append(out, New("published", after.ID, PublishedPayload{Job: after.ID, PR: p.PR}))
		case "failed":
			reason := p.Code
			if reason == "" {
				reason = p.Detail
			}
			out = append(out, New("publish_failed", after.ID, FailedPayload{Job: after.ID, Reason: reason}))
		}
	}
	kind, prev := factory.NeedsKind(after), factory.NeedsKind(before)
	if kind != prev || (kind != "" && (after.Question != before.Question || after.QKind != before.QKind || after.PublishError != before.PublishError || !reflect.DeepEqual(after.Asking, before.Asking))) {
		since := factory.NeedsSince(after)
		if since.IsZero() {
			since = now.UTC()
		}
		out = append(out, New("needs_you", after.ID, Payload{Job: after.ID, Kind: &kind, Since: &since}))
	}
	if (after.Question != "" || after.Asking != nil) && (after.Question != before.Question || after.QKind != before.QKind || !reflect.DeepEqual(after.Asking, before.Asking)) {
		qkind, _, _ := factory.QuestionForm(after)
		if qkind == "" {
			qkind = kind
		}
		out = append(out, New("question", after.ID, Payload{Job: after.ID, Kind: &qkind}))
	}
	if after.PublishError != "" && after.PublishError != before.PublishError && (p == nil || p.State != "failed") {
		out = append(out, New("publish_failed", after.ID, FailedPayload{Job: after.ID, Reason: after.PublishError}))
	}
	return out
}
