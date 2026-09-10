package session

// THE PROFILE IS DERIVED, SETTLED ONCE, AND CHANGES EXACTLY FOUR THINGS.
//
// promptprofile.go is a second shape of fixed prefix rather than a second
// product, so this file asks the two questions that keep it that way: is the
// derivation the one the design named (window, seat, pin — in that order), and
// does each of the four doors it opens do what it says and nothing else. The
// frontier arm's own guarantee is next door in prefixbudget_test.go
// ([TestAFrontierShapeIsUntouchedByTheProfile]), where the number that would
// notice a side-effect lives.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configpkg "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// leanConfig is a bare config on a named window, which is all the derivation
// needs: every predicate in promptprofile.go is answerable from the config
// alone, before any agent exists.
func windowConfig(t *testing.T, window int) Config {
	t.Helper()
	return Config{Workspace: t.TempDir(), Model: "test/model", ContextWindow: window}
}

// ── the derivation ──────────────────────────────────────────────────────────

// THE WINDOW IS THE FIRST FACT AND THE THRESHOLD IS THE LINE.
func TestTheWindowDecidesTheProfile(t *testing.T) {
	for _, want := range []struct {
		window  int
		profile promptProfile
	}{
		{window: 8_192, profile: profileLean},
		{window: 16_000, profile: profileLean},
		{window: leanWindowThreshold - 1, profile: profileLean},
		// The threshold itself is the first FULL window: a model with exactly
		// the line's worth of room keeps the whole page.
		{window: leanWindowThreshold, profile: profileFull},
		{window: 128_000, profile: profileFull},
		{window: 1_000_000, profile: profileFull},
		// Nothing said is the conservative default, which is a frontier window.
		{window: 0, profile: profileFull},
	} {
		if got := windowConfig(t, want.window).promptProfile(); got != want.profile {
			t.Errorf("a %d-token window resolved to the %s profile, want %s", want.window, got, want.profile)
		}
	}
}

// AND THE CATALOG'S ANSWER OUTRANKS THE CONFIGURED FIGURE, because that is the
// ladder [Agent.window] climbs and a profile reading a different one would be a
// second reading of the same question.
func TestTheCatalogsWindowOutranksTheConfiguredOne(t *testing.T) {
	config := windowConfig(t, 128_000)
	config.ContextWindowFor = func(string) int { return 8_192 }
	if got := config.promptProfile(); !got.lean() {
		t.Fatalf("a model the catalog gives 8,192 tokens resolved to %s", got)
	}
	// A catalog that cannot answer for this model leaves the configured figure
	// standing, which is the emptiness law applied to a measurement.
	config.ContextWindowFor = func(string) int { return 0 }
	if got := config.promptProfile(); got.lean() {
		t.Fatal("a catalog answering nothing was read as a small window")
	}
}

// THE SEAT IS THE OTHER FACT, and it outranks a big window: an open-weight
// worker model that claims a large one still reasons like a worker.
func TestTheWorkerSeatIsLeanWhateverItsWindowClaims(t *testing.T) {
	profileDir := t.TempDir()
	seat := configpkg.TierModelAt(profileDir, configpkg.ModelTierWorker)
	if strings.TrimSpace(seat) == "" {
		t.Fatal("the crew has no worker model, so this test is asserting against nothing")
	}

	config := windowConfig(t, 1_000_000)
	config.ProfileDir = profileDir
	config.Model = seat
	if got := config.promptProfile(); !got.lean() {
		t.Errorf("a conversation on the worker seat %q with a million tokens of room resolved to %s", seat, got)
	}
	// AND NOTHING ELSE IS THE WORKER SEAT. The predicate is an exact match
	// against the crew's own row, not a guess about which vendors are small.
	config.Model = seat + "-not-the-seat"
	if got := config.promptProfile(); got.lean() {
		t.Errorf("%q was read as the worker seat", config.Model)
	}
}

// THE PIN IS FOR A TEST AND A BENCH CELL, and it wins over both facts.
func TestTheEnvironmentPinDecidesBothWays(t *testing.T) {
	t.Setenv(promptProfileEnv, "lean")
	if got := windowConfig(t, 1_000_000).promptProfile(); !got.lean() {
		t.Errorf("a pinned lean profile resolved to %s on a million-token window", got)
	}
	t.Setenv(promptProfileEnv, "FULL")
	if got := windowConfig(t, 8_192).promptProfile(); got.lean() {
		t.Errorf("a pinned full profile resolved to %s on an 8k window", got)
	}
	// AN UNRECOGNISED PIN IS NOT A PIN. A stale or mistyped variable in
	// somebody's shell must not quietly move a conversation onto the other arm,
	// which is the reversal internal/splitgate's Mode states at length.
	for _, typo := range []string{"", "  ", "small", "true", "1", "leaner"} {
		t.Setenv(promptProfileEnv, typo)
		if got := windowConfig(t, 128_000).promptProfile(); got.lean() {
			t.Errorf("%q was read as a lean pin", typo)
		}
		if got := windowConfig(t, 8_192).promptProfile(); !got.lean() {
			t.Errorf("%q stopped an 8k window from deriving lean", typo)
		}
	}
}

// AND IT IS SETTLED ONCE, at construction, into the config every later reader
// reads (agent.go's newAgent).
func TestTheProfileIsSettledOnceAtConstruction(t *testing.T) {
	t.Setenv(promptProfileEnv, "lean")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		if config.profile != "" {
			t.Fatalf("the caller's config already carried the %s profile", config.profile)
		}
	})
	if agent.config.profile != profileLean {
		t.Fatalf("the agent kept the %q profile, want it settled to lean", agent.config.profile)
	}
	// AND THE SETTLED ANSWER IS WHAT IS READ AFTERWARDS, not the environment: a
	// pin that moved mid-session must not give one turn a page and the next a
	// different one, because message[0] is the prompt cache.
	t.Setenv(promptProfileEnv, "full")
	if !agent.config.promptProfile().lean() {
		t.Fatal("the settled profile followed the environment after construction")
	}
}

// ── the page ────────────────────────────────────────────────────────────────

// EVERY HEADING THE TABLE NAMES IS REALLY THERE. A row naming a section that has
// since been renamed keeps nothing and drops nothing, silently — which is the
// one way this table can rot.
func TestEveryRuledSectionIsAHeadingThePageHas(t *testing.T) {
	page := widestPage()
	for _, section := range leanPageSections {
		if !strings.Contains(page, "\n# "+section.heading+"\n") {
			t.Errorf("leanPageSections rules on %q, which the composed page has no `# ` heading for: the section was renamed and this row now decides nothing",
				section.heading)
		}
		if strings.TrimSpace(section.why) == "" {
			t.Errorf("the %q row gives no reason, and a list of headings nobody can review is how a page loses a law quietly", section.heading)
		}
	}
}

// AND THE CUT TAKES WHAT IT NAMES AND LEAVES WHAT IT DOES NOT.
func TestALeanPageDropsExactlyTheSectionsTheTableNames(t *testing.T) {
	config := windowConfig(t, leanWindow)
	config.Standing = &Standing{}
	config.standingItems = &fakeStanding{}
	full := strings.TrimRight(promptWithBeltFacts(config), "\n")
	lean := leanPage(full)

	for _, section := range leanPageSections {
		heading := "\n# " + section.heading + "\n"
		if !strings.Contains(full, heading) {
			t.Fatalf("the composed page has no %q section, so this test is asserting against the wrong page", section.heading)
		}
		if got := strings.Contains(lean, heading); got != section.keeps {
			verb := "kept"
			if !section.keeps {
				verb = "dropped"
			}
			t.Errorf("%q should be %s on a lean page and is not", section.heading, verb)
		}
	}
	// A SECTION NOBODY RULED ON IS KEPT. A law written into the page tomorrow
	// reaches both arms until somebody decides otherwise; dropping by default is
	// how the lean arm would quietly lose everything written after this file.
	for _, kept := range []string{"# Tone", "# Tool Policy", "# Workflow", "# Delivery", "# Critical", "# Session facts"} {
		if !strings.Contains(lean, "\n"+kept+"\n") {
			t.Errorf("a lean page dropped %q, which no row rules on", kept)
		}
	}
	if strings.Contains(lean, "\n\n\n") {
		t.Error("a dropped section left a paragraph gap of three newlines behind it")
	}
}

// THE WORKING DISCIPLINE IS BYTE-IDENTICAL ON BOTH ARMS. It is the one section
// with ablation evidence behind it, and it is the one page a worker and a
// conversation read word for word the same (taskprompt_test.go holds that pin),
// so a profile that trimmed it here would break both at once.
func TestTheDisciplineSurvivesTheLeanCutByteForByte(t *testing.T) {
	discipline := strings.TrimRight(disciplinePrompt, "\n")
	if strings.TrimSpace(discipline) == "" {
		t.Fatal("the discipline page is empty, so this test is asserting against nothing")
	}
	config := windowConfig(t, leanWindow)
	page := renderSystemAt(config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if !config.promptProfile().lean() {
		t.Fatal("the shape under test is not lean")
	}
	if !strings.Contains(page, discipline) {
		t.Error("a lean page no longer carries the working discipline byte for byte")
	}
}

// ── the shelf ───────────────────────────────────────────────────────────────

// THE LEAN TABLE IS WELL FORMED AND DOES NOT CLAIM A NAME THE SHIPPED ONE OWNS.
// A name in two tables would make which group owns it depend on which table
// [capabilityGroupOf] walks first.
func TestTheLeanCapabilityTableIsWellFormed(t *testing.T) {
	owner := map[string]string{}
	for _, group := range capabilityGroups {
		for _, member := range group.members {
			owner[member] = group.name
		}
	}
	names := map[string]bool{}
	for _, group := range capabilityGroups {
		names[group.name] = true
	}
	for _, group := range leanCapabilityGroups {
		if strings.TrimSpace(group.name) == "" {
			t.Errorf("a lean capability group has no name: %+v", group)
		}
		if names[group.name] {
			t.Errorf("the lean table reuses the shipped group word %q", group.name)
		}
		if len(group.members) == 0 {
			t.Errorf("the lean %s group names no tools", group.name)
		}
		if group.holds == nil {
			t.Errorf("the lean %s group has no predicate, so the page could send a model to a group this shape never had", group.name)
		}
		for _, member := range group.members {
			if had, taken := owner[member]; taken {
				t.Errorf("%s is claimed by both %s and the lean %s", member, had, group.name)
			}
			owner[member] = group.name
		}
	}
}

// AND EVERY NAME ON IT IS A TOOL THE LEAN BELT ACTUALLY BUILDS. A group word
// standing for nothing is a word in the model's catalog it can never spend.
func TestALeanBeltShelvesWhatTheTableNamesAndCarriesAsk(t *testing.T) {
	agent := leanShapedAgent(t)
	shelved := map[string]bool{}
	for _, name := range agent.shelvedNames() {
		shelved[name] = true
	}
	for _, group := range leanCapabilityGroups {
		if !group.holds(agent.config) {
			continue
		}
		for _, member := range group.members {
			if !shelved[member] {
				t.Errorf("`%s` is in the lean %s group and is not on this belt's shelf", member, group.name)
			}
			if agent.hasTool(member) {
				t.Errorf("`%s` is carried on a lean belt as well as shelved", member)
			}
			if !agent.offers(member) {
				t.Errorf("`%s` is not offered at all: shelving is not removing", member)
			}
		}
	}
	// THE SEVEN PI TOOLS AND THE VERBS A SMALL MODEL REACHES FOR EVERY TURN STAY
	// CARRIED. A lean belt that had to load its way to `read` would spend the
	// window it was built to save.
	for _, carried := range []string{"read", "write", "edit", "bash", "grep", "find", "ls", "manual"} {
		if !agent.hasTool(carried) {
			t.Errorf("`%s` is not carried on a lean belt", carried)
		}
	}
}

// `ask` IS HANDED OVER AND NOT OFFERED. A one-call-per-message model cannot do
// load-then-ask inside a turn, so the group is armed at construction and the
// loading verb must not list it — a catalog entry for something already in the
// tool list is a round trip the model will spend at the exact moment it had a
// question.
func TestTheQuestionsGroupIsPreArmedAndNotOffered(t *testing.T) {
	agent := leanShapedAgent(t)
	if !agent.hasTool("ask") {
		t.Fatal("`ask` is not on a lean belt")
	}
	for _, name := range agent.shelvedNames() {
		if name == "ask" {
			t.Error("`ask` is on the shelf of a lean belt as well as on it")
		}
	}
	loader, ok := toolNamed(agent.beltTools(), loadCapabilityToolName)
	if !ok {
		t.Fatal("a lean belt shelves four groups and carries no load_capability")
	}
	if strings.Contains(loader.Description, " "+questionsGroup+":") {
		t.Errorf("the loading verb still offers the pre-armed %s group:\n%s", questionsGroup, loader.Description)
	}
	if strings.Contains(string(loader.Schema), `"`+questionsGroup+`"`) {
		t.Errorf("the loading verb's enum still takes %q", questionsGroup)
	}
	// AND THE PAGE SAYS `ask` IS THERE rather than telling the model to fetch it.
	page := renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if strings.Contains(page, "`ask` waits in the `"+questionsGroup+"` group") {
		t.Error("a lean page tells the model to load `ask`, which is already in its tool list")
	}
	if !strings.Contains(page, "ask through `ask`, never in prose") {
		t.Error("a lean page no longer carries the present-case wording for `ask`")
	}
}

// A FULL BELT IS UNCHANGED BY EITHER OF THOSE. Same partition, same catalog.
func TestAFullBeltStillShelvesQuestionsAndNothingExtra(t *testing.T) {
	agent := v3ShapedAgent(t)
	if agent.hasTool("ask") {
		t.Error("`ask` is carried on a full belt: the pre-arming reached the wrong arm")
	}
	for _, group := range leanCapabilityGroups {
		for _, member := range group.members {
			if !agent.offers(member) {
				continue
			}
			if !agent.hasTool(member) {
				t.Errorf("`%s` is shelved on a full belt: the lean partition reached the wrong arm", member)
			}
		}
	}
}

// toolNamed finds one tool on a belt.
func toolNamed(belt []bare.Tool, want string) (bare.Tool, bool) {
	for _, tool := range belt {
		if tool.Name == want {
			return tool, true
		}
	}
	return bare.Tool{}, false
}

// ── the memory reflex ───────────────────────────────────────────────────────

// THE REFLEX DOES NOT RUN ON A LEAN SESSION, and it is off by the switch that
// already turns it off: no store is no block, no call and no verb
// (memory_test.go's [TestWithoutAStoreThereIsNoBlockNoCallAndNoTool] is the same
// law asserted from the other side).
func TestALeanSessionRunsNoMemoryReflex(t *testing.T) {
	t.Setenv(promptProfileEnv, "lean")
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "deploys", "we deploy from staging on Fridays")

	if block := agent.memoryBlock(context.Background(), "how do I deploy this"); block != "" {
		t.Fatalf("a lean session rendered the memory block %q", block)
	}
	if routes, extracts, decides := script.counts(); routes+extracts+decides != 0 {
		t.Fatalf("a lean session made %d/%d/%d reflex calls; the reflex is two model calls a turn nobody asked for", routes, extracts, decides)
	}
	if agent.remembers() {
		t.Error("a lean session still holds a brain, so the reflex has a way back")
	}
	if agent.hasTool("remember") {
		t.Error("`remember` is on a lean belt with no store behind it: absent, not broken")
	}
	// AND THE PAGE AGREES, which is the whole point of flipping the same switch
	// rather than inventing a second one: [Config.hasStore] is what the page's
	// memory sentence is composed from, so it says memory is off by itself.
	page := renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if !strings.Contains(page, "say plainly that memory is off") {
		t.Error("a lean page still promises `remember`")
	}
}

// AND A FULL SESSION WITH THE SAME BRAIN STILL ROUTES.
func TestAFullSessionKeepsItsReflex(t *testing.T) {
	t.Setenv(promptProfileEnv, "full")
	agent, _ := brainAgent(t, &reflexScript{}, nil)
	if !agent.remembers() {
		t.Fatal("a full session lost its brain")
	}
	if !agent.hasTool("remember") {
		t.Fatal("`remember` came off a full belt")
	}
}

// ── the project's own instructions ──────────────────────────────────────────

// ONE FILE, TWO KIB, AND THE MODEL IS TOLD IT WAS CUT.
func TestALeanPrefixQuotesOneInstructionFileUnderItsOwnBound(t *testing.T) {
	workspace := t.TempDir()
	long := strings.Repeat("house rule.\n", 900)
	if len(long) <= leanInstructionLimit {
		t.Fatal("the fixture is not longer than the lean bound, so this test proves nothing")
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write(agentsFileName, long)
	write(claudeFileName, "the second copy of the house rules")

	config := Config{Workspace: workspace, Model: "test/model", ContextWindow: leanWindow}
	page := renderSystemAt(config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if !strings.Contains(page, "AGENTS.md is longer than 2KiB") {
		t.Error("a lean prefix does not say the instruction file was cut at its own bound")
	}
	if strings.Contains(page, "the second copy of the house rules") {
		t.Error("a lean prefix quotes both instruction files")
	}
	if strings.Count(page, "house rule.") > leanInstructionLimit/len("house rule.") {
		t.Error("a lean prefix carried more of the instruction file than its bound allows")
	}

	// AND THE FIRST FILE FOUND IS THE ONE THAT RIDES: a project with only
	// CLAUDE.md still gets its rules.
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, claudeFileName), []byte("the only house rules"), 0o644); err != nil {
		t.Fatalf("write %s: %v", claudeFileName, err)
	}
	config.Workspace = only
	if !strings.Contains(renderSystemAt(config, time.Now()), "the only house rules") {
		t.Error("a lean prefix skipped a project whose only instruction file is CLAUDE.md")
	}

	// AND A FULL PREFIX IS UNCHANGED: both files, eight KiB each.
	config.Workspace, config.ContextWindow = workspace, 128_000
	full := renderSystemAt(config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if !strings.Contains(full, "the second copy of the house rules") {
		t.Error("a full prefix stopped quoting CLAUDE.md")
	}
	if strings.Contains(full, "AGENTS.md is longer than 2KiB") {
		t.Error("a full prefix bounded AGENTS.md at the lean limit")
	}
}
