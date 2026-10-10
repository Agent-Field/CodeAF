package store

import (
	"path/filepath"
	"testing"
	"time"
)

// The practice loop that wrote practice rounds is gone, and a journal it wrote
// is not. A rebuild has to land those rounds' questions exactly where they
// stood — resolved stays resolved, with its reason — or a store an older build
// left behind would reopen its own curriculum the first time anybody ran
// `codeaf rebuild` on it.
func TestPracticeRoundsAnOlderBuildJournaledStillRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "practice.db"))
	question, err := graph.RecordQuestion(RootID, "repo:/work/parser",
		"I didn't know which parser recovery preserves malformed records")
	if err != nil {
		t.Fatal(err)
	}
	if question.Kind != FactQuestion || question.Status != QuestionOpen {
		t.Fatalf("question = %+v", question)
	}
	const jobID = "practice-round-1"
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: jobID, Brief: "run go test against parser recovery", Stage: 1, Group: PracticeGroup,
	}}}, Provenance{Origin: OriginSelf, Intent: "practice measured gaps"}); err != nil {
		t.Fatal(err)
	}

	// The round, written into the log the way the practice loop wrote it.
	tx, err := graph.beginWrite()
	if err != nil {
		t.Fatal(err)
	}
	started := questionPracticeStarted{QuestionSeq: question.Seq, JobID: jobID,
		BaselineSurprise: 1, ExpectedTokens: 100}
	seq, _, err := appendEvent(tx, jobID, EventQuestionPracticeStarted, started)
	if err == nil {
		err = applyQuestionPracticeStarted(tx, started, seq)
	}
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	completed := questionPracticeCompleted{QuestionSeq: question.Seq, JobID: jobID,
		ResultSurprise: 0.5, Reduced: true, Status: QuestionResolved,
		Reason: "surprise fell from 1.000 to 0.500 in practice job " + jobID}
	seq, _, err = appendEvent(tx, jobID, EventQuestionPracticeCompleted, completed)
	if err == nil {
		err = applyQuestionPracticeCompleted(tx, completed, seq)
	}
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	resolved, _, err := graph.FactBySeq(question.Seq)
	if err != nil || resolved.Status != QuestionResolved || resolved.StatusNote != completed.Reason {
		t.Fatalf("the round did not resolve the question: %+v, %v", resolved, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, _, err := graph.FactBySeq(question.Seq)
	if err != nil || rebuilt.Status != resolved.Status || rebuilt.StatusNote != resolved.StatusNote {
		t.Fatalf("rebuilt question = %+v, want %+v, err=%v", rebuilt, resolved, err)
	}
	var rounds int
	if err := graph.db.QueryRow(`SELECT COUNT(*) FROM question_practices
		WHERE question_seq = ? AND completion_seq IS NOT NULL`, question.Seq).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if rounds != 1 {
		t.Fatalf("rebuilt %d finished rounds, want the one the journal holds", rounds)
	}
}

func TestUserIdleUsesJournaledUserWork(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "idle.db"))
	if idle, err := graph.UserIdle(time.Now(), 20*time.Minute); err != nil || !idle {
		t.Fatalf("an empty graph is idle: idle=%t err=%v", idle, err)
	}
	rootID := "parser-job"
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: rootID, Brief: "build parser and run go test", Stage: 1}}},
		Provenance{Origin: OriginUser, Intent: "build the parser and run go test"}); err != nil {
		t.Fatal(err)
	}
	if idle, err := graph.UserIdle(time.Now().Add(time.Hour), 0); err != nil || idle {
		t.Fatalf("in-flight user graph idle=%t err=%v", idle, err)
	}
	claim := mustClaim(t, graph, rootID, "worker")
	if err := graph.Complete(claim, "go test passed"); err != nil {
		t.Fatal(err)
	}
	if idle, err := graph.UserIdle(time.Now(), 20*time.Minute); err != nil || idle {
		t.Fatalf("recent user splice idle=%t err=%v", idle, err)
	}
	if idle, err := graph.UserIdle(time.Now().Add(21*time.Minute), 20*time.Minute); err != nil || !idle {
		t.Fatalf("quiet user graph idle=%t err=%v", idle, err)
	}
	if _, err := graph.UserIdle(time.Now(), -time.Minute); err == nil {
		t.Fatal("a negative quiet period was accepted")
	}
}
