package manual

import "testing"

// RETRIEVAL IS THE FEATURE, NOT THE PAGES.
//
// The chat manual is only worth having if a question asked in a person's own
// words reaches the page that answers it. Pages that are complete and correct
// and unreachable are pages the chat will talk over the top of, improvising
// exactly as it would with no manual at all.
//
// So the corpus is tested the way it is used: real questions, in the words
// somebody would actually type, against the number of sections the belt tool
// actually hands the model ([DefaultResults]). A question that stops reaching
// its page is a regression in the manual even when every page still reads well,
// and it is usually fixed by writing the asker's words into a heading rather
// than by touching this list.
func TestTheChatManualAnswersTheQuestionsPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what can you do", "what-i-can-do"},
		{"can you read a pdf file", "what-i-can-do"},
		{"can you search the web", "what-i-can-do"},
		{"do you remember me between conversations", "what-i-can-do"},
		{"how do I undo my last message", "sessions-and-rewind"},
		{"does rewind undo my files", "sessions-and-rewind"},
		{"can I open two terminals in the same folder", "sessions-and-rewind"},
		{"where are my conversations saved", "sessions-and-rewind"},
		{"what slash commands are there", "commands"},
		{"how do I export this conversation", "commands"},
		{"what does ctrl+b do", "keys"},
		{"how do I attach a screenshot", "keys"},
		{"how do I stop it mid answer", "keys"},
		{"why did it ask permission before running that", "permissions"},
		{"what does always mean when I press a", "permissions"},
		{"how do I connect my google account", "accounts"},
		{"which services can you connect to", "accounts"},
		{"can I run this on my dev box over ssh", "running-on-another-machine"},
		{"how many tasks can run at once", "tasks"},
		{"how do I stop a running task", "tasks"},
		{"do tasks touch my working copy", "how-tasks-run"},
		{"what does this conversation cost", "models-and-cost"},
		{"how do I switch model", "models-and-cost"},
		{"what happens when the conversation gets too long", "models-and-cost"},
		{"does it work on a narrow phone width terminal", "screen"},
		{"why is my table cut off", "screen"},
		{"what is a harness", "saved-shapes-of-work"},
		{"how do I start aforge", "starting-aforge"},

		// The second wave, added after probing the corpus the way it is
		// actually queried. Each of these reached the wrong page until the
		// asker's own words were written into a heading, which is the fix this
		// test is meant to provoke.
		{"can you delete files", "what-i-can-do"},
		{"can you run tests for me", "what-i-can-do"},
		{"can you start a dev server", "what-i-can-do"},
		{"how do I see what a background job printed", "what-i-can-do"},
		{"do you ask before running rm", "permissions"},
		{"what is yolo mode", "permissions"},
		{"how do I make it stop asking every time", "permissions"},
		{"what model is it using right now", "models-and-cost"},
		{"how do I give it a longer context", "models-and-cost"},
		{"how do I make it think harder", "models-and-cost"},
		{"why is it slow over ssh", "running-on-another-machine"},
		{"does it support markdown tables", "screen"},
		{"how do I copy text out", "keys"},
		{"can I turn off the mouse", "keys"},
		{"can you look at a screenshot I paste", "keys"},
		{"where do I change settings", "commands"},
		{"what is openaf", "starting-aforge"},
		{"can you access my email", "accounts"},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		if len(found) == 0 {
			t.Errorf("%q reaches nothing in the chat manual", ask.question)
			continue
		}
		var reached bool
		for _, section := range found {
			if section.Page == ask.page {
				reached = true
				break
			}
		}
		if !reached {
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
}

// The two corpora must stay strangers. This is the package-level half of the
// same law internal/session tests from the belt side: a chat page and a
// resident page may never share a name, because a name is how a page is asked
// for by hand and one name reaching two products is a coin toss.
func TestTheTwoCorporaShareNoPageName(t *testing.T) {
	resident := map[string]bool{}
	for _, name := range Pages() {
		resident[name] = true
	}
	for _, name := range Chat().Pages() {
		if resident[name] {
			t.Errorf("page %q exists in both the resident and chat manuals", name)
		}
	}
}
