// The manual from the command line: every page this build carries, one page as
// it is written, or the sections that answer a question.
//
// It exists because the manual had exactly one reader and it was not the person.
// Everything aforge knows about itself was reachable only through the belt's
// `manual` tool — which is a model call, so it needs an API key, costs money on
// every lookup, and hands back a RETELLING that nobody can tell from an invented
// one. That is the exact failure internal/manual was written to prevent, and the
// questions people ask most are the ones they ask BEFORE any of that is set up:
// what is this, what does it cost, what can it do, who can see my files.
//
// So this command needs no key, makes no model call, opens no store, spends
// nothing, and prints the pages VERBATIM. A person reading the manual here is
// reading the manual.
//
// AND NOTHING HERE IS CUT SHORT. internal/manual's caps exist because a model
// pays for its context by the token; a terminal does not, and a page truncated
// on the one surface where the whole of it is free would be a budget wearing a
// reason it does not have. Paging is the terminal's job, not this command's.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// manualQuestionSections is how many sections a typed question is answered
// from. It is deliberately more than the four a model is handed
// ([manual.DefaultResults]): that four is a budget — a model handed a document
// quotes the wrong half of it — and nobody is paying for this one.
const manualQuestionSections = 8

// runManual reads the CHAT's corpus, never the resident's. This binary's
// command line is the door onto the program a person sits in front of, and the
// resident's pages describe an employee that keeps working while the terminal
// is closed; answering out of the wrong one would be fluent and wrong.
func runManual(args []string) error { return runManualWith(args, os.Stdout) }

func runManualWith(args []string, out io.Writer) error {
	asked := strings.TrimSpace(strings.Join(args, " "))
	switch {
	case asked == "", asked == "-h", asked == "--help":
		// The list IS this command's help: what it can be asked for is the set
		// of pages, so a person who typed the word they know from every other
		// tool gets the answer rather than a refusal.
		return writeManualPages(out)
	case strings.ContainsAny(asked, " \t"):
		return writeManualAnswer(out, asked)
	default:
		return writeManualPage(out, asked)
	}
}

// writeManualPages is the bare form: every page, one per line, name then the
// title the page gives itself. The two roads out of it are printed under the
// list rather than over it, so the names stay the first thing on the screen and
// stay pipeable.
func writeManualPages(out io.Writer) error {
	_, err := fmt.Fprintf(out, "%s\n\nread one with `aforge manual <page>`, or ask in your own words: aforge manual \"who can see my files\"\n", manual.Chat().Listing())
	return err
}

// writeManualPage prints one page exactly as it is written. A PAGE ASKED FOR BY
// NAME IS AN EXACT REQUEST AND GETS AN EXACT ANSWER OR AN EXACT REFUSAL — never
// a search that quietly returns something else, which would read as though the
// page existed. It is the same law the belt's manual tool keeps, and the
// refusal names every page there is because a person one letter away from the
// name they wanted should not have to guess at it twice.
func writeManualPage(out io.Writer, name string) error {
	text, found := manual.Chat().Page(name)
	if !found {
		return fmt.Errorf("there is no manual page named %q\n\nask in your own words to search instead — aforge manual \"who can see my files\" — or read one of these:\n\n%s",
			name, manual.Chat().Listing())
	}
	_, err := fmt.Fprintln(out, text)
	return err
}

// writeManualAnswer prints the sections that answer a question, each one
// labelled with the page and the heading it came from, so what is quoted can be
// traced back to the page that authorized it. It is [manual.RenderWhole] and
// not [manual.Render] — the same arrangement the model reads, with none of the
// model's cap under it, because nothing on a terminal has to be cut.
func writeManualAnswer(out io.Writer, question string) error {
	sections := manual.Chat().Search(question, manualQuestionSections)
	if len(sections) == 0 {
		// NOT A FAILURE, and the difference is the whole of the exit code: the
		// manual having nothing on a topic is a fact about aforge worth
		// reporting — it usually means the answer is "no, it does not do that"
		// — while a non-zero exit would read as a broken command and invite a
		// retry with rephrased words that will find nothing either. A page
		// asked for BY NAME and missing is the other case, and that one really
		// did fail.
		_, err := fmt.Fprintf(out, "the manual has nothing on that, which usually means aforge does not do it\n\n%s\n", manual.Chat().Listing())
		return err
	}
	_, err := fmt.Fprintln(out, manual.RenderWhole(sections))
	return err
}
