# What aforge can do for you

This page is the honest inventory: what aforge can reach, what it refuses, and
what is simply not there in this build.

## Is this only for programming — is aforge only for code, or for any kind of work?

Not only programming. Nothing else on this page is about code in particular:
files, shell commands, the web, pictures, audio and video, your connected
accounts, your own settings. A folder of contracts, a pile of recordings to
transcribe and a repository are the same material to aforge — whatever is in the
folder it was pointed at. Where there is a repository a task hands its work back
on a branch; where there is none it works in the folder itself and says so:
`it worked directly in the workspace: there was no repository to branch`.

## Can you read, write, create, delete, rename or move files?

Yes. Three tools do this, and a path with nothing in front of it is read against the folder
aforge is standing in — the one you started it in. A path that names somewhere else, written
out in full or beginning with `~`, is read there. A write in the folder you are standing in
changes the file straight away; a write aimed at a folder you chose with `/folder` is kept
for this conversation until you run `/land`. Work handed to a task is bounded much more
tightly: a task writes in the one folder it stands on and nowhere else (*Where a task may
write*).

| Tool | What it does |
| --- | --- |
| `read` | Reads one file's contents, optionally from a start line (`offset`) for a number of lines (`limit`) |
| `write` | Creates or overwrites one file, making parent directories as needed; with `append:true` it adds to the end instead |
| `edit` | Replaces exact strings inside one file |

`read` output is cut at **2000 lines or 50KB**, whichever comes first, and the
cut is announced so paging is possible:
`[Showing lines 1-2000 of 5000. Use offset=2001 to continue.]`. An offset past
the end is an error: `Offset 900 is beyond end of file (120 lines total)`.
A single line over 50KB is reported, not shown.

`read` also opens **PDFs** — it extracts the text layer locally and for free,
with the same line and size caps.

`read` opens **images, audio and video** too, as a description rather than as
bytes. See "Can you look at an image, listen to audio or watch a video I have on
disk?" below for what comes back and what it costs.

`read` cannot open a **directory**. It answers
`Error reading file: read <path>: is a directory`. Use `ls` to list a directory.

Over `--host`, `read`, `write`, `edit` and `ls` run on the other machine, inside the
workspace shown for the session. A path in a task brief is read there too. A path the
model names in its reply can be opened here: aforge confirms it on the far disk and
fetches it through a short-lived local file door. Copy mode, `ctrl+s`, mouse drag-copy
and `m puts it in your message` only copy or compose words on this screen, so they work
the same way over a connection and do not move a file.

`edit` takes a list of replacements. Each `oldText` must appear exactly once,
and all of them are matched against the original file rather than one after the
other. Success reads `Successfully replaced N block(s) in <path>.`; a missing
file reads `Could not edit file: <path>. Error code: ENOENT.`

`write` reports `Successfully wrote N bytes to <path>`. With `append:true` it
adds the content to the end of the file instead of replacing it and reports
`Appended N lines to <path>; the file now has M lines.` — appending to a file
that does not exist yet simply creates it.

## Can you append to a file, or add to the end without rewriting it?

Yes. `write` takes an optional `append:true`, which adds the new content after
whatever the file already holds instead of replacing it. It is how a very large
file is written in parts, and how a write that was cut off mid-stream is
finished without paying for the whole file again (next section).

## What happens when a big write gets cut off — half-written, truncated, interrupted files

A model reply has an output limit, and a very large `write` can hit it partway
through the file's content. When that happens the complete lines that did
arrive are **saved to the file** — never a half line, so the file is not left
corrupted mid-word — and the tool result says so: it starts
`Saved what arrived:`, names the file, shows the last lines on disk, and asks
for one `write` with `append:true` carrying only the rest. The transcript row
for that call reads `wrote <path> (cut short; saved what arrived)`. The
continuation then costs the missing tail, not the whole file again.

If the cut fell too early for anything worth saving — inside the path, before
any complete line — nothing is written and the result says so:
`This write was cut off at the output limit before enough of it arrived to
save; nothing was written.` A cut that severs any **other** tool call — a
`bash` command, an `edit` — never runs on a guessed tail: the call is refused
with `nothing was run` and a suggestion to retry in smaller pieces.

`read` never asks your permission. `edit` and `write` follow whatever approval
mode you are in, which asks by default.

There is **no tool that deletes, renames or moves a file**. Those happen through
`bash`, by running `rm`, `mv` or `rename` like you would yourself — so they are
governed by the shell rules rather than the file rules, they ask before running
under the default approval mode, and the most destructive forms of `rm` are on
the short list of commands that always ask no matter what the settings say.

## Can you find a file or search the code?

Yes, three ways.

**`grep` — search inside files.** Arguments: `pattern` (required), `path`,
`glob`, `ignoreCase`, `literal`, `context`, `limit`. It returns at most **100
matches** by default and says so at the cap:
`100 matches limit reached. Use limit=200 for more, or refine pattern`.
Any single line longer than 500 characters is cut and marked `... [truncated]`.

**It works whether or not the machine has ripgrep.** With ripgrep it shells out
to it and respects `.gitignore`. Without ripgrep it walks the tree itself, with
the same arguments, the same caps and the same output — it just does not read
`.gitignore`, and skips `.git`, `node_modules`, `vendor` and files that look
binary instead. The tool description says which of the two you have. It never
answers `ripgrep (rg) is not available and could not be downloaded` any more:
that sentence was every grep call on a machine with no ripgrep and no way to
fetch one, and a search that cannot fail is worth more than an accurate excuse.

**`find` — find files by name.** It shells out to fd and respects `.gitignore`.
Arguments: `pattern` (required), `path`, `limit`. Default **1000 results**, and
at the cap: `1000 results limit reached. Use limit=2000 for more, or refine pattern`.
Without fd: `fd is not available and could not be downloaded`.

**`ls` — list a directory.** Alphabetical, dotfiles included, a `/` after each
directory. Default **500 entries**, and at the cap:
`500 entries limit reached. Use limit=1000 for more`. A bad path answers
`Path not found: <path>`.

All three are capped at 50KB of output, and all three are pure reads, so none of
them asks your permission.

## Can you run tests for me or start a dev server?

Yes. The `bash` tool runs tests, builds and development servers through `/bin/bash -c`
in your workspace. A server meant to stay up can start in the background immediately.
It streams stdout and stderr together, keeps unexpectedly long commands running as
background jobs instead of killing them, and reports a non-zero exit as
`Command exited with code N`.

## Run a command or build the project — background after and timeout

Yes. The `bash` tool runs a command through `/bin/bash -c` in your workspace,
with the environment aforge itself was started with.

- stdout and stderr arrive interleaved in one buffer, in the order they were
  written.
- Output is cut to the **last 2000 lines or 50KB**. When that happens the whole
  output is spilled to a temp file and the footer names it, e.g.
  `[Showing lines 900-1000 of 100000. Full output: /tmp/pi-bash-….log]`.
- Empty output reads `(no output)`.

**aforge waits up to the `background after` setting — 30 seconds unless you
change it.** If the foreground command is still running then, the same process
is kept as a background job; the call returns its output so far and a job id, and
the chat moves on. Nothing is killed or restarted.

The row is labelled **background after** on `/settings`' Safety tab; its key is
`bash.background_after_seconds`. The session engine and the visible countdown arm from
it together at launch, so a change applies to the **next session**. It is a machine brake,
so the model's `change_setting` tool refuses to widen it; change it yourself in the panel.

Set `background after` to **0** to turn that clock off and wait for the command's
own `timeout` instead. **600 seconds — ten minutes — is the timeout ceiling.** A
higher timeout is quietly clamped to 600. A timeout that is missing, null, zero
or negative gets the full ten minutes. The command timeout used to default to
two minutes; it was raised to ten because a four-minute script should not hit an
arbitrary two-minute handoff. The separate 30-second chat clock now controls
when the conversation moves on, while 0 preserves the timeout-only behaviour.

**A long command's output arrives while it runs, not all at once at the end.**
Programs writing to a pipe normally hold their output back in 4KB blocks —
Python especially — which used to leave a long job's log at zero bytes until the
moment it finished, so a job that was working perfectly looked dead. aforge runs
commands line-buffered (`stdbuf` where the machine has it, plus
`PYTHONUNBUFFERED=1` for Python, which does its own buffering), so the log fills
as the work happens. If you had already exported `PYTHONUNBUFFERED` yourself,
your value is left alone.

**Reaching either foreground bound does not kill the command.** The
background-after clock or an earlier command timeout keeps it on the job list
and it keeps running — see the next section.

A command that exits non-zero answers `Command exited with code N`. An
interrupted one answers `Command aborted`. If the workspace directory is gone:
`Working directory does not exist: <cwd>\nCannot execute bash commands.`

Anything you already know is meant to keep running — a server, a dev watcher —
is better started in the background from the start, where no clock runs at all.

`bash` follows your approval mode, which asks by default.

## The command took too long — is the work lost, or does it keep running?

It keeps running. A foreground command that reaches the `background after`
clock, or reaches its own timeout sooner, is **kept as a background job**, not
killed. The call answers with one line and then whatever the command had already
printed:

```
still running as job 3; log at ~/.aforge/v3/projects/-you-work/<session>/logs/jobs/3.log

collecting 120 cases
scored case 1
scored case 2
```

The first line is the same sentence a command started with `background: true`
answers with, and from that moment it *is* an ordinary job: a row in `jobs list`,
a tail in `jobs output`, `jobs kill` reaches its whole process group. The turn
carries on straight away rather than waiting.

So a fifteen-minute `make` behind the default 30-second chat clock keeps the
work already done. Nothing is thrown away and nothing is run twice. The old
behaviour — the process group killed and `Command timed out after N seconds` —
is what a plain shell tool with no session behind it still does; the chat does
not.

Two things it does **not** do:

- **A command you interrupted is interrupted.** Pressing `esc` cancels the turn,
  and a cancelled command is never kept as a job: it dies, no job appears, and the
  answer is `Command aborted`. Stop means stop.
- **It does not outlive the conversation.** A foreground command kept as a job is a job, so it is
  killed when the session closes, like every other one.

The log opens with the output you had already watched scroll past, and continues
with everything the command printed afterwards. For a command that had printed
truly enormous amounts before it was kept as a job, the log begins where aforge's own
rolling tail begins — the last few hundred kilobytes — rather than at the very
first line.

## Does aforge poll a background job, or does it get told — how does it know a job finished?

**It gets told, and it never has to poll.** Two things arrive without anybody
asking for them.

**While a job runs**, every tool result aforge reads carries one line per
outstanding job at the bottom of it, the way a shell prints its background jobs
under the prompt:

```
[job 1] running 3m12s · last: scored case 41
```

Three facts — it is alive, it has been alive this long, this is the last thing it
said — on every result, so "is it still going" is answered before it can be
asked. A finished or killed job drops off the list immediately.

**A forked hand is on that same footer**, named, because a hand is a job too (see
"Hands" in the tasks page):

```
[job 4] running 12m03s · hand 2 — the docs · last: edit docs/api.md
```

**When a job ends**, its exit code and last non-empty output line arrive in the
conversation on their own:

```
while you worked: job 3 exited 0: BUILD OK
```

If aforge is mid-turn the note lands at the next step; if the turn had already
ended, the note starts a new one, exactly as a finished task does. Several
session notes waiting at that boundary are one `while you worked:` message, not
several synthetic user messages between tool calls.

The rest stays behind `jobs output`: its default is the last 50 lines and its
footer names the whole log. This keeps a long build tail from dragging the model
away from the work it was already doing while preserving every line when it
needs the detail.

So you should never see aforge running `sleep 30 && tail …` to wait for
something. That loop was real — it cost one benchmark worker two thirds of its
wall clock, waiting on a log that was empty because of buffering — and the two
mechanisms above are what replaced it. `jobs output` is still there for an
intermediate look at a job you asked about; it is not how waiting is done.

## Can I send a running command to the background myself?

Yes — press **`ctrl+g`** while a foreground command is running. It is the same
handoff the background-after and timeout clocks use: the command is not killed
and not started again, it simply becomes a job, the row says which one (`job
3`), and the turn carries on.

With the mouse, move over that running row and press its right-hand
`click to background` offer. Only the offer keeps the command; pressing elsewhere
still opens the row. The offer is not drawn on a phone-width frame.

The background gesture is absent when there is nothing to send away — no command
running, a command that is already a background job, a call that is not `bash`, or
a session over `--host`, where the local surface has no handoff door. With no command
to keep, `ctrl+g` returns to its other job of hiding or restoring the task column. See
the keys page.

Starting a command with `background: true` makes it a job from the first instant.
For an ordinary foreground command whose length you did not know in advance, use
`ctrl+g` or the row's pointer offer; if neither happens first, the configured
background-after clock keeps it automatically.

## Does cd stick between commands — changing directory in bash

No. **Every `bash` call starts again in the workspace root** — including when the
conversation is also about another folder: `bash` runs where you are standing and touches
whatever it names, which is the one hand `/land` does not stand in front of. Each one is
its own
`/bin/bash -c`, its own process, run with the workspace as its working directory — so a
`cd` in one call is gone by the next, and nothing else a command changes about its own
shell (an exported variable, a `source`, a shell function, an activated environment)
carries either.

So anything that depends on being somewhere else has to be **one command**:

```
cd services/api && go test ./...
```

not a `cd` call followed by a `go test` call, which would run the tests at the root and
either fail or, worse, test the wrong thing quietly. The same goes for a variable a later
command needs: `export TOKEN=… && ./deploy.sh`, in one call.

Where you cannot chain, use the tool the command has for it — `go test ./services/api/...`,
`git -C services/api status`, `make -C build` — which is steadier than chaining anyway.

A background job is the same: it is started in the workspace root, and `cd`-ing inside it
changes nothing for any other call.

## Can you start a server and leave it running?

Yes. `bash` with `background: true` registers the command as a **job**, runs it
in its own process group, and returns immediately:

```
job 3 started; log at ~/.aforge/v3/projects/-you-work/<session>/logs/jobs/3.log
```

A background job never times out and is not tied to the turn that started it.
Everything it writes goes to that log file; the last **64KB** is also held in
memory for quick reads. When the job exits, aforge is told at the next step in
one boundary batch, e.g.
`while you worked: job 3 exited 1: make: *** [build] Error 1` — the last
non-empty log line, clipped to 120 characters. Use `jobs output` for the output
behind that headline.

The `jobs` tool looks at all of this. Its `action` is `list`, `output` or `kill`.

- `list` — one row per job: `job 1 · exited(0) · 12.4s · go build ./...`.
  Status is `running`, `exited(N)` or `killed`. Nothing running reads
  `No background jobs.` A job that started life as a foreground command and was
  kept as a job — by the background-after clock, its timeout, or `ctrl+g` — has
  exactly this row, with no mark saying where it came from: it is a job like any
  other. A forked **hand** is in
  this list too, as `job 4 · hand 2 · running · 12.0s · the docs`, and its status
  when it ends is `finished` — a hand has no exit code, it has a report.
- `output` — the last lines from the in-memory tail, **50 by default and 200 at
  most**, with a footer naming the full log:
  `[job 1 · running · showing last 50 lines · full log: <path>]`.
- `kill` — SIGTERM to the process group, SIGKILL after a **2-second** grace.
  Answers `job 1 killed`. Killing a **hand** answers
  `hand 2 (job 4) stopped; what it had already written is still in your working
  copy and may be half-made — no report is coming`.

Unknown ids answer `No job 9.`; a finished job answers `Job 1 already exited(0).`

**Jobs do not outlive the conversation.** When the session closes, every running
job is sent SIGTERM, given a shared 2-second grace, then killed. The log files
stay on disk for you to read afterwards, in **this conversation's own folder**
under `logs/jobs/` — never in your project. That is true of every job aforge
runs, including one a task's worker started in its own checkout: a job log is
the harness's own droppings, not your work, so it is kept beside the transcript
that explains what it was for and goes when you delete the conversation. Only a
conversation with no folder at all still keeps them at
`<workspace>/.aforge-v3/jobs/`.

**You can see a job without asking.** Every job this conversation starts is also a row on
the task column on the right, from the moment it starts until it ends — the command as its
name, and `job 3 · log <path>` under it while it runs, folded under the row once it has
ended (`→` on the row brings it back). A running job counts in the column's `N working`
tail. It has no room to walk into and no `✕`: the log is where a job is read, and `jobs
kill` is how one is ended. The tasks page has the whole of it under *Background jobs on the
column*.

**A job is the wrong door for work whose result is a deliverable.** What a background job
can leave you is a log file and an exit code — there is nobody inside it writing anything,
nothing to steer, and no report at the end. So a long command whose finishing *is* the
outcome belongs here, and gathering, deciding, writing or checking something — however many
parts it has, and however long it takes — belongs to a task instead, which gives you a room
to watch and a report you can read. If a multi-part piece of work was started as a
background command, say so: it can be handed to a task instead.

## Can you keep an eye on something and tell me when it changes?

Yes. The `watch` tool runs a command on a timer and speaks **only when there is
news**, which is the difference between it and re-running a command every turn:
re-running costs a turn and shows you the same output again, while a watch stays
quiet until something is different.

Arguments: `command` (required), `every_seconds`, `on` (`change`, `match`,
`always` or `quiet`), `pattern`, `until`, `quiet_ticks`, `name`.

- `on=change` (the default) reports the lines that are new since the previous
  tick, so a scrolling `tail -50` reports the two new lines rather than fifty.
- `on=match` reports only new lines matching `pattern`, which is required in
  this mode.
- `on=always` reports the last 10 lines every tick.
- `on=quiet` is the opposite of `change`: it fires when the output has **not**
  moved — see the next section.
- The first tick of `change`, `match` and `quiet` is silent — it is the baseline.
- `until` is checked on every tick including the first, and the first matching
  line ends the watch — and that last note wakes the conversation, so you are
  told without having to type.

The interval defaults to **10s**, with a **2s** minimum and a **3600s** maximum;
out-of-range values are clamped, not refused. One tick is bounded by the smaller
of the interval and 60 seconds. Each note carries at most 40 lines of 200
characters.

A watch **is** a job: same id, same log file, same row in `jobs list`, stopped
with `jobs kill`. Starting one answers
`watch tail-app.log started · every 10s · on change — kill with jobs`.

**At most 3 watches run at once.** Over that:
`this session already has 3 watches running, which is the limit — stop one with jobs kill first, or use bash background:true for a command that ends on its own`

Three identical failures in a row end a watch. An ordinary update never
interrupts a running turn and never wakes an idle session. At the next turn
boundary they arrive as one compact item such as
`codex-jobs watch: 3 updates — latest: 7 lines new — fixed the parser`; every
tick remains available through `jobs output`. **The tick that ENDS the watch is
different and it does wake you** — see the next section. Watches die with the
session like any other job.

For something that has to keep an eye on the world **after** this window is
closed — "tell me when CI goes red", "every Monday draft the update", "keep main
green" — a watch is the wrong tool and there is a right one: see the
keeping-an-eye page.

## Will it tell me when the watch finishes if I walk away — does a watch wake the conversation, or do I have to type first?

**A watch's ordinary updates are quiet, and the tick that ends it speaks.**

An update — the new lines in a log, the number that moved, a line matching your
pattern — waits for the next thing you say. That is deliberate: a reply every
time a log grows by a line would be a ticker tape, and every tick is already in
`jobs output` if it is wanted.

**The tick that ends the watch wakes the conversation, with nobody typing.** A
watch ends in exactly three ways, and all three are the answer you started it
for:

- the line `until` was waiting for appeared,
- the output went quiet for as long as you asked (`on=quiet`),
- the command failed three ticks in a row and the watch gave up.

When one of those happens, aforge starts a reply by itself carrying that final
note, so you get the sentence about it rather than a job row that quietly went
grey. Start a watch over `gh pr checks`, walk away, and you come back to "the
checks are green on both pull requests" — not to a conversation that learned it
and said nothing.

Nothing further ever comes from that watch: it is over, which is exactly why its
last note is the one worth waking for. `jobs list` no longer shows it running,
and its whole tick history stays at `jobs output <id>`.

A background command exiting, a video or music render landing and a forked hand
coming home wake a reply the same way. The tasks page has the rest of it under
*Waiting on something, and the limit on carrying on*.

## Can you tell me when something has finished — how do I know it went quiet or stopped changing?

Yes: `watch` with `on=quiet`. It is the inverse of every other mode — the news is
the output **not** moving. Use it for the very large class of things that finish
without announcing it: a build log that stops growing, a download whose byte
count stops climbing, a directory that stops filling up.

`quiet_ticks` is how many consecutive unchanged ticks count as finished —
**6 by default, minimum 2, maximum 100**, clamped rather than refused. On the
default 10-second interval, six ticks is a minute of silence. Two ticks that are
the same are a run of one, so a `quiet_ticks` of 6 means six ticks in a row that
each matched the one before.

Any change at all resets the count. Sameness is the **whole** of a tick's output,
which is the same identity `on=change` uses — a log that differs by one byte has
moved.

When it fires it **ends the watch** with one final note, the way `until` does:

```
watch build: quiet for 6 ticks (60s)
Linking target/release/app
```

— the terms that were met, then the last non-empty line the command was still
printing. Nothing more comes from that watch afterwards, and that note **starts a
reply by itself** rather than waiting for you to type.

**`on` is one mode, not a set of them.** `quiet` cannot be combined with
`change`, `match` or `always`, and asking for `quiet_ticks` in any other mode is
refused rather than ignored:

```
Invalid arguments: quiet_ticks only applies when on is quiet (got on=change)
```

An unknown mode answers
`Invalid arguments: on must be change, match, always, or quiet (got "sometimes")`.

`until` still works alongside `quiet` and is still checked first, so a watch can
end either because the line it was waiting for appeared or because everything
went quiet.

## Can you read a scanned PDF, a Word file or a photo of a receipt?

Yes, with `read_document`. It is the rung above the plain `read`.

`read` already handles a PDF that has a text layer — locally, in-process, free.
When there is no text layer it says so rather than guessing:

```
<path> is a scanned PDF with no text layer (12 pages, images only). No local text to read; use read_document (the OCR rung) or paste a page as an image.
```

`read_document` takes `path` (required), an optional `question`, and `offset` /
`limit` for paging. It accepts `.pdf .docx .xlsx .pptx .png .jpg .jpeg .webp
.gif` and refuses anything else:
`read_document reads PDFs, images (png, jpeg, webp, gif) and office documents (docx, xlsx, pptx); <path> is none of those`.
Plain text gets sent back to the cheaper tool:
`the plain read handles this: <path> is plain text — call read, which opens it locally and free`.

It tries rungs in order and names the one that answered, e.g.
`[read_document — local rung]`. A document over **25MB** is refused:
`<path> is over the 25MB document limit`. A path it cannot open answers
`could not read <path>`. With no API key on the session:
`the document rungs are out of reach: this session has no API key — read_document rides this session's own API key and base URL`.
When every rung fails, each is named, e.g.
`could not read <path> — native: 402 insufficient credits; cloudflare-ai: no readable text`.

Extraction is remembered for the conversation, so paging through a long document
costs nothing extra.

A picture — a photographed page, a receipt, a screenshot of a table — has one
rung only, which is a model's own eyes. When the model you are talking to cannot
see, that rung is sent to the **looking model** instead of to your blind one, so
`read_document` reads a photograph on any model. Everything else (PDFs, docx,
xlsx, pptx) goes to your own model and the parsers, as before.

## Can you look at an image, listen to audio or watch a video I have on disk?

Yes — with `read`. A file is a file, so the same tool that opens a source file
opens a screenshot, a voice memo or a screen recording. There is no separate
"look at this" or "transcribe this" tool for a file on disk, and aforge never
needs to write a script or install a library to decode one.

What comes back is a **description**, not the bytes, and it always says who
produced it:

| File | Line above the answer | What the answer is |
| --- | --- | --- |
| image | `[vision: <model>]` | every piece of text in the picture, transcribed, then the layout and content |
| audio, transcribed | `[transcript: <model>]` | the speech, exactly |
| audio, described | `[audio: <model>]` | the sound: genre, mood, instruments, structure |
| video | `[video: <model>]` | what happens, on-screen text, speech, style |

The formats are **png, jpg, jpeg, webp, gif** · **mp3, wav, m4a, ogg, flac** ·
**mp4, webm, mov**. A file saved with no extension is recognised from its first
bytes, so a screenshot pasted as `clipboard` still works.

Limits, refused before anything is sent: **10MB** for an image, **25MB** for
audio, **64MB** for video —
`<path> is over the 25MB audio limit`.

The answer is paged like any read — 2000 lines or 50KB, with
`Use offset=… to continue.` — and it is **remembered for the conversation**, so
paging through a long transcript costs nothing extra.

When no model is set for a sense, `read` says so instead of showing you binary:

```
<path> is audio, and this session has no model that can listen to one — set the listening model in settings.
```

The same sentence exists for an image (`…no model that can look at one — set the
looking model in settings, or attach the picture to a message.`) and for video
(`…no model that can watch one — set the watching model in settings.`).

A model that was reached and failed is named:
`could not look at <path> — <model>: <reason>`.

## Can you transcribe a recording, or tell me what a song sounds like?

Both, and you do not have to say which — `read` works it out.

Reading an audio file climbs a small ladder, and **the ladder picks the sense**:

1. **Transcription first.** The file goes to the transcription endpoint, which
   is the cheap, purpose-built one. A real transcript stops here, headed
   `[transcript: <model>]`.
2. **Listening second.** If the first rung fails, or returns almost nothing, or
   returns only what a speech recogniser says when there was no speech — a page
   of `[Music]`, or a bare `you` — the file itself goes to a model that can
   hear, and the answer is headed `[audio: <model>]`.

That second rung is why "what style is this track?" works: nobody had to decide
in advance whether the answer was words or music.

Two details worth knowing:

- **A short transcript is kept when there is nothing above it.** If no listening
  model is set, a four-second recording that transcribes to three words gives
  you those three words rather than a refusal.
- **When both rungs fail, both are named**, in the order they were tried:
  `could not read <path> — <model>: 402 insufficient credits; <model>: no endpoint`.
- If a transcription model is set but this session was never given a media
  client to reach it, the sentence says so rather than blaming the settings row
  that already names one:
  `<path> is audio, and <model> is set to transcribe it but this session has no media client to reach — set the listening model in settings, which rides the session's own model instead.`

Video has one rung and no ladder: there is nothing cheaper than a model that can
watch, so `read` either watches the file or says no model can.

## Can you look at a picture I send you?

**It depends on the model you are using.** Some models read images directly;
some cannot see at all.

You attach pictures to a message you type. **Drag a file onto the terminal, or
paste one you copied as a file**, and it is attached — your sentence gets a short
`[image #1]` token where the path would have gone, and you can then talk about
"image #1" and be understood. `/image <path>` and the `@` completion attach one
too. The picture travels **inside the message as the picture**, not as a path
somebody has to go and open. The "what the keys do" page has the whole of it.

The accepted formats are exactly **png, jpeg, webp and gif**. Limits: **10MB** per
image and **20MB** for all the images on one message.

- Over the per-image limit: `session: <path> is over the 10MB image limit`
- Over the per-message limit:
  `session: these images total more than the 20MB a single message may carry — send them across a few messages`
- Wrong format:
  `session: shot.tiff is not an image this surface can send — png, jpeg, webp and gif are`
- Unreadable: `session: could not read <path>`

When the model you are on **cannot** see, aforge does not simply refuse. It
sends the picture and your words to a vision model in one shot and that answer
becomes the reply, prefixed so you always know who spoke:

```
[vision: <model>]
```

The vision model is sent the picture and your words and nothing else — no
transcript, no tools. The conversation afterwards keeps text only, with a marker
line like `[attached image: shot.png]`, because the model you are talking to is
blind by construction.

If no vision model can be reached at all:
`session: <model> cannot read images — switch to a model with vision, or describe what the picture shows`

If a turn is already running:
`session: <model> cannot read images and <seer> answers between turns — wait for this one to finish, or switch to a model with vision`

If the vision model says nothing:
`session: <seer> returned no answer for the image`

Attaching is not the only way in. A picture already on disk is opened by `read`
(see "Can you look at an image, listen to audio or watch a video I have on
disk?"), which describes the whole thing, and by `read_document`, which is the
door for a photograph of a page you want extracted as a document.

## Can you make a picture, a voiceover, music or a video?

Yes, when a model for that kind of media is available on this machine — and each
kind is a separate answer, so drawing may be there while filming is not.

- `generate_image` draws from a prompt, and edits, restyles or combines pictures
  you already have when you give it `reference_paths`.
- `speak` turns text into an mp3, in the speech model's default voice unless you
  name one.
- `generate_music` composes a piece from a description of the music — genre,
  instruments, tempo, mood. It is a different model from `speak` and has no
  length argument: you get a piece of the model's own choosing, half a minute
  to a minute in practice, for a flat price per call. It **returns straight
  away with a background job**, like `generate_video`.
- `generate_video` renders a short video. It **returns straight away with a
  background job** because a render takes minutes; the finished file arrives as a
  note naming it, and `jobs kill` stops it. Both keep working while aforge
  carries on with other things.

Each saves a file and names its path — in the call's own answer for a picture or a
voiceover, in the job's note for music or a video — never the media itself, and each
costs real money, a video most of all. A path is all that goes into the
conversation, but **you see a picture without leaving the terminal and without
asking**: the moment `generate_image` finishes, the image is drawn under its row
in colour. Open the row for a bigger look and the file's whole absolute path
beneath it. A picture's path is absolute precisely so that a terminal which
cannot draw still leaves you something you can open.

**All four work in tasks, in adaptive runs and inside saved harnesses too**, not
only here. The page "making pictures, audio and video" has the arguments, the
exact wording of the results, where the files land, and what happens when one
fails.
**One model does all the looking.** The looking slot in the settings sheet is
the single answer to "what can see here": the fallback above, `read_document`'s
image rung, and the `view_image` tool below all use that one model. Change it
once and all three change. Leave it alone and aforge picks the best model that
publishes vision, so looking works on a machine that has never opened settings.

## Can you join videos, cut a longer video together, or add music to a video?

Yes, with `edit_video`, and it is the media verb that costs nothing: it is
ffmpeg on this machine, so it needs no model, no key and no money. It works on
video files that already exist — ones aforge rendered, and equally a screen
recording or a camera clip you dropped in the folder.

Four actions:

- **`measure`** — how long a video runs, whether it has sound, its frame size
  and rate, its size on disk. All read from the file, in about a tenth of a
  second. This is the arithmetic question; `read` on a video is the expensive one
  that answers what *happens* in it.
- **`frame`** — save one frame as a png. It takes the **closing** frame by
  default, which is the frame that makes two independent renders connect: hand it
  to `generate_video` as its opening `frame_paths` entry and the next shot
  continues out of this one.
- **`join`** — lay clips end to end into one longer video. **Every clip's audio
  is carried** (a silent clip gets silence of its own length), so a joined cut
  cannot go quiet part-way through, and every clip is letterboxed into the first
  clip's frame rather than stretched.
- **`score`** — lay an audio file under a video, **looped or trimmed to the
  video's own length** automatically, mixed underneath any sound the video
  already has rather than over it.

It is on the belt only when **ffmpeg and ffprobe are on this machine**; they are
not downloaded on demand, and without them the verb is absent rather than
present and refusing. There is deliberately no trim, crop, speed change or
transition — those are `bash` and ffmpeg directly. What lives here are the four
operations a generated film is assembled from, where a hand-written command goes
wrong silently.

The page "making pictures, audio and video" has every argument, the exact
wording of the answers and the refusals, and where the files land.

## Can you open an image file yourself, or do I have to attach it?

Both work. `view_image` opens a picture on disk on its own — a screenshot
somebody left in the folder, a chart or page rendered to a file, a photograph, or
an image aforge generated a moment ago and wants to check.

It takes `path` (required, relative to the conversation's directory or absolute)
and an optional `question` — "what does the error dialog say?", "is the legend
cut off?". With no question it asks for a full description: subject,
composition, any text verbatim, and anything malformed.

It reads **png, jpeg, webp and gif**, up to **10MB**. The answer names who
looked: `seen by <model>: <what it saw>`. The picture itself is **not** added to
the conversation, so everything you need about one image is worth asking in a
single call. **You see it too**: the picture is drawn under the `view_image` row
in colour as soon as the call finishes, and opening the row shows it bigger with
what the looking model said beneath.

Refusals, in its own words. The last three name the picture by its **whole
absolute path**, however you spelled it in the call:

- Not one of the four types:
  `shot.tiff is not an image this surface can send — png, jpeg, webp and gif are`
- Missing, a directory, or unreadable: `could not read <path>`
- Too big: `<path> is over the 10MB image limit`
- The looking model failed: `<model> could not look at <path>: <reason>`
- It said nothing: `<model> returned no answer for <path>`
- It never answered at all:
  `<model> did not answer about <path> within 10m 0s — try again, or ask about a smaller picture`

**One look gets ten minutes**, and then the tool answers without it. A model that
takes the picture and goes quiet used to leave the row running for the rest of
the conversation; now the window runs out and you get the line above instead.
Press esc and the look stops on the same beat everything else does.

**When no looking model can be reached, the tool is not there at all** — it is
left off the toolbelt rather than offered and made to refuse. Ask for a picture
to be looked at then and the answer is that aforge has no way to look, not a
failed attempt.

## Can you search the web?

Yes, and **no key or configuration is required** for it to work.

`web_search` takes a `query` (required) and a `count` — **5 by default, 8 at
most**, silently clamped rather than refused. Results come back as
`1. Title — URL` with the publication date and a snippet of up to 300
characters under each, and a footer like `5 of 12 results · firecrawl`. Nothing found reads
`no results · firecrawl`. A failed search names the back end it used:
`Search failed (firecrawl): <err>`.

`web_fetch` takes one absolute `url` with a scheme and returns the page's text
with the markup stripped, capped at **4000 bytes**, with the overflow announced
as `…… (18234 more bytes)`. An empty page reads `(empty page)`. A failure reads
`Fetch failed (jina): <err>`. It is not the tool for a local file — `read` opens
those.

Which back end answers is decided top-down: a pin in the `search.provider`
setting wins; otherwise the first keyed service you have a key for (`exa` needs
an Exa key, `jina-search` needs a Jina key); otherwise the zero-key default,
which is Firecrawl: keyless, with a free monthly allowance and no key needed.
DuckDuckGo remains available as an explicit pin. So keys change *which* engine
answers or raise its ceiling, never *whether* the web is reachable.

This matters more generally: aforge leaves a tool **off the list entirely** when
there is nothing behind it, rather than offering it and then refusing. If a
capability is missing, it is missing — you will not get a tool that pretends.

## Which search engine answered?

The last line of every successful `web_search` names it. A complete answer reads
`5 results · firecrawl`, a shortened answer reads `3 of 8 results · exa`, and an
empty one reads `no results · firecrawl`. The finished tool line carries that
same footer, so the compact receipt and the result sent to the model cannot
disagree.

`/status` has a `search` line for the next call, such as `firecrawl · keyless`,
`firecrawl · with your key`, or `exa · with your key`. If an explicit keyed pin
has no key, it says `exa · key not set — searches fail`. A session with no
`web_search` tool has no `search` line either; an empty status row would claim a
capability that is not there.

## I set a search key and nothing changed

Search settings are live: a provider or key written in `/settings` applies to
the **next search in this conversation**. You do not need `/new`, a restart, or
a new session. Work handed out from this conversation follows the same live
settings rather than keeping a launch-time copy.

With `search.provider` on `auto`, an Exa key raises the next search to `exa`; a
Firecrawl key keeps the provider named `firecrawl` and raises that provider's
ceiling, so the visible change is `firecrawl · with your key`. The selected
**searching** row in `/settings` → Context states the current answer. With no
keys it says `now firecrawl, keyless — set search.exaKey or
search.firecrawlKey to raise it`.

An explicit pin still wins. Pinning Exa without `search.exaKey` makes every call
answer `Search failed (exa): no API key`; the searching row says that before a
call is spent. Choose `auto` to return to the ladder, or set the named key.

## Can you remember what we worked out earlier in this conversation — do you remember me between conversations?

Yes, in two ways: the transcript itself, and three tools that hold working state
**outside** the transcript so a compaction cannot lose it.

(Carrying something into a *later* conversation is a different mechanism and has
its own page — `remember`, `/remember`, `/memories` and `/forget` are on
what-i-remember.)

**`track`** records one item. Arguments: `text` (required), `kind` (required —
`belief` for something true in the workspace now, `progress` for a subgoal
opened and not finished), `evidence` (**required** — it must name what actually
ran, like `bash: go test ./…` or `read: go.mod`), and `status` for progress
items (`open` or `blocked`). Tracking the same kind and text again updates the
record instead of duplicating it. Result:

```
Tracked p2 (progress, open):
p2 wire StateBlock into the compaction rebuild  ← grep: compact loop.go
```

`text` is capped at 240 characters and `evidence` at 160. The store holds 200
records; past that the oldest closed one is dropped, and an open subgoal or a
live belief is never dropped.

**`commit`** closes one record by `id`, e.g. `p2`. A progress item becomes
`done`; a belief becomes stale and leaves the block. It is the one hand that
declares work finished, so unlike the others it follows your approval mode and
asks by default.

**`recall`** shows the current block — beliefs first, then open items, then done,
newest first inside each. Empty, it answers:
`No working state yet. Track a belief or a subgoal when there is something a future compaction must not lose.`

**How long it lasts:** for this conversation, including a resume of it — the
records are kept in a file beside the session's own journal. They do not travel
to a different conversation.

## Can you hand a piece of work off to run on its own?

Yes. `propose_task` proposes a task with a `title`, `summary`, `brief` and
`acceptance`, plus optional `depends_on`, `model`, `max_steps` and
`no_progress`. `tasks` looks at what exists — searching, reading output, sending
a message, or resolving one.

Both are available in the conversation only. Inside a running task they are off,
along with `watch`.

The tasks pages in this manual cover how a task runs, what it costs and what you
see while it works.

## How do you decide how to go about a piece of work?

Three working habits are in aforge's own instructions, and **the chat reads the same
three as a task does** — the same words, from one page both are given. They are written
as principles rather than examples, because aforge is handed prose, research, data,
operations and code through the same door.

- **When the work comes with its own measure, that measure is the loop, not the report.**
  A check to run, a count to reach, a reading somebody will take: aforge works *between*
  readings rather than saving the reading for the end, and takes them **more often when
  the reading is zero**, changing less in between.
- **Nothing on every count is one shared fault, not many separate ones.** When everything
  reads zero, aforge looks for what they have in common — how they are reached, the step
  before any of them runs — and proves that shared path carries one case end to end
  before touching any single part. Uneven readings mean the opposite.
- **Before making a thing itself, it spends one step asking whether it already exists** in
  a form it can use: a tool, a source, a service, something the work already carries,
  something done here before. Asking costs one step; not asking costs the whole thing.
  It asks *before* the first piece exists.

The chat carries them because **the approach is chosen in the conversation**, usually in
the first minutes, before any task exists — measured over a set of runs of the same
brief, the ones whose conversation asked whether the thing already existed got to a real
result, and the ones that set about building it by hand did not. A habit that only
reached the worker arrived after the decision it was meant to shape. *How work on its
own actually runs* describes the same three from the task's side.

## Can I run a subharness — one of the typed programs?

`/subharness` (or `/sub`) lists them, `enter` opens that one's intake card, and
`/subharness <name>` opens the card straight away. The card shows every input
field, marks the required ones nobody has answered with `▲`, lets you fill them
in with `enter`, and the last row is `run it`. A run is a task from there on.

What it cannot do yet is stated on the *Subharnesses* page, and it is worth
knowing before you go looking: aforge does not offer one by itself yet, nothing
fills the card in from the conversation yet, and on a build with none wired the
command answers `no subharnesses here yet — a subharness is a saved program for
work that comes round again.` and opens nothing.

A harness is a subharness. One system, and `/harness` is the other door onto the
same programs — a picker and a typed request instead of a list and a card. A
harness you asked aforge to design for you is on `/subharness` from the moment
you approve its card. See *Saved shapes of work* for designing one.

## Can you tell me how you work?

Yes, and it does not answer from memory. aforge has a `manual` tool that reads
these pages, which are compiled into the binary from the same code they describe.
Ask it anything about aforge — what a tool does, what a command does, why it just
behaved a certain way — and it looks the answer up and tells you it looked it up.

`manual` takes either a `query`, in your own words, which returns the most
relevant sections, or a `page` name to read a page — whole when it is short, and
cut with a list of its headings when it is long (the next question). A page name
that does not exist gets an exact refusal listing the pages that do, never a
search result that reads as though the page existed.

When the manual has nothing on a topic, the answer is:
`The manual has nothing on that, which usually means aforge does not do it.`

**Looking something up never asks your permission and records nothing.** It is a
read, like `grep` — no journal line, no cost, no trace in the conversation.

## Can you read me a whole page, and what happens when the page is a long one?

Short pages arrive whole. A long one arrives cut, and says so.

A `page` lookup is bounded by the same limit a file read is bounded by, because
a result is a result whatever it read, and the longest pages in this manual are
several times that limit. When a page is over it, what comes back is the start of
the page — ending at a whole line, and never inside an example — followed by a
note in square brackets that begins `[Cut: … bytes of …. The rest of this page is
in its sections — ask for the same page again with section set to one of:` and
then lists every heading on that page, one to a line, before it closes.

Those headings are the addresses of the rest. Ask for the same page with one of
them in `section` and that part comes back whole, however far past the cut it
sat. A heading that is not on the page is refused by name, and the refusal lists
the headings that are — never a neighbouring section handed over as though it
were the one asked for.

So no part of the manual is out of reach, and no single lookup can fill the
conversation with one page.

## Can you change my aforge settings for me — set my daily budget, change a preference, or tell me what one is set to?

Yes to both, and a change is permanent. Ask in your own words — "use
`deepseek/deepseek-v4-pro` for planning", "set my daily budget to 5", "stop
drawing timestamps" — and aforge does it rather than telling you where the panel
is.

Two tools, because reading your configuration and rewriting it are different
acts and you get to answer them separately.

`settings` reads. With no arguments it lists every row of the settings registry
— the same rows `/settings` shows, in the registry's own six categories: `models`,
`spending`, `safety`, `tasks`, `memory & practice`, `interface` — one line each,
as `key · label · what it reads now`. Give it a `key` and it reads that one row
in full: what the row takes, what it governs, its current value, and whether
aforge may change it. Give it a `search` word and it lists only the rows whose
key, label or description mention it. **Reading never asks your permission**, the
way `manual` and `grep` do not: credential rows read masked — eight bullets and
the last four characters — through the registry itself, so there is nothing here
a question would be protecting.

`change_setting` writes one row. It takes that row's exact `key` and the new
`value` as text; an empty value clears the row back to its default. A row holding
a **list** — `models.roles`, `tools.approval`, `models.fallbacks` — is replaced
whole and never appended to, exactly as typing into that row in the panel is, so
aforge reads it first and writes the complete list back. The write
goes through the registry's own validation into your profile's `config.json` —
the same file, the same validation and the same wording as the panel — so the
change survives a restart and is there in `/settings` next time you open it.
**It asks you first**, like `edit` and `write`. Changing your configuration is an
act, not a read.

You are told on screen what moved. A dim line lands in the transcript:

```
settings · daily budget · $500 → $50
```

A key the registry does not have is never written. It comes back as
`No setting is called "…". Did you mean daily_budget_usd, plan_consent_usd?`,
naming the near misses. And there is no way around the tool: a value typed into
`config.json` with `write` or `edit` skips the validation, and aforge is told not
to do it.

**Some rows are refused on purpose** — the tool gate and the shell rules, the
spend rails, the machine ceilings, the check on task work, the attribution
trailer, and every credential row. The permissions page lists them exactly. A
model that could widen its own restraints would not have any.

Both tools are absent inside a running task, along with `watch`. A task node
works in a copy of its own with nobody watching it, and a permanent change to your
machine that no transcript ever showed you is the one thing this pair must not be
able to make. `propose_task` and `tasks` are NOT absent there: a task may hand
pieces of its own work out, two levels deep at most, and `tasks` shows it those
pieces and nothing else.

## What aforge cannot do

Plainly, so you do not have to find out the hard way.

- **It does not carry a conversation into the next one — but it can go and look
  one up.** The transcript stays where it was written; a new session opens on an
  empty screen with none of it in front of it. What it carries by itself is a
  handful of durable lines — a preference you stated, something you asked it to
  remember — and only those. What was actually *said* in an earlier conversation
  is searched for when you ask for it, with `search_conversations`, and quoted
  back from the words themselves; that is a look-up and not something it walks in
  already knowing. Both halves are on the what-i-remember page. Working state
  recorded with `track` survives a resume of *this* conversation and nothing
  further.
- **It cannot make media without a model for it.** `generate_image`, `speak`,
  `generate_music` and `generate_video` are each on the list only when this
  machine has a model for that kind of media; when there is none, the tool is
  absent rather than present and refusing, and aforge simply does not have that
  verb. The same rule applies inside a task, an adaptive run and a saved harness.
- **`generate_music` cannot be asked for a length.** The endpoint takes no
  duration, so the model writes a piece of its own choosing and the call costs
  the same however long it turns out. Its length stops mattering once the piece
  goes under a video: `edit_video`'s `score` loops or trims it to fit.
- **It cannot work on video files without ffmpeg** on the machine, and ffmpeg is
  not downloaded on demand. `edit_video` is absent rather than present and
  refusing, so measuring, framing, joining and scoring are all unavailable
  together. What it does **not** need is a video model or any money: with ffmpeg
  installed the verb is there on a machine that cannot render a single frame.
- **`read` cannot list a directory.** It errors. `ls` lists directories.
- **`read` cannot look at an image, listen to audio or watch a video when no
  model is set for that sense.** It says which one is missing rather than
  showing you the bytes.
- **`find` needs fd** on the machine, and it is not downloaded on demand; without
  it `find` says `fd is not available and could not be downloaded` and stops.
  `grep` is no longer in this position — it works without ripgrep, on its own
  legs, and only its treatment of `.gitignore` changes.
- **A foreground `bash` call waits no longer than `background after`** — 30
  seconds by default. Set it to 0 to wait for the command's timeout, capped at
  600 seconds. Reaching either bound does not throw the work away: the command
  becomes a background job and keeps going, and the call hands back what it had
  printed so far.
- **A scanned PDF is not readable by `read`**, only by `read_document`.
- **More than 3 watches at once is refused.**

## What does not survive the conversation ending

- **Background jobs and watches**, including a foreground command that was
  kept as one. Every running job is killed when the session closes. Their
  log files stay under this conversation's own folder, in `logs/jobs/`.
- **A "don't ask again" answer to a permission question.** It is held in memory
  for this session only and is never written down, so the next session asks
  again.
- **Which connected services were switched on.** Those are re-asked on a resume.

**What does survive it deliberately** is anything you set up with a card —
a reminder, a watch on the world, a rule, work that runs overnight. Those are
not jobs: they outlive the window on purpose, they fire into the conversation
that asked for them, and they are stopped by saying so. The keeping-an-eye page
is the whole account of them.

What does survive: the transcript itself, which is written to the session file
and replayed when you resume; the working state recorded with `track` and
`commit`, kept in a file beside that session file; **anything remembered**, with
the `remember` tool or `/remember` or by asking, which is kept per person and
read by every conversation after this one (see what-i-remember); **a setting
changed with `change_setting`**, which is written into your profile's
`config.json` and read by every conversation after it; and anything written to
disk by `write`, `edit` or a command you ran.
