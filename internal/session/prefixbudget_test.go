package session

// THE FIXED PREFIX, AND WHAT IT COSTS TO LET IT GROW.
//
// Two things ride in front of every request this session makes: the system
// prompt, which is message[0], and the tool schema block, which is encoded ahead
// of message[0]. Neither depends on what anybody said. They are sent again, in
// full, on every tool round of every turn — a measured benchmark cell made 69
// requests and paid for both 69 times.
//
// That is what makes a byte here different from a byte anywhere else in this
// repository. A sentence added to a Go comment is free. A sentence added to a
// tool description is bought roughly sixty times per task, and again on the next
// task, forever. When this prefix was last measured against a competing harness
// it was 26,400 tokens against their 3,500 — 45.6% of every prompt token that
// cell spent, on text that taught the model nothing it had not already been
// told twice on the same page.
//
// The prompt cache does not make this free either. Caching discounts the tokens;
// it does not stop them being sent, counted, or re-priced in full the moment any
// byte in front of them moves (prefixcache_test.go states that half of it).
//
// So this test is a BUDGET, not a measurement. It fails when the fixed prefix
// grows past what the diet left it plus a margin, and its failure message names
// the tool that grew, because the lane that adds a paragraph to a description is
// otherwise the one person in the loop who never sees the bill.
//
// WHAT TO DO WHEN IT FAILS. Not raise the number — that is the one move that
// makes the failure meaningless. Find the sentence you added and ask whether it
// states a rule the model does not already have. A schema description teaches
// what a field is FOR in the fewest words that keep the law; the system prompt
// states each law once. If the addition genuinely carries a new law, take the
// bytes back out of something that repeats one, and leave the budget where it
// is.

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/programguide"
)

// The rendered prefix is measured with the same page and tool schemas the
// session sends, so duplicated descriptions cannot silently grow the request.
const fixedPrefixBudget = fixedPrefixTarget + fixedPrefixWaiver

// fixedPrefixTarget is what the shipped prefix is SUPPOSED to be: the figure the
// last diet aimed at and the one [fixedPrefixBudget] held until the right belt
// was weighed.
//
// THE CAP IS THIS PLUS A WAIVER, AND THE WAIVER ONLY SHRINKS. See
// [prefixWaivers] for what each arm owes today and why a number that is only
// printed is a number nobody ever pays.
const fixedPrefixTarget = 48_000

// prefixWaivers records how far each rendered prefix exceeds its target. The
// measured limits prevent descriptions and schemas from growing unnoticed;
// removing text lowers the corresponding allowance.
const (
	fixedPrefixWaiver = 4_497
	leanPrefixWaiver  = 13_393
)

// The lean profile has a separate budget because its page and tool shelf
// differ from the full belt. Measure each rendered shape independently.
const leanPrefixBudget = leanPrefixTarget + leanPrefixWaiver

// leanPrefixTarget is what the lean prefix is SUPPOSED to be: the figure
// [leanPrefixBudget] held until the right belt was weighed. The cap is this plus
// [leanPrefixWaiver], which only ever shrinks.
const leanPrefixTarget = 31_500

// leanWindow is the window the lean budget is weighed at. Sixteen thousand
// tokens is the shape the profile was written for — a local open-weight model —
// and it is comfortably under [leanWindowThreshold], so the shape this measures
// is the shape a person on such a model actually gets.
const leanWindow = 16_000

// prefixPrograms is the declared widest program fixture. It uses the program's
// own guide but does not read the platform's carried registry: Windows carries
// none, while this budget must still weigh the Unix shipping maximum.
func prefixPrograms() []delegate.Delegate {
	return []delegate.Delegate{
		{Name: "review", Guide: programguide.Review, Lands: delegate.LandsText},
		{Name: "sec", Guide: programguide.Sec, Lands: delegate.LandsText},
		{Name: "senior-dev", Guide: programguide.SeniorDev, Lands: delegate.LandsTree},
	}
}

// widestPage is the page at its heaviest: prompts/system.md with every one of
// its tool-naming facts in the PRESENT case (beltfacts.go).
//
// THE BUDGET WEIGHS THE WIDEST PAGE AND NOT ONE SHAPE'S. Those facts are
// composed per agent now — a worker without `watch` reads one sentence where a
// conversation reads another — so there is no single string to measure any
// more, and the honest thing to bound is the most any agent can be handed. It
// is also the page the person's own conversation reads, which is the one that
// is paid for on every turn of every day.
func widestPage() string {
	page := systemPrompt
	// THE SAME LIST THE PAGE IS COMPOSED FROM (beltfacts.go's [promptSections]),
	// walked with every row in its PRESENT case. A section added there is weighed
	// here without a second edit, so the budget cannot go on measuring a page the
	// composer stopped building.
	for _, section := range promptSections {
		lines := make([]string, 0, len(section.facts))
		for _, fact := range section.facts {
			// AND THE HEAVIER OF THE TWO PRESENT CASES. A fact whose tools wait
			// on a shelf reads one sentence for the conversation that must
			// fetch them and a shorter one for the worker that carries them
			// (beltfacts.go), and the page this budget bounds is the widest any
			// agent can be handed.
			widest := fact.present
			if len(fact.shelved) > len(widest) {
				widest = fact.shelved
			}
			// AND A FILLED FACT IS WEIGHED FILLED. The programs paragraph is a
			// frame whose body is each carried program's own guide
			// (delegate_door.go), and weighing the frame alone once let a
			// paragraph of a few hundred bytes ride every request unseen. It is
			// filled with the declared widest program fixture, whose guide is
			// the same source the Unix chat door carries.
			if fact.fill != nil {
				widest = fact.fill(Config{Delegates: prefixPrograms()}, widest)
			}
			lines = append(lines, widest)
		}
		page = strings.Replace(page, section.token, strings.Join(lines, section.join), 1)
	}
	return page
}

// The prefix budget weighs the same maximum on a build that carries no
// programs. Its program paragraph is a declared measurement fixture, not a
// reading of whichever registry this test process happens to have.
func TestPrefixBudgetDoesNotDependOnTheCarriedProgramRegistry(t *testing.T) {
	want := widestPage()
	weighedAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	wantLeanPage := pageAsWeighed(leanShapedAgent(t).config, weighedAt)
	wantFixed, err := json.Marshal(widestBelt(t, fixedShapedAgent(t)))
	if err != nil {
		t.Fatal(err)
	}
	wantLean, err := json.Marshal(widestBelt(t, leanShapedAgent(t)))
	if err != nil {
		t.Fatal(err)
	}
	restore := builtin.Override(nil)
	defer restore()
	if got := widestPage(); got != want {
		t.Fatalf("the fixed page changed when this build carried no program: %d bytes became %d", len(want), len(got))
	}
	if got := pageAsWeighed(leanShapedAgent(t).config, weighedAt); got != wantLeanPage {
		t.Errorf("the lean page changed when this build carried no program: %d bytes became %d", wantLeanPage, got)
	}
	for _, arm := range []struct {
		name string
		want []byte
		new  func(*testing.T) *Agent
	}{{"full", wantFixed, fixedShapedAgent}, {"lean", wantLean, leanShapedAgent}} {
		got, err := json.Marshal(widestBelt(t, arm.new(t)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(arm.want) {
			t.Errorf("the %s tool block changed when this build carried no program: %d bytes became %d", arm.name, len(arm.want), len(got))
		}
	}
}

// The stand-in stays tied to the real Unix program. A new carried program or a
// changed guide must change the declared widest fixture in the same edit.
func TestPrefixProgramFixtureMatchesTheCarriedProgram(t *testing.T) {
	carried := builtin.All()
	if len(carried) == 0 {
		return // Windows has no runnable program, but weighs the Unix maximum.
	}
	standIn := prefixPrograms()
	if len(carried) != len(standIn) {
		t.Fatalf("the prefix fixture has %d programs, this build carries %d", len(standIn), len(carried))
	}
	for i, program := range carried {
		if program.Name != standIn[i].Name || program.Guide != standIn[i].Guide || program.Lands != standIn[i].Lands {
			t.Fatalf("prefix fixture %d does not match carried program %s", i, program.Name)
		}
	}
}

// atAFixedPlace is a config whose WORKING DIRECTORY is a constant.
//
// THE PAGE INTERPOLATES WHERE YOU ARE (prompt.go's `- Working directory: %s`),
// and a fixture's workspace is a temp directory named after the test and the
// platform's temp root: `/tmp/TestX123/001` on Linux and
// `/var/folders/9k/…/T/TestX456/001` on macOS. So this arm's number moved by
// forty-five bytes between two machines and failed on the second — the same
// class of defect as `grep`'s two sentences and the capability shelf, found the
// same way, in the gate whose entire purpose is to be one number for everyone.
//
// The page also carries `- Workstation: %s/%s` from runtime.GOOS/GOARCH, which
// is a byte of difference between darwin and linux and is left alone: it is a
// FACT ABOUT THE MACHINE the model is told on purpose, it cannot be made
// constant without lying to the fixture, and a byte is inside nobody's decision.
// The working directory is not that — it is a fixture artefact, and it is tens
// of bytes.
// The full arm does not need it: [widestPage] weighs `systemPrompt` with its
// belt-fact tokens substituted and never renders the machine lines at all, so
// that number is already the same everywhere — and it is an UNDER-count of the
// shipped page by those few lines, which is recorded here rather than fixed
// because moving it would move a ratchet for a reason that is not growth.
func pageAsWeighed(config Config, now time.Time) int {
	config.Workspace = "/w"
	page := renderSystemAt(config, now)
	// AND THE WORKSTATION LINE IS NORMALISED. `- Workstation: %s/%s` is
	// runtime.GOOS/GOARCH, which is `darwin/arm64` on one machine and
	// `linux/arm64` on another — ONE BYTE, and a ratchet with no headroom fails
	// on one byte. It is a fact about the machine the model is told on purpose
	// and cannot be made constant in the product, so it is made constant HERE,
	// at the only place that needs it to be.
	return len(strings.Replace(page, runtime.GOOS+"/"+runtime.GOARCH, "os/arch", 1))
}

// widestBelt is the shipped belt weighed as the WIDEST MACHINE pays for it.
//
// A TOOL WHOSE DESCRIPTION DEPENDS ON WHAT IS INSTALLED still sends those bytes
// on every request, and this gate runs on machines of both kinds: `grep` says
// one sentence where ripgrep is present and a longer one where it is not
// (bare's [bare.WidestGrepDescription]), and the difference measured 107 bytes
// of prefix — enough that the same commit passed on the laptop it was written on
// and failed on the runner that proved it. So the number below is one number
// everywhere, and it is the larger one.
func widestBelt(t *testing.T, agent *Agent) []ai.ToolDefinition {
	t.Helper()
	definitions := append([]ai.ToolDefinition(nil), agent.beltDefinitions()...)
	if len(definitions) == 0 {
		t.Fatal("the belt is empty, so this test would pass on nothing")
	}
	widest := map[string]string{
		// `grep` says a longer sentence about itself where ripgrep is absent.
		"grep": bare.WidestGrepDescription(agent.resultCaps()),
		// `load_capability` NAMES THE GROUPS THIS BUILD HAS, and a group whose
		// every member was gated off is not named at all — `edit_video` is built
		// only where ffmpeg is on PATH, so a machine that can edit video pays for
		// one more clause than one that cannot (tools_capabilities.go).
		//
		// IT IS THIS SHAPE'S OWN SHELF AND NOT THE FULL ONE. A lean belt pre-arms
		// `questions` instead of shelving it (promptprofile.go), so pasting the
		// full shape's sentence into the lean arm would have made that number a
		// hybrid of two belts — a figure neither of them pays.
		"load_capability": widestLoadCapability(agent.config),
	}
	swapped := 0
	for index := range definitions {
		longest, varies := widest[definitions[index].Function.Name]
		if !varies {
			continue
		}
		swapped++
		// ASSIGNED, NOT COMPARED. A swap that only fired when it made the number
		// bigger would be a swap that silently stopped firing the day the other
		// spelling grew — which is the same defeat, one level up. `widest` is
		// widest by construction and this takes it at its word.
		definitions[index].Function.Description = longest
	}
	// AND THE SWAP CANNOT SILENTLY STOP APPLYING. A tool that leaves the belt, or
	// is renamed, would take its machine-variance off this number without anybody
	// noticing the budget had quietly got easier.
	if swapped != len(widest) {
		t.Fatalf("%d of the %d tools that vary by machine are on the belt", swapped, len(widest))
	}
	return definitions
}

// widestLoadCapability is the loader's sentence for ONE SHAPE at the widest
// machine: every group that shape shelves rather than carries, each with every
// member the table declares — which is what it says where all the programs its
// tools shell out to are installed.
//
// It is built through the product's own [loadCapabilityDescription], so the
// three sentences after the group list are stated once and cannot drift out of
// this number.
//
// THE ONLY THING TAKEN OUT IS WHAT THIS SHAPE PRE-ARMS, because that is the one
// difference between the two belts' shelves that is a property of the SHAPE
// rather than of the machine. A group gated off by config would be
// over-counted here; that is the safe direction for a budget and it is the same
// answer everywhere, which is the whole point.
func widestLoadCapability(config Config) string {
	prearmed := map[string]bool{}
	for _, group := range config.prearmedGroups() {
		prearmed[group] = true
	}
	order := make([]string, 0, len(capabilityGroups))
	members := make(map[string][]string, len(capabilityGroups))
	for _, group := range capabilityGroups {
		if prearmed[group.name] {
			continue
		}
		order = append(order, group.name)
		members[group.name] = group.members
	}
	return loadCapabilityDescription(func(group string) []string { return members[group] }, order)
}

// fixedShapedAgent is the shipping conversation with the declared widest
// program fixture, so the belt measurement and the page weigh one shape on
// Linux, macOS and Windows alike.
func fixedShapedAgent(t *testing.T) *Agent {
	t.Helper()
	shape := beltShapeNamed(t, shippedBeltShape)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		shape.build(t, config)
		config.Delegates = prefixPrograms()
	})
	return agent
}

// TestTheFixedPrefixStaysUnderItsBudget weighs what every request carries before
// anybody has said anything.
func TestTheFixedPrefixStaysUnderItsBudget(t *testing.T) {
	reportPrefix(t, prefixArm{
		what:        "the fixed prefix",
		definitions: widestBelt(t, fixedShapedAgent(t)),
		page:        len(widestPage()),
		budget:      fixedPrefixBudget,
		target:      fixedPrefixTarget,
		owed:        "see this file's head for the bill and who owes it",
		howToPay: "every byte here is sent again on every request of every turn, so take the " +
			"addition back out of something that already says it rather than raising the budget",
	})
}

// prefixArm is one weighing: what to call it, what it carries, and the two
// figures it is judged against.
type prefixArm struct {
	what        string
	definitions []ai.ToolDefinition
	page        int
	budget      int
	target      int
	owed        string
	howToPay    string
}

// reportPrefix weighs one arm, says what is still owed on a green run, and names
// the bill biggest-first when the ratchet is broken.
//
// IT IS ONE FUNCTION BECAUSE THE TWO ARMS ARE ONE TEST with two sets of
// constants. They were forty lines each, written twice, and the copies had
// already drifted: only one of them printed its shortfall against the target,
// so the arm carrying the larger debt was the arm that never mentioned it.
func reportPrefix(t *testing.T, arm prefixArm) {
	t.Helper()
	block, err := json.Marshal(arm.definitions)
	if err != nil {
		t.Fatal(err)
	}
	tools := len(block)
	total := tools + arm.page
	t.Logf("%s is %d bytes (~%d tokens): prompt %d + tools %d over %d tools",
		arm.what, total, total/4, arm.page, tools, len(arm.definitions))
	// AND THE DEBT IS ENFORCED, NOT LOGGED. It was a t.Logf, which is invisible:
	// nothing in the Makefile passes -v, so the one line saying this prefix is
	// thousands of bytes over what it is supposed to be was printed where no
	// human and no CI log would ever show it. A debt nobody is reminded of is a
	// debt that has quietly become the floor.
	//
	// So the TARGET is the cap, and the overage is a WAIVER: one dated figure per
	// arm, in one place, which may only ever shrink ([prefixWaivers]). That is
	// the known-red discipline — `.github/known-red.txt` and its ratchet — applied
	// to bytes. A wave that grows the prefix has to raise a waiver, in a diff, with
	// its name on it; nothing can drift.
	if total <= arm.budget {
		return
	}
	// THE FAILURE NAMES THE BILL, BIGGEST FIRST. A lane reading this has just
	// grown one of these lines and has no other way to see which.
	type weighed struct {
		name  string
		bytes int
	}
	heaviest := make([]weighed, 0, len(arm.definitions))
	for _, definition := range arm.definitions {
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		heaviest = append(heaviest, weighed{definition.Function.Name, len(encoded)})
	}
	sort.Slice(heaviest, func(i, j int) bool { return heaviest[i].bytes > heaviest[j].bytes })
	report := fmt.Sprintf("%s is %d bytes (~%d tokens), over its %d budget by %d\n"+
		"  the page            %6d\n"+
		"  the tool block      %6d over %d tools\n"+
		"the heaviest tools:\n",
		arm.what, total, total/4, arm.budget, total-arm.budget, arm.page, tools, len(arm.definitions))
	for index, tool := range heaviest {
		if index == 8 {
			break
		}
		report += fmt.Sprintf("  %-20s%6d\n", tool.name, tool.bytes)
	}
	t.Fatalf("%sthe cap is the %d target plus a waiver of %d, and a waiver only ever shrinks "+
		"([prefixWaivers]). %s", report, arm.target, arm.budget-arm.target, arm.howToPay)
}

// ── the lean arm ────────────────────────────────────────────────────────────

// leanShapedAgent is the SHIPPING CONVERSATION on a small window: the shape
// [shippedShapeAgent] builds, with the one thing that differs — the window a
// local open-weight model actually has — changed and nothing else. The
// difference between the two numbers below is therefore the profile, and only
// the profile.
//
// ── IT WEIGHED A THINNER AGENT THAN ANYBODY RUNS, AND THIS IS THAT REPAIR ───
//
// It used to build its own config: no memory store, no accounts hub, no standing
// items, no saved programs. That is #576's shape again and the same one the full
// arm was repaired for in this PR — a gate pointed at something nobody runs
// passes without having tested anything. A real lean conversation carries
// `stand` (9.6 KB on its own), `remember` and `search_conversations`, and
// [Config.leanCapabilityGroups] shelves none of the three, so every one of those
// bytes is bought on every request of every turn by exactly the person whose
// window has the least room for them.
func leanShapedAgent(t *testing.T) *Agent {
	t.Helper()
	shape := beltShapeNamed(t, shippedBeltShape)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		shape.build(t, config)
		config.Delegates = prefixPrograms()
		config.ContextWindow = leanWindow
	})
	return agent
}

// TestTheLeanPrefixStaysUnderItsBudget weighs the other arm.
//
// IT WEIGHS THE PAGE THE SHAPE ACTUALLY READS and not the widest one, which is
// the difference between the two budgets and is deliberate: the full budget
// bounds the most any agent can be handed, because one page is read by every
// shape and a lane adding a sentence must see the worst case. The lean arm is
// one shape — a conversation on a small window — and what it costs that person
// is what it renders for them.
func TestTheLeanPrefixStaysUnderItsBudget(t *testing.T) {
	agent := leanShapedAgent(t)
	if !agent.config.promptProfile().lean() {
		t.Fatalf("a %d-token window did not resolve to the lean profile, so this test is weighing the wrong arm", leanWindow)
	}
	// AND `ask` IS IN THE BLOCK, because it is pre-armed rather than shelved
	// ([Config.prearmedGroups]). A lean belt that had to load its way to a
	// question would be measured lighter here and be unable to ask one, which is
	// the one regression this number could hide.
	if !agent.hasTool("ask") {
		t.Fatal("`ask` is not carried on a lean belt: a one-call-per-message model cannot load-then-ask")
	}
	reportPrefix(t, prefixArm{
		what:        "the lean prefix",
		definitions: widestBelt(t, agent),
		page:        pageAsWeighed(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)),
		budget:      leanPrefixBudget,
		target:      leanPrefixTarget,
		owed:        "see [leanPrefixBudget] for the bill and who owes it",
		howToPay: "this arm's number only ever comes down: shelve the verb, cut the law that is " +
			"stated twice, or leave it — never raise the budget",
	})
}

// TestAFrontierShapeIsUntouchedByTheProfile is the other half of the deal.
//
// A DIET THAT MOVED THE DEFAULT ARM WOULD BE A DIET NOBODY MEASURED. The lean
// profile is a second shape and not a change to the shipped one, so every door
// promptprofile.go opens is asserted to be the IDENTITY on a frontier window —
// including the page door itself, which is proved on bytes: the composed page
// enters [renderSystemAt]'s output untouched, exactly as it did before there was
// a profile at all.
func TestAFrontierShapeIsUntouchedByTheProfile(t *testing.T) {
	agent := v3ShapedAgent(t)
	config := agent.config
	if config.promptProfile().lean() {
		t.Fatalf("the shipping conversation shape resolved to the lean profile, so every assertion below is about the wrong arm")
	}
	if window := config.promptWindow(); window < leanWindowThreshold {
		t.Fatalf("the frontier shape's window is %d, under the %d threshold: this test is not weighing a frontier shape", window, leanWindowThreshold)
	}

	// THE PAGE DOOR, ON BYTES. What the composer built is what the render
	// carries: no section dropped, no line added.
	composed := strings.TrimRight(promptWithBeltFacts(config), "\n")
	page := renderSystemAt(config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(page, composed) {
		t.Error("a frontier page is no longer the composed page byte for byte: the profile's cut is reaching the full arm")
	}
	if pointer := config.leanShelfPointer(); pointer != "" {
		t.Errorf("a frontier page is handed the lean shelf line %q", pointer)
	}

	// THE SHELF DOOR. The partition is the shipped table and nothing else, and
	// nothing is handed over rather than fetched.
	shelf := config.capabilityShelf()
	if len(shelf) != len(capabilityGroups) {
		t.Fatalf("a frontier belt partitions %d groups, want the shipped %d", len(shelf), len(capabilityGroups))
	}
	for index, group := range shelf {
		if group.name != capabilityGroups[index].name {
			t.Errorf("the frontier partition's group %d is %q, want %q", index, group.name, capabilityGroups[index].name)
		}
	}
	if armed := config.prearmedGroups(); len(armed) != 0 {
		t.Errorf("a frontier belt pre-arms %v; every group waits on the shelf as it always did", armed)
	}
	if !config.shelvesFact(beltFacts[0]) {
		t.Error("`ask` is no longer shelved on a frontier belt, so the page stopped telling it how to fetch one")
	}

	// AND THE TWO NUMBERS prompt.go reads through the profile.
	if got := config.instructionLimit(); got != agentsFileLimit {
		t.Errorf("a frontier prefix bounds the project's instructions at %d, want %d", got, agentsFileLimit)
	}
	if config.onlyOneInstructionFile() {
		t.Error("a frontier prefix quotes one instruction file; both have always ridden")
	}
}
