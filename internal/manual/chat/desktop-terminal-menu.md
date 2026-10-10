# Desktop terminal and job menus

## What is in a finished job's terminal menu?

The terminal header's More menu offers “Run again”, a separator, “Close tab”, and “Remove job”. Run again starts the same command in a new tab. Close tab detaches the view and keeps the job and its output. Remove job removes the engine job and its kept output, then closes the tab.

“Open log” comes before Run again only when a retained log file can be opened. The current desktop terminal client does not report a retained log file, so Open log is absent. Kept terminal output does not imply an available log file.

## Why is Stop missing from the running terminal menu?

A running job or shell offers “Copy output”, a separator, and its Remove action. Stop lives on the header button while the process runs; the menu has no disabled Stop item. Copy output copies the selected text, or the recent output when nothing is selected. The finished job header has no Stop button.
