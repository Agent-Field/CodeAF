package head

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// What the turn is DOING, said out loud while it does it.
//
// THE DEFECT, in the reader's words: while the head runs its tool loop the
// person watches one pulsing line that says `thinking` and nothing else. A turn
// that reads the board, opens two results, opens the file one of them wrote and
// then commissions work spends most of a minute looking — from the outside —
// exactly like a turn that has hung. The tokens the model streams are the only
// thing the surface ever hears about, and between two tool calls there are no
// tokens.
//
// So the loop narrates itself. Around every belt call it emits two boundaries on
// the SAME channel the tokens ride ([provider.Emit]): a begin carrying a
// person-readable gloss of the call, and an end carrying its outcome and, where
// it is cheap, a short hint about what came back. The surface interleaves them
// with the words, and when the turn lands it folds them into one row.
//
// TWO LAWS ON THE GLOSS, and both of them are about not leaking the machine.
//
//   - IT IS NEVER THE ARGUMENTS. `{"q":"navctx","status":"running"}` is the
//     shape 5.14 calls the never-shown tier: a reader who wanted to know what a
//     JSON object looked like would not be waiting for an answer. Every gloss
//     here is a sentence with a verb in it.
//   - IT IS NEVER A LIE ABOUT WHAT WAS ASKED. A gloss that cannot find the
//     argument it wanted falls back to the plain shape of the call ("looking
//     something up") rather than inventing a subject — the same rule the rest of
//     this surface calls honest degradation.
//
// The vocabulary is the person's, not the belt's: `board` is "looking at the
// work", `recall` is "searching", `task` is "putting work in hand". §14's ban on
// machinery vocabulary applies to a line a person reads, and this is one.

// executeWatched runs one belt call with the turn narrating it.
//
// It is the ONE seam. Both loops that run this belt — the ordinary turn
// (loop.go) and the delivery absorption that answers when work lands
// (absorb.go) — call it instead of [beltRun.execute], so the activity a person
// sees is the activity that happened, in both directions, with no second
// emitter to keep in step. Everything about the call itself is unchanged: same
// arguments, same result, same failure flag.
//
// A context with no observer on it — every headless run, every test that does
// not ask — pays two context lookups and emits nothing.
func (run *beltRun) executeWatched(ctx context.Context, name, arguments string) (string, bool) {
	provider.Emit(ctx, provider.StreamToolBegin, toolGloss(name, arguments))
	result, failed := run.execute(name, arguments)
	kind := provider.StreamToolEnd
	if failed {
		kind = provider.StreamToolFailed
	}
	provider.Emit(ctx, kind, toolHint(name, result, failed))
	return result, failed
}

// toolGloss is the one gloss table: what this call is, in a person's words.
//
// One function rather than a method per tool, because the table IS the
// vocabulary and a vocabulary spread across twenty files is one nobody can read
// in one sitting. The default arm is the honest floor: a tool this table has
// never heard of is glossed by its own name, which is at least true.
func toolGloss(name, arguments string) string {
	args := glossArgs(arguments)
	switch name {
	case beltToolBoard:
		if id := glossArg(args, "id"); id != "" {
			return "looking at " + quoteGloss(id)
		}
		if q := glossArg(args, "q"); q != "" {
			return "looking through the work for " + quoteGloss(q)
		}
		return "looking at the work"
	case beltToolSearch, beltToolRecall:
		if q := glossArg(args, "q"); q != "" {
			return "searching for " + quoteGloss(q)
		}
		return "searching what was said"
	case beltToolTask:
		if words := glossArg(args, "instruction"); words != "" {
			return "putting work in hand: " + quoteGloss(words)
		}
		return "putting work in hand"
	case beltToolChange:
		if target := glossArg(args, "target"); target != "" {
			return "changing " + quoteGloss(target)
		}
		return "changing something already under way"
	case beltToolStop:
		return "withdrawing work"
	case beltToolManual:
		if page := glossArg(args, "page"); page != "" {
			return "reading the manual on " + quoteGloss(page)
		}
		return "reading aforge's own manual"
	case beltToolResult:
		// The subject rides at the END of every gloss here, and it is a grammar
		// rule rather than a preference: the summary row downstream says what
		// KIND of act a step was by cutting the gloss at its subject, so a gloss
		// that keeps words after its subject loses them and is left dangling —
		// "reading what «task-8» came back with" cut at the quote is "read what",
		// which is not a phrase. Subject last, and the cut is clean.
		if id := glossArg(args, "id"); id != "" {
			return "reading what came back from " + quoteGloss(id)
		}
		return "reading what came back"
	case beltToolPlan:
		if job := glossArg(args, "job"); job != "" {
			return "reading the plan for " + quoteGloss(job)
		}
		return "reading the plan"
	case beltToolRead:
		if file := glossArg(args, "file"); file != "" {
			return "reading " + quoteGloss(file)
		}
		if job := glossArg(args, "job"); job != "" {
			return "reading a file from " + quoteGloss(job)
		}
		return "reading a file"
	case beltToolOpen:
		if id := glossArg(args, "id"); id != "" {
			return "opening " + quoteGloss(id)
		}
		return "opening what was found"
	case beltToolCompetence:
		return "checking what it is good at"
	case beltToolStanding:
		return "checking what is standing"
	case beltToolSpending:
		return "checking the spend"
	case beltToolHistory:
		return "looking back over what was done"
	case beltToolThread:
		return "reading an earlier conversation"
	case beltToolStatus:
		return "checking where things stand"
	case beltToolNote:
		return "writing something down to remember"
	case beltToolForget:
		return "letting something go"
	case beltToolWrite:
		if name := glossArg(args, "name"); name != "" {
			return "writing " + quoteGloss(name)
		}
		return "writing a document"
	case beltToolBash:
		// The one gloss that quotes something the MACHINE would read, and it is
		// allowed because a shell command is exactly what the person at the
		// keyboard would have typed themselves — the tool's own boundary
		// (bash.go). It is still their vocabulary, not the belt's.
		if command := glossArg(args, "command"); command != "" {
			return "running " + quoteGloss(command)
		}
		return "doing it now"
	case beltToolAnswerQuestion:
		return "answering a worker's question"
	case beltToolSay:
		return "saying something"
	case beltToolAsk:
		return "asking a question"
	case beltToolInterrupt:
		return "stopping the turn"
	}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		// A tool the table has never heard of. Its NAME is the only honest thing
		// left, and it is still not the arguments.
		return trimmed
	}
	return "working"
}

// toolHint is the short thing worth saying about how a call came back.
//
// CHEAP OR NOTHING. It is allowed to count rows and to name a file, because both
// are already in front of it; it is not allowed to summarize, because a summary
// of a tool result is a second answer competing with the one the turn is about
// to give. Almost every call ends with an empty hint, which renders as a settled
// line and no words — which is the honest picture of "that worked".
func toolHint(name, result string, failed bool) string {
	trimmed := strings.TrimSpace(result)
	if failed {
		// A failure says the FIRST line of what went wrong and never the whole
		// error: the loop is about to speak, and this row is a witness rather
		// than a report.
		return clipGloss(firstLineOf(strings.TrimPrefix(trimmed, "ERROR: ")), toolHintCap)
	}
	switch name {
	case beltToolBoard, beltToolSearch, beltToolRecall, beltToolHistory:
		if rows := countGlossRows(trimmed); rows > 0 {
			return strconv.Itoa(rows) + " " + plural(rows, "row", "rows")
		}
		return "nothing"
	case beltToolRead, beltToolWrite:
		if trimmed == "" {
			return ""
		}
		return sizeGloss(len(trimmed))
	}
	return ""
}

// toolHintCap is how much of a failure's own words the row carries. One clause.
const toolHintCap = 60

// glossArgs decodes a call's arguments defensively. A model mid-stream can hand
// over anything at all, and a gloss is a decoration: it never fails, it just has
// less to say.
func glossArgs(arguments string) map[string]any {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		return nil
	}
	return args
}

// glossArg reads one string argument, flattened to a single line and clipped to
// a phrase. The clip is what keeps the promise the row makes: one line, at the
// tier of a whisper, whatever the model passed.
func glossArg(args map[string]any, key string) string {
	value, ok := args[key].(string)
	if !ok {
		return ""
	}
	return clipGloss(firstLineOf(value), glossArgCap)
}

// glossArgCap is how much of an argument survives into the gloss. It is a
// PHRASE and not a sentence: the row it lands on is dim, indented and one of
// several, and the surface truncates at its own measure on top of this.
const glossArgCap = 48

func firstLineOf(text string) string {
	text = strings.TrimSpace(text)
	for _, cut := range []string{"\n", "\r"} {
		if index := strings.Index(text, cut); index >= 0 {
			text = text[:index]
		}
	}
	return strings.TrimSpace(text)
}

// clipGloss shortens on a word boundary and marks that it did. It counts RUNES
// rather than bytes, so a clip never lands inside a character.
func clipGloss(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)[:limit]
	if space := strings.LastIndexByte(string(runes), ' '); space > limit/2 {
		runes = []rune(string(runes)[:space])
	}
	return strings.TrimRight(strings.TrimSpace(string(runes)), ",.;:") + "…"
}

// quoteGloss puts the person's own subject in the guillemets this product
// quotes with, so a gloss reads as "searching for «navctx»" rather than as a
// sentence that has swallowed a search term.
func quoteGloss(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "«" + text + "»"
}

// countGlossRows counts the non-empty lines of a rendered read. It is the
// cheapest true thing that can be said about a board or a search: how much came
// back. It stops counting at a bound, because a hint is a small number and a
// scan of a large result would be paid inside the tool loop.
func countGlossRows(text string) int {
	if text == "" {
		return 0
	}
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows++
		if rows >= glossRowCap {
			break
		}
	}
	return rows
}

// glossRowCap bounds the count above. Past it the number stops being news.
const glossRowCap = 999

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// sizeGloss says how much text came back, in the units a person reads.
func sizeGloss(bytes int) string {
	if bytes < 1024 {
		return strconv.Itoa(bytes) + " chars"
	}
	return strconv.Itoa((bytes+512)/1024) + " KB"
}
