# Desktop place knowledge storage

## What happened to my place instructions and paragraphs?

The desktop place graph can store knowledge as individual lines. Each line keeps its place, text, source and creation time. When an older places file is opened, its instructions migrate once: each nonempty paragraph becomes one line whose source is `you-wrote`. Wrapped lines within a paragraph stay together. The old `instructions` field remains readable and is written empty. A persisted migration marker prevents another import on reopening, including after an older build writes instructions again.

This is the storage layer. It does not by itself add a Home editor, a chat “remember” tool, contradiction detection or a “still true?” prompt.

## Where do knowledge line sources come from?

A knowledge line records one of four sources: `you-wrote`, `said-in-chat`, `learned`, or `file`. Chat identifiers, source times, answer counts and file paths are retained when supplied; unknown values stay absent.

Migration also records file-source lines for existing file and URL references with an explicit text suffix, such as `.md`, `.txt` or `.json`. The line uses the source label, or the reference if no label exists. It retains the path or URL and source time. Migration never opens a file or fetches a URL. Binary and unknown references, folders, repositories and chat references stay only in the unchanged source list.

## Can knowledge line edits and deletion be undone?

Knowledge additions, edits and deletions return the place graph's Undo receipt. Undo restores the text and provenance together. Deleting a place removes its knowledge; merging a place moves the lines to the surviving place. Those operations can also be undone.

Undo receipts belong to the running store and do not survive a restart. A later structural change from another window prevents an older receipt from silently overwriting that change. The graph retains up to 20 receipts.
