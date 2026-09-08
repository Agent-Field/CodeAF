package manual

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
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
		// Lanes behind a base of the person's own: the question the hostname gate
		// (issue #373) used to answer wrongly, in the three ways it gets asked.
		{"do lanes work with a custom base url", "lanes"},
		{"AFORGE_BASE_URL proxy no lanes", "lanes"},
		{"self-hosted router endpoints page", "lanes"},
		// The model-call log: the file every outbound call writes a line to.
		// Both spellings people actually use — one asks for the file, the other
		// asks what was on the wire.
		{"where are the logs", "models-and-cost"},
		{"what did you send the model", "models-and-cost"},
		// And the row that is short of a figure: the note names the number it
		// left out, which is the first thing somebody greps for when a line has
		// a sentence where a cost should be.
		{"why is cost_s missing on a call log row", "models-and-cost"},
		// And the money on a headless run's last line, asked both ways people
		// meet it: reading the figure, and finding it beside the call log.
		{"what does the total at the end of aforge do include", "models-and-cost"},
		{"why is the printed cost different from the call log", "models-and-cost"},
		// And the reader itself: a person with a log in front of them wants one
		// call out of it, or wants the rows a program can read.
		{"find one call in the log", "models-and-cost"},
		{"show me the raw rows", "models-and-cost"},
		{"filter aforge logs by model", "models-and-cost"},
		{"open a call's body", "models-and-cost"},
		// And what a line's two token figures are. Somebody reading a bill or
		// a context size asks for "tokens", never for "in" and "out", so the
		// asker's word is what the heading has to carry.
		{"how many tokens did that call use", "models-and-cost"},
		// THE DEBUG RECORD, asked the four ways somebody arrives at it: wanting
		// the record, wanting the switch, hunting the folder afterwards, and
		// bringing the word every other program taught them for it.
		{"how do I see what happened", "debug-record"},
		{"how do I turn on the debug log", "debug-record"},
		{"where is the debug record", "debug-record"},
		{"turn on logging", "debug-record"},
		{"what does /debug do", "debug-record"},
		// And the OTHER log, which is a different question with the same word in
		// it: where the running program writes its own warnings and recovered
		// faults, on every door rather than only the one that starts a session
		// here (#404).
		{"where does aforge write its log file", "starting-aforge"},
		{"what is chat.log", "starting-aforge"},
		{"does status show background checks on the remote machine", "keeping-an-eye"},
		// The ↻ line, asked the way somebody meets it: they saw a piece of work
		// go round again and want the sentence that says what ended it.
		{"why was my work picked up again", "adaptive-runs"},
		{"whose model context window is used over host", "models-and-cost"},
		{"can you read a pdf file", "what-i-can-do"},
		{"can you search the web", "what-i-can-do"},
		{"which search engine answered?", "what-i-can-do"},
		{"I set a search key and nothing changed", "what-i-can-do"},
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
		// Escape pressed on a turn that keeps going, asked the three ways people
		// say it: the key that seemed to do nothing, how long the wait lasts, and
		// what happens when it will not let go.
		{"I pressed escape and it is still running", "keys"},
		{"how long does stop take", "keys"},
		{"what happens if it will not stop", "keys"},
		{"what happens if I kill the aforge process", "keys"},
		{"I closed the terminal window while a task was running", "keys"},
		{"does kill -INT stop my tasks", "keys"},
		// A machine that refused the work, asked in the word the screen puts on
		// the row: the provider named in the refusal, the model nothing will
		// serve, and why that word is not the one for slow.
		{"it says coreweave refused", "models-and-cost"},
		{"a machine will not serve my model", "lanes"},
		// And the refusal that is about no machine at all, asked in the router's
		// own sentence and in the words somebody reaches for after reading it.
		{"all providers have been ignored", "lanes"},
		{"why did every provider get ignored", "lanes"},
		{"why does it say refused instead of slow", "models-and-cost"},
		{"why does it say paid model training violation", "models-and-cost"},
		{"what does guardrail restrictions and data policy mean", "models-and-cost"},
		{"0 endpoints out of 1 requested", "models-and-cost"},
		// The switcher, asked in the words people bring to it: the gesture they
		// already know from every other program, the thing they are looking for,
		// and the two spellings of the key.
		{"alt tab between conversations", "keys"},
		{"switch chats without pressing enter", "keys"},
		{"ctrl+k switched me right away how do I stop that", "keys"},
		{"it goes when I stop pressing", "keys"},
		{"switch between my open chats", "keys"},
		{"is there a conversation switcher", "keys"},
		{"how do I get to my other conversation without going home", "keys"},
		{"what does ctrl+k do", "keys"},
		{"why does ctrl+tab do nothing", "keys"},
		{"what did my other chats do while I was away", "keys"},
		// And the tasks page's tree, asked by somebody looking at a page that is
		// hiding rows from them on purpose.
		{"where did the workers go on the task page", "tasks"},
		{"what does +3 under mean", "tasks"},
		{"expand a task to see what ran under it", "tasks"},
		{"how do I undo my last message", "sessions-and-rewind"},
		{"does rewind undo my files", "sessions-and-rewind"},
		{"can I open two terminals in the same folder", "sessions-and-rewind"},
		// The defect this build ended, asked the way somebody meets it: they
		// opened a second terminal and were handed a conversation that was not
		// the one they came back for.
		{"I opened aforge in a second terminal and it started a new conversation", "sessions-and-rewind"},
		{"why did it start a new conversation", "sessions-and-rewind"},
		// And the same person's actual want, said the way they want it: not two
		// windows on one chat, but this chat, here, now.
		{"continue this chat in another terminal", "sessions-and-rewind"},
		{"where are my conversations saved", "sessions-and-rewind"},
		// A secret that reached a tool result, asked the two ways it is asked:
		// before it happens, by somebody deciding whether to trust the thing, and
		// after it happens, by somebody who has just watched a token go past.
		{"does aforge save my API keys", "sessions-and-rewind"},
		{"I printed a token, is it in the transcript", "sessions-and-rewind"},
		{"if a task prints a token is it saved", "sessions-and-rewind"},
		{"why is my session called name this session in 8 words", "sessions-and-rewind"},
		{"what slash commands are there", "commands"},
		{"how do I export this conversation", "commands"},
		// THE FOLDER PICKER, asked in the four vocabularies people bring to it.
		// "folder" is the word most people say, "directory" is what a terminal
		// person says, "repo" is what somebody with several checkouts says, and
		// the fourth is the ask underneath all three — which the page has to
		// answer honestly: the conversation comes to be ABOUT the folder, and
		// where it is STANDING does not move.
		{"how do I choose a folder", "choosing-a-folder"},
		{"pick a directory", "choosing-a-folder"},
		{"work on a different repo in this chat", "choosing-a-folder"},
		{"open another project", "choosing-a-folder"},
		{"can I attach a folder", "choosing-a-folder"},
		// AND THE SAME ASK IN THE WORDS OF SOMEBODY WHO BELIEVES IT IS IMPOSSIBLE.
		// Every one of these used to be answered "no, start another conversation"
		// by a page that was right when it was written and is not any more, so
		// each is here to hold the corrected answer in place.
		{"work on two projects in one chat", "choosing-a-folder"},
		{"how do I switch folders", "choosing-a-folder"},
		{"can you see my other repo", "choosing-a-folder"},
		{"do I have to start a new conversation for another project", "choosing-a-folder"},
		{"which folders is this conversation about", "choosing-a-folder"},
		{"how do I change directory", "choosing-a-folder"},
		// AND THE OTHER END OF IT, asked the way somebody asks when the folder
		// they are looking at has not moved. "where did my changes go" is the
		// first question; "merge what you did" and "land the work" are the two
		// verbs people reach for next; and the fourth is the fear underneath all
		// three, which the page answers by saying nothing reached the folder at
		// all until they said so.
		{"where did my changes go", "choosing-a-folder"},
		{"merge what you did into my folder", "choosing-a-folder"},
		{"put the changes into the folder", "choosing-a-folder"},
		{"you changed my files?", "choosing-a-folder"},
		{"undo what you did to my folder", "choosing-a-folder"},
		{"work in that folder directly", "choosing-a-folder"},
		{"what does ctrl+b do", "keys"},
		// The spell-it-out gesture, asked the three ways people meet it: wanting
		// it, seeing the hint and not knowing what it is, and being unhappy about
		// what came back.
		{"can you make my prompt better", "keys"},
		{"what does spell it out mean", "keys"},
		{"it added details I didn't ask for", "keys"},
		{"how do I attach a screenshot", "keys"},
		{"why did my paste turn into a tag", "attaching-files"},
		{"does the model see my whole paste or just the tag", "attaching-files"},
		{"the model says it cannot see what I pasted", "attaching-files"},
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
		{"how do I connect my Slack workspace", "accounts"},
		{"which services can you connect to", "accounts"},
		{"can I run this on my dev box over ssh", "running-on-another-machine"},
		{"why is the task roster empty over host", "running-on-another-machine"},
		{"can I start a task on the other machine", "running-on-another-machine"},
		{"task says no task door", "running-on-another-machine"},
		{"can I read a task room from another machine", "tasks"},
		{"I clicked a running task over ssh and it said no task rooms", "tasks"},
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
		// C14: repository placement, protected landings and kept dependency
		// inheritance are reachable in the words a person uses after meeting them.
		{"why didn't my task merge", "how-tasks-run"},
		{"aforge committed to dev", "how-tasks-run"},
		{"my checkout is on main where did the work go", "how-tasks-run"},
		{"I said in place but it made a branch", "how-tasks-run"},
		{"does a task that depends on kept work see it", "how-tasks-run"},
		{"which branches does aforge refuse to write", "how-tasks-run"},
		// WHAT A WORKER'S BELT DOES NOT CARRY, asked the way people meet it: as a
		// thing they want done from inside a task, and as the sentence a worker
		// says back when it cannot.
		{"can a task change my settings", "how-tasks-run"},
		{"can a task look up an old conversation", "how-tasks-run"},
		{"can a task start a watch", "how-tasks-run"},
		// What a task inherits that git cannot see, asked the four ways people
		// ask it: the file they are worried about, the folder they do not want
		// reinstalled, and the general form of the question.
		{"does my task see my .env", "how-tasks-run"},
		{"does a task get node_modules", "how-tasks-run"},
		{"can a task run my tests without installing dependencies", "how-tasks-run"},
		{"do tasks get the files git ignores", "how-tasks-run"},
		// WHICH PROJECT THE WORK IS ABOUT, asked the four ways people meet it: the
		// plain question, the conversation opened in the wrong place, the wish to
		// send work somewhere else, and the complaint after it went wrong.
		{"which folder does a task work in", "how-tasks-run"},
		{"I opened aforge in my home folder where will the task work", "how-tasks-run"},
		{"can a task work in a different repo", "how-tasks-run"},
		{"my task worked in the wrong project", "how-tasks-run"},
		// AND THE ONE PLACE THE GROUND STOPS CLIMBING. `git rev-parse` walks up
		// and out of the folder it is handed, so a workspace made inside the
		// machine's temporary directory used to reach whatever repository
		// happened to be above it; it does not any more, and a task there runs
		// in place instead. These are the words of somebody who noticed the
		// missing branch (#578).
		{"my task under /tmp ran in place instead of getting its own branch", "how-tasks-run"},
		// AND WHAT THE CARD CALLS THAT PLACE. The settled card used to label the
		// directory `worktree`; it says `a branch of your repository` or `its own
		// copy of the folder` now, from the rung that made the world (#194), and
		// somebody reading either phrase for the first time asks this.
		{"where does my task work", "how-tasks-run"},
		{"what does its own copy of the folder mean on the task card", "how-tasks-run"},
		// AND WHETHER THE ADDRESSES IN THE BRIEF ARE ITS OWN. The contract a task
		// is handed is written in the worker's own copy (#566), so somebody who
		// watched their own checkout's path go into a brief asks whether that is
		// where it will write — in the words of the path, the brief, or the
		// contract.
		{"the brief names my project path can the task write there", "how-tasks-run"},
		{"does the task follow the paths in its brief", "how-tasks-run"},
		{"my task was given a path in my checkout will it write there", "how-tasks-run"},
		{"the contract names my repo path", "how-tasks-run"},
		// AND WHAT HAPPENS WHEN THE TWO OF YOU WRITE THE SAME FILE. Somebody who
		// edited a note while a folder task was running asks this in the words of
		// the thing they are afraid of, not in the harness's (#258).
		{"the task wrote over my own edit", "how-tasks-run"},
		{"I changed a file while the task was running", "how-tasks-run"},
		// Written from a real run: the forming block's spinner and count-up stood
		// still for the whole shaping call, because nothing had started the frame
		// clock. These are the words somebody watching that types.
		{"the forming line is stuck", "how-tasks-run"},
		{"the task spinner is not moving", "how-tasks-run"},
		// Written from a real run: a task proposed in a conversation opened with
		// no project died in no time at all with "could not prepare a working
		// copy: this task needs a project". It does not any more, and these are
		// the words somebody who saw that sentence types.
		{"task failed saying it needs a project", "how-tasks-run"},
		{"task in a conversation with no folder", "how-tasks-run"},
		{"do tasks work when there is no repository", "how-tasks-run"},
		{"where does a task run if I did not open a project", "how-tasks-run"},
		// Written from a real run: a task that generated two images was landed
		// as "6 steps without progress" and the person had to go and find the
		// files themselves. Both halves are questions they then ask.
		{"why did my task stop for no progress", "how-tasks-run"},
		{"does generating an image count as progress", "how-tasks-run"},
		{"where did the files go when my task was stopped", "how-tasks-run"},
		// A headless worker that repeatedly reaches one command's timeout names
		// its own ending now, and these are the words of somebody watching it.
		{"my command keeps timing out and it just runs it again", "adaptive-runs"},
		{"why did the worker stop after the same command timed out", "adaptive-runs"},
		// Written from #568: a task ran an eight-minute test suite, could not
		// wait on it, polled it with sleep and tail, was read as repeating
		// itself and killed a run that was passing. Both halves are what the
		// person then asks — is my task stuck, and does the wait cost it.
		{"is my task stuck while it waits for its test suite", "how-tasks-run"},
		{"does waiting for a long command use up my task's hour", "how-tasks-run"},
		// The words under a stopped row, asked the way the rail spells them and
		// the way a person who has not read it asks.
		{"my task says lost the connection what does that mean", "how-tasks-run"},
		{"task went in circles", "how-tasks-run"},
		// Written from a real run: a worker whose last six calls were distinct,
		// successful git commands was cut off and its row said it had gone in
		// circles. Both halves of the correction have to be findable — the word
		// under the row, and the note the worker was actually being handed.
		{"it said going in circles but it was working", "how-tasks-run"},
		{"what does the silent note mean", "keys"},
		{"what does the exclamation mark next to a task mean", "how-tasks-run"},
		{"task blocked by another task", "how-tasks-run"},
		{"did my task see all of the earlier tasks' work", "how-tasks-run"},
		{"why did my task only see part of the earlier task's report", "how-tasks-run"},
		{"does the task get everything the previous task found", "how-tasks-run"},
		{"why does it say not accepted under my task", "how-tasks-run"},
		// Written from a real run too: a task that split its work sat waiting for
		// its parts and was killed as stuck, and a sibling fast-forwarded its own
		// copy onto main and reported somebody else's fixes as its own.
		{"why is my task waiting on its parts", "how-tasks-run"},
		{"can a task run git pull", "how-tasks-run"},
		// The two stages a person waits through with the work not obviously
		// moving. They are asked in front of a card that has only just appeared
		// and a status line nobody has read before, so they are asked in the
		// words on the screen and in the words of somebody who thinks it hung.
		{"my task says sizing the work", "how-tasks-run"},
		{"task appeared and then nothing happened", "how-tasks-run"},
		{"how long does sizing the work take", "how-tasks-run"},
		{"what does briefing a worker mean", "how-tasks-run"},
		{"it said briefing a worker and nothing appeared", "how-tasks-run"},
		// The failure that happens BEFORE any of that: the folder itself would
		// not freeze. It is asked with the sentence on screen, and the sentence
		// belongs to the failure table rather than to the brief that did not
		// match its world, which is the other landing with "world" in it.
		{"my task's world could not be sealed", "how-tasks-run"},
		// WHERE a task may write, which is a different question from WHOSE work
		// it may take — and the one a person asks after reading a refusal that
		// named a directory they recognise.
		{"can a task edit files in another repo", "how-tasks-run"},
		{"why did the task say outside your copy", "how-tasks-run"},
		{"my task wrote in the wrong repository", "how-tasks-run"},
		{"can a task push or open a PR", "how-tasks-run"},
		{"why can a task not push", "how-tasks-run"},
		{"is a task allowed to use gh api", "how-tasks-run"},
		{"why was my task not allowed to merge main", "how-tasks-run"},
		{"my task says git merge is not yours to run", "how-tasks-run"},
		// #85 flagged the token on a task's belt as an open hole; the read is
		// refused inside a task now, and these are the words somebody meets it in.
		{"can a task read my github token", "how-tasks-run"},
		{"my task said gh auth token is not yours to run", "how-tasks-run"},
		{"does a task have my credentials", "how-tasks-run"},
		// Written from a real run: a task made a symlink so a scorer would find
		// its fixtures, measured against the symlink, and reported the work done.
		// The check now runs somewhere the symlink is not, and these are the
		// questions somebody asks when a task comes back incomplete over it.
		{"where does the check run", "how-tasks-run"},
		{"why did my task fail on a file it says it created", "how-tasks-run"},
		{"does the checker see the files my task installed", "how-tasks-run"},
		// Written from a real run: the worker's last line scrolled past and then
		// the card said nothing for minutes while a check and a round ran, and
		// the person watching concluded the work had hung.
		{"task says checking what it left", "how-tasks-run"},
		{"closing gaps what does that mean", "how-tasks-run"},
		{"the task finished but the card is still busy", "how-tasks-run"},
		{"my task went quiet after the last line", "how-tasks-run"},
		{"what does round 1 of 1 mean under my task", "how-tasks-run"},
		{"why does my task say not done under it", "how-tasks-run"},
		// Written from a real run: a task was landed, lost bash, read and grep to
		// the landing turn, was answered "Unknown tool" eight times, was nudged
		// three times for the retries that answer invited, and then saved two
		// source files it had no way left to build.
		{"why does my task say a tool was withdrawn", "how-tasks-run"},
		{"my task says unknown tool bash", "how-tasks-run"},
		{"why were the files my task saved called unverified", "how-tasks-run"},
		// And the other half: a fork whose hands declared their files in full and
		// were refused every single write.
		{"how do I say which files each hand may write", "tasks"},
		// And what a hand may reach for at all: it reads the caller's own
		// instructions, so the question is asked as "which of these are mine".
		{"what tools does a hand have", "tasks"},
		{"why was my fork refused over its scope", "tasks"},
		{"what does this conversation cost", "models-and-cost"},
		// The same question in the two plainest ways somebody types it, which
		// are both about the money and neither of which uses the word cost as a
		// verb the way the line above does.
		{"how much has this conversation cost", "models-and-cost"},
		{"what has this chat cost me", "models-and-cost"},
		// And the money read off the row rather than out of /cost: the figure
		// that used to arrive from the conversation somebody had just left.
		{"the money jumped when I switched chats", "models-and-cost"},
		{"how do I switch model", "models-and-cost"},
		{"what happens when the conversation gets too long", "models-and-cost"},
		// Written from a live task that compacted fifteen times in six minutes.
		{"why does it keep compacting", "compacting-over-and-over"},
		{"it compacts after every step", "compacting-over-and-over"},
		{"compacting over and over", "compacting-over-and-over"},
		// And the knob for it, asked the four ways somebody reaches for it: the
		// flag, the settings row's own words, and the two things they want it to
		// do.
		{"what does --context-fill do", "compacting-over-and-over"},
		{"context fill setting", "compacting-over-and-over"},
		{"make it compact sooner", "compacting-over-and-over"},
		{"stop it compacting so early", "compacting-over-and-over"},
		// The fold marker now names the journal path, asked the ways somebody
		// meets a conversation that just got shorter: where the work went, and
		// how to read it back.
		{"where did the folded messages go", "compacting-over-and-over"},
		{"how do I get the compacted text back", "compacting-over-and-over"},
		{"what happened to the earlier messages", "compacting-over-and-over"},
		{"does it work on a narrow phone width terminal", "screen"},
		{"why is my table cut off", "screen"},
		{"why does the receipt say the compiler supplied no reading", "adaptive-runs"},
		{"the run said empty goal and did nothing", "adaptive-runs"},
		// A file the review called a change even though the run only read it,
		// asked in the three ways the person meets the false account.
		{"it said I only changed one file and I changed none", "adaptive-runs"},
		{"why did it name a file I only told it to read", "adaptive-runs"},
		{"the review complained about a file I never wrote", "adaptive-runs"},
		// A rule the person stated about what the run may DO, asked the four ways
		// somebody meets it: before they run, and after the run broke it.
		{"I said change no files and it changed files", "adaptive-runs"},
		{"can I tell it not to touch anything", "adaptive-runs"},
		{"how do I stop a run writing outside one folder", "adaptive-runs"},
		{"it broke a rule I set", "adaptive-runs"},
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
		// The unattended run that would not finish, asked the four ways somebody
		// meets it: the line it stopped on, the loop they watched, the note at
		// the cap, and the check it kept failing over a file they never wrote a
		// check for (#468).
		{"it stopped and said the same thing was still left", "starting-aforge"},
		{"it kept repeating the same thing", "starting-aforge"},
		{"why does it say carry on", "tasks"},
		{"it said a file does not pass", "starting-aforge"},
		{"a task waiting on one that did not finish", "starting-aforge"},
		// AND THE THREE ENDINGS OF THE SAME RUN (#513), asked the ways somebody
		// meets them: the turn that ended instead of starting more work, the
		// landing nobody could check under the posture that decided it, and the
		// work that never came home.
		{"why did it stop at a task that was finished", "starting-aforge"},
		{"it ended without starting more work", "starting-aforge"},
		{"it said nothing was left but the work was not finished", "starting-aforge"},
		{"it kept working after everything was finished", "starting-aforge"},
		{"a task died on the wire and the run would not stop", "starting-aforge"},
		{"it keeps saying the tests fail but they were already failing", "starting-aforge"},
		{"why did it move my work to a task after five minutes", "starting-aforge"},
		{"it kept running tests for ten minutes and then handed the work over", "starting-aforge"},
		{"it says nothing has been finished yet but it did the work itself", "starting-aforge"},
		// The headless door of the same unattended posture (#535), asked as the
		// budget, the missing start and the screenless carry-on somebody meets.
		{"can I leave a headless run going with a budget", "starting-aforge"},
		{"my --once yolo run never started a task", "starting-aforge"},
		{"does a run with no screen carry its own work on", "starting-aforge"},
		{"it says needs your look but I ran it with yolo", "tasks"},
		{"what does taken as it stands mean", "tasks"},
		{"it says it could not be brought home", "tasks"},
		{"why am I asked twice about a task that could not be brought home", "tasks"},
		{"checked on the second try", "how-tasks-run"},
		{"one call ran without answering and was abandoned", "how-tasks-run"},
		{"the window closed before a second", "how-tasks-run"},
		// The isolation people meet as a bug: the task read the committed file
		// and they are looking at an edited one.
		{"the task did not see my unsaved changes", "how-tasks-run"},
		{"my task worked on an old version of the file", "how-tasks-run"},
		// A task that split itself while holding unfinished work: its parts start
		// from that world, frozen once at the split. Asked the three ways a
		// person meets it — the artefacts, the freeze, and the commit they find
		// in the history.
		// A reply moved to a task while pieces the conversation handed out are
		// still running: the coordinating half stays here (#567). Asked the three
		// ways somebody meets it — the line they read, the block left in the
		// transcript, and the worker that could not see what it was told to wait
		// for.
		{"it said the rest stays here for when the pieces already out land", "tasks"},
		{"what does what stays here for when the pieces already out land mean", "tasks"},
		{"my task was told to wait for my other tasks and could not see them", "tasks"},
		{"it said this is all about the pieces already out", "tasks"},
		{"do the parts see the parent's unfinished work", "tasks"},
		{"when is the parent's work frozen for its parts", "tasks"},
		{"what is the wip commit before a split", "tasks"},
		// The owned workspace, asked the two ways it actually gets discovered:
		// before, wondering where the work will land, and after, when the folder
		// went and took the work with it.
		{"where do task files go when I did not open a project", "starting-aforge"},
		{"deleted my chat and lost the files the task made", "starting-aforge"},
		// The first-run setup, asked the four ways somebody meets it: arriving,
		// wanting the key in, seeing the screen, and wanting to undo an answer.
		{"first time setup", "getting-started"},
		{"set up my api key", "getting-started"},
		{"openrouter key", "getting-started"},
		{"change what I picked during setup", "getting-started"},
		{"first prompt hung", "getting-started"},
		{"/model switches", "getting-started"},

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
		// A ROOM THAT WAS DEAD WHILE THE WORK WAS ALIVE. The first is the exact
		// sentence the screenshot provoked — a running job's page drawing the
		// landed foot — and the second is what the person wanted the page to be
		// doing instead. The third is how somebody says it before they have
		// noticed which of the two is wrong.
		{"the task page says finished but it is still running", "reading-a-task-page"},
		{"can I watch a background job's log", "reading-a-task-page"},
		{"the task page looks stuck", "reading-a-task-page"},
		// And the word at the top of that page, asked by somebody reading one
		// they have never seen before. The header has a stage the page did not
		// list until the sizing reading was drawn on it.
		{"the task page header says sizing the work", "reading-a-task-page"},
		// WHAT A TASK'S PAGE SAYS ABOUT ITS OWN WORK, now that it says it: the
		// figure at the end of a call's row, the line a cut attempt leaves
		// behind, and the one dim line on the page that names a gate nobody
		// pressed a key for. All three were facts the conversation had and a
		// task's page did not (#252).
		{"how long did that call take in the task", "reading-a-task-page"},
		{"why does the task page say the model went quiet mid-reply", "reading-a-task-page"},
		{"guardian allowed on my task page", "reading-a-task-page"},
		// This pair separates one task's own clock from the machine-wide tasks
		// place row whose age a person is reading.
		{"when did this task start", "reading-a-task-page"},
		{"an old task says now", "tasks"},
		// THE CLAUSE ON A CORRECTION, asked by somebody who has just watched it
		// appear and fade (#252). They are not looking for "steering" — the word
		// on their screen is `delivered`, and it is beside words they typed
		// themselves, so that is what they type.
		{"what does delivered mean after the message I typed into a task", "reading-a-task-page"},
		{"I steered a task that was waiting on its pieces, what is the line after my words", "tasks"},
		// AND THE SAME PAGE ONCE IT FOLDS ITS SETTLED WORK (#252, ruling 1). The
		// chips are new furniture on a page people already knew, so they are asked
		// the three ways somebody meets one: not knowing what the mark is, not
		// knowing what the key beside it does, and — the way it is actually
		// reported — not seeing the calls at all and assuming they are gone.
		{"what are the little chips on the task page", "reading-a-task-page"},
		{"what does ctrl+e do on a task page", "reading-a-task-page"},
		{"why can I not see the tool calls the task made", "reading-a-task-page"},
		{"my task page is hiding most of the work", "reading-a-task-page"},
		{"what is that line over the tool calls", "reading-a-task-page"},
		{"why did the work collapse into captions", "reading-a-task-page"},
		{"how do I see what a caption did", "reading-a-task-page"},
		// And the landing word that stopped being the engine's: a person reading
		// `in your own folder` off a settled card has to be able to ask what it
		// means in exactly those words.
		{"the card says in your own folder, what does that mean", "tasks"},

		// The second wave, added after probing the corpus the way it is
		// actually queried. Each of these reached the wrong page until the
		// asker's own words were written into a heading, which is the fix this
		// test is meant to provoke.
		// The working discipline the chat itself is taught, asked the two ways
		// people meet it: wanting to know how aforge will go about the job, and
		// asking why it went looking before it started building.
		{"how do you decide how to go about a piece of work", "what-i-can-do"},
		{"why did you search for something that already exists before building it", "what-i-can-do"},
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
		{"the job is drawing over my chat", "tasks"},
		{"stray lines painted over the conversation", "tasks"},
		{"what is the row for my dev server on the right", "tasks"},
		{"can I stop a background job from the sidebar", "tasks"},
		// The jobs-own-section wave: a job is a third section, named, paged, and
		// stoppable from that page. These have to land on the page that owns the
		// column, in the words somebody actually types.
		{"how do I stop a background job", "tasks"},
		{"kill a server I started", "tasks"},
		{"what is running in the background", "tasks"},
		{"the jobs list on the right", "tasks"},
		{"why is my job called that", "tasks"},
		{"where is a job's log path", "tasks"},
		{"stop a job from its page", "tasks"},
		{"do you ask before running rm", "permissions"},
		{"why did it say denied by the person when I did not deny", "permissions"},
		{"does git status need approval", "permissions"},
		{"what is yolo mode", "permissions"},
		{"does --yolo show on the status line", "screen"},
		{"how do I make it stop asking every time", "permissions"},
		{"what model is it using right now", "models-and-cost"},
		{"how do I give it a longer context", "models-and-cost"},
		{"how do I make it think harder", "models-and-cost"},
		// And the reading of the crew that is not the profile's: a run under
		// `--one-model` names the flag on the status line and is owed no crew
		// receipt, because the flag is what seats every call (#444).
		{"why does the status line say one model", "models-and-cost"},
		{"why is it slow over ssh", "running-on-another-machine"},
		// A thought that has gone quiet, asked the way somebody asks it: the
		// patience is measured against how the model usually thinks, so
		// the question has to reach the lanes page rather than the model one.
		{"how does aforge choose its patience for a model that is thinking?", "lanes"},
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
		// And from the screenshot that provoked the one-row clamp: a long bash
		// command spilling over the frame, asked in the four vocabularies people
		// actually reach for — wrapping, spilling, taking too many lines, and
		// wanting the whole command back.
		{"why does a long bash command wrap onto several lines", "screen"},
		{"a tool call is running off the edge of the screen", "screen"},
		{"one tool call is taking up four rows", "screen"},
		{"how do I see the whole command of a tool call", "screen"},
		{"why is the colour from go test output missing", "screen"},
		{"how do I copy text out", "keys"},
		// The pointer gestures themselves: the one everybody already owns, and
		// the complaint they arrive with when the sweep took more than they
		// meant it to.
		{"select a word with the mouse", "keys"},
		{"why did copying take the whole line instead of the words I dragged over", "keys"},
		{"can I turn off the mouse", "keys"},
		{"can you look at a screenshot I paste", "keys"},
		{"do I see my own screenshot in the conversation", "keys"},
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
		// Home's box answers a slash the way a chat's does, and the question is
		// asked in all the words people reach for: the command by name, the verb,
		// and the worry that it will be sent as a message instead.
		{"can I type /settings on home", "home"},
		{"can I run a slash command from the home screen", "home"},
		{"typing /model on home starts a conversation instead of running it", "home"},
		{"open another project from home", "home"},
		{"enter does nothing on home", "home"},
		{"why does it say elsewhere", "home"},
		{"can I work on two repos in one terminal", "home"},
		{"can I work on two projects at once", "home"},
		{"how do I switch back to the last conversation", "keys"},
		{"how do I switch to my other chat", "home"},
		{"is my other conversation still running", "home"},
		{"does my draft move when I switch", "home"},
		// HOW MANY CAN BE OPEN AT ONCE, asked by somebody who remembers being
		// refused at eight and by somebody who has never heard of the cap and
		// simply wants to know where the ceiling is. There is no ceiling, and the
		// page that answers is the one that says what an open conversation costs.
		{"how many conversations can I have open at once", "home"},
		{"is there a limit on how many chats I can open", "home"},
		{"why can I not open another conversation", "home"},
		// WHAT A ROW SAYS WHEN ITS CONVERSATION IS ABOUT SOMEWHERE ELSE, and the
		// search that finds it. The first is somebody reading a word off their
		// own screen; the rest are somebody looking for a conversation they know
		// the subject of and not the folder it was held in.
		{"what does also about mean on home", "home"},
		{"why does a row say also about", "home"},
		{"find the conversation about a folder", "home"},
		{"search home by folder name", "home"},
		// The onboarding wave: home is always reachable, and an empty home is a
		// designed screen. Each of these is asked on a fresh machine, by
		// somebody who tried the gesture on day one.
		{"space space does nothing", "home"},
		{"home is empty", "home"},
		{"how do I get back to home with one chat", "home"},
		{"why is home empty", "home"},
		{"can I open home with only one conversation", "home"},
		// The gesture widened: two spaces answer from every place, not only
		// from a conversation, and these are asked from where a person is
		// standing when they want out of it.
		{"how do I get back to the home screen from the tasks page", "home"},
		{"double space does not go home from the memory page", "home"},
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

		// THE SPENDING WAVE, in the words of the person asking
		// (docs/design/spending/DESIGN.md acceptance 9). Money has one editor
		// and many doors, and every one of these is somebody standing at a
		// different door asking the same question.
		{"how do I remove the daily limit", "models-and-cost"},
		{"why did it stop and ask me about money", "models-and-cost"},
		{"what does per plan mean", "models-and-cost"},
		{"where do I set what aforge may spend", "models-and-cost"},
		{"how much money can a task spend of its own", "models-and-cost"},
		{"why does the limit say no limit instead of $0", "models-and-cost"},
		// Issue #168: a fork's hands were on the machine's day figure twice, and
		// the person who notices is the one asking what their hands are costing.
		{"does the money on the status line include what my hands are spending", "models-and-cost"},
		{"is a hand's spend counted twice in my daily total", "models-and-cost"},
		{"how do I set a limit without opening settings", "commands"},
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
		// The wave that made a watch's LAST tick wake the conversation. Both are
		// asked by somebody deciding whether they can walk away from a watch they
		// just started, which is the only reason to start one.
		{"will it tell me when the watch finishes if I walk away", "what-i-can-do"},
		{"does a watch wake the conversation or do I have to type first", "what-i-can-do"},

		// The streams wave. Every one of these was asked with a running job on
		// screen and a model that either could not see it or was burning turns
		// asking about it: the polling loop, the empty log, the search that
		// failed on every machine without ripgrep.
		{"does it poll a background job or does it get told", "what-i-can-do"},
		{"why is it running sleep and tail over and over", "what-i-can-do"},
		{"how does aforge know a job finished", "what-i-can-do"},
		{"why is the job log empty while it is still running", "what-i-can-do"},
		{"how long does a command wait before it goes to the background", "what-i-can-do"},
		{"does grep work without ripgrep", "what-i-can-do"},
		{"can you search the code on a machine with no rg", "what-i-can-do"},

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
		{"why did it break my job into stages", "adaptive-runs"},
		{"why did it plan the whole thing again instead of just doing it", "adaptive-runs"},
		{"how does it know the work is too big for one worker", "adaptive-runs"},
		{"the run said the brief could not be written, what happened", "adaptive-runs"},
		// The run that had the answer and kept going, asked the two ways it was
		// actually reported: as time and money spent after the fact, and as the
		// word `partial` printed over tests that were green.
		{"why did it keep going after it had the answer", "adaptive-runs"},
		{"it said partial but the tests were green", "adaptive-runs"},
		{"it said done but never ran the tests", "adaptive-runs"},
		{"the request was met as stated", "adaptive-runs"},
		// A stopped run now answers the person's original list point by point.
		// These are the two ways the missing line was reported: asking what did
		// land, and naming the one item the old ending silently dropped.
		{"the run stopped early — which of the things I asked for did it actually do", "adaptive-runs"},
		{"it fixed three of my four and never said which one it missed", "adaptive-runs"},
		// And the same ending arriving the other way round: the work is done,
		// the checks are green, and the run reports a failure because the
		// worker's last call to the model never came back. Both are asked in
		// the words a person has in front of them — the exit code and the
		// provider's own sentence.
		{"it failed but the tests were green", "adaptive-runs"},
		{"the model dropped out after finishing", "adaptive-runs"},
		{"the retry died instantly but my fix is already on disk", "adaptive-runs"},
		{"why did it run the whole test suite when I asked about one package", "adaptive-runs"},
		{"why did it run the tests nine times", "adaptive-runs"},
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
		// The wall a run started from a terminal is given, asked the two ways
		// somebody meets it: typing a length of time with a unit on it, and
		// wanting to know how long the thing waits before it stops.
		{"can I pass 5m as the timeout to aforge do", "adaptive-runs"},
		{"how long does aforge do wait before the timeout stops it", "adaptive-runs"},
		// And a person standing over a headless run that ended wrong. Two of
		// these are asked in front of the evidence rather than about it: a
		// folder they did not expect, and a flag somebody told them about
		// afterwards. Only the third is the question the section was named for.
		{"keep the run's files", "adaptive-runs"},
		{"why is there a folder left behind after aforge do", "adaptive-runs"},
		{"my headless run failed where is its record", "adaptive-runs"},
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
		// The cutting verb, asked the way people reach for it. The first three
		// are the same intention in three vocabularies — join, stitch,
		// concatenate — because which one somebody types is a coin toss and the
		// answer is one page either way.
		{"can you join two videos together", "making-pictures-audio-and-video"},
		{"stitch these clips into one video", "making-pictures-audio-and-video"},
		{"how do I concatenate the mp4 files", "making-pictures-audio-and-video"},
		{"add a soundtrack to this video", "making-pictures-audio-and-video"},
		{"put music under the video", "making-pictures-audio-and-video"},
		{"how long is this video and does it have sound", "making-pictures-audio-and-video"},
		{"get a still out of the video", "making-pictures-audio-and-video"},
		{"save the last frame of the clip", "making-pictures-audio-and-video"},
		// And the two limits people meet: the machine without the binaries, and
		// the edit this verb deliberately does not do.
		{"do you need ffmpeg installed", "making-pictures-audio-and-video"},
		{"can you edit video without a video model", "making-pictures-audio-and-video"},

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
		// V6: The proposal hold and bare-no wording are retrievable in the exact
		// words a person uses after an unwanted auto-start.
		{"the task started before I could say no", "tasks"},
		{"I typed no and it started anyway", "tasks"},
		{"how do I stop a proposed task from starting", "tasks"},
		// And the wait itself: it used to sit there dead, so the words somebody
		// says while looking at it have to reach the page that says it is alive.
		{"is it stuck on shaping the brief", "tasks"},
		// And the shaping call that was cut: the one dim line under the started
		// row is the whole of what a person has to go on, so they type it back
		// verbatim, or they describe what they noticed — a task that started on
		// their own sentence with nothing added to it.
		{"why does my task say brief kept as you wrote it", "tasks"},
		{"my brief was not shaped", "tasks"},
		// The proposal's own forming card is a different block from `/task`'s
		// shaping line. Its still head mark is deliberate; the row below must move.
		{"the proposal card is frozen", "tasks"},
		{"the forming card is not moving", "tasks"},
		// The wait a person can now see into: the preview row under the phase
		// row, asked the three ways somebody meets it — wanting it, describing
		// it, and asking what the extra line is.
		{"can I see the brief while it is being written", "commands"},
		{"what is the line under shaping the brief", "commands"},
		{"several tasks forming at once", "commands"},
		{"why is the line under shaping the brief in italics", "commands"},

		// The answer that gets moved because it ran long. People meet this as a
		// line that appeared under a reply they were reading, so they say it back
		// in the words of the line and in the words of what it did to them: the
		// answer stopped, and something started instead.
		{"why did it say this is running long", "tasks"},
		{"it stopped answering and started a task instead", "tasks"},
		{"my answer was moved to a task", "tasks"},
		{"how long can one answer go before it hands the work over", "tasks"},
		// And the version of it where almost nothing went with the work: the
		// second model could not write the brief, so the task started on the
		// person's own message alone and the line said so.
		{"why did my task start with just my message and nothing else", "tasks"},
		{"the brief could not be written for my task", "tasks"},
		// And the reading of that line that is not a wire failure at all: no model
		// is configured for the reader or the writer, which is a row nobody wrote
		// rather than a provider that went quiet (#443).
		{"what does no second model is set mean", "tasks"},
		// And the case where it must NOT happen: a turn whose whole remaining
		// work is waiting on pieces it already handed out. People meet this as
		// the junk task that appeared while they were watching, and as the
		// question of why the same turn no longer produces one.
		{"it made a task out of me waiting for the other pieces", "tasks"},
		{"does watching a running task count towards moving my answer", "tasks"},

		// And the other end of the same meter: a reply that STOPPED before the
		// question was finished. People meet this as the dim line that said the ask
		// is not finished, or as the thing they noticed happening without them —
		// and the ones who meet it before it fires ask the question that names the
		// old defect, which is why it stopped with the job half done.
		{"the ask is not finished carrying on", "tasks"},
		{"my reply stopped halfway through what I asked for", "tasks"},
		{"it said it would do the rest and then stopped", "tasks"},
		{"why did it keep going without me after it finished answering", "tasks"},
		{"does it check whether my question is actually done", "tasks"},

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
		// The other end of the same impatience: not "decide for me" but "give me
		// longer to speak up", which is a row in the settings panel and a page
		// that has to name the right tab of it.
		{"where is the setting for how long a proposal waits", "tasks"},
		// And the same state met from inside the task's room, where the owner sat
		// with `look it over` on the roster and nothing to press on the page.
		{"how do I approve a task", "tasks"},
		{"task needs my look but there is no button", "tasks"},
		{"accept a finished task", "tasks"},
		{"can I accept the task from inside the room", "tasks"},
		{"why am I not being asked about the sub tasks", "how-tasks-run"},
		// And the same family, asked by somebody who now CAN see the demand and
		// wants to know whether they are allowed to answer it yet. The fold used
		// to be a mute, and both of these were answered "no" by a page that was
		// describing a build nobody runs any more.
		{"can I accept a sub task before its parent finishes", "how-tasks-run"},
		{"does a sub task ask me for a look while the parent is running", "how-tasks-run"},

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
		// Continue is a verb on a settled task, not a narration and not a new
		// proposal. People say the number they saw on the roster; the page has
		// to name those words, and the honest miss when this conversation
		// never held that graph.
		{"continue task 1", "tasks"},
		{"keep going on task 4", "tasks"},
		{"can I continue a task from another conversation", "tasks"},
		{"No task 1 in this project", "tasks"},

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
		// And the pin the width floor moved onto. On 2026-09-02 a designed
		// experiment took that floor out of the default, so the two questions
		// people now bring are the one asked by somebody who WANTS the splitting
		// ("can I make it always split the work") and the one asked by somebody
		// who did not expect it ("why did it split my task into parts") — the
		// second of which nobody asked while a floor was refusing most of them.
		{"can I make it always split the work", "tasks"},
		{"why did it split my task into parts", "tasks"},
		// And the refusal a person meets in the transcript rather than in a
		// design: the split did not happen because two of the parts wanted the
		// same file. The words are the ones they read there.
		{"why was the split refused because two parts wanted the same file", "tasks"},
		// And the three ways somebody asks WHICH of a part's files were even
		// weighed. A brief names everything a part reads; only the
		// done-condition says what it owns, and a person who has just had a
		// split refused reads the two as the same list until the page says
		// otherwise.
		{"what makes a part own a file, its brief or its done-condition", "tasks"},
		{"two parts both mention the same file in their briefs, is that an overlap", "tasks"},
		{"can two parts read the same shared file", "tasks"},
		// And the other half of the same refusal, which a person meets as parts
		// that were told to run the SAME check: a done-condition every part
		// shares is a file every part owns, so nothing can be divided (#569).
		// These are the words they type after reading that.
		{"two parts told to run the same check", "tasks"},
		{"the parts all had the same done-condition and it would not split", "tasks"},
		{"each part should only check its own files", "tasks"},
		{"it refused the split because the parts share a check", "tasks"},
		// Hands: the third weight, and the one a person meets as a line they did
		// not ask for in the middle of their own answer. They ask in the words on
		// the screen — "three hands on it" — or in the words for what they saw,
		// which is one reply doing several things at once.
		{"what does three hands on it mean", "tasks"},
		{"can you work on several parts of my answer at once", "tasks"},
		// And the half of division a person meets from the other side: they walk
		// into the parent's room, type, and the page does not move — because the
		// task is waiting on its own pieces and their line is what wakes it.
		{"I typed into a task and nothing happened", "tasks"},
		{"my task is waiting on its pieces", "tasks"},
		{"why did it warn me about another window", "tasks"},
		{"two windows working on the same files", "tasks"},

		// And the question the division road left standing on the other page.
		// A run used to be what broad work reached for; a chat turn cannot open
		// one at all now, and somebody who expected a planner and watched one
		// worker start asks it in front of that worker.
		{"should this be an adaptive run or one worker", "adaptive-runs"},
		{"why didn't you start an adaptive run for this", "adaptive-runs"},
		{"when do you use a run instead of a task", "adaptive-runs"},
		{"how do I start an adaptive run", "adaptive-runs"},

		// And the door that closed after them: the typed cue. Somebody who
		// learned the words asks in the words they learned, and the page has to
		// say the absence out loud — there is nothing to refuse them with.
		{"I typed orchestrate and it just answered me", "adaptive-runs"},
		{"what words open an adaptive run", "adaptive-runs"},
		{"can I still type orchestrate to start a planned run", "adaptive-runs"},

		// The doors that have nobody in front of them. Somebody wiring aforge
		// into a script asks the first two before they have run anything; the
		// second two are asked afterwards, with the terminal still on screen —
		// one at the flag list they are choosing from, one at the last line the
		// run printed, which is the only line a person cannot read the exit code
		// off.
		{"how do I run aforge without the screen", "adaptive-runs"},
		{"run one task from a script", "adaptive-runs"},
		{"what flags does aforge do take", "adaptive-runs"},
		{"what does the last line of aforge do mean", "adaptive-runs"},
		{"the plan said not settled, what do I do", "adaptive-runs"},
		// And the door below that one: a single worker with no plan behind it,
		// asked either by its name or by what it does.
		{"what is aforge exec for", "adaptive-runs"},
		{"run one worker with no plan", "adaptive-runs"},

		// The one-road wave: work now STARTS on its own after a words-only turn,
		// with a line on the transcript and no card to answer. Every one of these
		// is asked by somebody looking at a task they did not ask for, in the
		// words the transcript handed them.
		{"why did a task start on its own", "tasks"},
		{"this looked like work so task started", "tasks"},
		{"aforge started work I did not ask for", "tasks"},
		{"how do I stop it starting tasks by itself", "tasks"},
		{"what happened to the card asking whether to run it", "tasks"},
		// The spawn floor: a one-command ask that used to become a task and
		// drop the deliverable. Asked in the words the person typed.
		{"why did commit become a task", "tasks"},
		{"undo started a task", "tasks"},
		{"fix this one line became a task", "tasks"},

		// And the shape that reads strangest of all, because the reply had
		// already started: a reply can stop halfway and hand itself over, when a
		// second model reads what is left of it and finds independent parts.
		// Somebody watching that happen asks about the half-answer, not about a
		// judge — and they say the line back in its own words.
		{"this has parts handing it to a task", "tasks"},
		{"why did it hand my answer to a team", "tasks"},
		{"it started answering and then handed the work over", "tasks"},
		{"why did my reply stop halfway and become a task", "tasks"},

		// The fifteenth wave: the crew that looks like it did nothing. `/crew max`
		// writes four class models and the session picks them up on its next
		// call — and the model on the status line does not move, because that one
		// is the CONVERSATION's and the crew never touches it. So a person reads
		// the confirmation, looks at a frame that says exactly what it said
		// before, and asks these in front of it.
		{"I changed the crew but the model didn't change", "models-and-cost"},
		{"why does the bottom still show the old model after /crew", "models-and-cost"},
		{"does /crew change the model I'm talking to", "models-and-cost"},
		// A crew older than the worker class: the run says `inherited` and the
		// person asks about the word, or about the model they never picked.
		{"why does my run say inherited", "models-and-cost"},
		{"my crew was set before the work seat existed", "models-and-cost"},
		// And the same substitution met in the conversation, where the person
		// has no models line to read the word off — they ask about the task.
		{"why is my task running on a model I did not pick", "models-and-cost"},
		{"my work seat is inherited from small work", "models-and-cost"},
		// The same crew question asked from outside the conversation, by
		// somebody whose runs happen with nobody watching.
		{"what models does a headless run use", "models-and-cost"},
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
		{"if I quit aforge while a task is splitting into child work does it all stop", "keys"},

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
		// THE PHASE CLOCK (internal/tui3's phase.go). The connection now says which
		// of its own slownesses a request is in, so the questions people ask while
		// nothing is arriving have a page with the exact words on it. The last two
		// are the reported defect itself: a stalled reasoning pass that said
		// nothing but "still working", and no sense of when anything would be done
		// about it.
		{"what is it doing right now", "screen"},
		{"it says thinking and nothing is on the screen", "screen"},
		{"what does first word mean while it is waiting", "screen"},
		{"how long until it gives up on this one", "screen"},
		{"what does still working mean", "screen"},
		// AND THE STAGE THAT LASTS. A reading between rounds can run for minutes,
		// and until the beat was written the line went blank after fifteen
		// seconds while the work carried on — so these are asked in the words of
		// somebody watching that happen, and in the word the line now wears.
		{"what does taking stock mean", "screen"},
		{"the status line went blank while it was still working", "screen"},
		{"does a slow stage stop being shown", "screen"},
		// THE ANSWER HIERARCHY (internal/tui3's hierarchy.go). A turn's narration
		// now recedes into the work column at a quieter shade and only the block
		// the turn ended on is drawn as the answer, so somebody looking at a reply
		// half in one tier and half in another asks the first three of these — and
		// somebody who pressed esc and watched nothing turn into an answer asks the
		// last two. They are the screen's, because every word of the question is
		// about what is on it.
		{"why is part of the reply grey", "screen"},
		// #225: the surface being unsure what the final answer is, in the three
		// shapes it was reported in — an answer filed as thinking, a `<think>`
		// tag typed into the reply, and a reply that stayed raw until the next
		// question was asked.
		{"why did my answer show up as thinking", "screen"},
		{"the model thought and never answered", "screen"},
		{"why is there a <think> tag in my answer", "screen"},
		{"the reply stayed raw until I asked something else", "screen"},
		{"the reply pops in as a block instead of streaming smoothly", "screen"},
		{"why does the text jump", "screen"},
		{"the answer arrives as a blob of text", "screen"},
		{"why do the tokens on the status line jump instead of counting up", "models-and-cost"},
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
		// 2026-09-01: a screen full of the model's own think-closer, stopped
		// by hand. Asked as what was on the screen, as what happened to it
		// after the stop, and as the question about the thinking pass.
		{"the reply was just </think> repeated over and over", "models-and-cost"},
		{"I stopped it while it was printing garbage, is that still in the conversation", "models-and-cost"},
		{"does it watch the thinking for repetition too", "models-and-cost"},
		// The same stop asked by somebody who did NOT know the reply had come
		// apart: they pressed a key and their text went, so they ask what took
		// it and where it went, in the two spellings of that key.
		{"why was the text deleted when I pressed escape", "models-and-cost"},
		{"where did the reply go after I hit esc", "models-and-cost"},

		// The 2026-08-31 incident, asked the ways a person describes what they
		// saw: a serving endpoint leaked the model's own tool grammar as text,
		// and the screen filled with markup instead of an answer.
		{"the screen filled with weird tokens instead of an answer", "models-and-cost"},
		{"the reply was full of tool markup and angle brackets", "models-and-cost"},
		{"the model kept writing its own internal markup", "models-and-cost"},

		// /model while a task runs: the task keeps its model, and the person
		// who watched the old voice continue asks why.
		{"I changed the model but my task is still on the old one", "models-and-cost"},
		{"does /model change the model my running task uses", "models-and-cost"},
		{"why does the via name keep changing on the model list", "models-and-cost"},
		{"why does the model picker keep jumping", "models-and-cost"},
		{"how do I turn off the reply guard", "models-and-cost"},
		{"the model stopped answering halfway through", "models-and-cost"},
		// Asked from a bill rather than from a screen: a cost autopsy found one
		// turn hopping across six endpoints, each hop paying full price for a
		// context the last endpoint already had. Both halves of that are things
		// somebody asks — the money, and the hopping.
		{"why does the same conversation suddenly cost more", "models-and-cost"},
		{"does it keep the prompt cache warm", "models-and-cost"},
		{"why did old tool results turn into pointers during one long answer", "models-and-cost"},
		{"what does folded results tokens mean", "models-and-cost"},

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

		// Sampling is the provider's own dial on every call, so it is asked
		// about in the API's own words — temperature, top_p — and as the
		// creativity knob a person from another tool went looking for.
		{"what temperature do you use", "models-and-cost"},
		{"can I change the sampling settings", "models-and-cost"},

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
		// The two clocks, asked apart: how often aforge LOOKS, which is one
		// figure for everything standing, and a cadence somebody gave an item
		// themselves, which is the one they name when they want it to stop.
		{"how often does my watch check", "keeping-an-eye"},
		{"stop the thing that runs every hour", "keeping-an-eye"},
		// Asked by somebody who has not set a key yet and wants to know whether
		// the standing side is doing anything at all in the meantime. The answer
		// is half yes — the walk happens, the judgments do not — and it has to
		// come from the page rather than be guessed at.
		{"does the background check run before I set an API key?", "keeping-an-eye"},
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
		{"continue this chat in another terminal", "home"},
		{"move the conversation to this window", "home"},
		{"it says open in another window", "home"},
		// The transfer wave: every state of a move, asked the way somebody
		// staring at one asks it. The first is the report's own complaint —
		// nothing appeared to happen — and the rest are the states that used to
		// be silent.
		{"why is moving a conversation so slow", "home"},
		{"it says coming here and nothing happens", "home"},
		{"that window did not answer", "home"},
		{"I pressed enter to move a chat and nothing happened", "home"},
		{"how do I cancel moving a conversation here", "home"},
		// And the resting row's own words, which is where somebody looking at
		// `another window` starts: they are staring at a margin, not at a card.
		{"the row says another window, how do I get it back", "home"},
		{"how do I bring a conversation back to this window", "home"},
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
		{"why is the answer from home showing asterisks and hashes", "asking-from-home"},
		{"does the ask here pane format the answer", "asking-from-home"},
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
		{"i have two copies of aforge, which one runs the background checks", "keeping-an-eye"},
		{"two copies of aforge and my reminders fired twice", "keeping-an-eye"},
		{"does AFORGE_HOME move the background timer", "keeping-an-eye"},
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
		// The word nobody on this surface uses and everybody types: there is no
		// delete, and the page has to be reachable by the two ways somebody asks
		// for one before they learn that stopping is what it is called.
		{"delete a standing order", "standing-orders"},
		{"get rid of a standing order", "standing-orders"},
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
		// The wave that made a KEYSTROKE-shaped drop land. The owner met both of
		// these on a real terminal over --host, and both are asked in the words
		// they used: the refusal they were given, and what they were looking at
		// when they gave up on it.
		{"I dropped a file and it said unknown command", "attaching-files"},
		{"drag and drop shows the path as text", "attaching-files"},
		{"why is the path of my screenshot in the message box", "attaching-files"},
		{"my screenshot path has spaces and stays text", "attaching-files"},
		{"my terminal types the drop instead of pasting it", "attaching-files"},
		{"I dragged a screenshot from Windows and it pasted c:/Users as text", "attaching-files"},
		{"does drag and drop work on WSL", "attaching-files"},
		{"can I paste an image from the clipboard", "attaching-files"},
		{"can I drop a file after a slash command", "attaching-files"},
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
		// The hosted rail, met the way it was actually reported: work that ran on
		// the far machine with nothing on the column beside the conversation.
		{"I started a task over ssh and the sidebar stayed empty", "tasks"},
		{"my task ran on the remote machine but there is no row for it", "tasks"},
		{"task started over host does not show on the roster", "tasks"},
		{"the task page does not list the task I just started", "tasks"},
		{"a run's transcript could not be read on that machine", "adaptive-runs"},

		// The wave that gave the room one keyboard. Two windows on one hosted
		// conversation used to race each other in silence; now the newest one
		// types and the rest watch. These are the sentences somebody types with a
		// composer that has just turned into a line they did not ask for — and
		// the ones they type at the OTHER window, wondering what it did.
		{"someone else is typing", "staying-on-that-machine"},
		{"why can't I type", "staying-on-that-machine"},
		{"two terminals on the same chat", "staying-on-that-machine"},
		{"what does --no-host do", "staying-on-that-machine"},
		{"take over the keyboard", "staying-on-that-machine"},
		{"my input box turned into one line", "staying-on-that-machine"},
		{"what does typing from now mean", "staying-on-that-machine"},
		{"can two windows share one conversation", "staying-on-that-machine"},
		{"how do I get the keyboard back", "staying-on-that-machine"},
		{"is my draft lost when the other window takes over", "staying-on-that-machine"},
		{"does the other window see the turn I started", "staying-on-that-machine"},
		{"what happens to the keyboard when a window closes", "staying-on-that-machine"},
		{"my window came back and now I cannot type", "when-the-connection-drops"},
		{"it said a reading fell over once", "when-the-connection-drops"},
		{"it says the keyboard is on another machine", "staying-on-that-machine"},

		// THE MANUAL'S OWN DOOR. Until it had one the manual had exactly one
		// reader and it was not the person: every lookup was a model call, so it
		// wanted a key and cost money, and what came back was a retelling. These
		// are the words somebody uses when they want to read it themselves —
		// from the conversation, and from a terminal where nothing is set up yet.
		// LANES — the machine behind the model id. The page had no probe of its
		// own until now, which meant every question about the endpoint that
		// answered was reaching whichever page happened to repeat the word.
		// One per heading, in the words somebody says in front of it.
		//
		// The floating alias, asked the three ways it is met: the word on the
		// end of the shipped model's name, the record naming a build the person
		// never picked, and the character in front of it that looks like a path.
		{"what does latest mean in the model name", "lanes"},
		{"why does via say 0731 when I picked latest", "lanes"},
		{"the model name has a tilde in it", "lanes"},
		// AUTO, asked from a terminal rather than from a conversation. The
		// first two are somebody deciding whether the headless doors get any of
		// this at all, and the third is the one people ask on day one, before
		// aforge has timed anything of theirs.
		{"does aforge do pick the fastest endpoint too", "lanes"},
		{"does a headless run choose between lanes", "lanes"},
		{"why did it pick that provider on my very first message", "lanes"},
		// Naming the machine yourself — asked as the worry underneath it, which
		// is whether a pin is honoured — and reading the line that says which
		// machine actually answered.
		{"will it send my work to a different lane than the one I pinned", "lanes"},
		{"does aforge do use the lane I pinned", "lanes"},
		{"is my pinned provider used when I run from a terminal", "lanes"},
		// And the one thing that ends a pin without the person: the router
		// saying that machine cannot serve that model at all (issue #456). It
		// is asked as somebody reads it on the screen and wants to know what it
		// costs them.
		{"I pinned a provider and it says it cannot serve this model, what happens now", "lanes"},
		// And the base that will not carry the pin at all, which is the #433
		// question in the words somebody on a proxy actually types.
		{"I pinned a lane but I am on a proxy, does the pin still work", "lanes"},
		{"what does via cloudflare mean on the status line", "lanes"},
		// The offer, from the side the page owns: what the answer NO would be.
		// The key itself is the keys page's, because that is where somebody
		// looking for a keystroke looks.
		{"how do I say no to switch to auto", "lanes"},
		{"what key answers switch to auto", "keys"},
		// The wait nothing can end, and the row that turns it off.
		{"what does all lanes slow still waiting mean", "lanes"},
		{"how do I turn off endpoint routing", "lanes"},
		{"how do I read the manual", "commands"},
		{"is there a help page", "commands"},
		{"show me the page about a command", "commands"},
		{"can I read the manual from the terminal", "commands"},
		{"does reading the manual cost anything", "commands"},
		{"list every page of the manual", "commands"},

		// PAIRING, ASKED BY SOMEBODY WHO WATCHED IT CONTRADICT ITSELF. The machine
		// used to say "paired" before it had written the device into its list, so a
		// first `--at` on a slow disk could be told in the next breath that the
		// device had been stopped. It cannot any more, and the words a person brings
		// to that are the two sentences they just read on their own screen.
		{"it said paired and then that the device was stopped, what happened?", "reaching-this-machine-without-ssh"},
		// THE TERMINAL VERBS. Six of them — `why`, `notebook`, `competence`,
		// `services`, `wake` and `rebuild` — were in no page at all, so a person
		// who asked the chat how to see what a piece of work did was answered by
		// improvisation. These are the words a developer actually types, and the
		// gate that would have caught the omission is
		// TestTheChatManualMentionsEveryVerbTheCommandLineAnswersTo.
		{"can I run this without the chat", "running-from-the-terminal"},
		// The ending #593 added, in the words somebody meets it in: on the
		// stderr line they have just read, on the word in `--json`, and on the
		// exit code they are staring at with a perfectly good answer above it.
		// The page had none, and a page whose retrieval nothing holds is a page
		// the chat talks over the top of — see the lanes page and #453.
		{"what does it mean when a run says it was delivered without a check", "running-from-the-terminal"},
		{"what does stop unchecked mean", "running-from-the-terminal"},
		{"why did my headless run exit 2 when the answer looks fine", "running-from-the-terminal"},
		{"what does aforge wake do", "running-from-the-terminal"},
		// "how do I see what a task did" is deliberately NOT here: in the chat a
		// task's own room is that question's answer, and how-tasks-run rightly
		// wins it. The terminal reader is asked for in the words of the thing it
		// reads — a step of a headless run, a node, its turns and tool calls.
		{"how do I see what one step of a headless run did", "running-from-the-terminal"},
		{"how do I read a node's record", "running-from-the-terminal"},
		{"show me the turns and tool calls of one piece of work", "running-from-the-terminal"},
		{"how do I read a headless run's record afterwards", "running-from-the-terminal"},
		{"what have I spent today from the terminal", "running-from-the-terminal"},
		{"how do I retract a lesson aforge learned", "running-from-the-terminal"},
		{"what is aforge measured as being good at", "running-from-the-terminal"},
		{"how do I stop a dev server aforge started", "running-from-the-terminal"},
		{"what background processes are still running", "running-from-the-terminal"},
		{"how do I replay the journal and rebuild the tables", "running-from-the-terminal"},
		{"which commands need no api key", "running-from-the-terminal"},
		{"how do I read a plan file back as a table", "running-from-the-terminal"},
		{"why does aforge show --help print a file error", "running-from-the-terminal"},
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

// THE CHAT MANUAL SPEAKS THE PERSON'S WORDS AND NOT THE HARNESS'S.
//
// `auditor`, `verdict`, `verified` and `refuted` are the machinery's own
// vocabulary, and internal/session bans them from every string a person or the
// chat model reads (task_audit.go's vocabulary law, held there by
// `assertPlainWords`). The manual is read by both — the chat answers "why did it
// do that" out of these pages — so the same law holds here, and it needs a gate
// of its own because a page is written by hand and no landing passes through it.
//
// A WORD INSIDE BACKTICKS IS AN ADDRESS AND NOT A FINDING, which is the same
// carve-out the engine's own test makes for `task.audit` and `reaudit`: the role
// a spend row is filed under is called `auditor`, a person reading their bill has
// to be able to find it, and a page that renamed it would be a page whose word
// their machine does not answer to.
//
// AND IT IS THE FOUR WORDS AND NOT THE WHOLE LIST. `audit` is in `task.audit` and
// in half the sentences about it; `unverified` is what bare's own file rows are
// called. Those are addresses too, and the four here are the ones that describe
// WORK — which is what the law is about.
func TestNoChatPageSpeaksTheHarnessesOwnVocabulary(t *testing.T) {
	banned := regexp.MustCompile(`(?i)\b(auditors?|verdicts?|verified|refuted)\b`)
	// Everything inside backticks is a handle somebody types or reads back.
	handles := regexp.MustCompile("`[^`]*`")
	for _, section := range Chat().Sections() {
		scanned := handles.ReplaceAllString(section.Title+"\n"+section.Body, "")
		if found := banned.FindString(scanned); found != "" {
			t.Errorf("%s · %q says %q, which is the harness's own vocabulary and not the person's",
				section.Page, section.Title, found)
		}
	}
}

// C14: the manual's protected-name list is held against the engine's one policy
// list, so changing a branch name cannot leave the person reading stale advice.
func TestC14TheChatManualNamesEveryProtectedBranch(t *testing.T) {
	const source = "../session/task_branch_protection.go"
	parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	var names []string
	for _, decl := range parsed.Decls {
		block, ok := decl.(*ast.GenDecl)
		if !ok || block.Tok != token.VAR {
			continue
		}
		for _, item := range block.Specs {
			value, ok := item.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "protectedBranchNames" || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s: protectedBranchNames is not one literal list", source)
			}
			for _, element := range literal.Elts {
				word, ok := element.(*ast.BasicLit)
				if !ok || word.Kind != token.STRING {
					t.Fatalf("%s: protectedBranchNames contains a non-string entry", source)
				}
				name, err := strconv.Unquote(word.Value)
				if err != nil {
					t.Fatal(err)
				}
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s: protectedBranchNames was not found", source)
	}
	page, ok := Chat().Page("how-tasks-run")
	if !ok {
		t.Fatal("the chat manual has no how-tasks-run page")
	}
	for _, name := range names {
		if !strings.Contains(page, "`"+name+"`") {
			t.Errorf("how-tasks-run does not name protected branch %q", name)
		}
	}
}

// #334: a row of the model-call log may be short of a number JSON cannot spell,
// and it says which one in its note. The page-level probe above is not enough
// to hold that: the whole page is about calls and costs, so it reaches
// models-and-cost with or without the sentence. This pins the sentence itself
// to the section a person asking the question is actually handed.
func TestTheCallLogPageSaysWhyAFigureIsMissingFromARow(t *testing.T) {
	const said = "cost_s was +Inf"
	for _, section := range Chat().Search("why is cost_s missing on a call log row", DefaultResults) {
		if section.Page == "models-and-cost" && strings.Contains(section.Body, said) {
			return
		}
	}
	t.Fatalf("the question does not reach a section that says %q; a row missing a figure reads as a broken row", said)
}

// #578: a ground never climbs out of the machine's scratch, so a workspace under
// the temporary directory runs in place instead of cutting a branch off whatever
// repository happens to sit above it. The page-level probe above cannot hold
// that on its own — how-tasks-run wins every question with the words "task" and
// "branch" in it, edit or no edit — so this pins the sentence to the section the
// person who noticed the missing branch is actually handed.
func TestTheGroundPageSaysAWorkspaceInScratchDoesNotClimbOutOfIt(t *testing.T) {
	const said = "climbs out of the machine's scratch"
	for _, section := range Chat().Search("my task under /tmp ran in place instead of getting its own branch", DefaultResults) {
		if section.Page == "how-tasks-run" && strings.Contains(section.Body, said) {
			return
		}
	}
	t.Fatalf("the question does not reach a section that says %q; somebody whose task got no branch is left with the ladder alone, which reads as though rung 3 had answered", said)
}

func TestCanYouSearchTheWebReadsTheFirecrawlLadder(t *testing.T) {
	// V6: The question a person asks retrieves the implemented zero-key ladder
	// and the exact Firecrawl failure string in one self-contained section.
	found := Chat().Search("can you search the web", DefaultResults)
	for _, section := range found {
		if section.Page != "what-i-can-do" || section.Title != "Can you search the web?" {
			continue
		}
		for _, want := range []string{
			"Firecrawl: keyless, with a free monthly allowance and no key needed",
			"DuckDuckGo remains available as an explicit pin",
			"Search failed (firecrawl): <err>",
			"5 of 12 results · firecrawl",
		} {
			if !strings.Contains(section.Body, want) {
				t.Errorf("search section does not contain %q:\n%s", want, section.Body)
			}
		}
		return
	}
	t.Fatalf("search question did not retrieve its section: %#v", found)
}

// V6 and V9: the searchable account says settings are live, names receipts,
// and contains no stale next-session promise about search.
func TestSearchManualDescribesLiveSettingsAndNamedReceipts(t *testing.T) {
	for _, test := range []struct {
		question string
		title    string
		wants    []string
	}{
		{
			question: "which search engine answered?",
			title:    "Which search engine answered?",
			wants:    []string{"5 results · firecrawl", "3 of 8 results · exa", "no results · firecrawl", "search"},
		},
		{
			question: "I set a search key and nothing changed",
			title:    "I set a search key and nothing changed",
			wants:    []string{"next search in this conversation", "Search failed (exa): no API key", "now firecrawl, keyless"},
		},
	} {
		found := Chat().Search(test.question, DefaultResults)
		var body string
		for _, section := range found {
			if section.Page == "what-i-can-do" && section.Title == test.title {
				body = section.Body
				break
			}
		}
		if body == "" {
			t.Errorf("%q did not retrieve %q: %#v", test.question, test.title, found)
			continue
		}
		for _, want := range test.wants {
			if !strings.Contains(body, want) {
				t.Errorf("%q section does not contain %q:\n%s", test.title, want, body)
			}
		}
	}

	for _, section := range Chat().Sections() {
		body := strings.ToLower(strings.Join(strings.Fields(section.Title+"\n"+section.Body), " "))
		for _, sentence := range strings.Split(body, ". ") {
			if (strings.Contains(sentence, "web_search") || strings.Contains(sentence, "search.exakey") || strings.Contains(sentence, "search.firecrawlkey")) && strings.Contains(sentence, "next session") {
				t.Errorf("search sentence in %s/%s still promises the next session: %s", section.Page, section.Title, strings.TrimSpace(sentence))
			}
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
