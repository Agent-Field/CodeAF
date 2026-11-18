package factorytest

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestMain(m *testing.M) {
	code := m.Run()
	CleanScenarios()
	os.Exit(code)
}

// THE WHOLE ROAD: with a person who says yes and a GitHub that goes green, the
// job reaches done in a bounded number of steps, in Manual and in Auto, and
// the properties hold at every rest on the way.
func TestScenarioWholeRoad(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{factory.AutonomyAuto, factory.AutonomyManual} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := NewScenario(t, mode)
			it := s.Do("start", func() { s.MustPress(factory.DoStart) })
			for step := 0; step < 40; step++ {
				if it.Wait.Who == factory.WaitDone {
					t.Log("\n" + s.Story())
					return
				}
				switch it.Wait.Who {
				case factory.WaitYou:
					it = s.Do("the person says yes", func() { s.Yes() })
				case factory.WaitGitHub:
					it = s.Do("GitHub goes green and is read", func() {
						s.GitHub.SetChecks(2, 0, 0)
						s.GitHub.Review("mei", "APPROVED", "", s.Now())
						s.Poll()
					})
				default:
					s.Advance(time.Second)
					it = s.Do("time passes", nil)
				}
			}
			t.Fatalf("the job did not reach done in 40 steps\n%s", s.Story())
		})
	}
}

// A STEP'S QUESTION GOES TO THE MAIN AGENT FIRST. Its answer goes back to the
// step, and a question it sends on is the person's to answer.
//
// KNOWN GAP, held here as it is: while the main agent holds the question the
// service does not know. The runner keeps that on its own copy of the job and
// reports a question only once it is the person's (run/diff.go), so the
// stored wait reads "working" with the runner's hold live, never
// "main_agent" with its five-minute deadline. [factory.WaitMainAgent] is
// reached only where the runner writes the service's own store. A main agent
// that never answers is therefore seen as a step that never ends.
func TestScenarioAQuestionTheMainAgentHolds(t *testing.T) {
	t.Parallel()
	for _, passes := range []bool{false, true} {
		name := "answers"
		if passes {
			name = "sends it on"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := Opened(t, factory.AutonomyAuto)
			asked, release := make(chan struct{}), make(chan struct{})
			s.MainAgent(func(q factory.Asked) (string, string) {
				close(asked)
				<-release
				if passes {
					return "", "it is a taste call"
				}
				return "Yes, rename it.", ""
			})
			s.Delegate.Next(factory.FollowPRStep, Round{Ask: "Rename the public constant too?"})
			s.Events = append(s.Events, "a check fails and the Follow PR round asks a question")
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
			select {
			case <-asked:
			case <-time.After(20 * time.Second):
				t.Fatalf("the main agent was never asked\n%s", s.Story())
			}
			it := s.Item()
			if who := it.Wait.Who; who != factory.WaitWorking && who != factory.WaitMainAgent {
				t.Fatalf("while the main agent holds the question the job waits on %s\n%s", waitKey(it), s.Story())
			}
			if it.Wait.Who == factory.WaitMainAgent && it.Wait.Deadline.IsZero() {
				t.Fatalf("the main agent's wait has no deadline\n%s", s.Story())
			}
			if !s.held() {
				t.Fatalf("no runner holds the job while its question is with the main agent\n%s", s.Story())
			}
			s.Check()
			close(release)
			it = s.Do("the main agent takes its turn", nil)
			if passes {
				if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer {
					t.Fatalf("the question sent on is not the person's: %s\n%s", waitKey(it), s.Story())
				}
				it = s.Do("the person answers", func() { s.MustPress(factory.DoAnswer) })
			}
			if it.Wait.Who == factory.WaitMainAgent || it.Asking != nil {
				t.Fatalf("the answered question is still on the job: %s\n%s", waitKey(it), s.Story())
			}
		})
	}
}
