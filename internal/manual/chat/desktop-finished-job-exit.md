# Finished job exit on the tab

## Exit 0 after the name on a finished job tab

When a job has finished on its own, its tab shows the job's name and then `exit 0`, or `exit` and the number the job actually returned. Those words are muted. The name still fades at the end of the title when the tab runs out of room; the exit words sit after that fade, so they stay readable.

A job that is still running shows nothing after its name. A shell shows nothing after its name. A job you stopped yourself shows nothing after its name; the header says it was closed.

The tab does not show how long ago the job ended. That time stays in the header, next to the same exit number.

## A running job tab shows nothing after its name

A job that is still running shows only its terminal icon and its name. There is no exit line, no elapsed time, and no spinner on the tab. Running and the elapsed time are written in the header inside the tab.

Until the tab has read the job, it also shows nothing after the name. An unknown exit is not drawn as zero ahead of that.

## A shell tab shows no exit code

An interactive shell's tab is the terminal icon and the shell's name. It does not show an exit code while the shell is open, and it does not show one after the shell ends. Exit words are only for a job that finished on its own.

## Non-zero exit on a finished job tab

A job that finished with an exit other than zero still shows that number after its name, muted, as `exit 2` or whichever number it returned. The words are not red. The tab's icon becomes the failed dot, the same mark any failed tab uses.
