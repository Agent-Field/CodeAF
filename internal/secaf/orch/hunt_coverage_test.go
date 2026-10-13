package orch

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/secaf/phases"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

func TestIncompleteHuntCheckpointKeepsItsFailures(t *testing.T) {
	o, _ := newTestOrchestrator(t)
	hunt := schemas.NewHuntResult()
	hunt.Coverage = &schemas.HuntCoverage{Attempted: 2, Failures: []schemas.HuntFailure{{Strategy: "auth", Reason: "no reply"}}}
	if err := o.WriteCheckpoint(PhaseHunt, hunt); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCheckpoint(o, PhaseHunt, phases.BindHuntResult)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Coverage, hunt.Coverage) {
		t.Fatalf("checkpoint dropped failures: %+v", back)
	}
}

func TestReconFindingsDoNotEraseHuntFailures(t *testing.T) {
	hunt := schemas.NewHuntResult()
	hunt.Coverage = &schemas.HuntCoverage{Attempted: 2, Failures: []schemas.HuntFailure{{Strategy: "auth", Reason: "no reply"}}}
	merged := MergeReconFindingsIntoHunt(hunt, []schemas.RawFinding{schemas.NewRawFinding()})
	if !reflect.DeepEqual(merged.Coverage, hunt.Coverage) || len(merged.Findings) != 1 {
		t.Fatalf("merge lost findings or failures: %+v", merged)
	}
}
