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
		{"how do I open my tasks on a phone", "tasks"},
		{"how do I get back from a task on my phone", "tasks"},
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
		// The wave that made a dropped file attach: the words people use for it
		// are "drag", "drop" and the token they then find in their own sentence.
		{"can I drag and drop an image into the message box", "keys"},
		{"what does image #1 in my message mean", "keys"},
		{"why did the path I pasted turn into a token", "keys"},
		{"where do I change settings", "commands"},
		{"what is openaf", "starting-aforge"},
		{"can you access my email", "accounts"},

		// The eighth wave: the chat knowing about the person's OTHER terminals.
		// Both of these were asked of a build where the answer was "no", and
		// both are asked again the moment it becomes yes — a person with three
		// windows open on one repository wants to know whether the one in front
		// of them is working from a picture of the world that includes the
		// others.
		{"do you know what my other windows are doing", "tasks"},
		{"will you notice work from another terminal", "tasks"},
		{"what else is running on this project right now", "tasks"},
		{"what tasks are running outside this chat", "tasks"},
		{"what is running in my other projects", "tasks"},

		// The ninth wave: one terminal, several conversations. The first two are
		// asked by somebody who read the old refusal and wants to know whether
		// it still stands; the rest are the four things a person does with the
		// feature the moment they find it.
		{"open another project from home", "home"},
		{"enter does nothing on home", "home"},
		{"why does it say elsewhere", "home"},
		{"can I work on two repos in one terminal", "home"},
		{"can I work on two projects at once", "home"},
		{"how do I switch back to the last conversation", "home"},
		{"how do I switch to my other chat", "home"},
		{"is my other conversation still running", "home"},
		{"does my draft move when I switch", "home"},
		{"does closing one conversation quit aforge", "commands"},
		{"how do I close just this chat", "commands"},
		{"will ctrl+c kill my other project's tasks", "keys"},
		{"does my approval question expire while I am in another chat", "permissions"},

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
		{"why did it say superseded", "what-i-remember"},
		{"does it know if a memory actually helped", "what-i-remember"},

		// The eleventh wave: the words are no longer only carried, they can be
		// SEARCHED. Somebody asking either of these is asking about the
		// conversation itself rather than about a task that ran, and the answer
		// is the page that names the tool — never the task history, which is a
		// record of work and not of what was said.
		{"what did we decide last week", "what-i-remember"},
		{"search my old conversations", "what-i-remember"},

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
		// The naming wave. A row that shows a path or the front of somebody's
		// sentence is the thing people actually see, and they say it in the words
		// of what is on screen — a folder, a file path, "the first few words" —
		// long before they would say "title".
		{"my task is named after a folder path", "tasks"},
		{"the task on the right is called /var/folders", "tasks"},
		{"why is the task named the first few words of what I typed", "tasks"},
		{"the name on the task changed by itself a few seconds later", "tasks"},
		{"can I give a task a short name", "tasks"},
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
		// The thirteenth wave: asking the record. Every task leaves a journal of
		// what it actually did, and the chat reads it to answer why a piece of
		// old work went the way it did — so the words somebody says in front of a
		// landed task they do not understand have to reach the page that says
		// this is a question they may simply ask.
		{"why did the auth task pin the clock", "tasks"},
		{"what exactly did that task change", "tasks"},
		{"can I ask you about old work", "tasks"},
		{"how do I find out what a task actually did", "tasks"},

		// The fourteenth wave: the preflight warning. It is the first thing on
		// this surface that says something about ANOTHER window while you are
		// deciding about your own work, so a person meets it having never been
		// told the feature exists — and the words they reach for are about the
		// warning, not about the machinery behind it.
		{"why did it warn me about another window", "tasks"},
		{"two windows working on the same files", "tasks"},

		// The fifteenth wave: the crew that looks like it did nothing. `/crew max`
		// writes four class models and the session picks them up on its next
		// call — and the model on the status line does not move, because that one
		// is the CONVERSATION's and the crew never touches it. So a person reads
		// the confirmation, looks at a frame that says exactly what it said
		// before, and asks these in front of it.
		{"I changed the crew but the model didn't change", "models-and-cost"},
		{"why does the bottom still show the old model after /crew", "models-and-cost"},
		{"does /crew change the model I'm talking to", "models-and-cost"},

		{"how do I quit", "keys"},
		{"how do I exit aforge", "keys"},
		{"how do I close aforge", "keys"},
		{"ctrl+c didn't quit", "keys"},
		{"why doesn't ctrl+c close it", "keys"},

		// The sixteenth wave: the reply that came apart. A real conversation on
		// 2026-08-20 watched deepseek-v4-pro collapse twice at 150k tokens —
		// paragraphs of mixed-alphabet soup, then several thousand repetitions of
		// one line — and both were recorded and re-sent. aforge now cuts a reply
		// like that, drops it everywhere including the screen, and asks again. So
		// a person watches an answer they were reading VANISH, sees words they
		// have never seen before in its place, and asks these. The two halves are
		// on two pages on purpose: what is on the screen is the screen's, and what
		// the model did is the model's.
		{"why did the reply restart", "screen"},
		{"it says trying again", "screen"},
		{"where did the answer that was on screen go", "screen"},
		{"the text it was writing disappeared", "screen"},
		{"stuck on waiting for the model", "screen"},
		{"the model was printing garbage", "models-and-cost"},
		{"the reply came back as gibberish", "models-and-cost"},
		{"it started repeating the same line over and over", "models-and-cost"},
		{"how do I turn off the reply guard", "models-and-cost"},
		{"the model stopped answering halfway through", "models-and-cost"},

		// The eleventh wave: the ambient side — the things a conversation leaves
		// behind that keep working after the window is closed. Every one of
		// these is said in the ordinary words somebody uses when they are NOT
		// asking for work now, which is the whole recognition problem: "run the
		// tests" is a turn, and "run the tests whenever I push" is one of these.
		{"remind me at 6 to leave", "keeping-an-eye"},
		{"tell me when ci goes red", "keeping-an-eye"},
		{"can you run something every monday morning", "keeping-an-eye"},
		{"does it keep working when I close the terminal", "keeping-an-eye"},
		{"how do I stop a reminder", "keeping-an-eye"},
		// The eleventh wave: the ambient side arriving on home. Each of these is
		// asked by somebody LOOKING at a row they did not expect — a glyph they
		// have not met, a segment at the foot of the frame, a card that appeared
		// mid-conversation — so the words are the ones they would say out loud
		// about what is in front of them.
		{"what is the little circle row on home", "home"},
		{"how do I pause a reminder", "home"},
		{"what does keeping an eye on 2 mean", "home"},
		// The twelfth wave: the cursor on a PROJECT rather than on one of its
		// chats. Both of these are asked while looking at a name with a card
		// beside it, and neither of them is a question about one conversation.
		{"what does the right side show for a whole project", "home"},
		{"how many conversations does a project have", "home"},
		// The eleventh wave: the errand you say from home. Every one of these is
		// asked with home on screen and a sentence half typed, and the honest
		// answers — that there is a second row, that the exchange is stored
		// somewhere home does not list, and how to turn it into an ordinary
		// conversation — are all on one page.
		// The twelfth wave: what the card actually does while somebody is
		// reading it, and where an errand said at home leaves its record. Both
		// were asked by the first person to use this, and the corpus answered
		// the first one wrongly — it described a countdown that is now gone.
		{"how long do I have to answer the card", "keeping-an-eye"},
		{"where is the record of a reminder I made from home", "keeping-an-eye"},
		// The twelfth wave: answering a question from home. Both are asked by
		// somebody looking at a `▲` row and wondering whether they have to walk
		// to the terminal it belongs to — which, for the ordinary answers, they
		// no longer do.
		{"can I approve a command from home", "home"},
		{"how do I answer a question in another window", "home"},
		{"can I set a reminder from home", "asking-from-home"},
		{"what is ask here", "asking-from-home"},
		{"where did that exchange go", "asking-from-home"},
		// Both written from one person's first run of `ask here`: the pane took
		// the keyboard and they could not find the way out of it, and a click on
		// a row lit the row up without moving the keyboard with it.
		{"how do I get back to the list from ask here", "asking-from-home"},
		{"why can't I click a row while asking", "asking-from-home"},
		{"what happened in this conversation while I was away", "home"},
		{"where are the files this conversation produced", "home"},
		{"where did we leave off in this conversation", "home"},
		{"what branch is this home conversation on", "home"},
		{"what does y do on a home card", "home"},
		{"what is next up on this home card", "home"},
		{"what has this home conversation spent", "home"},
		// The thirteenth wave: home's left column in two tiers, and the work band
		// on the right. All three are asked by somebody LOOKING at the screen and
		// finding something missing — most of their projects, the rows behind a
		// `▸`, and the rest of the tasks behind a fold line.
		{"why are most projects collapsed on home", "home"},
		{"how do I open a collapsed project", "home"},
		{"how do I see more tasks on the right", "home"},
		{"why did the end of a narrow home card move to another line", "home"},
		{"why doesn't hovering change the right side", "home"},
		// The fourteenth wave, both written from one person's own transcripts.
		// A reminder made from `ask here` fired into the exchange behind home's
		// pane and they were never told, sitting two panes away in an ordinary
		// chat; and every "remind me in 2 mins" opened with a `bash date …` row
		// they could see and asked about.
		{"where does my reminder show up when it fires", "keeping-an-eye"},
		{"why did it run date before setting the reminder", "keeping-an-eye"},
		// The wave that made an exchange a row that outlives the screen it was
		// asked on. All three are the person's own words after using `ask here`:
		// the card was answered for them because looking at another chat closed
		// the errand, the pane drew one dim `…` for a whole turn, and nothing
		// said whether a second errand was allowed at all.
		{"why did my reminder card disappear when I opened another chat", "asking-from-home"},
		{"how do I know ask here is doing something", "asking-from-home"},
		{"can I ask two things from home at once", "asking-from-home"},
		{"why did it set my reminder for a time that already passed", "keeping-an-eye"},
		{"why is there no once on my reminder card", "keeping-an-eye"},
		// The e2e suite found the firing that reached a conversation and was
		// never drawn in it. The page now says what is drawn and when, and this
		// is the sentence a person types when it looks like nothing happened.
		{"a reminder fired but nothing showed up in my chat", "keeping-an-eye"},
		// The wave that gave a standing card an outright no on the two surfaces
		// that have no `esc` to spare. Both of these are what a person types
		// when a card is up and they do not want the thing.
		{"how do I say no to a reminder from home", "home"},
		{"how do I decline a standing card", "keeping-an-eye"},
		// The wave that gave the engine on the far machine its own ambient
		// side. This is what a person asks before they rely on it.
		{"do reminders work over --host", "keeping-an-eye"},
		{"where does a reminder I set up over --host actually run", "running-on-another-machine"},
		// The wave that made home able to answer about money and about the
		// ambient side being idle. The first is asked by somebody looking at a
		// card and wanting the whole bill, not the tasks' half of it; the second
		// by somebody who typed /status because they suspect nothing is
		// happening; the third by somebody deciding whether a watch earns its
		// keep.
		{"how much has this conversation cost on home", "home"},
		{"is the ambient side off", "home"},
		{"how many times did my watch run this week", "keeping-an-eye"},
		// `●` used to mean only "firing", and only inside the one window doing
		// it. It is now a marker on disk that every window reads, so a person
		// can meet the dot without having started anything themselves.
		{"why does my watch show a filled dot right now", "keeping-an-eye"},
		// The wave that gave home a phone shape. Three sentences a person types
		// with the terminal in one hand: what this screen even is at that width,
		// how to answer another window's question from it, and how to reach a
		// task without a keyboard.
		{"how do I use home on my phone", "home"},
		{"how do I approve a command from my phone", "home"},
		{"how do I open a task by tapping", "tasks"},
		// Background checks are on out of the box now, and the two sentences a
		// person says about that are the plain question and the plain wish.
		{"does it run when my terminal is closed", "keeping-an-eye"},
		{"turn off background checks", "keeping-an-eye"},
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
