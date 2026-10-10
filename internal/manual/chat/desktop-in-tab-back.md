# Desktop in-tab back

## How do I go back from a desktop task to its conversation?

A task opened inside a desktop conversation keeps the same tab. Its header starts
with an arrow-left and the immediate parent's name, then `/` and the current task
name. A root task names its conversation as the parent. Click that parent button
to go back one history step. The header does not list every ancestor.

On macOS, Command+[ goes back and Command+] goes forward. Elsewhere, Control+[
and Control+] do the same. Shifted brackets are reserved for tab navigation.
Drilling into a task records the previous route; returning does not open a new tab.

## Does the desktop parent button use window focus history?

When the shell supplies window focus history, the parent button and bracket
shortcuts both travel one step in that history, including a previous tab or
place. The label still names the task's parent; it is a return action rather
than a jump directly to that ancestor. Without the window-history provider,
they use the conversation's local task route history. Background task opens
leave the current conversation in focus.
