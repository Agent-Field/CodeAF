package pipeline_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func labelled() factory.Item {
	return factory.Item{Kind: factory.KindIssue, Labels: []string{"ready", "size:S"}, Cap: 1}
}

func TestAWrittenCostCapAsksWhenTheCostIsUnknown(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [ready]\n  max_cost: 3\n")
	if err != nil {
		t.Fatal(err)
	}
	d := f.DecideJob(labelled(), nil, pipeline.Measured{})
	if !d.Ask || !strings.Contains(strings.Join(d.Reasons, "; "), "no cost history yet") {
		t.Fatalf("written cap, unknown cost = %+v, want ask", d)
	}
}

func TestTheDefaultCostCapStartsWhenTheCostIsUnknown(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	if d := f.DecideJob(labelled(), nil, pipeline.Measured{}); !d.Start {
		t.Fatalf("default cap, unknown cost = %+v, want start", d)
	}
}

func TestTheDefaultCostCapAsksWhenMeasuredJobsCostMore(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	over := pipeline.Measured{USD: pipeline.DefaultMaxCost + 1, Known: true}
	d := f.DecideJob(labelled(), nil, over)
	if !d.Ask || !strings.Contains(strings.Join(d.Reasons, "; "), "jobs like it cost up to") {
		t.Fatalf("default cap, measured p80 over = %+v, want ask", d)
	}
	under := pipeline.Measured{USD: 0.5, Known: true}
	if d := f.DecideJob(labelled(), nil, under); !d.Start {
		t.Fatalf("default cap, measured p80 under = %+v, want start", d)
	}
}
