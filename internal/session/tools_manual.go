package session

// The manual: what this chat knows about ITSELF.
//
// Every other tool on the belt reaches outward — a file, a page, a process, an
// account. This one reaches inward, and it exists because of a failure mode
// none of the others have. Asked "can you read a PDF?" or "what does rewind
// undo?", a model will always produce a fluent answer, and a fluent answer
// about the product is indistinguishable from a remembered one right up until
// the person acts on it. A WRONG ANSWER ABOUT AFORGE IS WORSE THAN NO ANSWER,
// because the person cannot check it against anything: they asked precisely
// because they did not know.
//
// So the pages are written from the code, they ship inside the binary
// (internal/manual's chat/ folder), and completeness tests fail the build when a
// landed feature has no page. What the model reads here is the same text a
// person would read, which is the only arrangement where the answer and the
// product cannot drift apart.
//
// IT IS A READ AND RECORDS NOTHING. Looking something up is not an event in the
// conversation; it leaves no journal line and no memory, exactly as grep does.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// manualSections is how much of the manual one read returns. It is the same
// bargain the search tools make: enough to answer a question and its obvious
// follow-up, not so much that the answer becomes a quotation contest between
// four pages that all mention the word.
const manualSections = 4

// manualDescription is what makes the model reach for this instead of
// improvising, so it says the quiet part out loud: you do not know this, and
// what you would produce instead is a guess.
//
// It is also prompt text billed on EVERY request of every turn, so it says that
// in the fewest words that still carry it. The sentence naming the two arguments
// went with the diet — the schema below names them, at the moment the model is
// choosing between them — and the schema itself is now compact rather than
// pretty-printed, which is how the rest of this package writes one.
const manualDescription = "Read aforge's own manual: what this chat does, how a mechanism works, what a command or key does. THE ONLY AUTHORITATIVE SOURCE about aforge - your training data lacks this program, so memory produces fiction. Look it up and say you did."

const manualSchemaJSON = `{"type":"object","properties":{"query":{"type":"string","description":"What you want to know, in the person's words"},"page":{"type":"string","description":"A whole page by name instead of searching"}}}`

// manualTool is the belt's window onto [manual.Chat]. The corpus it reads is
// the CHAT's, never the resident's: this program is a conversation you sit in
// front of, and the resident's pages describe an employee that keeps working
// while the terminal is closed. Answering out of the wrong one would be fluent
// and wrong, which is the exact failure this tool exists to prevent.
func (a *Agent) manualTool() bare.Tool {
	return bare.Tool{
		Name:        "manual",
		Description: manualDescription,
		Schema:      json.RawMessage(manualSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Query string `json:"query"`
				Page  string `json:"page"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			pages := manual.Chat().Pages()

			// A named page is an exact request and gets an exact answer or an
			// exact refusal — never a search that quietly returns something
			// else, which would read as though the page existed.
			if name := strings.TrimSpace(parsed.Page); name != "" {
				text, found := manual.Chat().Page(name)
				if !found {
					return "There is no manual page named " + name + ". The pages are: " +
						strings.Join(pages, ", "), true, nil
				}
				return text, false, nil
			}

			query := strings.TrimSpace(parsed.Query)
			if query == "" {
				return "Give either a query or a page. The pages are: " +
					strings.Join(pages, ", "), true, nil
			}
			sections := manual.Chat().Search(query, manualSections)
			if len(sections) == 0 {
				// NOT AN ERROR, and the difference matters: the manual having
				// nothing on a topic is a fact about aforge worth reporting to
				// the person — it usually means the answer is "no, it does not
				// do that" — while an error would invite a retry with rephrased
				// words that will find nothing either.
				return "The manual has nothing on that, which usually means aforge does not do it. The pages are: " +
					strings.Join(pages, ", "), false, nil
			}
			return manual.Render(sections), false, nil
		},
	}
}
