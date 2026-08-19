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

`read` opens **images, audio and video** too, as a description rather than as
bytes. See "Can you look at an image, listen to audio or watch a video I have on
disk?" below for what comes back and what it costs.

`read` cannot open a **directory**. It answers
`Error reading file: read <path>: is a directory`. Use `ls` to list a directory.

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
the maximum** you can ask for. A higher `timeout` is quietly clamped to 600. A
`timeout` that is missing, null, zero or negative is the same as not asking: 120
seconds is written in for it, so there is no way to spell a foreground command
that runs unbounded. On a timeout the whole process group is killed and the
result is `Command timed out after N seconds` — and the call returns there even
if something it started in the background is still holding the output pipe open.

A command that exits non-zero answers `Command exited with code N`. An
interrupted one answers `Command aborted`. If the workspace directory is gone:
`Working directory does not exist: <cwd>\nCannot execute bash commands.`

Anything that is meant to keep running — a server, a dev watcher, a long build —
should be started in the background instead, where it never times out.

`bash` follows your approval mode, which asks by default.

## Does cd stick between commands — changing directory in bash

No. **Every `bash` call starts again in the workspace root.** Each one is its own
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
  length argument: you get a piece of the model's own choosing, around a minute,
  for a flat price per call.
- `generate_video` renders a short video. It **returns straight away with a
  background job** because a render takes minutes; the finished file arrives as a
  note naming it, and `jobs kill` stops it.

Each saves a file and answers with its path — never the media itself — and each
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

## Can you change my aforge settings for me, or tell me what a preference is set to?

Yes to both, and a change is permanent. Ask in your own words — "use
`deepseek/deepseek-v4-pro` for planning", "set my daily budget to 5", "stop
drawing timestamps" — and aforge does it rather than telling you where the panel
is.

Two tools, because reading your configuration and rewriting it are different
acts and you get to answer them separately.

`settings` reads. With no arguments it lists every row of the settings registry
— the same rows `/settings` shows, in the same four categories — one line each,
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
settings · daily budget · $10 → $5
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
works in a worktree with nobody watching it, and a permanent change to your
machine that no transcript ever showed you is the one thing this pair must not be
able to make. `propose_task` and `tasks` are NOT absent there: a task may hand
pieces of its own work out, two levels deep at most, and `tasks` shows it those
pieces and nothing else.

## What aforge cannot do

Plainly, so you do not have to find out the hard way.

- **It does not carry a conversation into the next one.** The transcript stays
  where it was written; a new session opens on an empty screen and knows nothing
  about what was said in the last one. What it *does* carry is a handful of
  durable lines — a preference you stated, something you asked it to remember —
  and only those; see the what-i-remember page for what is kept and how to read
  it. Working state recorded with `track` survives a resume of *this*
  conversation and nothing further.
- **It cannot make media without a model for it.** `generate_image`, `speak`,
  `generate_music` and `generate_video` are each on the list only when this
  machine has a model for that kind of media; when there is none, the tool is
  absent rather than present and refusing, and aforge simply does not have that
  verb. The same rule applies inside a task, an adaptive run and a saved harness.
- **`generate_music` cannot be asked for a length.** The endpoint takes no
  duration, so the model writes a piece of its own choosing and the call costs
  the same however long it turns out.
- **`read` cannot list a directory.** It errors. `ls` lists directories.
- **`read` cannot look at an image, listen to audio or watch a video when no
  model is set for that sense.** It says which one is missing rather than
  showing you the bytes.
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
`commit`, kept in a file beside that session file; **anything remembered**, with
the `remember` tool or `/remember` or by asking, which is kept per person and
read by every conversation after this one (see what-i-remember); **a setting
changed with `change_setting`**, which is written into your profile's
`config.json` and read by every conversation after it; and anything written to
disk by `write`, `edit` or a command you ran.
