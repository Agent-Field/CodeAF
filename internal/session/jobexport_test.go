package session

import "testing"

func TestJobNoticesListsLiveAndSettledJobs(t *testing.T) {
	agent, _ := jobsAgent(t)
	if got := agent.JobNotices(); got != nil {
		t.Fatalf("an agent with no jobs answered %+v, want nothing", got)
	}

	sleeper := startJob(t, agent, "sleep 30")
	quick := startJob(t, agent, "exit 7")
	waitExited(t, agent, quick)

	got := agent.JobNotices()
	if len(got) != 2 {
		t.Fatalf("JobNotices = %+v, want the live and the settled job", got)
	}
	if got[0].ID != quick || got[1].ID != sleeper {
		t.Fatalf("order = %d, %d; want newest first (%d, %d)", got[0].ID, got[1].ID, quick, sleeper)
	}
	if got[1].State == got[0].State {
		t.Fatalf("running and exited jobs share state %v", got[0].State)
	}
	if got[1].Command != "sleep 30" {
		t.Fatalf("Command = %q", got[1].Command)
	}
	if want := noticeOf(agent.jobs.find(quick).info()); got[0] != want {
		t.Fatalf("row differs from noticeOf:\n got %+v\nwant %+v", got[0], want)
	}
}
