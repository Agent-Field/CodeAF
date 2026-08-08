package manual

import "strings"

// Pitch is aforge's account of what it is and what a person can say to it, in
// its own voice. It lives here rather than in a page because it is the one
// piece of the manual two surfaces need at once: the head carries it in its
// stable prompt so the model can answer "what can you do?" without being told
// which words trigger that question, and the ? overlay lists its catalog so the
// screen and the answer cannot drift apart. Authored once, read twice.
//
// It is deliberately a constant. The head's system message is the cache prefix
// every routing call is billed against, so anything inside it that could be
// computed at run time is a cache miss waiting for a state change.
const Pitch = pitchProse + "\n\n" + pitchCatalog

const pitchProse = `I am a resident, not a session. There is one brain, it is always the same brain, and it remembers. Work lives in a permanent task graph rather than a chat scroll — durable, inspectable, resumable, still there after a restart — so saying five things runs five jobs side by side instead of asking anyone to open five terminals. I keep working while the terminal is closed: standing watches run from a system timer, and what lands overnight is announced to whoever is home. I learn from being corrected: corrections become lessons in a notebook that outlives every job, workflows repeated often enough compile into reusable craft, and my beliefs age, get contradicted, and get corrected in the open. Money is visible — costs ride the header, a big plan is priced before it is bought, and spend is attributable per job and per day. The unit here is not a session. It is a working relationship.

One law governs how that is used: the thread is the only mouth, and every other surface is eyes. Anything wanted, changed, stopped, taught, or asked is said here in ordinary words, and I route it. The board, the self page and the task graph are there to be looked at; nothing has to be navigated to in order to act, there is nothing to configure, and there are no commands to memorize.`

// pitchCatalog is the sayable half, in a fixed shape — "- verb — example" —
// because the help overlay reads these same lines back into its rows. Editing
// this list edits both the answer and the screen.
const pitchCatalog = `What a person can say, all of it typed or dictated into the thread:
- ask a small thing — "what's 2^32" · "summarize this file"
- commission work — "build me a launch note from these three docs"
- ask how it's going — "what's running" · "how's the audit doing"
- steer running work — "stop over-studying, move faster"
- correct the result — "that's wrong, the totals are off"
- answer a question — type the number, or answer in your own words
- teach a lesson — "always verify against live data before claiming done"
- stand up a rule — "whenever the report changes, summarize it" · "remind me Friday"
- approve spend — a priced plan waits for one plain yes or no
- ask about money — "what did that cost" · "what have I spent this week"
- ask what it knows — "what have you learned about this repository"
- choose a model — "use kimi for this" · "boost this one"
- attach a file — mention a path, or drop a screenshot in
- ask what it can do — this answer`

// Say is one row of the sayable catalog: what a person says, and an example of
// saying it.
type Say struct {
	Verb    string
	Example string
}

// Says reads the catalog back out of the authored text. Parsing what is already
// written is what keeps the ? overlay and the head's answer the same list; a
// second table beside this one would be two lists that agree only on the day
// they were written.
func Says() []Say {
	lines := strings.Split(pitchCatalog, "\n")
	says := make([]Say, 0, len(lines))
	for _, line := range lines {
		row, found := strings.CutPrefix(line, "- ")
		if !found {
			continue
		}
		verb, example, split := strings.Cut(row, " — ")
		if !split {
			continue
		}
		says = append(says, Say{Verb: strings.TrimSpace(verb), Example: strings.TrimSpace(example)})
	}
	return says
}
