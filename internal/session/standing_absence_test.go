package session

// WHAT A SESSION WITH NO SCHEDULING IS TOLD, AND WHAT IT MUST NOT BE TOLD.
//
// `stand` is the sharpest absence on this belt (beltfacts.go's [Config.mayStand]
// states why), so the page composes a whole section from it: the mechanics where
// the verb is present, one sentence where it is not. That sentence is easy to
// write too wide — the first draft said nothing this agent does keeps working
// once the window closes — and too wide is not a smaller claim, it is a FALSE
// one: work handed to a task outlives the turn that started it, is checkpointed
// and comes home on its own (task_run.go), and the session itself is restored
// rather than lost. A worker told otherwise would decline to hand work over, or
// tell the person their task dies with the window.
//
// So this file holds the absent case to exactly one denial — scheduling — and
// holds both cases to a clock rule that does not contradict itself.

import (
	"strings"
	"testing"
)

// standingSectionFor renders the standing section alone, for a config that has
// the store and one that does not.
func standingSectionFor(mayStand bool) string {
	config := Config{}
	if mayStand {
		config.standingItems = &fakeStanding{}
	}
	return renderBeltFacts(config, standingFacts, "\n\n")
}

func TestTheSectionWithoutSchedulingDeniesOnlyScheduling(t *testing.T) {
	absent := standingSectionFor(false)

	// FORWARD: it says the one true thing.
	for _, want := range []string{"NOTHING CAN BE SCHEDULED FROM HERE", "reminder", "check back later"} {
		if !strings.Contains(absent, want) {
			t.Errorf("the no-scheduling sentence does not say %q:\n%s", want, absent)
		}
	}

	// REVERSE: it does not deny anything else that outlives the window. Each of
	// these is a claim this build would be making falsely — a task is
	// checkpointed and reported when it lands, and a session is restored.
	for _, overreach := range []string{
		"keeps working once this window closes",
		"nothing you do here keeps working",
		"is lost when this window closes",
		"does not survive",
	} {
		if strings.Contains(strings.ToLower(absent), strings.ToLower(overreach)) {
			t.Errorf("the no-scheduling sentence says %q, which denies task and session continuity this build has:\n%s", overreach, absent)
		}
	}
	// AND IT SAYS SO POSITIVELY, so a worker reading it does not infer the wider
	// claim from the narrower one.
	if !strings.Contains(absent, "Work already handed off is a different thing") {
		t.Errorf("the no-scheduling sentence does not exempt handed-off work, so a worker may read it as \"nothing survives\":\n%s", absent)
	}
}

// AND THE PRESENT CASE IS STILL THE WHOLE MECHANIC. The reduction that made the
// section conditional must not have made it conditional AND thinner.
func TestTheSectionWithSchedulingStillTeachesTheMechanics(t *testing.T) {
	present := standingSectionFor(true)
	for _, want := range []string{
		"WAKING OR HOLDING",
		"`when.kind: hold`",
		"UNSURE MEANS INSTRUCTION PLUS AN OFFER",
		"SAYING WHEN",
		"when.in",
		"when.at",
		"A CARD OFFERS",
		"NOTHING STANDS UNTIL THEY SAY",
		"WHERE A FIRING ARRIVES",
		"BACKGROUND CHECKS ARE ON AND NOBODY IS ASKED",
		"[something you set up fired]",
	} {
		if !strings.Contains(present, want) {
			t.Errorf("the standing section no longer says %q", want)
		}
	}
}

// THE CLOCK BULLET AGREES WITH ITSELF IN BOTH CASES. The footer gives four facts
// and the page forbids shelling out for them; with no `stand` the shell is the
// only clock there is, so the exception must be NAMED rather than left as a rule
// that says never and a sentence that says go and look.
func TestTheClockBulletDoesNotContradictItself(t *testing.T) {
	for _, shape := range []struct {
		name     string
		mayStand bool
	}{{"with scheduling", true}, {"without scheduling", false}} {
		t.Run(shape.name, func(t *testing.T) {
			config := Config{}
			if shape.mayStand {
				config.standingItems = &fakeStanding{}
			}
			bullet := ""
			for _, line := range strings.Split(renderBeltFacts(config, beltFacts, "\n"), "\n") {
				if strings.Contains(line, "YOU KNOW WHAT TIME IT IS") {
					bullet = line
				}
			}
			if bullet == "" {
				t.Fatal("the clock bullet is not in the rendered session facts")
			}
			// The four facts the footer already gives are never shelled out for.
			if !strings.Contains(bullet, "never shell out") && !strings.Contains(bullet, "NEVER run `date`") {
				t.Errorf("the clock bullet does not keep the rule against shelling out for the date:\n%s", bullet)
			}
			// And where it sends the model for a minute-exact moment must be
			// something this shape HAS: `stand` where there is scheduling, and a
			// named exception where there is not.
			if shape.mayStand {
				if !strings.Contains(bullet, "`stand`'s `when.in`") {
					t.Errorf("the clock bullet does not point at `stand` where it exists:\n%s", bullet)
				}
				return
			}
			if strings.Contains(bullet, "stand") {
				t.Errorf("the clock bullet names `stand` on a belt without it:\n%s", bullet)
			}
			if !strings.Contains(bullet, "one case for a single `date` call") {
				t.Errorf("the clock bullet forbids the shell and names no exception, so a minute-exact moment has nowhere to come from:\n%s", bullet)
			}
		})
	}
}

// AND THE BELT AND THE PAGE READ ONE AVAILABILITY. [Config.mayStand] and
// [Agent.standingTools] must not be two readings of the same two fields, which
// is what they were: a door handing the store over some other way could put the
// tool on the belt and "nothing can be scheduled from here" on the page.
func TestTheStandingPredicateIsTheOneTheBeltBuildsFrom(t *testing.T) {
	for _, shape := range []struct {
		name  string
		build func(*Config)
	}{
		{"nothing behind it", func(*Config) {}},
		{"a store handed over directly", func(config *Config) { config.standingItems = &fakeStanding{} }},
		{"the door's own seam", func(config *Config) { config.Standing = &Standing{Store: nil} }},
	} {
		t.Run(shape.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.System = ""
				shape.build(config)
			})
			onBelt := agent.hasTool("stand")
			if onBelt != agent.config.mayStand() {
				t.Fatalf("the belt says stand=%v and the page is composed from %v", onBelt, agent.config.mayStand())
			}
			page := systemTextOf(agent)
			if onBelt && strings.Contains(page, "NOTHING CAN BE SCHEDULED FROM HERE") {
				t.Error("the page says nothing can be scheduled and the belt carries `stand`")
			}
			if !onBelt && !strings.Contains(page, "NOTHING CAN BE SCHEDULED FROM HERE") {
				t.Error("the belt has no `stand` and the page does not say so")
			}
		})
	}
}
