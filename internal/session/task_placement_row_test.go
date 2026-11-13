package session

import "testing"

// R9.1 requires the finished A-side row to carry the placement words after
// the worker's phase lane has been replaced by its final landing notice.
func TestTheFinishedTaskRowKeepsTheExecutorWords(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, tc := range []struct {
		name   string
		failed bool
		want   string
	}{{"beta", false, "ran on beta"}, {"beta", true, ""}, {"", false, ""}} {
		run := &beltRun{row: 7, title: "Create hello.txt", remoteEngine: &scriptedTaskEngine{}, remote: &TaskRemoteRecord{Executor: TaskExecutor{Name: tc.name}, Finished: true, StartFailed: tc.failed}}
		notice := a.beltRunNotice(run, RunSummary{Outcome: beltRunOutcomeDone}, RunLanding{})
		if notice.Doing != tc.want {
			t.Errorf("executor %q, failed %v: finished row lost its placement words: got %q, want %q", tc.name, tc.failed, notice.Doing, tc.want)
		}
	}
}
