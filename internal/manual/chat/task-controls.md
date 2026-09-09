# Task controls and the task page

## What is at the top of a task page

The pinned header carries the breadcrumb trail and Back, a bold task heading,
and the task's state, time, activity and known cost. In the expanded layout, the breadcrumb names ancestors, the current
task has its own heading, and the divider comes below the complete header.
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
cycle control changes the open task, like `ctrl+v` in that task's room. Unsupported
controls are absent, and settled values are read-only. An adaptive run's published
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

## Change this task's model

`/model` and the model row under Task setup target the open ordinary running
task. They do not change the conversation or sibling tasks. A model that cannot
be changed is shown as a fact without a clickable control. The model picker
continues to be available in the task status line on compact terminals.
