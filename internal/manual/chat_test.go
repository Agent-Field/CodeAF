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
		{"how do I start a task", "tasks"},
		{"can aforge parallelize my task", "tasks"},
		{"do tasks touch my working copy", "how-tasks-run"},
		// Written from a real run: a task that generated two images was landed
		// as "6 steps without progress" and the person had to go and find the
		// files themselves. Both halves are questions they then ask.
		{"why did my task stop for no progress", "how-tasks-run"},
		{"does generating an image count as progress", "how-tasks-run"},
		{"where did the files go when my task was stopped", "how-tasks-run"},
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

		// The fourth wave, and it is the one this build most needs to answer
		// out of a page rather than out of the model's imagination: aforge
		// carrying something from one conversation into the next. Every one of
		// these is a question somebody asks the first time they notice it
		// happening, and the honest answers — what is kept, who decides, and how
		// to empty it — are all on one page.
		{"how do I see what you remember about me", "what-i-remember"},
		{"how do I make you forget something", "what-i-remember"},
		{"can you remember my preferences for next time", "what-i-remember"},
		{"what happened to my memory.md file", "what-i-remember"},
		{"how do I turn memory off", "what-i-remember"},
		{"how do I see what aforge remembers", "what-i-remember"},
		{"how do I edit a memory", "what-i-remember"},
		{"how do I undo forgetting one", "what-i-remember"},
		{"where did a memory come from", "what-i-remember"},

		// The third wave: aforge changing a person's own settings for them.
		// Both halves have to reach a page — that it can, and the rows where
		// it will not — because the second is the answer somebody gets when
		// they ask for the first and are refused.
		{"can you change my settings", "what-i-can-do"},
		{"set my daily budget to 5", "what-i-can-do"},
		{"why won't you change my approval mode", "permissions"},
		{"what does the indented part mean", "keys"},
		{"how do I see what aforge did", "keys"},
		{"how do I keep everything expanded", "keys"},

		// The fifth wave: long-running commands. A person meets these at the
		// exact moment a build outruns its bound, so the answer has to be the
		// page and not the model's memory of what timeouts usually do.
		{"my command timed out was the work lost", "what-i-can-do"},
		{"does a command get killed when it takes too long", "what-i-can-do"},
		{"how do I send a running command to the background", "keys"},
		{"what does ctrl+g do", "keys"},
		{"tell me when the build stops changing", "what-i-can-do"},
		{"how do I know when something has finished", "what-i-can-do"},

		// The sixth wave: work that went wrong quietly. Every one of these is
		// asked with a screen full of something that looks finished and is not —
		// a row still saying "running" long after anything was, a worker that
		// announced a file it never wrote, the same brief sent out again and
		// again — and the honest answer has to come off a page, because the
		// model's own account of what happened is exactly what was wrong.
		{"why does my run still say running", "adaptive-runs"},
		{"does an adaptive run survive a restart", "adaptive-runs"},
		{"what happens to a run if aforge restarts", "adaptive-runs"},
		{"the run said it wrote a file but there is nothing there", "adaptive-runs"},
		{"why did it keep spawning the same worker over and over", "adaptive-runs"},
		{"what happens to a harness design if I restart", "saved-shapes-of-work"},
		{"does a design resume after a restart", "saved-shapes-of-work"},

		// The seventh wave, and it is written from one real report: a person
		// generated a book cover, got a row of text saying
		// `book/cover.jpg — 768×1376 jpeg, 776.9KB`, and could neither see the
		// picture nor find the file — the path was relative to a directory they
		// were not standing in. Pictures now draw themselves under the row, and
		// the path is absolute where they cannot. Both halves of what that
		// person then asked have to land on a page.
		{"why don't I see the image", "screen"},
		{"where did my generated image go", "screen"},
		{"you only gave me text, where is the picture", "screen"},
		// The eighth wave: the project's own record. The page holding it is
		// /history and it used to be /tasks, which collided with the three /task
		// rows that START work — so the words a person reaches for when they want
		// the record ("history", "old tasks", "previous sessions") have to land on
		// the pages that describe the page and the command, and never on the ones
		// about starting one.
		{"where is my task history", "tasks"},
		{"how do I see tasks from previous sessions", "tasks"},
		{"how do I search my old tasks", "tasks"},
		{"what did we do last week", "tasks"},
		{"is there a history command", "commands"},

		// These two are asked ABOUT THE PICTURE rather than about the screen, so
		// the making page is the right answer and says the same thing: the image
		// draws itself under the row, and the path it names is whole.
		{"can I see the picture you made without opening the row",
			"making-pictures-audio-and-video"},
		{"how do I find the file for the image you generated", "making-pictures-audio-and-video"},

		// The eighth wave, written from the complaint that produced the
		// feature: file paths in a reply looked like text, cmd-click caught
		// half of a wrapped one, and nothing said they were clickable at all.
		// A person meets this holding a mouse, so the words are the ones they
		// would say out loud.
		{"can I click a file path to open it", "screen"},
		{"how do I open a file from the chat", "screen"},
		{"why is a path underlined", "screen"},
		{"cmd click on a file name does nothing", "screen"},
		{"why is this file path not clickable", "screen"},

		// The ninth wave, from two things a person did with the landed build.
		// Clicking an old task only tagged it in the message box, when what they
		// meant was "let me in" — so the words for going into finished work have
		// to reach the page that now has a card behind them. And a column closed
		// with ctrl+g used to leave nothing on the frame at all, so the words
		// somebody says when a panel they can no longer see has gone have to
		// reach the page that says where it went.
		{"how do I see what an old task did", "tasks"},
		{"open a past task", "tasks"},
		// "read a finished task's report" is deliberately NOT pinned here. It is
		// the same question from the other side — a report is a thing a task
		// WRITES — and it lands on how-tasks-run, whose own section now names the
		// card and says where to open it. Retrieval was right and the page was
		// missing a sentence.
		{"the task bar disappeared how do I get it back", "tasks"},
		{"how do I bring back the right sidebar", "tasks"},

		// The tenth wave, from three things a person hit in one sitting. Two are
		// about a key that is bound and never arrives — the answer is which
		// terminal they are in, and it has to come off a page rather than out of a
		// model that will happily invent a setting. The third is asked looking at a
		// roster row whose name they did not write, and the honest answer is that a
		// model wrote it, on purpose, out of the call that was already running.
		{"delete a whole line", "keys"},
		{"cmd backspace does nothing", "keys"},
		{"why does my task have a weird name", "tasks"},
		{"who decides what my task is called", "tasks"},
		// And the wait itself: it used to sit there dead, so the words somebody
		// says while looking at it have to reach the page that says it is alive.
		{"is it stuck on shaping the brief", "tasks"},

		// The eleventh wave, from the one state people found genuinely stuck: a
		// task that lands "needs your look" and sits there. Three questions get
		// asked in front of it — what am I supposed to do, what does each answer
		// do, and can you just decide — and a fourth is asked about a family, when
		// somebody notices they are not being asked about the pieces.
		{"why is the task waiting for me", "tasks"},
		{"finished but needs your look", "tasks"},
		{"how do I accept a task", "tasks"},
		{"can the chat decide on its own", "tasks"},
		{"stop asking me about tasks", "tasks"},
		{"why am I not being asked about the sub tasks", "how-tasks-run"},

		// The twelfth wave, from the key that stopped doing what the habit
		// expects: ctrl+c no longer leaves on one press. Every one of these is
		// asked with a terminal that did not close, and the answer — press it
		// twice, inside a second and a half — has to come off a page rather than
		// out of a model that will confidently say one press quits.
		{"how do I quit", "keys"},
		{"how do I exit aforge", "keys"},
		{"how do I close aforge", "keys"},
		{"ctrl+c didn't quit", "keys"},
		{"why doesn't ctrl+c close it", "keys"},
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
