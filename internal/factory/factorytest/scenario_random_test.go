package factorytest

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE RANDOM RUNS. A seed picks a sequence of events over a job whose pull
// request is open: what GitHub does, what the delegate does with its next
// round, what the person presses, what happens to the runner and the
// service. After each event the job comes to rest and the same properties are
// held as in the replays. A failure prints its seed and the events so far,
// and the same seed plays the same events again.

// step is one event a random run can take. ok says whether it applies to the
// job as it stands; do makes it happen.
type step struct {
	name   string
	weight int
	ok     func(s *Scenario, it factory.Item) bool
	do     func(s *Scenario, it factory.Item)
}

func always(*Scenario, factory.Item) bool { return true }

func waitsOnYou(_ *Scenario, it factory.Item) bool { return it.Wait.Who == factory.WaitYou }

// randomYes is what the person presses when a random run has them say yes.
// Merge is left out: it ends the job, and has an event of its own.
var randomYes = []string{factory.DoApprove, factory.DoAnswer, factory.DoRetryPublish, factory.DoResume, factory.DoRerunStep, factory.DoResumePipeline, factory.DoRaiseBudget, factory.DoStart}

func offersAny(it factory.Item, actions []string) string {
	for _, action := range actions {
		if factory.Offered(it, factory.OfferByYou, action) {
			return action
		}
	}
	return ""
}

// steps is every event, with how often it is picked.
func steps(rng *rand.Rand) []step {
	read := func(change func(s *Scenario)) func(*Scenario, factory.Item) {
		return func(s *Scenario, _ factory.Item) {
			change(s)
			s.Poll()
		}
	}
	return []step{
		{"a check fails", 5, always, read(func(s *Scenario) { s.GitHub.SetChecks(1, 1, 0) })},
		{"the checks pass", 5, always, read(func(s *Scenario) { s.GitHub.SetChecks(2, 0, 0) })},
		{"a reviewer requests changes", 3, always, read(func(s *Scenario) {
			s.GitHub.Review("mei", "CHANGES_REQUESTED", "Name the constant.", s.Now())
		})},
		{"a reviewer approves", 3, always, read(func(s *Scenario) { s.GitHub.Review("mei", "APPROVED", "", s.Now()) })},
		{"a review comment arrives", 3, always, read(func(s *Scenario) {
			s.GitHub.AddComment(fmt.Sprintf("Could this be a method? (%d)", rng.IntN(1000)))
		})},
		{"the pull request conflicts with its base", 3, always, read(func(s *Scenario) { s.GitHub.Conflict(true) })},
		{"the base branch moves", 2, always, read(func(s *Scenario) { s.MoveBase() })},
		{"someone else pushes to the branch", 2, always, read(func(s *Scenario) { s.OutsidePush() })},
		{"GitHub is read", 2, always, func(s *Scenario, _ factory.Item) { s.Poll() }},
		{"the person says yes", 10, func(_ *Scenario, it factory.Item) bool {
			return it.Wait.Who == factory.WaitYou && offersAny(it, randomYes) != ""
		}, func(s *Scenario, it factory.Item) { s.MustPress(offersAny(it, randomYes)) }},
		{"the person presses what the wait names", 4, func(_ *Scenario, it factory.Item) bool {
			doors := factory.WaitMoves(it)
			return len(doors) > 0 && doors[0] != factory.DoMerge && doors[0] != factory.DoMarkReady
		}, func(s *Scenario, it factory.Item) { s.MustPress(factory.WaitMoves(it)[0]) }},
		{"the person tells Follow PR what to do", 2, func(_ *Scenario, it factory.Item) bool {
			return factory.Offered(it, factory.OfferByYou, factory.DoResumePipeline)
		}, func(s *Scenario, _ factory.Item) {
			if got := s.Post("control", map[string]any{"mode": "resume_pipeline", "words": "Keep the public names as they are."}); !got.OK() {
				s.T.Fatalf("resume_pipeline with words was refused: %s\n%s", got, s.Story())
			}
		}},
		{"the person raises the budget", 2, func(_ *Scenario, it factory.Item) bool {
			return it.Cap < 30
		}, func(s *Scenario, it factory.Item) {
			// The budget is the person's to raise at any time; the job does
			// not have to be offering it.
			if got := s.Post("raise_budget", map[string]any{"budget": raisedBudget(it)}); !got.OK() {
				s.Events[len(s.Events)-1] += " (refused: " + got.Error + ")"
			}
			s.Poll()
		}},
		{"the person merges", 1, func(_ *Scenario, it factory.Item) bool {
			return factory.Offered(it, factory.OfferByYou, factory.DoMerge)
		}, func(s *Scenario, _ factory.Item) { s.MustPress(factory.DoMerge) }},
		{"the person switches the mode", 1, always, func(s *Scenario, it factory.Item) {
			mode := factory.AutonomyAuto
			if it.Autonomy == factory.AutonomyAuto {
				mode = factory.AutonomyManual
			}
			// A job that is publishing keeps its mode; that refusal protects
			// the publish and is not a stall.
			if got := s.Post("control", map[string]any{"mode": mode}); !got.OK() {
				s.Events[len(s.Events)-1] += " (refused: " + got.Error + ")"
			}
		}},
		{"the runner restarts", 2, always, func(s *Scenario, _ factory.Item) {
			s.StopRunner()
			s.StartRunner()
		}},
		{"the app restarts", 2, always, func(s *Scenario, _ factory.Item) { s.RestartService() }},
		{"the runner is away for ten minutes", 1, always, func(s *Scenario, _ factory.Item) {
			s.StopRunner()
			s.Advance(10 * time.Minute)
			s.Tick()
			s.StartRunner()
		}},
		{"an hour passes", 1, always, func(s *Scenario, _ factory.Item) {
			s.Advance(time.Hour)
			s.Tick()
		}},
		{"a check fails and the runner dies in the middle of the round", 2, func(s *Scenario, it factory.Item) bool {
			f := it.FollowUp
			return s.RunnerOnline() && it.State == factory.StateShipped && f != nil && (f.State == "watching" || f.State == "healthy")
		}, func(s *Scenario, _ factory.Item) {
			s.KillRunnerMidRound(func() {
				s.GitHub.SetChecks(1, 1, 0)
				s.Poll()
			})
		}},
		{"the budget runs out in the job's chat", 1, func(_ *Scenario, it factory.Item) bool {
			return it.Stream != nil && it.Cap > 0 && factory.JobLifetimeSpent(it) < it.Cap
		}, func(s *Scenario, _ factory.Item) {
			// No model runs here, so the test writes what the chat cost.
			s.Change(func(it *factory.Item) { it.Stream.Spent = it.Cap - it.SpentBefore })
		}},
		// What the delegate does with its next Follow PR round.
		{"next round: the delegate asks what is in scope", 2, always, func(s *Scenario, _ factory.Item) {
			s.Delegate.Next(factory.FollowPRStep, Round{Result: NeedsYou("The failure predates this change; it needs a scope decision.")})
		}},
		{"next round: the delegate errors", 2, always, func(s *Scenario, _ factory.Item) {
			s.Delegate.Next(factory.FollowPRStep, Round{Err: errors.New("the model refused the request: invalid tool schema")})
		}},
		{"next round: the connection drops", 1, always, func(s *Scenario, _ factory.Item) {
			s.Delegate.Next(factory.FollowPRStep, Round{Err: errors.New("connection reset by peer")})
		}},
		{"next round: the delegate is not done", 2, always, func(s *Scenario, _ factory.Item) {
			s.Delegate.Next(factory.FollowPRStep, Round{Edit: true, Result: &factory.StageResult{Done: false, Output: "One failure is left.",
				Repair: &factory.CIRepairResult{Decision: "needs_repair", Reason: "One failure is left."}}})
		}},
		{"next round: the delegate asks a question", 1, always, func(s *Scenario, _ factory.Item) {
			s.Delegate.Next(factory.FollowPRStep, Round{Ask: "Rename the public constant too?"})
		}},
		{"next round: the delegate finds nothing to change", 1, always, func(s *Scenario, _ factory.Item) {
			s.Delegate.Next(factory.FollowPRStep, Round{Result: &factory.StageResult{Done: true,
				Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "The request is already satisfied."}}})
		}},
	}
}

// randomRun plays length events picked by seed over a job with an open pull
// request, in mode, holding the properties after each.
func randomRun(t *testing.T, mode string, seed uint64, length int) {
	s := Opened(t, mode)
	s.seed = fmt.Sprintf("%d (mode %s, %d events): SCENARIO_SEED=%d go test ./internal/factory/factorytest/ -run TestRandomRuns", seed, mode, length, seed)
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	all := steps(rng)
	it := s.Item()
	for n := 0; n < length; n++ {
		if it.Wait.Who == factory.WaitDone {
			return
		}
		var can []step
		total := 0
		for _, st := range all {
			if st.ok(s, it) {
				can = append(can, st)
				total += st.weight
			}
		}
		pick := rng.IntN(total)
		var chosen step
		for _, st := range can {
			if pick < st.weight {
				chosen = st
				break
			}
			pick -= st.weight
		}
		it = s.Do(chosen.name, func() { chosen.do(s, it) })
	}
}

// The seeds every run of the package plays, in each mode, and how many events
// each plays. SCENARIO_SEED plays one other seed; SCENARIO_SEEDS=n plays n
// seeds from SCENARIO_FIRST (or 1), and SCENARIO_LENGTH sets how many events
// each plays, for a longer look than the package's minute allows.
var randomSeeds = []uint64{1, 2, 3, 4, 5}

const randomLength = 7

func TestRandomRuns(t *testing.T) {
	t.Parallel()
	seeds, length := randomSeeds, randomLength
	if one, err := strconv.ParseUint(os.Getenv("SCENARIO_SEED"), 10, 64); err == nil {
		seeds = []uint64{one}
	}
	if n, err := strconv.Atoi(os.Getenv("SCENARIO_SEEDS")); err == nil && n > 0 {
		first, _ := strconv.Atoi(os.Getenv("SCENARIO_FIRST"))
		seeds = nil
		for seed := max(first, 1); seed < max(first, 1)+n; seed++ {
			seeds = append(seeds, uint64(seed))
		}
	}
	if n, err := strconv.Atoi(os.Getenv("SCENARIO_LENGTH")); err == nil && n > 0 {
		length = n
	}
	for _, mode := range []string{factory.AutonomyManual, factory.AutonomyAuto} {
		for _, seed := range seeds {
			t.Run(fmt.Sprintf("%s/seed=%d", mode, seed), func(t *testing.T) {
				t.Parallel()
				randomRun(t, mode, seed, length)
			})
		}
	}
}
