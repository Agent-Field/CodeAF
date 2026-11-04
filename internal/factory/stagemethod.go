package factory

// StageMethod leaves a person's replacement ask free of shipped instructions.
func StageMethod(st Stage) string {
	shipped, ok := ShippedStage(st.Name)
	if !ok || st.Ask != shipped.Ask {
		return ""
	}
	return stageMethods[st.Name]
}

var stageMethods = map[string]string{
	"plan":     "Follow the issue's code, tests and conventions before deciding. Change no files. Name assumptions and what would disprove them; order the smallest end-to-end slices, each leaving working behaviour, with public test interfaces and expected behaviour; name risks and exclusions. Give each planned file a reason. Finish with an implementable plan. Ask when competing readings need different designs and the code cannot settle them; otherwise name the simpler assumption.",
	"write":    "Work one slice at a time: write a test at a public interface, run it and observe the expected failure; write the least code that passes, then rerun it. For a bug, that failure is the reproduction: fix the cause where it lives. If no test can reach the behaviour, explain the gap and check directly where possible. Put unrelated discoveries in notes. If the plan is wrong, make and report small corrections. Ask when a different design is needed. Finish when all slices and their tests pass on the latest code.",
	"test":     "Find the repository's test commands. Reuse passing evidence for unchanged code. Run missing coverage for the change and behaviour it could break once; repeat after edits or an unresolved failure. Diagnose failures before editing; fix the cause or correct a wrong test with the reason, keeping it as strict. Preserve the checkout. Finish when relevant checks pass.",
	"review":   "Review and repair the diff, reading callers, error paths and tests. Judge scope against the issue and plan, separately from edges, failures and written standards. Findings need a concrete failing path or broken standard, file, line and severity: blocker or major. Put minors and taste in notes. Fix blockers and majors, adding and running regression tests for changed behaviour. Report findings and their disposition; count unresolved blockers and majors. Finish with none open. Name the riskiest path checked even in a clean review. Ask when a fix needs a product decision.",
	"security": "Trace untrusted input to use, authorization for changed actions and secrets reaching code, logs or errors. Findings need a reachable attack path, effect, file, line and severity. Put unsupported concerns and minors in notes. Repair vulnerabilities, adding and running regression tests where possible. Finish with none open. Ask when the intended trust boundary or permission is unclear.",
	"proof":    "Challenge each issue promise and earlier claim with an observation that could disprove it: run the test or use the program as a person would. Evidence is what you observed this round on the current checkout. Run only what the claims need. Leave unverified claims ok false with why. Ask when acceptance is ambiguous.",
	"read":     "Inspect without edits. Explain whether the change matches its description and linked issue. Trace the riskiest paths through callers and tests. Report risks worst first with file and reason, open questions and what you did not read.",
	"checks":   "For each claim choose what would disprove it, find the repository's check and run it. Report one claim per check with command and result. Leave unreachable claims ok false with why.",
	"bisect":   "Reproduce with one fast command failing for the reported reason. Confirm passing and failing revisions, then narrow if revision movement is permitted. Change no code; leave the checkout on its branch. If movement is refused, narrow using history and recorded runs, distinguishing them from your runs and naming revisions you did not run. Report a first failing change only with supporting results either side and the causal link; otherwise give the narrowed range and attempts.",
	"fix":      "Run the reproduction and observe the reported failure before editing. Find its cause, using the bisect result if available, and fix it there with the least change. Add a regression test failing without the fix if none exists. Run the reproduction to green, then surrounding tests. Weakening a check, adding retries or widening a limit requires evidence that the check, retry policy or limit caused the defect.",
}
