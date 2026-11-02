package pipeline_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// TURNING THE ARRIVAL READ ON OR OFF NEVER CHANGES A START: the same item
// read (a size and a rubric from the read, or from a look closer) and unread
// gets the same decision under every way a file reads sizes, because only a
// size label decides. An unlabelled issue under a file that names no size mode
// stays M either way.
func TestAReadNeverChangesAStartDecision(t *testing.T) {
	files := []string{
		"start:\n  labels: [ready]\n",
		"start:\n  labels: [ready]\n  max_size: S\n",
		"size:\n  issue: label\nstart:\n  labels: [ready]\n",
		"size:\n  issue: model\nstart:\n  labels: [ready]\n  max_size: M\n",
	}
	large := &factory.Rubric{From: factory.RubricFromScout, Scope: factory.Factor{Score: 2}, Clarity: factory.Factor{Score: 2}, Coupling: factory.Factor{Score: 2}, Proof: factory.Factor{Score: 1}, Novelty: factory.Factor{Score: 1}}
	small := &factory.Rubric{From: factory.RubricFromRead}
	items := []factory.Item{
		{Kind: factory.KindIssue, Labels: []string{"ready"}, Cap: 1},
		{Kind: factory.KindIssue, Labels: []string{"ready", "size:S"}, Cap: 1},
		{Kind: factory.KindIssue, Labels: []string{"ready", "size:L"}, Cap: 1},
	}
	for _, src := range files {
		f, err := pipeline.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, unread := range items {
			want := f.DecideJob(unread, nil, pipeline.Measured{})
			for _, read := range []factory.Triage{
				{Size: "L", SizeFrom: factory.SizeFromRead, Rubric: large},
				{Size: "S", SizeFrom: factory.SizeFromRead, Rubric: small},
			} {
				it := unread
				it.Triage = read
				if got := f.DecideJob(it, nil, pipeline.Measured{}); !reflect.DeepEqual(got, want) {
					t.Errorf("file %q, labels %v: read %s decided %+v, unread %+v", src, unread.Labels, read.Size, got, want)
				}
			}
		}
	}
}
