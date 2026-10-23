package run

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── WHAT A STEP IS TOLD ABOUT THE STEPS BEFORE IT ──────────────────────────
//
// THE PLAN IS THE PIPELINE'S MOST IMPORTANT HAND-OVER, AND IT ARRIVES WHOLE.
// Until 2026-10-20 every earlier step reached a brief as one line, `done · 3
// claims` and the first 120 characters of what it said, so a plan written the
// way the tool asks (in output) was cut to its opening sentence, the write
// step worked from the issue alone, and proof could not see what the steps
// before it had claimed. So a brief now carries, after the notes:
//
//   - the plan step's output as its own paragraph, up to [briefPlanMost];
//   - the files the plan names, for the write step, so the plan's list does
//     work rather than only being drawn on the job;
//   - every earlier step's counts and up to [briefStepMost] of what it said;
//   - every claim made so far with its step, ok or not, and its evidence,
//     which proof is told to confirm or correct rather than invent its own.
//
// ALL OF IT IS HELD TO [briefEarlierMost], THE OLDEST STEPS' WORDS CUT FIRST
// AND THE PLAN NEVER. A run of many steps cannot crowd the item out of the
// brief, and the step it most depends on is the one thing never shortened.
// Notes stay where they were (the `notes:` paragraph), carried whole.

// The caps, in characters.
const (
	// briefPlanMost is how much of the plan the brief carries: as much as of
	// the item's own body, so a plan is never cut shorter than the issue it
	// answers.
	briefPlanMost = briefBodyMost
	// briefStepMost is how much of what each other earlier step said.
	briefStepMost = 1000
	// briefClaimMost is how much of one claim's text, and of its evidence.
	briefClaimMost = 300
	// briefEarlierMost is the whole of what earlier steps hand this one: the
	// plan, the steps' lines and the claims.
	briefEarlierMost = 12000
)

// The words this file writes into a brief.
const (
	briefPlanHead   = "the plan (from the plan step):"
	briefFilesHead  = "files the plan names:"
	briefBeforeHead = "before this stage:"
	briefClaimsHead = "claims made so far:"
	// briefProofClaims heads the claims in a proof step's brief.
	briefProofClaims = "claims made so far; confirm or correct each in your own claims, rather than inventing new ones:"
	// briefPlanWrite is the plan step's own instruction: the plan goes where
	// the steps after it will read it.
	briefPlanWrite = "Write the plan in output: what you'll change, in which files, and how you'll know it works."
	// briefPlanAbove stands for the plan's words on its own line, which the
	// plan's paragraph already carries whole.
	briefPlanAbove = "its plan is above"
	// briefStepCut is said on a step whose words [briefEarlierMost] took.
	briefStepCut = "what it said is cut to fit this brief"
)

// The step names this file reads by. A stage's name is one word and two
// stages never share one ([factory.OneStageWord]), so at most one earlier step
// is the plan.
const (
	planStep  = "plan"
	writeStep = "write"
)

// planBrief is the plan step's instruction, "" for every other step.
func planBrief(job Job) string {
	if !strings.EqualFold(stageLabel(job.Stage), planStep) {
		return ""
	}
	return briefPlanWrite
}

// earlier is one step that ran before this one, as the brief names it.
type earlier struct {
	name   string
	result factory.StageResult
}

// earlierSteps is Prior with each result's step name ([Job.PriorNames]), or
// its place, `stage 2`, when the name is not known.
func earlierSteps(job Job) []earlier {
	out := make([]earlier, 0, len(job.Prior))
	for i, r := range job.Prior {
		name := ""
		if i < len(job.PriorNames) {
			name = oneLine(job.PriorNames[i])
		}
		if name == "" {
			name = "stage " + strconv.Itoa(i+1)
		}
		out = append(out, earlier{name: name, result: r})
	}
	return out
}

// handover is what the steps before this one hand it, as the brief's
// paragraphs in order: the plan, the files it names (write only), one entry
// per earlier step, and the claims made so far. Each is left out when it has
// nothing to say; nil before any step has run.
func handover(job Job) [][]string {
	steps := earlierSteps(job)
	if len(steps) == 0 {
		return nil
	}
	plan := -1
	for i, s := range steps {
		if strings.EqualFold(s.name, planStep) {
			plan = i
		}
	}

	var planPara []string
	if plan >= 0 {
		if out := strings.TrimSpace(steps[plan].result.Output); out != "" {
			head, text := briefPlanHead, cutAt(out, briefPlanMost)
			if text != out {
				head = "the plan (from the plan step; its first " + strconv.Itoa(briefPlanMost) + " characters, the rest cut):"
			}
			planPara = []string{head, text}
		}
	}

	// ONE ENTRY PER STEP: a head line with its counts, and what it said under
	// it. The plan's words are its own paragraph, so its entry points there.
	type entry struct {
		head string
		text []string
	}
	entries := make([]entry, len(steps))
	for i, s := range steps {
		e := entry{head: "- " + s.name + ": " + resultWords(s.result)}
		if i == plan && planPara != nil {
			e.head += " · " + briefPlanAbove
		} else {
			e.text = indented(cutAt(s.result.Output, briefStepMost))
		}
		entries[i] = e
	}

	// Every claim, oldest step first, each remembering whose it is so the
	// oldest are the ones cut.
	var claims []string
	for _, s := range steps {
		for _, cl := range s.result.Claims {
			if line := claimLine(s.name, cl); line != "" {
				claims = append(claims, line)
			}
		}
	}

	files := planFiles(job, steps, plan)
	claimsHead := briefClaimsHead
	if factory.IsProofStage(job.Stage) {
		claimsHead = briefProofClaims
	}

	// THE SIZE IS EVERY LINE THE CARRY WRITES, its heads included, so the cap
	// is what the brief really holds. The plan and the file list are counted
	// and never cut.
	size := chars(planPara...) + chars(files...) + chars(briefBeforeHead)
	for _, e := range entries {
		size += chars(e.head) + chars(e.text...)
	}
	if len(claims) > 0 {
		size += chars(claimsHead) + chars(claims...)
	}
	// THE OLDEST STEPS' WORDS GO FIRST, AND THE PLAN'S NEVER: each step's
	// text is taken whole, oldest first, until the carry fits; then, if it
	// still does not, the oldest claims, said as a count.
	for i := range entries {
		if size <= briefEarlierMost {
			break
		}
		if i == plan || len(entries[i].text) == 0 {
			continue
		}
		size -= chars(entries[i].text...)
		entries[i].text = nil
		entries[i].head += " · " + briefStepCut
		size += utf8.RuneCountInString(" · " + briefStepCut)
	}
	cut := 0
	for cut < len(claims) && size+chars(claimsCut(cut)...) > briefEarlierMost {
		size -= chars(claims[cut])
		cut++
	}

	var paras [][]string
	if planPara != nil {
		paras = append(paras, planPara)
	}
	if len(files) > 0 {
		paras = append(paras, files)
	}
	before := []string{briefBeforeHead}
	for _, e := range entries {
		before = append(before, e.head)
		before = append(before, e.text...)
	}
	paras = append(paras, before)
	if len(claims) > 0 {
		para := append([]string{claimsHead}, claimsCut(cut)...)
		paras = append(paras, append(para, claims[cut:]...))
	}
	return paras
}

// claimsCut is the line that stands for the n oldest claims the cap took,
// none when it took none.
func claimsCut(n int) []string {
	if n == 0 {
		return nil
	}
	return []string{"… " + plural(n, "earlier claim") + " cut to fit this brief"}
}

// planFiles is the write step's `files the plan names:` paragraph: the paths
// the finished plan declares ([factory.ParsePlanFiles], the same reading the
// job's planned files are drawn from), at most [briefFilesMost] of them. nil
// for every other step, for a plan that did not finish, and for one that
// declared no list.
func planFiles(job Job, steps []earlier, plan int) []string {
	if plan < 0 || !steps[plan].result.Done || !strings.EqualFold(stageLabel(job.Stage), writeStep) {
		return nil
	}
	files := factory.ParsePlanFiles(steps[plan].result.Output)
	if len(files) == 0 {
		return nil
	}
	lines := []string{briefFilesHead}
	for i, f := range files {
		if i == briefFilesMost {
			lines = append(lines, "… "+plural(len(files)-i, "more file"))
			break
		}
		lines = append(lines, "- "+f)
	}
	return lines
}

// claimLine is one claim as the brief lists it: `- write · ok · refunds count
// once · evidence: TestRefund (test)`, its text and evidence each cut to
// [briefClaimMost]. "" for a claim with no text.
func claimLine(step string, cl factory.Claim) string {
	text := cutAt(oneLine(cl.Text), briefClaimMost)
	if text == "" {
		return ""
	}
	state := "not ok"
	if cl.OK {
		state = "ok"
	}
	line := "- " + step + " · " + state + " · " + text
	if ev := cutAt(oneLine(cl.Evidence), briefClaimMost); ev != "" {
		line += " · evidence: " + ev
		if m := oneLine(cl.Medium); m != "" {
			line += " (" + m + ")"
		}
	}
	return line
}

// indented is text as lines under an entry's head, two spaces in, blank lines
// left out so one step's words stay one block.
func indented(text string) []string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimRight(l, " \t\r"); strings.TrimSpace(l) != "" {
			lines = append(lines, "  "+l)
		}
	}
	return lines
}

// cutAt is s trimmed and at most most characters, cut at a rune and marked
// with ` …` when cut.
func cutAt(s string, most int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= most {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:most])) + " …"
}

// chars is how many characters lines take in the brief, a line break each.
func chars(lines ...string) int {
	n := 0
	for _, l := range lines {
		n += utf8.RuneCountInString(l) + 1
	}
	return n
}
