package manual

import (
	"strings"
	"testing"
)

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
		{"does status show background checks on the remote machine", "keeping-an-eye"},
		{"whose model context window is used over host", "models-and-cost"},
		{"can you read a pdf file", "what-i-can-do"},
		{"can you search the web", "what-i-can-do"},
		{"do you remember me between conversations", "what-i-can-do"},
		// A finished task's room after aforge was closed and opened again: the
		// blank page people met, asked the three ways they meet it.
		{"task page is empty", "task-rooms-after-restart"},
		{"task finished but no chat shown", "task-rooms-after-restart"},
		{"see what a task did after restarting", "task-rooms-after-restart"},
		// And the same blank met from INSIDE a row that was never a task at all —
		// a background job, which has a log where a task has a transcript. This is
		// the way the defect was actually reported: "inside a task I cannot see
		// the chat session or the output".
		{"inside a task I cannot see the chat session or the output", "task-rooms-after-restart"},
		{"why is the task page hidden", "task-rooms-after-restart"},
		// And the same blank met on a task that has NOT finished — the queued one,
		// and the one opened the instant it was started. It is the same sentence a
		// person says about it, so it has to reach a page that names the line the
		// screen is actually showing them.
		{"I clicked on the task and there is nothing there at all", "task-rooms-after-restart"},
		{"nothing on this page yet", "reading-a-task-page"},
		{"how do I undo my last message", "sessions-and-rewind"},
		{"does rewind undo my files", "sessions-and-rewind"},
		{"can I open two terminals in the same folder", "sessions-and-rewind"},
		{"where are my conversations saved", "sessions-and-rewind"},
		{"why is my session called name this session in 8 words", "sessions-and-rewind"},
		{"what slash commands are there", "commands"},
		{"how do I export this conversation", "commands"},
		{"what does ctrl+b do", "keys"},
		// The spell-it-out gesture, asked the three ways people meet it: wanting
		// it, seeing the hint and not knowing what it is, and being unhappy about
		// what came back.
		{"can you make my prompt better", "keys"},
		{"what does spell it out mean", "keys"},
		{"it added details I didn't ask for", "keys"},
		{"how do I attach a screenshot", "keys"},
		{"why did my paste turn into a tag", "attaching-files"},
		{"how much text becomes a paste chip", "attaching-files"},
		{"how do I edit what I pasted", "attaching-files"},
		{"how do I stop it mid answer", "keys"},
		// BARGE-IN, asked the four ways people meet it: wanting to correct a
		// running answer, seeing the chord in the hint slot and not knowing what
		// it is, pressing it and finding nothing happened, and asking whether the
		// key that used to do nothing does something now.
		{"how do I interrupt it and say something else", "keys"},
		{"stop it and tell it something different at the same time", "keys"},
		{"what does shift+enter do", "keys"},
		{"shift enter does nothing for me", "keys"},
		// THE SPLICE, asked the five ways people meet it: wanting to correct a
		// running answer WITHOUT paying to stop it, reaching for the chord by
		// name, meeting the arrow on the waiting message's own dim line, and
		// finding out what happened when the answer finished first.
		{"how do I correct it without stopping it", "keys"},
		{"send a message into the running answer", "keys"},
		{"what does cmd+enter do", "keys"},
		{"what does steers it in mean", "keys"},
		{"my message went in too late", "keys"},
		// THE SCOPED THINKING CHORD, asked the five ways people meet it: reaching
		// for paste and finding it bound, wanting one task to think harder,
		// wanting the machine's own default moved, wanting one reminder raised
		// off the standing floor, and asking what the word on a card means.
		{"what does ctrl+v do", "keys"},
		{"is ctrl+v paste", "keys"},
		{"make this one task think harder", "keys"},
		{"change how hard everything on this machine thinks", "keys"},
		{"why does the card say thinking high", "keys"},
		{"how hard does a reminder think", "home"},
		{"make one reminder think harder", "home"},
		{"why did it ask permission before running that", "permissions"},
		{"what does always mean when I press a", "permissions"},
		{"how do I connect my google account", "accounts"},
		{"which services can you connect to", "accounts"},
		{"can I run this on my dev box over ssh", "running-on-another-machine"},
		{"why is the task roster empty over host", "running-on-another-machine"},
		{"can I start a task on the other machine", "running-on-another-machine"},
		{"task says no task door", "running-on-another-machine"},
		{"can I read a task room from another machine", "tasks"},
		// Host parity is asked from the symptom, not from the name of the wire
		// door. Each phrase therefore has to retrieve the page that owns the
		// visible answer.
		{"why is home empty over ssh", "places"},
		{"someone else is typing", "staying-on-that-machine"},
		{"how fast is the connection", "screen"},
		{"my click does nothing on the server", "opening-files-from-that-machine"},
		{"does export save to my laptop", "commands"},
		{"which machine's settings are these", "commands"},
		{"the far machine says a different version", "running-on-another-machine"},
		{"how do I stop the old engine", "staying-on-that-machine"},
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
		// Written from a live task that compacted fifteen times in six minutes.
		{"why does it keep compacting", "compacting-over-and-over"},
		{"it compacts after every step", "compacting-over-and-over"},
		{"compacting over and over", "compacting-over-and-over"},
		{"does it work on a narrow phone width terminal", "screen"},
		{"why is my table cut off", "screen"},
		{"what is a harness", "saved-shapes-of-work"},
		{"the harness I just had built is not in /subharness", "subharnesses"},
		{"how do I run a harness I had designed", "subharnesses"},
		// The card aforge raises by itself, asked the three ways somebody meets
		// it: a card they did not open, the answer they want to give it, and
		// the one that arrived while they were away from the keyboard.
		{"a card popped up offering to run a program", "subharnesses"},
		{"how do I say no to the offer to run something", "subharnesses"},
		{"what happens if I ignore the card it raised", "subharnesses"},
		{"how do I start aforge", "starting-aforge"},
		// The first-run setup, asked the four ways somebody meets it: arriving,
		// wanting the key in, seeing the screen, and wanting to undo an answer.
		{"first time setup", "getting-started"},
		{"set up my api key", "getting-started"},
		{"openrouter key", "getting-started"},
		{"change what I picked during setup", "getting-started"},

		// The empty screen, asked the ways somebody meets it: an opening frame
		// with nothing on it, a column they expected and cannot see, a box that
		// is not where boxes usually are, and a status row with no numbers.
		{"why is the screen empty when I open aforge", "empty-screen"},
		{"where is the task column on a new conversation", "empty-screen"},
		{"what happened to the sidebar", "empty-screen"},
		{"why is the message box in the middle of the screen", "empty-screen"},
		{"why does the status line not show the cost before I type", "empty-screen"},
		{"what does try what is in this folder mean", "empty-screen"},
		{"where did the recent sessions list go", "empty-screen"},

		// A task's page with heavy tool use, asked the ways the screenshot
		// provoked: the wheel doing nothing, the calls that are not there, and
		// a frame that is mostly blank.
		{"can't scroll in a task", "reading-a-task-page"},
		{"how do I see earlier tool calls in a task", "reading-a-task-page"},
		{"task page is empty", "reading-a-task-page"},
		{"the task page is stuck at the top", "reading-a-task-page"},
		{"what does scroll up or ctrl+o mean", "reading-a-task-page"},

		// The second wave, added after probing the corpus the way it is
		// actually queried. Each of these reached the wrong page until the
		// asker's own words were written into a heading, which is the fix this
		// test is meant to provoke.
		{"can you delete files", "what-i-can-do"},
		{"do write edit and ls use the far disk over --host", "what-i-can-do"},
		{"can you run tests for me", "what-i-can-do"},
		{"can you start a dev server", "what-i-can-do"},
		{"how do I see what a background job printed", "what-i-can-do"},
		// The jobs-on-the-column wave. A person watching an empty right-hand
		// column while a long command ran asks about the COLUMN, not about the
		// jobs tool, so these have to land on the page that owns the column.
		{"why is nothing showing on the right while a command is running", "tasks"},
		{"does a background job show up on the task column", "tasks"},
		{"what is the row for my dev server on the right", "tasks"},
		{"can I stop a background job from the sidebar", "tasks"},
		{"do you ask before running rm", "permissions"},
		{"what is yolo mode", "permissions"},
		{"how do I make it stop asking every time", "permissions"},
		{"what model is it using right now", "models-and-cost"},
		{"how do I give it a longer context", "models-and-cost"},
		{"how do I make it think harder", "models-and-cost"},
		{"why is it slow over ssh", "running-on-another-machine"},
		// The echo and the push, in the words somebody actually types when they
		// notice either one.
		{"why is my message dimmer than usual over --host", "running-on-another-machine"},
		{"my message disappeared after I sent it over ssh", "running-on-another-machine"},
		{"does my message show up straight away over --host", "running-on-another-machine"},
		{"why did the status line stop moving when the connection dropped", "running-on-another-machine"},
		{"does it support markdown tables", "screen"},
		// From the screenshot that provoked the right column's rebuild: the
		// figures at a tool row's right end were cut down to `0…`, and "what
		// does that mean" is the first thing anyone asks about them.
		{"what does the time on the right of a tool call mean", "screen"},
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
		// The onboarding wave: home is always reachable, and an empty home is a
		// designed screen. Each of these is asked on a fresh machine, by
		// somebody who tried the gesture on day one.
		{"space space does nothing", "home"},
		{"home is empty", "home"},
		{"how do I get back to home with one chat", "home"},
		{"why is home empty", "home"},
		{"can I open home with only one conversation", "home"},
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
		{"it said memory is off but I never turned it off", "what-i-remember"},
		{"why does it say could not open graph.db", "what-i-remember"},
		{"aforge printed out of memory 14 on startup", "what-i-remember"},
		{"where is my memory file kept on disk", "what-i-remember"},
		{"can I copy my memories to another machine", "what-i-remember"},
		{"how do I see what aforge remembers", "what-i-remember"},
		{"how do I edit a memory", "what-i-remember"},
		{"how do I undo forgetting one", "what-i-remember"},
		{"where did a memory come from", "what-i-remember"},
		// SCREEN 1f's own verb, asked the three ways somebody meets it: from the
		// foot they are reading, from the key they just pressed, and from what
		// they wanted to do with the line in front of them.
		{"ask me about it", "what-i-remember"},
		{"what does enter do on a memory line", "what-i-remember"},
		{"talk about something you remember about me", "what-i-remember"},
		{"why did my memories get merged", "what-i-remember"},
		{"does it clean up old memories", "what-i-remember"},
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
		{"why won't permissions show the rules on the machine I used with host", "running-on-another-machine"},
		{"did cache clean delete the cache on my laptop or the remote machine", "running-on-another-machine"},
		{"why didn't crew max change the crew on the remote machine", "running-on-another-machine"},
		{"why does remember over host not say whether memory is off", "running-on-another-machine"},
		{"does subharness know whether the remote machine has saved programs", "running-on-another-machine"},
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
		// A person reading a column of workers all called the same thing, and a
		// person watching a run that has not drawn anything yet. Both are asked
		// with the screen in front of them, in the words the screen gave them.
		{"why are all my workers called You are a", "adaptive-runs"},
		{"the sub task names are just the prompt", "adaptive-runs"},
		{"who names the workers under a run", "adaptive-runs"},
		{"what does forming the work mean", "adaptive-runs"},
		{"nothing happens for a minute after a run starts", "adaptive-runs"},
		// And a person reading a column of ids. These are asked with the ids
		// themselves in the question, because that is what the screen handed them.
		// And a person who came back to a conversation and looked at the column.
		// The rows are there now, settled — the question used to be about a
		// column that was empty, and it is still the question they ask.
		{"where did my run's rows go", "adaptive-runs"},
		{"my run disappeared from the task column when I switched away", "adaptive-runs"},
		{"do run rows come back when I reopen a conversation", "adaptive-runs"},
		{"my run's rows are called r1 r2 r3", "adaptive-runs"},
		{"why is a subtask called synth", "adaptive-runs"},
		{"the tasks under my run have ids instead of names", "adaptive-runs"},
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
		{"why is the picture you generated over --host not painted in my terminal", "making-pictures-audio-and-video"},
		// Written from a real run: twelve clips rendered in parallel with
		// nothing shared, stitched with a video-only crossfade — the person
		// asked all three of these, in these words, and the answers were
		// improvised because no page held them.
		{"can you make a 2 minute video", "making-pictures-audio-and-video"},
		{"why is the video you made not coherent", "making-pictures-audio-and-video"},
		{"the stitched video has no sound after the first clip", "making-pictures-audio-and-video"},
		{"keep the same character across the clips", "making-pictures-audio-and-video"},
		{"how long is the music you compose", "making-pictures-audio-and-video"},
		{"can you keep working while the music is composing", "making-pictures-audio-and-video"},
		// Written from the complaint that produced the quality section: renders
		// were landing generic and soft, and the person said it in these words.
		{"the image you made looks like generic ai slop", "making-pictures-audio-and-video"},
		{"can you make the video higher resolution", "making-pictures-audio-and-video"},
		// From the hero-image batch: eight models, one clichéd prompt, eight
		// copies of the same picture — a detailed prompt is not yet a
		// distinctive one.
		{"all the images you generated look the same generic style", "making-pictures-audio-and-video"},
		// The complaint survived the first fix: the render swapped palette and
		// metaphor and kept the genre's deepest habit anyway.
		{"why is everything you make glowing on a dark background", "making-pictures-audio-and-video"},

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
		// And the same state met from inside the task's room, where the owner sat
		// with `look it over` on the roster and nothing to press on the page.
		{"how do I approve a task", "tasks"},
		{"task needs my look but there is no button", "tasks"},
		{"accept a finished task", "tasks"},
		{"can I accept the task from inside the room", "tasks"},
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
		// Division: the wave that let a single worker discover its work is wider
		// than one pair of hands. People ask about it in the words they watched
		// happen — "it split itself", "why are there suddenly three of them".
		{"my task split itself into parts", "tasks"},
		{"why did one task turn into several workers", "tasks"},
		{"can a task divide its own work when it turns out to be too big", "tasks"},
		{"what decides whether work gets split", "tasks"},
		// And the half of division a person meets from the other side: they walk
		// into the parent's room, type, and the page does not move — because the
		// task is waiting on its own pieces and their line is what wakes it.
		{"I typed into a task and nothing happened", "tasks"},
		{"my task is waiting on its pieces", "tasks"},
		{"why did it warn me about another window", "tasks"},
		{"two windows working on the same files", "tasks"},

		// And the question the division road left standing on the other page.
		// A run used to be what broad work reached for; it is the exception now,
		// and somebody who expected a planner and watched one worker start asks
		// it in front of that worker.
		{"should this be an adaptive run or one worker", "adaptive-runs"},
		{"why didn't you start an adaptive run for this", "adaptive-runs"},
		{"when do you use a run instead of a task", "adaptive-runs"},

		// The fifteenth wave: the crew that looks like it did nothing. `/crew max`
		// writes four class models and the session picks them up on its next
		// call — and the model on the status line does not move, because that one
		// is the CONVERSATION's and the crew never touches it. So a person reads
		// the confirmation, looks at a frame that says exactly what it said
		// before, and asks these in front of it.
		{"I changed the crew but the model didn't change", "models-and-cost"},
		{"why does the bottom still show the old model after /crew", "models-and-cost"},
		{"does /crew change the model I'm talking to", "models-and-cost"},
		// The onboarding wave: the five seats. /crew and /model became two dials
		// a person can see as two — the confirm line names the model it left
		// alone, bare /crew opens with seat one, and the status line carries
		// `crew max` beside the model — and these are the questions the framing
		// invites.
		{"what are the five models", "models-and-cost"},
		{"does /crew change my chat model", "models-and-cost"},
		{"why did my model not change", "models-and-cost"},
		{"what does crew max on the status line mean", "models-and-cost"},
		{"what is the you talk to line in /crew", "commands"},

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
		// THE ANSWER HIERARCHY (internal/tui3's hierarchy.go). A turn's narration
		// now recedes into the work column at a quieter shade and only the block
		// the turn ended on is drawn as the answer, so somebody looking at a reply
		// half in one tier and half in another asks the first three of these — and
		// somebody who pressed esc and watched nothing turn into an answer asks the
		// last two. They are the screen's, because every word of the question is
		// about what is on it.
		{"why is part of the reply grey", "screen"},
		{"why is some of the answer dimmer than the rest", "screen"},
		{"where is the actual answer in all this", "screen"},
		{"I stopped it and the text stayed grey", "screen"},
		{"nothing became the answer after I pressed esc", "screen"},
		// A SENTENCE TYPED INTO A TURN THAT WAS ALREADY RUNNING, asked the five
		// ways people meet it: seeing the row and not knowing what drew it,
		// seeing the word beside it, and — the ones that matter most — looking for words that
		// left the question they were aimed at. Every one of them is about what
		// is on the screen, so every one of them is the screen's.
		{"what is the line under my message", "screen"},
		{"why is there a corner glyph under what I typed", "screen"},
		{"what does steering next to my correction mean", "screen"},
		{"where did my correction go", "screen"},
		{"I typed something while it was working and it disappeared", "screen"},
		{"the model was printing garbage", "models-and-cost"},
		{"the reply came back as gibberish", "models-and-cost"},
		{"it started repeating the same line over and over", "models-and-cost"},
		{"how do I turn off the reply guard", "models-and-cost"},
		{"the model stopped answering halfway through", "models-and-cost"},

		// The effort ladder. One dial with five rungs under a default of `high`,
		// so every question about it is asked in the words somebody uses for the
		// *feeling* they want: a deeper answer, a faster one, a model that is
		// thinking too long for what they asked. The last two are asked by
		// somebody who has met a rung name in the settings row and wants to know
		// what it costs them.
		{"how do I make it think less", "models-and-cost"},
		{"how do I make it think deeper", "models-and-cost"},
		{"how do I get faster answers from the model", "models-and-cost"},
		{"what does xhigh mean", "models-and-cost"},
		{"what is the default reasoning effort", "models-and-cost"},
		{"does the thinking level stick after a restart", "models-and-cost"},

		// And the CONVERSATION's own rung, which is a chip and a chord rather
		// than a setting — so it is asked about as a thing on the screen ("what
		// is that symbol above the box") and as a key somebody pressed by
		// accident, neither of which reaches a page about models and cost.
		{"what is the chip above the message box", "keys"},
		{"what does ctrl+v do", "keys"},
		{"how do I make this one chat think harder", "keys"},

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
		// Asked in a person's own words after they met their first card and
		// could not see a way out of it, could not tell what the options meant,
		// and expected to be able to change the `where ·` band they were being
		// shown. Every one of these is now on the card itself; the pages say so.
		{"how do I cancel this card", "keeping-an-eye"},
		{"I don't understand these options", "keeping-an-eye"},
		{"what does just once mean", "keeping-an-eye"},
		{"can I change everywhere to just this project", "standing-orders"},
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
		// The wave that gave the ambient side a reach: an order that governs one
		// chat, one project, or everything. Each of these is what somebody types
		// looking at the page, at the card's `where` band, or at the one line a
		// conversation opens with.
		{"what rules do you have here", "standing-orders"},
		{"how do I make it always do something", "standing-orders"},
		{"do you have automations", "standing-orders"},
		{"what does the standing orders page show", "standing-orders"},
		{"does this rule apply to all my projects", "standing-orders"},
		{"not in this project", "standing-orders"},
		{"why does it say 3 standing orders here", "standing-orders"},
		// The wave that gave a rule with no trigger a shape of its own. These are
		// the words somebody uses for one before they have heard the word
		// "standing" at all — a style rule, a convention, a preference — plus the
		// two questions the shape provokes: how do I set one, and how does it know
		// I meant always rather than just now.
		{"always do it this way", "standing-orders"},
		{"can you remember my coding style rule", "standing-orders"},
		{"how do I set a standing order", "standing-orders"},
		{"how does it know I mean always", "standing-orders"},
		{"is that an instruction or a rule", "standing-orders"},
		{"our conventions for this repo", "standing-orders"},
		// The deliberate gesture and the visible door, in the words of somebody
		// reaching for them — or noticing that recognition missed.
		{"how do I force it to be standing", "standing-orders"},
		{"how do I make this permanent", "standing-orders"},
		{"it didn't notice this was a rule and did it once", "standing-orders"},
		{"can I click keeping an eye on 2", "standing-orders"},
		// The wave that gave home's landed rows an aim. Somebody looking at a
		// `needs you` row that has sat for four days asks two things — what does
		// pressing it actually show me, and how do I make it go away — and both
		// used to be asked in front of a door that opened a conversation with no
		// trace of the task the row was named after.
		{"I clicked the needs you row and it just opened the chat", "home"},
		{"what opens when I press a landed row on home", "home"},
		{"the needs you row has been there for days", "home"},
		{"how do I clear a needs your look row", "home"},
		// The wave that fixed where the cursor wakes on a wide home, and gave a
		// zone with nothing under it something to say. Both are asked by somebody
		// looking at the screen: the arrow went somewhere they did not expect, and
		// a dim word is sitting over what looks like nothing.
		{"why does the cursor start in the middle of home", "home"},
		{"where does the first down arrow go on home", "home"},
		{"what is that line under needs you", "home"},
		{"what goes in the moving column", "home"},
		// The wave that gave the section holding the cursor a marked heading.
		// Somebody sees one heading darker than the rest and asks what it means;
		// somebody else has the opposite problem and cannot tell which of three
		// columns their arrow keys are in. Both land on the same two sections.
		{"why is the needs you heading highlighted", "home"},
		{"how do I know which column I am in", "home"},
		{"why is one project name darker than the others", "home"},
		{"which section is my cursor in on home", "home"},
		// The wave that stopped guessing at the terminal's background and asked
		// it. Three of these are somebody checking whether the surface knows what
		// it is sitting on — the first two before switching to a light terminal,
		// the third from a terminal that never answers and gets the built-in
		// palette. The fourth is the defect the measurement fixes, in the words
		// of the person looking at it.
		{"does it detect my terminal theme", "screen"},
		{"can it use my terminal's light background", "screen"},
		{"why are the colors the same on every terminal", "screen"},
		{"the text is too bright on my black terminal", "screen"},

		// The earned hints. Asked the four ways somebody meets them: seeing a
		// sentence in the border and not knowing what it is, noticing it has
		// gone, wanting it gone, and wondering what a line about a new build was.
		{"what was that tip above the message box", "hints-and-tips"},
		{"why did the hint disappear", "hints-and-tips"},
		{"how do I turn off hints", "hints-and-tips"},
		{"stop showing tips", "hints-and-tips"},
		{"what is a news line", "hints-and-tips"},

		// The wave that made the places follow the session's machine. These are
		// the owner's own sentences, from the report that started it: they
		// attached over --host, pressed space-space, got one dim line, and said
		// "i thought we can open multiple terminals and things would just auto
		// transfer and work". Every one of these has to land somewhere that
		// answers rather than somewhere that used to.
		{"i pressed space space over --host and got one line", "places"},
		{"why is home empty when I connect to another machine", "places"},
		{"can I search old chats on the machine I connected to", "places"},
		{"can I put away a conversation on the other machine", "places"},
		{"whose projects am I looking at over --host", "places"},
		{"does the tasks page show the other machine's work", "places"},
		{"the tasks tab showed work that is not on this machine", "places"},
		{"what does on spark at the end of the tab bar mean", "places"},
		{"which places work over --host", "places"},
		{"why does spend say this session is on another machine", "places"},
		{"can I search my old chats over --host", "places"},
		{"does memory work when I connect to another machine", "places"},
		{"why is nothing marked folder gone on a remote home", "places"},
		{"do the tab numbers get mixed up between two machines", "places"},
		{"can I open multiple terminals and have things just transfer", "running-on-another-machine"},
		{"how do I work on the same conversation from two computers", "running-on-another-machine"},
		{"what is the difference between another window and another machine", "running-on-another-machine"},
		{"does home work over --host", "running-on-another-machine"},

		// The wave that stopped a rebuild on the far machine from trapping
		// somebody. These are the words a person actually uses at the moment it
		// happens: they updated aforge over there, `aforge version` agreed, and
		// the connection still told them to update the older half.
		{"I updated aforge on that machine and it still says the versions differ", "running-on-another-machine"},
		{"I rebuilt aforge on my dev box and --host still refuses", "running-on-another-machine"},
		{"it says the two halves have to be the same build but they are", "running-on-another-machine"},
		{"it says spark is still running an older aforge", "running-on-another-machine"},
		{"how do I stop the thing holding my session on that machine", "staying-on-that-machine"},
		{"what does aforge engine --stop do", "staying-on-that-machine"},
		{"does the session host notice when I rebuild aforge", "staying-on-that-machine"},

		// The wave that made the files on the far machine things this one can
		// open. These are the sentences somebody types with a hosted session in
		// front of them: the click, the download, the drag, the picture they
		// cannot see, and the two questions the copies provoke.
		{"how do I open a file that is on the other machine", "opening-files-from-that-machine"},
		{"why is that path not clickable over --host", "opening-files-from-that-machine"},
		{"can I tab complete a path for /attach", "attaching-files"},
		{"where does an attached file go over --host", "attaching-files"},
		{"I dropped a file and nothing happened", "attaching-files"},
		{"can I attach a whole folder", "attaching-files"},
		{"how do I download a file from my dev box", "opening-files-from-that-machine"},
		{"can I drag a file onto the browse page to upload it", "opening-files-from-that-machine"},
		{"where do the files I fetched from the other machine go", "opening-files-from-that-machine"},
		{"if I edit the copy does it change the file over there", "opening-files-from-that-machine"},
		{"how do I see the picture it made on the far machine", "opening-files-from-that-machine"},
		{"cmd click does not open the file on the server", "opening-files-from-that-machine"},
		{"where did my picture go over ssh", "making-pictures-audio-and-video"},
		{"how do I browse the folders on the other machine", "opening-files-from-that-machine"},
		{"can anyone else open these 127.0.0.1 links", "opening-files-from-that-machine"},
		{"how big a file can I bring back over a connection", "opening-files-from-that-machine"},

		// The wave written from a screenshot of a crowded column: thirteen
		// landed jobs three lines each, `standing` squeezed to one cut-off row,
		// and a wheel over the whole thing scrolling the conversation. A person
		// meets this with a mouse in their hand and says "sidebar", "scroll" and
		// "where did the details go", so those are the words.
		{"why won't the sidebar scroll with my mouse", "screen"},
		{"how do I scroll the task column", "screen"},
		{"where did my finished task's details go on the column", "screen"},
		{"how do I see the log path of a job that already finished", "screen"},
		{"why does the right column only show three standing orders", "screen"},

		// THE FOLDED INSTRUCTION, asked the four ways people meet it: complaining
		// about the space it used to take, seeing the fold line and not knowing
		// what it is, wanting the rest of it, and wanting it small again.
		{"the task description takes up the whole page", "tasks"},
		{"what does 14 more lines mean at the top of a task", "tasks"},
		{"how do I see the full task description", "tasks"},
		{"how do I collapse the long brief in a task", "tasks"},

		// THE PLACES, asked the way somebody meets them: seeing a row of words
		// under the top line and not knowing what it is, wanting a key for one,
		// finding a page that says what it is for and nothing else, and hitting
		// the one state where a letter is not a letter.
		{"what is the row of words at the top of the screen", "places"},
		{"how do I get to the tasks page without a command", "places"},
		{"is there a keyboard shortcut to jump between pages", "places"},
		{"what does the number beside a tab mean", "places"},
		{"why does the spend page only have three sentences on it", "places"},
		{"how do I see all the keyboard shortcuts", "places"},
		{"what does the right arrow do on a row", "places"},
		{"where do I type on the standing page", "places"},
		{"what is the here ~/ thing next to the box", "places"},
		{"how do I start a task from any page", "places"},
		{"what are the three lines that appear when I press alt+enter", "places"},
		{"how do I change which project a task runs in before I send it", "places"},
		{"what does alt+w do", "places"},
		// THE PLATFORM QUESTION, in the four shapes it actually arrives in: the Mac
		// user whose option key is composing accents (which is what they SEE, so
		// they ask about the character rather than about the modifier), the person
		// wondering whether the manual's `alt+` is their `⌥`, the one who tried
		// `ctrl+1` because the number is drawn on the tab, and the Windows user
		// checking whether any of it applies to them.
		{"why does option type ¡ instead of jumping to a place", "screen"},
		{"use option as meta", "screen"},
		{"is alt the same as option on a mac", "screen"},
		{"does ctrl+1 go to a place", "keys"},

		// THE OWNER'S OWN WORDS, on the day the jumps were dead everywhere but
		// the message box: "option left right and cmd and clicking does not seem
		// to work". A person reports the KEY they pressed, never the name the
		// terminal sent it under, so the page has to answer `option+←` and
		// `cmd+←` and not only `alt+b` and `ctrl+a`.
		{"option left right and cmd does not seem to work", "keys"},
		{"option left doesn't work", "keys"},
		{"option arrow does not jump a word", "keys"},
		{"cmd left does nothing", "keys"},
		{"how do I jump a word on a mac", "keys"},
		{"jump to the start of the line", "keys"},
		{"why does ctrl+left do nothing on a mac", "keys"},
		{"word jump does not work in the home box", "keys"},
		{"clicking the box does not move the cursor", "keys"},
		{"cmd right archived my conversation", "keys"},
		{"natural text editing iterm2", "keys"},
		{"do the alt chords work on windows", "screen"},
		{"how do I pick the model a task runs on before starting it", "places"},
		{"why did pressing alt+enter not send my task straight away", "places"},
		{"how much money can a task spend before it stops and asks me", "tasks"},
		{"how do I set a spend limit on a task before I send it", "tasks"},

		// The wave that gave the room one keyboard. Two windows on one hosted
		// conversation used to race each other in silence; now the newest one
		// types and the rest watch. These are the sentences somebody types with a
		// composer that has just turned into a line they did not ask for — and
		// the ones they type at the OTHER window, wondering what it did.
		{"someone else is typing", "staying-on-that-machine"},
		{"why can't I type", "staying-on-that-machine"},
		{"two terminals on the same chat", "staying-on-that-machine"},
		{"take over the keyboard", "staying-on-that-machine"},
		{"my input box turned into one line", "staying-on-that-machine"},
		{"what does typing from now mean", "staying-on-that-machine"},
		{"can two windows share one conversation", "staying-on-that-machine"},
		{"how do I get the keyboard back", "staying-on-that-machine"},
		{"is my draft lost when the other window takes over", "staying-on-that-machine"},
		{"does the other window see the turn I started", "staying-on-that-machine"},
		{"what happens to the keyboard when a window closes", "staying-on-that-machine"},
		{"my window came back and now I cannot type", "when-the-connection-drops"},
		{"it says the keyboard is on another machine", "staying-on-that-machine"},
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

// EVERY PLACE OPENS, ALWAYS — AND NO PAGE MAY SAY OTHERWISE.
//
// The three gates around this corpus check that a name is MENTIONED. None of
// them can see whether the sentence around the name is true, and that is how a
// wave which made three refusals impossible shipped with three pages still
// stating them: `tab` skipping a shut room, a place asked for by name saying why
// it will not open, and `ctrl+.` doing nothing on a machine that has run
// nothing. The pages are the only thing the model knows about this program, so
// on a fresh machine it told people that the key they had just been given did
// nothing — the exact experience the wave was built to end.
//
// This is the truth-side gate for the one claim that was retired: a place
// refusing. Each phrase below shipped in the corpus and each is now false of the
// code — nextPage walks the order table unconditionally, showPage has no refusal
// path left in it, and both doors onto the tasks place are the same door.
//
// IT IS A SHORT LIST ON PURPOSE. A gate that tried to read English would fail
// on the pages that tell the story of the retired refusal, which several
// deliberately do; these are the sentences that ASSERTED it.
func TestNoChatPageSaysAPlaceCanRefuseToOpen(t *testing.T) {
	retired := []string{
		"goes past a place that has nothing to open",
		"Two rooms can be shut",
		"still says why\nit will not open",
		"Does nothing when nothing has run",
		"A room\nthat has nothing to open says so in the conversation",
		"and open nothing.",
	}
	for _, section := range Chat().Sections() {
		for _, phrase := range retired {
			if strings.Contains(section.Body, phrase) {
				t.Errorf("%s · %q still says a place can refuse to open: %q",
					section.Page, section.Title, phrase)
			}
		}
	}
}
