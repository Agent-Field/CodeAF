package serve

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// FS0/FS15: the activity feed and the audit export are record events, and a
// peer's machine appears in none of their bytes (JSON or CSV), operator
// included, while the owner still reads their own and everyone's jobs, kinds,
// actors and times stay.
func TestActivityAndAuditNameNoPeerMachine(t *testing.T) {
	const secret = "raj-secret-box"
	c := capacitySetup(t)
	c.attach(t, c.priya, "priya", "priya-box", 0)
	c.attach(t, c.raj, "raj", secret, 3)
	job := c.add(t, "raj")
	// A real take, then a real capacity change, both through the handlers.
	c.request(t, "POST", "/v1/jobs/"+strconv.Itoa(job.ID)+"/take", c.raj, nil, 200, nil)
	c.request(t, "PUT", "/v1/runners/"+secret, c.raj, api.RunnerSettings{Slots: 2}, 200, nil)
	// Rows written before the machine left Took, replayed as stored.
	rec := c.h.config.Record
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Took, Job: job.ID, Handle: "raj", Detail: secret}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Assigned, Job: job.ID, Handle: "raj", Detail: secret}); err != nil {
		t.Fatal(err)
	}
	// A person's own words and an assignee are not machines.
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Assigned, Job: job.ID, Handle: "raj", Detail: "@priya"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Assigned, Job: job.ID, Handle: "raj", Detail: "pairing on this"}); err != nil {
		t.Fatal(err)
	}
	// Persisted bytes name the machine; only the served view is redacted.
	if all, err := rec.Since(context.Background(), c.now.Add(-24*3600e9)); err != nil || !strings.Contains(strings.Join(eventText(all), "\n"), secret) {
		t.Fatalf("setup: the record should hold the machine as written (err %v)", err)
	}

	// No current registration proves what a legacy assigned detail means.
	const departed = "departed-raj-machine"
	if _, err := rec.Append(context.Background(), record.Event{Kind: record.Assigned, Job: job.ID, Handle: "raj", Detail: departed}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/v1/activity", "/v1/audit", "/v1/audit?format=csv", "/v1/activity?format=csv", "/v1/activity?on=runner:" + secret} {
		for _, who := range []struct{ name, token string }{{"priya", c.priya}, {"operator", "operator-secret"}} {
			body := c.request(t, "GET", path, who.token, nil, 200, nil)
			if strings.Contains(body, secret) || strings.Contains(body, departed) {
				t.Fatalf("%s %s names raj's machine: %s", who.name, path, body)
			}
			if path == "/v1/activity" && (!strings.Contains(body, `"runner_added"`) && !strings.Contains(body, `"added runner"`) || !strings.Contains(body, "@priya")) {
				t.Fatalf("%s lost the rest of the activity: %s", who.name, body)
			}
		}
		own := c.request(t, "GET", path, c.raj, nil, 200, nil)
		if !strings.Contains(own, secret) && !strings.Contains(path, "?on=") {
			t.Fatalf("raj lost his own machine in %s: %s", path, own)
		}
	}

	// Another person's job, status and activity remain for priya.
	var got struct{ Events []record.Event }
	c.request(t, "GET", "/v1/activity", c.priya, nil, 200, &got)
	var took, runner int
	for _, e := range got.Events {
		switch {
		case e.Verb == "assigned" && e.Handle == "raj" && e.Job == job.ID:
			took++
		case e.Kind == record.RunnerAdded && e.Handle == "raj":
			runner++
			if e.Object != "runner" || e.Detail != "" {
				t.Fatalf("runner_added not generic: %+v", e)
			}
		}
	}
	if took < 3 || runner != 1 {
		t.Fatalf("raj's job and runner events must stay: took=%d runner=%d %+v", took, runner, got.Events)
	}
	// A new take writes no machine at all.
	all, _ := rec.Since(context.Background(), c.now.Add(-24*3600e9))
	if first := firstOf(all, record.Took); first.Detail != "" {
		t.Fatalf("a new Took still records a machine: %+v", first)
	}
}

func eventText(events []record.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind+" "+e.Object+" "+e.Detail)
	}
	return out
}

func firstOf(events []record.Event, kind string) record.Event {
	for _, e := range events {
		if e.Kind == kind {
			return e
		}
	}
	return record.Event{}
}
