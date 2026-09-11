package session

// WHAT A SESSION WITH NO SCHEDULING IS TOLD, AND WHAT IT MUST NOT BE TOLD.
//
// `stand` is the sharpest absence on this belt (beltfacts.go's [Config.mayStand]
// states why), so the page composes its own paragraph from it: one sentence
// saying the verb exists where it does, one saying nothing can be scheduled
// where it does not. That second sentence is easy to
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

// AND THE PRESENT CASE IS EXISTENCE AND NOTHING ELSE.
//
// THE LAW MOVED, SO THIS TEST MOVED WITH IT. Eleven fragments of the old
// 2,482-byte section were pinned here — the waking kinds, the `when.in` and
// `when.at` grammar, the card's answers, the background-checks row, the
// `[something you set up fired]` frame. Every one of them is now delivered by
// whoever needs it and only then: tools_standing.go's [standDescription] and
// [standSchemaJSON] carry the mechanics beside the field each governs,
// standing_run.go's [standingNewsRule] rides under the firing's own line, and
// the chat manual's keeping-an-eye page carries the rest in the words a person
// asks them in. Pinning them here as well would be pinning the SECOND copy of a
// law and would fail the day the first copy is the only one left, which is
// today (docs/design/prompt-diet/DESIGN.md §2).
//
// So what is held here now is the thing the page alone can say, which is that
// there IS somewhere to leave a sentence, and that the verb for it is proposed
// rather than performed.
func TestTheSectionWithSchedulingIsExistenceAndNothingElse(t *testing.T) {
	present := standingSectionFor(true)

	// FORWARD: the model can recognise one of these sentences and knows the verb.
	for _, want := range []string{
		"SOMETHING TO LEAVE BEHIND",
		"`stand`",
		"PROPOSE IT",
		"remind me at 6",
	} {
		if !strings.Contains(present, want) {
			t.Errorf("the standing line no longer says %q:\n%s", want, present)
		}
	}

	// REVERSE: none of the mechanics has crept back onto a page that is re-sent
	// in front of every request of every turn. Each fragment below is owned
	// somewhere cheaper, and the comment above names where.
	for _, elsewhere := range []string{
		"WAKING OR HOLDING",
		"when.kind",
		"when.in",
		"when.at",
		"RFC3339",
		"A CARD OFFERS",
		"BACKGROUND CHECKS",
		"[something you set up fired]",
	} {
		if strings.Contains(present, elsewhere) {
			t.Errorf("%q is back on the page; it is `stand`'s description, its schema, or the firing's own line:\n%s", elsewhere, present)
		}
	}

	// AND IT IS ONE SHORT PARAGRAPH. The section was 2,482 bytes of message[0];
	// a ceiling here is what stops it growing back a sentence at a time.
	if len(present) > 600 {
		t.Errorf("the standing line is %d bytes, which is a section again and not an existence line:\n%s", len(present), present)
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
