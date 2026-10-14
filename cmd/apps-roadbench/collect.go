package main

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// trace is everything the owner's desk would have seen for one app.
type trace struct {
	builds     []bridge.BuildEvent
	drafts     []bridge.DraftEvent
	firstDraft time.Time
	firstPlan  time.Time
}

// collector files the events of one socket under their app. The socket
// reader calls add; the app's driver calls take.
type collector struct {
	mu     sync.Mutex
	traces map[string]*trace
}

func newCollector() *collector { return &collector{traces: map[string]*trace{}} }

// add files one event of topic app/<id>/build or app/<id>/draft.
func (c *collector) add(topic string, raw json.RawMessage, at time.Time) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.traces[parts[1]]
	if t == nil {
		t = &trace{}
		c.traces[parts[1]] = t
	}
	switch parts[2] {
	case "build":
		var b bridge.BuildEvent
		if json.Unmarshal(raw, &b) == nil {
			t.builds = append(t.builds, b)
			if b.Plan != nil && t.firstPlan.IsZero() {
				t.firstPlan = at
			}
		}
	case "draft":
		var d bridge.DraftEvent
		if json.Unmarshal(raw, &d) == nil {
			t.drafts = append(t.drafts, d)
			if t.firstDraft.IsZero() {
				t.firstDraft = at
			}
		}
	}
}

// take returns a copy of one app's trace.
func (c *collector) take(app string) trace {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.traces[app]; t != nil {
		return trace{append([]bridge.BuildEvent(nil), t.builds...), append([]bridge.DraftEvent(nil), t.drafts...), t.firstDraft, t.firstPlan}
	}
	return trace{}
}

// lines are the texts shown to the owner: build logs and draft lines.
func (t trace) lines() []string {
	var out []string
	for _, b := range t.builds {
		if b.Log != "" {
			out = append(out, b.Log)
		}
	}
	for _, d := range t.drafts {
		if d.Line != "" {
			out = append(out, d.Line)
		}
	}
	return out
}

// firstLine is when the owner first saw the build move: a draft line on the
// loop road, the plan lines on the calls road, whichever came first.
func (t trace) firstLine() time.Time {
	switch {
	case t.firstDraft.IsZero():
		return t.firstPlan
	case t.firstPlan.IsZero() || t.firstDraft.Before(t.firstPlan):
		return t.firstDraft
	}
	return t.firstPlan
}

// cost is the last spend any event carried.
func (t trace) cost() float64 {
	spent := 0.0
	for _, b := range t.builds {
		spent = max(spent, b.Cost.Spent)
	}
	return spent
}

// lastCheck is the final Check report the owner's desk would have shown.
func (t trace) lastCheck() *bridge.CheckReport {
	var last *bridge.CheckReport
	for _, b := range t.builds {
		if b.Check != nil {
			last = b.Check
		}
	}
	return last
}

// lastFailure is the last failed build line, for the error column.
func (t trace) lastFailure() string {
	out := ""
	for _, b := range t.builds {
		if b.Status == bridge.BuildFailed {
			out = b.Log
		}
	}
	return out
}

// stageSet says how far the build got: plan, write, check, preview.
type stageSet struct{ Plan, Write, Check, Preview, Keep, Use bool }

// stages reads write, check and preview off the events; plan is set by the
// driver, which also polls for plan.json because the first events can pass
// before the desk has subscribed.
func (t trace) stages() stageSet {
	var s stageSet
	for _, b := range t.builds {
		switch {
		case b.Step == bridge.StepPlan && b.Status == bridge.BuildDone:
			s.Plan = true
		case b.Step == bridge.StepWrite && b.Status == bridge.BuildDone:
			s.Write = true
		case b.Check != nil && b.Check.Pass:
			s.Check = true
		case b.Step == bridge.StepPreview && b.Status == bridge.BuildDone:
			s.Preview = true
		}
	}
	return s
}
