# Task controls and the task page

## What is at the top of a task page

The pinned header carries the breadcrumb trail and Back. In the expanded layout,
the breadcrumb names ancestors; one row below it combines the bold task title
with state, activity, time and known cost. The divider comes below that row.
The footer explicitly labels its figures `Conversation totals`. The right column keeps the
existing task tree at the top. The input says `To: <task name>` when the terminal
has room for that row. Back and Escape change the view; neither stops work.

## Scroll the task tree and conversation context independently

On a side column with enough height, the task tree is at the top. A separate
Conversation section appears only when standing instructions or background jobs
exist. Each has its own scroll window. Turn the mouse wheel over the section you
want to read. `alt+pgup` and `alt+pgdown` scroll the Conversation section while
the task page has the keyboard. The tree retains its existing keyboard navigation.
Scrolling over task setup does not move the transcript.

Task setup sits below these windows. `Model` names the working model; `Thinking`
shows the task's own reasoning override, or `auto` when none is set. Clicking its
cycle control changes the open task, like `ctrl+v` in that task's room. It cycles
`auto → low → medium → high → xhigh → max → auto`. `auto` clears the task
override; on a completed task, that choice is saved for its continuation. Unsupported
controls are absent. Ordinary settled tasks show `Next run setup`: model and
thinking choices are saved for when you continue, without restarting work or
rewriting the last attempt. An adaptive run's published
planner and budget are shown as facts. Assignment and acceptance remain in the
main task content rather than being repeated in a narrow sidebar.

Deep task trees keep their full ancestry. When indentation would hide a task
name, `…` replaces outer connectors; breadcrumbs still open the actual ancestors.
Long sections show their position; resizing clamps each section to its available
rows. Short or narrow terminals retain the compact column and existing controls.

## Stop this task — /stop and the Stop task button

Click `Stop task…` under Task setup, click `Stop` in the compact task header, or type
`/stop` while inside the task. These open the same confirmation; work is stopped
only after you choose `stop it`. The initial selection is `keep going`.
`x` is also a shortcut, but only when the input is empty. A stopped task's branch
is kept; a design task can discard its unsaved page, and the confirmation says
which consequence applies. Runs and background jobs use their own existing
stop confirmations. The header and controls stop the work on the open page.

## Asking the chat to stop a task — can I tell the chat to stop a task, I asked it to stop task 2 and it kept going, the model said it stopped but the task is still running, cancel task 2

**Yes, and it is a real stop.** Say "stop task 2", "cancel task 2" or "drop task 2" in the
conversation and the model calls `tasks` with that id and `stop`. That is the same door
`/stop`, `x` and the `Stop task…` row take, so the ending is identical whichever hand pulls
it: the worker is cut off where it stands, its branch is kept with its work committed on it,
the row reads `stopped`, and what it spent freezes where it was.

**It asks you nothing.** Your own `x` raises a confirmation card, because one bare keystroke
over a list should not be able to end an hour of work. The sentence you typed already *is*
the decision, so the model does not hand it back to you as a question. If you would rather
have the card, press `x` or type `/stop` inside the task.

**Your reason goes with it.** Whatever you gave — "I changed my mind", "task 3 covers this"
— is written onto the task's own record beside the word `stopped`, so the row afterwards
says why. What the model reads back is the line you would have seen yourself, with your
reason in it: `stopping task 2 (Port the parser): I changed my mind — its branch is kept`.

**Telling a task to stop is not stopping it.** A line sent *into* a running task — the
model's `say`, or your own words typed in its room — is a message, and a message can be
ignored, or answered "stopped as instructed" while the work carries on. Worse: a worker that
ends a turn having delivered nothing looks exactly like one that ran out of road, so the
check says it is not done, `closing gaps · round 1 of 1` opens, and the task keeps working
and spending. A real stop never reaches the check and is never handed back for a round.

**A task that is not running answers with what it is**, rather than refusing: `task 2 is
done`, or `stopped`, or `your call`, in the words your screen is showing. Work running in
**another aforge window** cannot be stopped from here — the window that owns it has to.

## I told aforge to decide and it still says your call — the model says there is no graph left, the task graph expired, I pressed let aforge decide twice

Pressing `let aforge decide` on a your-call card hands **that one card** to the model
(it changes no setting) and puts a line in front of it about the work. The model then
reads the branch and answers with accept, not right, or check it again.

**If the card stayed `your call`, the model's door used to be narrower than yours.** Your
press finds the task by its id in the conversation you are sitting in. The model's `tasks`
tool looked the number up in the **project's** list of finished work — and every
conversation in a project appends to that one list, while task numbers start again at 1
in each conversation. So "task 2" could match a different conversation's task 2, and the
model was told `task 2 ran in an earlier conversation, so there is no graph left to settle
it in` and pointed at a worktree belonging to work it had never seen. A number now means
**this conversation's** task, so the two doors reach the same work. A *name* — `@hidden-rental-digs` —
still means the newest task anywhere in the project that wears it, which is what a name is for.

**Pressing it twice does nothing the first press did not do.** The second press answers
`already handed to aforge` and sends the model nothing further; the card keeps reading
`handed to aforge for this one`. If you want it back, press `take it back` and the chips
return.

**A recovered task is this conversation's task.** Reopening a conversation brings its
graph back — `recovered task graph: 1 done · 1 your call` — and everything the card offers
works on those tasks, the model's answers included.

## The merge round stopped when aforge restarted — I pressed resolve it and nothing happened, my conflict still says your call

`resolve it` on a conflict card starts one round: your branch is merged into the task's
branch inside the task's own copy, a worker settles the clashing files, the work is checked
again and the landing is retried. It costs a model call and it takes minutes.

**If aforge is restarted or the engine is replaced while that round is running, the round
dies with it.** Nothing is lost and nothing is broken: the task is exactly where it was —
`your call`, its files still clashing, its branch untouched — and the card offers the same
three answers. What was missing was any word about it, so the card read as though you had
never pressed. The line when the conversation comes back now says so:

```
recovered task graph: 1 done · 1 your call (its merge round was cut)
```

**Nothing restarts it for you.** A round spends a model call, and a session that spent one
on its own while opening would be spending your money on a decision you did not make. Press
`resolve it` again if you still want the round, or take the branch yourself — `git merge task/…`
— or answer the card another way.

## Change this task's model

`/model` and the model row under Task setup target the open ordinary task.
A running task changes on its next turn. A queued task takes the new model when
it starts. A settled task saves the choice under `Next run setup`; continuing
applies it to the next attempt. Choosing a model alone does not continue work. They do not change the conversation or sibling tasks. A model that cannot
be changed is shown as a fact without a clickable control. The model picker
continues to be available in the task status line on compact terminals.

## Where is the original task brief and acceptance

The task request keeps its existing three-line preview and clickable disclosure;
`ctrl+o` expands or collapses it. Its completion criteria and context remain inside
that request. Completed work keeps its existing `ctrl+e` disclosure. The sidebar
does not repeat the request or change the transcript's presentation.

## Task setup through the session host

Model and thinking controls work through the normal local session host and remote
connections as well as in-process sessions. Choices are bound to the open
conversation and task. Rendering reads thinking settings from task updates.
An older connected engine shows `Engine update needed` instead of editing controls: update that engine and
reconnect. An older local host holding work keeps running until it goes quiet;
rebuilding the terminal binary alone does not replace a busy host.
