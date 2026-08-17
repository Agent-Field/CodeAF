# What aforge can do for you

This page is the honest inventory: what aforge can reach, what it refuses, and
what is simply not there in this build.

## Can you read, write, create, delete, rename or move files?

Yes. Three tools do this, and they work on the workspace you started aforge in.

| Tool | What it does |
| --- | --- |
| `read` | Reads one file's contents, optionally from a start line (`offset`) for a number of lines (`limit`) |
| `write` | Creates or overwrites one file, making parent directories as needed |
| `edit` | Replaces exact strings inside one file |

`read` output is cut at **2000 lines or 50KB**, whichever comes first, and the
cut is announced so paging is possible:
`[Showing lines 1-2000 of 5000. Use offset=2001 to continue.]`. An offset past
the end is an error: `Offset 900 is beyond end of file (120 lines total)`.
A single line over 50KB is reported, not shown.

`read` also opens **PDFs** — it extracts the text layer locally and for free,
with the same line and size caps.

`read` cannot open a **directory**. It answers
`Error reading file: read <path>: is a directory`. Use `ls` to list a directory.

`read` cannot open a **picture**, even though its own description says it can.
The bytes are decoded as text.

`edit` takes a list of replacements. Each `oldText` must appear exactly once,
and all of them are matched against the original file rather than one after the
other. Success reads `Successfully replaced N block(s) in <path>.`; a missing
file reads `Could not edit file: <path>. Error code: ENOENT.`

`write` reports `Successfully wrote N bytes to <path>`.

`read` never asks your permission. `edit` and `write` follow whatever approval
mode you are in, which asks by default.

There is **no tool that deletes, renames or moves a file**. Those happen through
`bash`, by running `rm`, `mv` or `rename` like you would yourself — so they are
governed by the shell rules rather than the file rules, they ask before running
under the default approval mode, and the most destructive forms of `rm` are on
the short list of commands that always ask no matter what the settings say.

## Can you find a file or search the code?

Yes, three ways.

**`grep` — search inside files.** It shells out to ripgrep and respects
`.gitignore`. Arguments: `pattern` (required), `path`, `glob`, `ignoreCase`,
`literal`, `context`, `limit`. It returns at most **100 matches** by default and
says so at the cap:
`100 matches limit reached. Use limit=200 for more, or refine pattern`.
Any single line longer than 500 characters is cut and marked `... [truncated]`.
Without ripgrep on the machine it answers
`ripgrep (rg) is not available and could not be downloaded`.

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

## Can you run a command, run my tests, or build the project?

Yes. The `bash` tool runs a command through `/bin/bash -c` in your workspace,
with the environment aforge itself was started with.

- stdout and stderr arrive interleaved in one buffer, in the order they were
  written.
- Output is cut to the **last 2000 lines or 50KB**. When that happens the whole
  output is spilled to a temp file and the footer names it, e.g.
  `[Showing lines 900-1000 of 100000. Full output: /tmp/pi-bash-….log]`.
- Empty output reads `(no output)`.

**Foreground commands time out after 120 seconds by default, and 600 seconds is
the maximum** you can ask for. A higher `timeout` is quietly clamped to 600. On a
timeout the whole process group is killed and the result is
`Command timed out after N seconds`.

A command that exits non-zero answers `Command exited with code N`. An
interrupted one answers `Command aborted`. If the workspace directory is gone:
`Working directory does not exist: <cwd>\nCannot execute bash commands.`

Anything that is meant to keep running — a server, a dev watcher, a long build —
should be started in the background instead, where it never times out.

`bash` follows your approval mode, which asks by default.

## Can you start a server and leave it running?

Yes. `bash` with `background: true` registers the command as a **job**, runs it
in its own process group, and returns immediately:

```
job 3 started; log at /path/to/workspace/.aforge-v3/jobs/3.log
```

A background job never times out and is not tied to the turn that started it.
Everything it writes goes to that log file; the last **64KB** is also held in
memory for quick reads. When the job exits, aforge is told at the next step,
e.g. `job 3 exited 1: make: *** [build] Error 1` — the last non-empty log line,
clipped to 120 characters.

The `jobs` tool looks at all of this. Its `action` is `list`, `output` or `kill`.

- `list` — one row per job: `job 1 · exited(0) · 12.4s · go build ./...`.
  Status is `running`, `exited(N)` or `killed`. Nothing running reads
  `No background jobs.`
- `output` — the last lines from the in-memory tail, **50 by default and 200 at
  most**, with a footer naming the full log:
  `[job 1 · running · showing last 50 lines · full log: <path>]`.
- `kill` — SIGTERM to the process group, SIGKILL after a **2-second** grace.
  Answers `job 1 killed`.

Unknown ids answer `No job 9.`; a finished job answers `Job 1 already exited(0).`

**Jobs do not outlive the conversation.** When the session closes, every running
job is sent SIGTERM, given a shared 2-second grace, then killed. The log files
under `<workspace>/.aforge-v3/jobs/` stay on disk for you to read afterwards.

## Can you keep an eye on something and tell me when it changes?

Yes. The `watch` tool runs a command on a timer and speaks **only when there is
news**, which is the difference between it and re-running a command every turn:
re-running costs a turn and shows you the same output again, while a watch stays
quiet until something is different.

Arguments: `command` (required), `every_seconds`, `on` (`change`, `match` or
`always`), `pattern`, `until`, `name`.

- `on=change` (the default) reports the lines that are new since the previous
  tick, so a scrolling `tail -50` reports the two new lines rather than fifty.
- `on=match` reports only new lines matching `pattern`, which is required in
  this mode.
- `on=always` reports the last 10 lines every tick.
- The first tick of `change` and `match` is silent — it is the baseline.
- `until` is checked on every tick including the first, and the first matching
  line ends the watch.

The interval defaults to **10s**, with a **2s** minimum and a **3600s** maximum;
out-of-range values are clamped, not refused. One tick is bounded by the smaller
of the interval and 60 seconds. Each note carries at most 40 lines of 200
characters.

A watch **is** a job: same id, same log file, same row in `jobs list`, stopped
with `jobs kill`. Starting one answers
`watch tail-app.log started · every 10s · on change — kill with jobs`.

**At most 3 watches run at once.** Over that:
`this session already has 3 watches running, which is the limit — stop one with jobs kill first, or use bash background:true for a command that ends on its own`

Three identical failures in a row end a watch. A watch note wakes an idle
session, and watches die with the session like any other job.

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

## Can you look at a picture I send you?

**It depends on the model you are using.** Some models read images directly;
some cannot see at all.

You attach pictures to a message you type. The accepted formats are exactly
**png, jpeg, webp and gif**. Limits: **10MB** per image and **20MB** for all the
images on one message.

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

Note that `read` is not the way to open a picture — it decodes the bytes as
text. Attach it, or use `read_document` for a photograph of a page.

**One model does all the looking.** The looking slot in the settings sheet is
the single answer to "what can see here": the fallback above, `read_document`'s
image rung, and the `view_image` tool below all use that one model. Change it
once and all three change. Leave it alone and aforge picks the best model that
publishes vision, so looking works on a machine that has never opened settings.

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
single call.

Refusals, in its own words:

- Not one of the four types:
  `shot.tiff is not an image this surface can send — png, jpeg, webp and gif are`
- Missing, a directory, or unreadable: `could not read <path>`
- Too big: `<path> is over the 10MB image limit`
- The looking model failed: `<model> could not look at <path>: <reason>`
- It said nothing: `<model> returned no answer for <path>`

**When no looking model can be reached, the tool is not there at all** — it is
left off the toolbelt rather than offered and made to refuse. Ask for a picture
to be looked at then and the answer is that aforge has no way to look, not a
failed attempt.

## Can you search the web?

Yes, and **no key or configuration is required** for it to work.

`web_search` takes a `query` (required) and a `count` — **5 by default, 8 at
most**, silently clamped rather than refused. Results come back as
`1. Title — URL` with the publication date and a snippet of up to 300
characters under each, and a footer like `5 of 12 results`. Nothing found reads
`no results`. A failed search names the back end it used:
`Search failed (duckduckgo): <err>`.

`web_fetch` takes one absolute `url` with a scheme and returns the page's text
with the markup stripped, capped at **4000 bytes**, with the overflow announced
as `…… (18234 more bytes)`. An empty page reads `(empty page)`. A failure reads
`Fetch failed (jina): <err>`. It is not the tool for a local file — `read` opens
those.

Which back end answers is decided top-down: a pin in the `search.provider`
setting wins; otherwise the first keyed service you have a key for (`exa` needs
an Exa key, `jina-search` needs a Jina key); otherwise the zero-key default,
which is DuckDuckGo's HTML endpoint and is always available. So keys change
*which* engine answers, never *whether* the web is reachable.

This matters more generally: aforge leaves a tool **off the list entirely** when
there is nothing behind it, rather than offering it and then refusing. If a
capability is missing, it is missing — you will not get a tool that pretends.

## Can you remember what we worked out earlier in this conversation?

Yes, in two ways: the transcript itself, and three tools that hold working state
**outside** the transcript so a compaction cannot lose it.

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

## Can you tell me how you work?

Yes, and it does not answer from memory. aforge has a `manual` tool that reads
these pages, which are compiled into the binary from the same code they describe.
Ask it anything about aforge — what a tool does, what a command does, why it just
behaved a certain way — and it looks the answer up and tells you it looked it up.

`manual` takes either a `query`, in your own words, which returns the most
relevant sections, or a `page` name to read a whole page. A page name that does
not exist gets an exact refusal listing the pages that do, never a search result
that reads as though the page existed.

When the manual has nothing on a topic, the answer is:
`The manual has nothing on that, which usually means aforge does not do it.`

**Looking something up never asks your permission and records nothing.** It is a
read, like `grep` — no journal line, no cost, no trace in the conversation.

## What aforge cannot do

Plainly, so you do not have to find out the hard way.

- **It does not remember anything between conversations.** There is no durable
  memory in this build. The tools that would write one — `note` and `forget` —
  are not on the list at all, so asking aforge to remember something for next
  time will not work. Working state kept with `track` survives a resume of *this*
  conversation and nothing further.
- **It cannot generate images.** There is no image-making tool on the list. It
  can read pictures (see the vision section) but it cannot paint one.
- **`read` cannot open a picture**, despite what its own description says. Attach
  the image to a message, or use `read_document`.
- **`read` cannot list a directory.** It errors. `ls` lists directories.
- **`grep` needs ripgrep and `find` needs fd** on the machine. Neither is
  downloaded on demand; without them those tools say so and stop.
- **`bash` in the foreground cannot run longer than 600 seconds.** Anything
  longer belongs in the background.
- **A scanned PDF is not readable by `read`**, only by `read_document`.
- **More than 3 watches at once is refused.**

## What does not survive the conversation ending

- **Background jobs and watches.** Every running job is killed when the session
  closes. Their log files stay under `<workspace>/.aforge-v3/jobs/`.
- **A "don't ask again" answer to a permission question.** It is held in memory
  for this session only and is never written down, so the next session asks
  again.
- **Which connected services were switched on.** Those are re-asked on a resume.

What does survive: the transcript itself, which is written to the session file
and replayed when you resume; the working state recorded with `track` and
`commit`, kept in a file beside that session file; and anything written to disk
by `write`, `edit` or a command you ran.
