package record_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// P11: policy replacements are settings activity, with no invented job number.
func TestPolicySettingActivity(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := open(t, st, func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) })
	e, err := r.Append(context.Background(), record.Event{Kind: record.PolicyChanged, Verb: "changed policy", Object: "group:g", Handle: "owner", Detail: "policy version 1; stopped=false"})
	if err != nil || e.Job != 0 || e.Actor.Kind != "person" || e.Object != "group:g" {
		t.Fatalf("activity = %+v, %v", e, err)
	}
	events, err := r.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.PolicyChanged {
		t.Fatalf("read = %+v, %v", events, err)
	}
}
