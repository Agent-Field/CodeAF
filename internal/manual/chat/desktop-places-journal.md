# The record of organizing places

## Where is the record of an organize run

codeaf writes one record of each organize run beside your places, in `~/.codeaf/desktop/places-journal.jsonl`. One line is one thing that happened, in order: the run starting, each change it proposed, and the run ending. Reading that file back lists those changes in the order they were written. The record is not the undo. Taking a whole run back is the one undo receipt for that run.

## What if organizing places crashes halfway through

A crash between lines leaves every finished line readable. The line that was still being written is dropped the next time the record is opened, and the lines before it stay. The record is saved to disk when a run ends, so a finished run is the one that survives a power loss. A run that had not ended can lose the lines written since the previous finished run.

## Does the places organize record grow without limit

When the live file reaches 32 MB, codeaf starts a new one and keeps the old one, named `places-journal.jsonl.1`, `.2`, and so on. `.1` is the newest kept file and a higher number is older. Reading a run back looks through the kept files and the live file, in the order the lines were written. Older runs stay readable. The kept files are not deleted.
