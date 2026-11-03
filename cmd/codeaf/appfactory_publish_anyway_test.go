package main

import (
 "testing"
 "github.com/Agent-Field/codeaf/internal/factory"
)

func TestPersonPublishesAnywayThroughJobAction(t *testing.T) {
 f := startActionFactory(t)
 alice := signedIn(t, f, 1, "alice")
 it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "alice", HeadCommit: actionHead, Question: "Publishing waits", QKind: factory.QKindOutOfAttempts,
 Change: &factory.Change{Files: []factory.ChangedFile{{Path: "docs.md"}}},
 Stages: []factory.Stage{{Name:"inspect", Kind:factory.StageCheck, When:"touches auth"}, {Name:"post", Kind:factory.StagePost, Ask:"pr"}},
 Stream: &factory.Stream{Cur:1, Phases: []factory.Phase{{Name:"inspect", Kind:factory.StageCheck, State:factory.PhaseSkipped, SkipReason:"when touches auth does not match the change files"}, {Name:"post", Kind:factory.StagePost, State:factory.PhaseWaiting, Result:&factory.StageResult{Claims:[]factory.Claim{{Medium:"policy", Evidence:"tests pass before anything posts"}}}}}}})
 out := alice.act(t, "factory.job_publish_anyway", it.ID, nil)
 if out.Current == nil { t.Fatal("publish anyway did not return the job") }
}
