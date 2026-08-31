package provider

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The fourth failure plane: a 200 whose content is the model's own tool
// grammar.
//
// Some endpoints serve a model without parsing its chat template's tool-call
// tokens, and the model — asked to call a tool — writes the call in its
// private syntax as plain text. Nothing else in the harness owns that reply:
// the babble watch sees varied text, the ladder sees a well-formed 200, the
// nudge ceiling sees fresh progress. On 2026-08-31 a deepseek endpoint did
// exactly this and the turn re-asked the same question every few minutes,
// rendering the same markup to the person each time.
//
// NOTHING HERE MATCHES ANY PROVIDER'S SYNTAX. The session's own fixture law
// (checkpoint_test.go's dsmlSentinel) already states why: a pattern for
// today's sentinel breaks on the next provider's — one of which fences its
// tokens in fullwidth bars, another in bracketed ASCII words. What every leak
// has in common is structural, and it is the only thing read here: the reply
// came back with no tool call on it, yet it SPELLS THE NAME OF A TOOL THE
// REQUEST DECLARED, walled in symbol runes rather than sitting in a sentence,
// inside text that is mostly symbols. A model talking ABOUT a tool writes its
// name in prose; only a model talking IN its tool grammar writes it fenced.

// machineryDensityFloor is the fraction of symbol runes above which a reply
// reads as grammar rather than language. The live leak measures about a third;
// ordinary prose sits under a twentieth, and even prose thick with inline JSON
// stays under a tenth. A tenth sits in the gap.
const machineryDensityFloor = 0.10

// MachineryLeak reports that a reply is the model's own tool grammar written as
// text: the request declared tools, the answer called none, and the content is
// symbol-dense text that spells a declared tool's name fenced in symbol runes.
//
// IT NEVER SPEAKS WHERE A FENCE IS OPEN. A reply that carries ``` anywhere is
// showing code on purpose — a person asked what a tool call looks like, and
// the honest answer is symbol-dense and names a tool. The cost of that
// conservatism is one failure mode deliberately kept: a leak that happens to
// emit a code fence goes uncut and the person sees it — which is exactly the
// old behaviour, not a regression.
func MachineryLeak(request *ai.Request, response *ai.Response) bool {
	if request == nil || len(request.Tools) == 0 || answeredWithToolCalls(response) {
		return false
	}
	text := responseText(response)
	if text == "" || strings.Contains(text, "```") {
		return false
	}
	if symbolDensity(text) < machineryDensityFloor {
		return false
	}
	for _, tool := range request.Tools {
		if name := strings.TrimSpace(tool.Function.Name); name != "" && fencedInSymbols(text, name) {
			return true
		}
	}
	return false
}

// responseText is the reply's own words and only its words: reasoning is the
// pass before the answer, and tool arguments are structure, so neither belongs
// in a reading of what the person was shown.
func responseText(response *ai.Response) string {
	if response == nil || len(response.Choices) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range response.Choices[0].Message.Content {
		b.WriteString(part.Text)
	}
	return strings.TrimSpace(b.String())
}

// machinerySymbol is the property that makes a rune fence material rather than
// language: not a letter, not a digit, not spacing, and not the underscore
// that lives inside tool names. It is a PROPERTY and not a list, because the
// live leak fences its tokens in fullwidth bars a byte set would never hold.
func machinerySymbol(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) && r != '_'
}

func symbolDensity(text string) float64 {
	total, symbols := 0, 0
	for _, r := range text {
		total++
		if machinerySymbol(r) {
			symbols++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(symbols) / float64(total)
}

// fencedInSymbols reports the name appearing with symbol runes as BOTH
// immediate neighbours — `_query>` and `｜web_search>` count, `use web_search`
// and `web_search is` do not, because prose puts spacing on at least one side.
// Underscores on either flank are walked through: grammars decorate names
// (`_web_search`) and the decorated spelling is still the declared name and
// nothing like a sentence.
func fencedInSymbols(text, name string) bool {
	for from := 0; ; {
		at := strings.Index(text[from:], name)
		if at < 0 {
			return false
		}
		at += from
		// The edge of the text reads as spacing, not as fence material: a
		// reply that merely opens or closes on the bare name is how prose can
		// start too.
		before, sizeBefore := utf8.DecodeLastRuneInString(strings.TrimRight(text[:at], "_"))
		after, sizeAfter := utf8.DecodeRuneInString(strings.TrimLeft(text[at+len(name):], "_"))
		if sizeBefore > 0 && sizeAfter > 0 && machinerySymbol(before) && machinerySymbol(after) {
			return true
		}
		from = at + len(name)
	}
}
