package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func TestWorkstreamDoneCommittedAfterDoorResolutionRefusesTheWrite(t *testing.T) {
	for _, operation := range []string{"record", "create", "save"} {
		t.Run(operation, func(t *testing.T) {
			b, rec, _, door := workstreamAgentFixture(t)
			ctx := context.Background()
			s, w, err := door.(*workstreamAgentDoor).workstreamOwner(ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			p, _, err := s.CreatePage(ctx, workstream.PageCreate{WorkstreamID: w.ID, Title: "Existing", Owner: "agent"})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.MarkDone(ctx, w.ID, "Finished after resolution"); err != nil {
				t.Fatal(err)
			}
			before, err := s.Get(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			events := len(workstreamEvents(rec))
			change, err := workstreamWriteAfterResolution(ctx, s, w.ID, p.ID, operation)
			b.workstreamChanged(change)
			if !errors.Is(workstreamAgentError(err), session.ErrWorkstreamDone) || len(workstreamEvents(rec)) != events {
				t.Fatalf("done after resolution: %+v %v", change, err)
			}
			after, err := s.Get(ctx, w.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("refused write changed the workstream: %+v %v", after, err)
			}
		})
	}
}

func workstreamWriteAfterResolution(ctx context.Context, s *workstream.Store, id, pageID, operation string) (workstream.Change, error) {
	if operation == "record" {
		_, c, err := s.ProposeRecord(ctx, workstream.RecordPropose{WorkstreamID: id, Kind: "decision", Text: "Late", By: "agent"})
		return c, err
	}
	ask := session.WorkstreamPageWrite{Title: "Late"}
	if operation == "save" {
		ask.PageID = pageID
	}
	_, c, err := workstreamAgentWritePage(ctx, s, id, ask, json.RawMessage(`{"type":"doc","content":[]}`), workstream.Attribution{By: "agent"})
	return c, err
}
