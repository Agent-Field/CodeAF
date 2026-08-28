package session

// THE MEDIA VERBS TRAVEL, and this file is what says so on every surface that
// carries a belt.
//
// The conversation had them and nothing else reliably did. A task node was
// handed the client and the resolver and then had its media verbs taken away at
// the one moment it was ordered to produce; a saved harness was told in writing
// that image generation was "something a conversation reaches for" and its lint
// refused the whitelist. Each of those was invisible in exactly the same way: the
// verb was simply absent, and an absent verb is indistinguishable from a machine
// with no model for it.
//
// So each surface is pinned twice — carries them when a model exists, lacks them
// when it does not — because a test that only asserted presence would pass just
// as happily on a belt that had stopped obeying the absence law.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// theFourGenerationVerbs is what "the media family" means everywhere below.
// view_image is deliberately not in it: it is the LOOKING verb, it is gated on a
// seer rather than on a generation model, and a surface can honestly have one
// without the other.
var theFourGenerationVerbs = []string{"generate_image", "speak", "generate_music", "generate_video"}

// allMediaModels is a resolver that answers for every media modality — the shape
// of a machine with the media slots filled in. Vision is in it because
// view_image is gated on the LOOKING slot and not on a generation model, so a
// belt asked about it needs the resolver to answer that word too.
func allMediaModels() func(string) string {
	return mediaModels(map[string]string{
		modalityImage:  "paint/model",
		modalitySpeech: "talk/model",
		modalityMusic:  "compose/model",
		modalityVideo:  "film/model",
		modalityVision: "see/model",
	})
}

func toolNameSet(tools []bare.Tool) map[string]bool {
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

// ── (1) the task node ───────────────────────────────────────────────────────

// A TASK NODE IS THE SAME WORKER SOMEWHERE QUIETER, media included. task_run.go
// passes Media and MediaModel down to the child config; what this holds is that
// doing so actually buys the verbs, so a wiring mistake there cannot be silent.
func TestATaskNodeCarriesTheMediaVerbsAndLacksThemWithoutModels(t *testing.T) {
	wired, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
	})
	for _, verb := range theFourGenerationVerbs {
		if !hasTool(wired, verb) {
			t.Errorf("a task node with a media client and a resolver is missing %s", verb)
		}
	}

	bare, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	for _, verb := range theFourGenerationVerbs {
		if hasTool(bare, verb) {
			t.Errorf("a task node with no media wiring carries %s — absent-not-broken", verb)
		}
	}
}

// ── (2) an adaptive run's node ──────────────────────────────────────────────

// AN ADAPTIVE RUN'S NODE IS THE SAME AGENT under a different scheduler
// (orchestrate.go builds its child from the same Config and the same belt), so
// the media family reaches it by the same route and under the same condition.
// The one thing that differs is the write scope, which is a path rule and not a
// tool rule — a node that may write only under `assets/` still HAS the verb that
// makes a picture.
func TestAnAdaptiveRunNodeCarriesTheMediaVerbsAndLacksThemWithoutModels(t *testing.T) {
	wired, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.writeScope = []string{"assets/"}
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
	})
	for _, verb := range theFourGenerationVerbs {
		if !hasTool(wired, verb) {
			t.Errorf("an adaptive run's node is missing %s", verb)
		}
	}

	dry, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.writeScope = []string{"assets/"}
	})
	for _, verb := range theFourGenerationVerbs {
		if hasTool(dry, verb) {
			t.Errorf("an adaptive run's node carries %s with no model behind it", verb)
		}
	}
}

// ── (3) the landing turn ────────────────────────────────────────────────────

// THE BUG THIS WHOLE LANE STARTED FROM.
//
// A stopped node gets one LAND NOW turn to save what it has, and that turn used
// to keep two tools chosen by name — write and edit. A node that had spent its
// whole life painting was therefore told to save its deliverable with the verb
// that saves one taken away, and answered "I have no image tooling available
// now." The landing belt is [savingTools] now, which is the same map that
// decides whether a call SAVED something — one question, one answer.
func TestTheLandingBeltKeepsEveryVerbThatSavesADeliverable(t *testing.T) {
	agent, _ := newMediaAgent(t, &scriptedMedia{}, nil)
	landing := toolNameSet(landingBelt(agent.tools))

	for _, verb := range []string{"write", "edit", "generate_image", "speak"} {
		if !landing[verb] {
			t.Errorf("the landing belt dropped %s — a node whose deliverable is made with it cannot produce one", verb)
		}
	}
	// The verbs that answer with a job and land minutes later are OFF the
	// landing belt: the node is closed the moment the turn ends and Close
	// kills the render, so handing them out would promise a file that cannot
	// arrive ([landsLater]).
	for _, verb := range []string{"generate_video", "generate_music"} {
		if landing[verb] {
			t.Errorf("the landing belt kept %s, whose file lands after the node is already closed", verb)
		}
	}
	// And it is a NARROWING and not the whole belt: landing is for finishing,
	// so the hands that only look must be gone.
	for _, verb := range []string{"read", "bash", "grep", "find", "ls"} {
		if landing[verb] {
			t.Errorf("the landing belt kept %s; the turn forbids new exploration", verb)
		}
	}
}

// AND THE SENTENCE MATCHES THE BELT. The instruction is generated from the
// hands that survived, so a machine with no media models is never told it may
// call a verb it does not have — which is what a hard-coded sentence would do
// the moment the belt became conditional.
func TestTheLandingInstructionNamesExactlyTheHandsItKept(t *testing.T) {
	withMedia, _ := newMediaAgent(t, &scriptedMedia{}, nil)
	said := landingInstruction(landingBelt(withMedia.tools))
	for _, verb := range []string{"generate_image", "speak"} {
		if !strings.Contains(said, verb) {
			t.Errorf("the landing instruction never names %s, which the landing belt carries:\n%s", verb, said)
		}
	}
	for _, verb := range []string{"generate_video", "generate_music"} {
		if strings.Contains(said, verb) {
			t.Errorf("the landing instruction names %s, which the landing belt does not carry:\n%s", verb, said)
		}
	}

	plain, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	quiet := landingInstruction(landingBelt(plain.tools))
	for _, verb := range theFourGenerationVerbs {
		if strings.Contains(quiet, verb) {
			t.Errorf("a machine with no media models was told it could call %s:\n%s", verb, quiet)
		}
	}
	if !strings.Contains(quiet, "edit") || !strings.Contains(quiet, "write") {
		t.Errorf("the landing instruction lost its two ordinary hands:\n%s", quiet)
	}
}

// ── (4) the saved harness ───────────────────────────────────────────────────

// A HARNESS MAY MAKE THINGS. The belt a design is offered, the belt its
// whitelist is linted against and the belt a run resolves against are one call
// (harness_belt.go), and the media family is on it under the same condition it
// is on every other belt.
func TestTheHarnessBeltCarriesTheMediaVerbsAndLacksThemWithoutModels(t *testing.T) {
	workspace := t.TempDir()

	wired := HarnessBeltNames(workspace, HarnessBeltSeams{
		Media: &scriptedMedia{}, MediaModel: allMediaModels(), Seer: &scriptedCompleter{},
	})
	for _, verb := range append(theFourGenerationVerbs, "view_image") {
		if !wired[verb] {
			t.Errorf("a harness on a machine with media models cannot name %s on its whitelist", verb)
		}
	}
	// The seven wire tools are still all there: the media family was ADDED to
	// what a harness could always reach, never traded against it.
	for _, verb := range []string{"read", "bash", "edit", "write", "grep", "find", "ls"} {
		if !wired[verb] {
			t.Errorf("the harness belt lost the wire tool %s", verb)
		}
	}

	// And the whole belt a conversation carries is still NOT what a harness
	// gets: a saved procedure must not inherit the person's settings or the task
	// verbs months after anybody read it.
	for _, verb := range []string{"change_setting", "settings", "propose_task", "watch", "jobs", "remember"} {
		if wired[verb] {
			t.Errorf("a harness inherited %s, which belongs to a conversation and not to a saved procedure", verb)
		}
	}

	empty := HarnessBeltNames(workspace, HarnessBeltSeams{})
	for _, verb := range append(theFourGenerationVerbs, "view_image") {
		if empty[verb] {
			t.Errorf("a harness on a machine with no media wiring can name %s — absent-not-broken", verb)
		}
	}
	if len(empty) != 7 {
		t.Fatalf("a harness belt with no media seams carries %d tools, want the seven wire tools: %v", len(empty), empty)
	}
}

// THE DESIGNER IS TOLD EXACTLY WHAT THE LINT WILL ACCEPT. These were three
// independent calls to bare.AllTools before, so a verb offered by one was not
// necessarily a verb the others knew; the property worth pinning is that the
// guide's list and the lint's set are the same set.
func TestTheDesignerIsOfferedTheSameBeltTheLintAccepts(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = &scriptedMedia{}
		config.MediaModel = allMediaModels()
	})
	offered := agent.harnessMachinery()["tools"]
	accepted := agent.harnessToolNames()
	if len(accepted) == 0 {
		t.Fatal("the lint accepts no tool names at all")
	}
	for name := range accepted {
		if !strings.Contains(offered, name) {
			t.Errorf("the lint accepts %q but the designer is never shown it", name)
		}
	}
	for _, verb := range theFourGenerationVerbs {
		if !accepted[verb] {
			t.Errorf("the harness lint refuses %s on a machine that has the model for it", verb)
		}
	}
}
