# The body of a job tab on the desktop

## What does a background job tab show

A job tab shows the job's name, then `job`, then its state on the right: `Running · 2m 14s` while it runs, `exit 0 · 2m ago` once it has ended, or `stopped` if you stopped it. The log sits on the dark field below, read-only: nothing you type reaches the job. Copy output copies the text you selected, or else the log shown. The place the job runs in is not shown, because the engine does not report one.

## How do I stop a running background job

A running job has a Stop button in its header. It stops that one job; the header then reads `stopped`. A job that has already ended has no Stop button. If the engine refuses, one muted line above the log says `Could not stop:` and why.

## Why does the log say earlier output was trimmed

The tab shows the end of a job's log, not all of it. When the engine cut the front off, one muted line above the field reads "Earlier output was trimmed. Showing the end of the log." A running job's log refreshes about every second and a half; a finished job's is read once.

## Ask about a job's output

The "Ask codeaf about this output" field under a job's log starts a new conversation in a new tab. The log, or the text you selected, goes with your question as an attached file. It never adds a message to the conversation the job belongs to.

## What happens to a job tab after I reload or the job is gone

After a reload the tab finds its job again by the job's number and shows the log. If the engine no longer has that job, the tab says "The engine no longer has this job." and nothing else: no log, no Ask field. Remove job closes the tab; the engine has no way to delete a job's record.
