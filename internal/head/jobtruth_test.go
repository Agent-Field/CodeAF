package head

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The incident, reconstructed.
//
// A craft job had been running thirty-two minutes. Six parts were done and
// showing done on the rail. A dollar and three cents had been spent. Its root
// was on its second attempt after a deadline restart, and its workers had been
// posting progress the whole way — "benchmark done", "writeup nearly there".
// The person asked the head to look at the tasks and summarize what was working.
// The head answered: "The benchmark run (craft-4958) is still in its first step.
// It just started, running under a minute, $1.00 of spend so far."
//
// Four wrong things, and not one of them was a wrong sentence. The clock came
// off the CURRENT ATTEMPT's claim stamp, so a restart made a long job new. The
// grain was AgeLabel's, whose finest reading under an hour is "just now". The
// plan said one step, which was true and told the person nothing. And the raw
// id was the first column of the read, so the composition picked it up on its
// way past. Every one of them is a read, which is why every fix here is one.

// seedRestartedBenchmark is the incident's shape: a job with a plan, its parts
// finished, its root running again after a restart, with its workers talking.
func seedRestartedBenchmark(t *testing.T, graph *store.Store) store.Node {
	t.Helper()
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("bench", "", "SVM parity benchmark", "benchmark the svm implementations and write it up"),
		spec("bench-n1", "bench", "Run the sweep", "run the parameter sweep"))
	completeNodeWith(t, graph, "bench-n1", "Sweep complete: 41 configurations measured.")

	// The root is taken on, released the way a deadline releases it — which
	// clears started_at outright — and taken on again. Attempt 2, and a claim
	// stamp seconds old on work half an hour deep.
	claim, won, err := graph.Claim("bench", "worker")
	if err != nil || !won {
		t.Fatalf("claim bench: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Release(claim); err != nil {
		t.Fatal(err)
	}
	startNode(t, graph, "bench")

	node, found, err := graph.Node("bench")
	if err != nil || !found {
		t.Fatalf("node bench found=%t err=%v", found, err)
	}
	if node.Attempt != 2 {
		t.Fatalf("the seed did not produce a restart: attempt %d", node.Attempt)
	}
	if node.StartedAt.IsZero() {
		t.Fatal("the seed's root carries no attempt stamp, so the defect is not in play")
	}
	return node
}

// The run history a running job's read must carry, with the clock handed in so
// the answer is knowable. Thirty-two minutes of job, forty seconds of attempt:
// the read must spend the first and must not spend the second anywhere.
func TestARestartedJobReadsAsItsWholeLifeAndNotAsItsCurrentAttempt(t *testing.T) {
	graph := openHeadStore(t)
	node := seedRestartedBenchmark(t, graph)
	posted, err := graph.PostMessage(store.Message{SessionID: "bench", Role: store.RoleSystem,
		NodeID: "bench", Body: "benchmark done, writeup nearly there"})
	if err != nil {
		t.Fatal(err)
	}
	now := posted.Time.Add(2 * time.Minute)
	life := store.JobLife{
		NodeID: "bench", Status: store.Running,
		Admitted:       now.Add(-32 * time.Minute),
		Attempt:        2,
		AttemptStarted: now.Add(-40 * time.Second),
	}
	history := New(&beltClient{}, graph).lensRunHistory(node, life, now)

	if !strings.Contains(history, "32m since this work was taken on") {
		t.Fatalf("the job's own clock is missing:\n%s", history)
	}
	if !strings.Contains(history, "on its second attempt") {
		t.Fatalf("a restarted job did not say it had been restarted:\n%s", history)
	}
	if !strings.Contains(history, "last word 2m ago") {
		t.Fatalf("the latest line came without an age:\n%s", history)
	}
	if !strings.Contains(history, "benchmark done, writeup nearly there") {
		t.Fatalf("the job's own last word is missing:\n%s", history)
	}
	// The current attempt's clock appears NOWHERE. It is a true figure about a
	// different question, and the whole incident is what happens when it is
	// spent on this one.
	for _, forbidden := range []string{"under a minute", "just now", "40s"} {
		if strings.Contains(history, forbidden) {
			t.Fatalf("the current attempt's short clock reached the read as %q:\n%s", forbidden, history)
		}
	}
}

// The same job through the tool the head actually calls. This pins the route
// rather than the sentence: the attempt count and the whole-job clause reach
// the composition, and the plan's shape no longer stands on its own.
func TestOpeningARunningJobPairsItsPlanWithItsRunHistory(t *testing.T) {
	graph := openHeadStore(t)
	seedRestartedBenchmark(t, graph)
	if _, err := graph.PostMessage(store.Message{SessionID: "bench", Role: store.RoleSystem,
		NodeID: "bench", Body: "benchmark done, writeup nearly there"}); err != nil {
		t.Fatal(err)
	}
	run := lensRun(t, graph, "bench")

	read, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "bench"}))
	if failed {
		t.Fatalf("open failed: %s", read)
	}
	if !strings.Contains(read, "run so far:") {
		t.Fatalf("a running job was opened with no run history in it:\n%s", read)
	}
	if !strings.Contains(read, "on its second attempt") {
		t.Fatalf("the restart is invisible in the read:\n%s", read)
	}
	if !strings.Contains(read, "benchmark done, writeup nearly there") {
		t.Fatalf("the job's last word never reached the read:\n%s", read)
	}
	// The run history stands ABOVE the plan, so a reader cannot reach plan shape
	// without having read how the run is going.
	history, plan := strings.Index(read, "run so far:"), strings.Index(read, "plan for ")
	if plan >= 0 && history > plan {
		t.Fatalf("the plan is read before the run history:\n%s", read)
	}
	// A job seeded a millisecond ago is honestly under a minute old, so the
	// figure is not what this test can pin — the ROUTE is. What must not be
	// here is AgeLabel's grain, which is the read that turned half an hour into
	// "just now" and cannot say anything finer than an hour.
	if strings.Contains(read, "just now") {
		t.Fatalf("the hour-grained age label is still on a running job's read:\n%s", read)
	}
}

// The id, demoted. The title leads the line a composition starts from, and the
// id is one line down wearing the marking absorbPromptFor gives it — the same
// mechanism, not a second one, and not a list of words the model may not say.
func TestOpeningAJobLeadsWithItsTitleAndHandsTheIdOverAsAHandle(t *testing.T) {
	graph := openHeadStore(t)
	seedRestartedBenchmark(t, graph)
	run := lensRun(t, graph, "bench")

	read, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "bench"}))
	if failed {
		t.Fatalf("open failed: %s", read)
	}
	lines := strings.Split(read, "\n")
	if len(lines) < 2 {
		t.Fatalf("the read is too short to have a shape:\n%s", read)
	}
	if !strings.HasPrefix(lines[0], "SVM parity benchmark | ") {
		t.Fatalf("the read does not lead with the job's title:\n%s", lines[0])
	}
	if strings.Contains(lines[0], "bench |") || strings.HasPrefix(lines[0], "bench") {
		t.Fatalf("the id is still in the line a composition starts from:\n%s", lines[0])
	}
	// The id is still HANDED OVER — every verb on the belt takes one — and it is
	// marked as machinery where it is.
	if !strings.Contains(read, "id bench — for your reads, never say an id to them") {
		t.Fatalf("the id was demoted out of existence rather than out of the way:\n%s", read)
	}
	// And the word the column carries is the product's, not the schema's.
	if strings.Contains(lines[0], "claimed") {
		t.Fatalf("the raw status column reached the read:\n%s", lines[0])
	}
}

// The board, which is the read the incident's answer was actually composed
// from. Its clock came off boardRunningFor, which takes the longest live
// member's own StartedAt — and on a job whose parts have all finished, the only
// live member is the restarted root, whose stamp is seconds old.
func TestTheBoardClocksARestartedJobFromWhenItWasTakenOnNotFromItsRestart(t *testing.T) {
	graph := openHeadStore(t)
	seedRestartedBenchmark(t, graph)
	// Half an hour after the whole thing was admitted, and seconds after the
	// restart: the exact instant the incident was reported at.
	board := New(nil, graph).boardFor("surgery", "", nil, time.Now().Add(32*time.Minute))

	var row string
	for _, line := range strings.Split(board, "\n") {
		if strings.HasPrefix(line, "- bench |") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("the running job is not on the board:\n%s", board)
	}
	if !strings.Contains(row, "running 32m") {
		t.Fatalf("the board clocked the restart instead of the job:\n%s", row)
	}
	if strings.Contains(row, "running under a minute") {
		t.Fatalf("the restart's own clock is still what the board reports:\n%s", row)
	}
	if !strings.Contains(row, "second attempt") {
		t.Fatalf("a long clock arrived with no reason for it:\n%s", row)
	}

	// A job on its first go says nothing about attempts — "first attempt" is
	// noise on every job that never went wrong.
	plain := openHeadStore(t)
	spliceJobTree(t, plain, store.OriginUser, "",
		spec("plain", "", "An ordinary errand", "run the ordinary errand"),
		spec("plain-n1", "plain", "Its one step", "do the step"))
	startNode(t, plain, "plain-n1")
	first := New(nil, plain).boardFor("surgery", "", nil, time.Now().Add(3*time.Minute))
	if strings.Contains(first, "attempt") {
		t.Fatalf("a job on its first go announced an attempt count:\n%s", first)
	}
	if !strings.Contains(first, "running 3m") {
		t.Fatalf("an ordinary running job lost its clock:\n%s", first)
	}
}

// A settled job keeps the clock it earned and says when it landed. The two
// halves are different questions and the read answers both.
func TestASettledJobSaysHowLongItTookAndWhenItLanded(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "note", "Jot it down", "write one line")
	completeNodeWith(t, graph, "note", "Written.")
	run := lensRun(t, graph, "note")

	read, failed := run.execute(beltToolOpen, mustJSON(map[string]any{"id": "note"}))
	if failed {
		t.Fatalf("open failed: %s", read)
	}
	first := strings.SplitN(read, "\n", 2)[0]
	if !strings.HasPrefix(first, "Jot it down | done | ") {
		t.Fatalf("a settled job's headline lost its title or its state:\n%s", first)
	}
	if !strings.Contains(first, "took ") {
		t.Fatalf("a settled job did not say how long it took:\n%s", first)
	}
	if !strings.Contains(first, "finished just now") {
		t.Fatalf("a settled job did not say when it landed:\n%s", first)
	}
	// Run history belongs to running work. A finished job's whole truth is its
	// result, and a "run so far" line on it would be narrating in the wrong
	// tense.
	if strings.Contains(read, "run so far:") {
		t.Fatalf("a settled job carries a running job's line:\n%s", read)
	}
}
