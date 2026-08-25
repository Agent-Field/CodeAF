# Making pictures, audio and video

aforge can produce media as well as read it: a picture from a description, a
spoken audio file from text, a piece of music from a brief, a short video from a
prompt. Each one is a tool on the list, each one costs money, and each one saves
a file and tells you where it went.

**These tools are only there when a model behind them is.** aforge resolves one
model per kind of media — drawing, speaking, composing, filming — from your
settings, and a kind with no model available simply has no tool, rather than a
tool that refuses. So `generate_image`, `speak`, `generate_music` and
`generate_video` may each be present or absent independently, and asking for one
that is absent gets you an honest "I do not have that here" rather than a failed
attempt.

**All four work in tasks, in adaptive runs and inside saved harnesses**, not only
in this conversation — see "Can a task or a harness make media?" below.

## Can you draw me a picture or make an image?

Yes, with `generate_image`, when a drawing model is available.

Arguments: `prompt` (required), `reference_paths`, `aspect_ratio`, `size`, `path`,
`model`.

The picture is written to a file, and the result aforge reads is one line naming
it — **the whole path, absolute, from the root** — like:

```
/home/you/work/.aforge-v3/images/20260817-142201-sunset-over-the-harbour.png — 1024×1024 png, 1.4MB, generated on <model>
```

The path is whole because that line is what you are shown in place of the
picture on a terminal that cannot draw one, and a path relative to a directory
you are not standing in is a path you cannot open.

The image itself never enters the conversation, because a picture carried in the
transcript is re-sent on every step of every turn afterwards. If aforge needs to
look at what it made, it opens the file like any other picture. **You do not
have to open the file yourself** — see the next section.

Leave `path` out and the name is a timestamp plus a few words of your prompt,
saved where this session keeps its pictures. Give `path` and you choose the name
and folder; an existing file there is overwritten, exactly as `write` would.

`aspect_ratio` (for example `16:9`) and `size` (for example `1024x1024`) are
passed to the image model untouched. Leave them out for its own default.

## Do I only get a file path, or can I see the image you made?

**You see it, in colour, in the terminal, without doing anything at all.**

The line above is what aforge itself reads — a path is all that goes into the
conversation — but the screen does more with it. The moment the call finishes,
the picture is drawn under its row as a thumbnail, at most 12 rows tall (4 at
phone width). No click, no key. That happens in this conversation and in a
task's room alike.

**Open the row** — click it, or select it with `↑`/`↓` and press `enter` — for
the bigger look: the picture again at up to 20 rows, with one dim line under it
giving the file's whole absolute path, its pixel size and its size on disk. That
path is a link — cmd+click it (ctrl+click on Linux) and the picture opens the way
your desktop would open it. A `view_image` row also shows what the looking model
said, under the picture.

It is drawn from half-block characters, two stacked pixels to a cell, so it needs
a terminal with **256 colours or better** and a UTF-8 locale — iTerm2,
Terminal.app, kitty, Alacritty, WezTerm, Ghostty, GNOME Terminal, and tmux or ssh
over any of them all qualify. Where those are missing, or the file is gone, the
row shows its result line exactly as before and never an error — and that line
carries the file's whole absolute path, clickable in the same way, so you can
still open it yourself. png, jpeg, gif and webp are drawn.

The "what is on the screen" page has the sizes, the wrapping rule for the path,
which terminals can open a path and which cannot, and the full list of cases
where no picture is drawn.

## Can you edit, restyle or combine images I already have?

Yes — that is `reference_paths`, and it is the same tool.

Give one path and the prompt becomes an instruction about that picture: change
the background, ink the sketch, put it in another style. Give several and they
are combined. The references are files in your workspace — **png, jpeg, webp or
gif, up to 10MB each** — and anything else is refused by name, e.g.
`could not read art/missing.png`, before a penny is spent.

The useful part: **the path `generate_image` just returned is itself a valid
reference**, so aforge can pass its own last render back in and iterate — a
diagram redrawn until it is right, a character kept the same across pictures.

## Can you use a different model for one picture, sound or video?

Yes. All four making tools — `generate_image`, `speak`, `generate_music`,
`generate_video` — take an optional `model` argument: which model to use **for
that one call**, when the default is wrong for it. A photoreal render on one
model, a diagram on another, a different voice vendor for one line — without
touching any setting.

The word is matched against the catalog **within that kind of media**: a full
slug works, and so does a fragment like a vendor or family name — `seedream`,
`gemini`, `grok`. The word `best` picks the strongest advertised model of that
kind. A word that matches nothing is refused before any money is spent — `no
image model matches "xyz"` — and a word that names a model of the wrong kind is
refused by naming what it actually makes, e.g. `fish-audio/s1 makes speech, not
image`.

**Leaving `model` out uses the session's default**, resolved from your settings
as ever, and the next call without the argument rides the default again — a
one-call choice never changes any setting. The result line always names the
model that actually generated the file, so you can tell which one made what.

This is the same freedom you have yourself in `/settings` → Providers, handed
to aforge per call: ask it to "draw this one with gemini" or "try the best
image model" and it can, just in time.

## Why does a picture or video look generic, blurry, or like AI slop?

Four causes, all fixable — none of them is "the model is bad at this".

**The prompt left too much undecided.** Every dimension a prompt does not
decide — the medium, the light, the palette, the mood, the era — the image or
video model fills with its statistical average, and that average is exactly what
generic AI output looks like: over-smooth, over-lit, style-less. The fix is
specificity: aforge writes the decisions into the prompt rather than asking for
"a nice picture of X" and hoping. Ask it to redo a generic render "as a
photograph, natural light" or "as a flat diagram, two colours" and the words go
straight to the model.

**Every dimension was decided — to the genre's own cliché.** A fully detailed
prompt can still land on the average when each detail is what everyone in that
genre writes: the glowing shape on a dark field, the neon palette, the adjective
pile ("ultra-detailed", "cinematic"). Eight models given that prompt return
eight competent copies of the same picture, because the prompt asked for the
mean of the genre. And the mean cannot be escaped from inside the genre —
recolor a glowing dark-mode network and it is still a glowing dark-mode
network. The exit is a **real medium, named**: a print process, a photographic
setup, a drafting or filmmaking tradition. A real medium carries its own
physics and its own, different average — a risograph poster or an editorial
photograph simply is not drawn from the pool "digital AI art" comes from. This
applies however the render is made: the same law covers a prompt sent through
`generate_image` and one a script of aforge's own sends to an API.

## Why is everything you make glowing on a dark background?

Because that is the statistical center of the genre the prompt stayed inside —
"digital tech illustration" resolves to luminous lines on a dark field almost
regardless of the other words — and because saying **"no glow" does not work**:
image and video models barely read negation, so the word "glow" in "no glow"
pulls toward glow. Two fixes, and they work together:

- **Leave the genre, do not redecorate it.** Name a real medium with real
  physics — "flat vector print, two spot colors on warm paper", "daylight
  editorial photograph", "pencil technical drawing on vellum". Each of those has
  its own average, and none of them glows.
- **Specify positively until the default has no room.** Instead of forbidding,
  describe what IS there: the surface (matte paper, cloth, brushed metal), the
  light (overcast daylight, one window, flat studio), the palette by name.
  Matte ink on cream paper *cannot* glow; a prompt that establishes it never
  needs the word "no".

When a render comes back, aforge judges it against the genre as well as the
brief — "could this be mistaken for every other image of its kind?" — and
iterates when the answer is yes.

**Nothing asked for sharpness.** `generate_image` takes `size` (for example
`1024x1024`) and `generate_video` takes `resolution` (for example `1080p`), both
passed to the model untouched; left out, the model's own default decides, and a
default can be modest. Ask for "1080p" or "a larger size" and it is passed
through — a sharper render costs more and, for video, takes longer.

**The first render was accepted as the last.** A first render is a draft. aforge
can look at what it made (`view_image`, or `read` on the file), judge it against
the brief, and iterate — the path a render returned is a valid
`reference_paths` entry, so "fix the hands, keep everything else" is one more
call, not a fresh roll of the dice. For video, `seed` holds a shot steady while
one thing about it is changed.

A different model is also a real lever: the `model` argument tries another one
for a single call, and `best` picks the strongest advertised — see "Can you use
a different model for one picture, sound or video?".

## Can you read this out loud, or make a voiceover?

Yes, with `speak`, when a speech model is available.

Arguments: `text` (required), `voice`, `path`, `model`. It writes an **mp3** and answers
with the path, the file size and the model, e.g.

```
.aforge-v3/audio/20260817-142433-good-morning-harbour-road.mp3 — 84.2KB of mp3 audio, spoken by <model>
```

**Leave `voice` out and the provider's default voice speaks.** Name one only if
you asked for a particular voice, because a voice the model does not have is a
failed generation rather than a near miss.

With no speech model set in `/settings` → Providers, the default is
`fish-audio/s2.1-pro`, falling back to `fish-audio/s1`, then `hexgrad/kokoro-82m`
and then `openai/gpt-4o-mini-tts` on a catalog that does not advertise it. A
model you set yourself wins over all of them.

There is no duration in the result: nothing here opens the mp3 to measure it, and
a guessed length would be worse than none. Play the file to hear it — aforge
cannot listen to audio.

## Can you write me music, compose a song, or make a backing track?

Yes, with `generate_music`, when a music model is available. It is a **different
model and a different tool from `speak`** — one composes, the other reads text
aloud — so a machine can easily have one and not the other.

Arguments: `prompt` (required), `path` and `model`. The prompt describes the **music** —
genre, instruments, tempo, key or mood, how it should develop — and is not lyrics
to sing and not text to be read out.

**The call returns immediately with a background job**, exactly as
`generate_video` does, because a compose takes most of a minute:

```
job 4 started; composing on <model> — the finished piece arrives as a note naming the file. Log at /path/to/.aforge-v3/jobs/4.log
```

aforge keeps working — on other clips, on a stitch, on the conversation —
while the piece is written, and when it lands aforge is told in a note at the
next step:
`job 4 finished: .aforge-v3/music/20260818-160204-a-calm-solo-piano-loop.mp3 — 1.6MB of mp3 audio, composed by <model>`.
A compose that fails says so the same way: `job 4 failed: music generation
failed (<model>): …`. It shows in `jobs list` as `job 4 · music · running · 12.3s
· a calm solo piano loop`, and `jobs kill 4` stops it — `music (job 4) stopped;
no music was saved`. Like every job, it dies when the conversation ends.

**There is no length argument**, because the endpoint has none: the model writes
a piece of its own choosing — half a minute to a minute in practice — and you
cannot ask for eight seconds, or for three minutes. Nor is there a format
argument; you get what the model sends. To put a piece under anything timed — a
video, a slideshow — the file has to be measured and then looped or trimmed to
fit, which is shell work with ffmpeg that aforge does on request; the tool
itself neither measures nor trims, because the length is the model's choice,
not the brief's.

**Every call costs the same whatever comes back**, around **$0.08**, because the
price is per call and not per second. That makes a short clip and a long one the
same money, so it is worth writing a full description and iterating on the
description rather than calling it repeatedly hoping for something shorter.

Music and speech land in **different folders** — `music/` and `audio/` — so a
session's takes of a theme are not mixed in with its voiceovers.

## Can a task or a harness make media, or only this conversation?

All of them can, under the same rule.

- **A task** (`propose_task`) carries the same media verbs this conversation
  does. A task briefed to draw a diagram has the hand that draws it.
- **An adaptive run**'s nodes carry them too.
- **A saved harness** may whitelist `generate_image`, `speak`, `generate_music`,
  `generate_video` and `view_image`, and a step may call them. The list the
  designer is offered is the same list the run resolves against, so a design can
  never name a verb the run could not execute. See the "saved shapes of work"
  page.

The rule is the same everywhere: **no model for that kind of media, no verb** —
absent rather than present and refusing.

One thing worth knowing about tasks: when a task is stopped for running too long,
it gets a final "land now" turn to save what it has. That turn keeps the saving
tools — `write`, `edit` and every media verb it had — so a task whose deliverable
is a picture can still produce it. It used to keep only `write` and `edit`, and a
task that had spent its whole life painting would answer that it had no image
tooling available.

## Can you make a video?

Yes, with `generate_video`, when a video model is available — and this one
behaves differently from most tools, because a render takes **minutes**.
(`generate_music` behaves the same way, for the same reason.)

**The call returns immediately with a background job**, like `bash` with
`background: true`:

```
job 3 started; filming on <model> — the finished video arrives as a note naming the file. Log at /path/to/.aforge-v3/jobs/3.log
```

It keeps working while you and aforge carry on talking. When it lands, aforge is
told in a note at the next step:
`job 3 finished: .aforge-v3/video/20260817-143001-a-ferry-at-dawn.mp4 — 4.2MB of mp4 video, 8.0s with sound, filmed on <model>`.
The length and the sound answer are measured from the file itself — a clip that
landed silent says `without sound` — and when the file cannot be measured the
note simply omits both rather than guessing. A render that fails says so the
same way: `job 3 failed: video generation timed out (<model>); no video was
saved`. Nothing waits for it and nothing polls it.

With no video model set in `/settings` → Providers, the default is
`bytedance/seedance-2.5`, falling back to `bytedance/seedance-2.0-mini` on a
catalog that does not advertise it. A model you set yourself wins over both.

Arguments: `prompt` (required), `duration` in seconds, `aspect_ratio`,
`resolution`, `seed`, `model`, `frame_paths`, `reference_paths`, `path`.

- `frame_paths` pins the motion: the first picture is the opening frame, a second
  is the closing one. **More than two is refused** — the wire has no third slot.
- `reference_paths` sets the look — style, palette, a face — and not the motion.
- An image `generate_image` just made is a valid frame or reference.
- `resolution` is how sharp the render is, spelled the video model's way (for
  example `720p` or `1080p`) and passed through untouched. Leave it out for the
  model's own default; a higher resolution is a slower, costlier render.
- `seed` is a fixed number that makes the render's randomness repeatable, when
  the model takes one. The same seed with the same prompt and pictures
  re-renders close to the same shot — hold it steady to change one thing about
  a shot that was mostly right, leave it out for a fresh roll.

The render **is** a job: it shows in `jobs list` as
`job 3 · video · running · 42.1s · a ferry at dawn`, and `jobs kill 3` stops it —
`video (job 3) stopped; no video was saved`. Like every job, it dies when the
conversation ends. The provider gives up after **10 minutes** and no file is
saved.

## Can you make a longer video — several clips, a whole story, 2 minutes of film?

Not in one render, and yes by joining several — a single render is a short
clip, because the video providers top out around ten seconds; nothing in
aforge extends one render. A longer video is several
`generate_video` calls stitched together with ffmpeg in the shell, and whether
the result hangs together is decided by three facts about the tool:

- **Every render is independent.** The video model sees one prompt and the
  pictures passed to that one call — never the conversation, never an earlier
  clip. A prompt that says "the hero" without describing him reaches a model
  that has never met the hero, so everything that must match across clips is
  described in every prompt.
- **Pictures are the only thread between clips.** The same `reference_paths`
  handed to every call keep a face and a costume steady. For clips that should
  **connect** — one shot flowing into the next — the last frame of a finished
  clip is passed as the next call's opening `frame_paths` entry (in a saved
  harness step, whose `generate_video` has no `frame_paths`, the same slot is
  the first `reference_paths` entry). That chain makes connected clips a
  sequence: they cannot all render in parallel.
- **Each clip lands with its own sound**, and its note says so. A stitch keeps
  that sound only if the join carries the audio streams as well as the video,
  and a continuous score is `generate_music` — a background job of its own,
  whose file exists only once its note has landed — looped under the whole cut.

Even chained, clips are distinct shots with some drift between them — a
stitched video is a cut, not one continuous take. Fewer scenes in one setting
read as more coherent than many scattered ones.

## Why is a stitched video incoherent, or silent after the first clip?

Both come from the facts above, and both are fixable.

**The story or the characters are not coherent:** the clips were rendered
independently with nothing shared — each `generate_video` call reaches the
video model alone, with no memory of the other clips. The fixes are the
threads that do cross: the same reference images on every call, every prompt
describing everything that must match, and — for shots that should flow into
each other — the previous clip's final frame passed as the next clip's opening
frame, which means rendering those clips one after another rather than all at
once.

**No sound, or no audio after the first clip:** the clips almost certainly
landed with sound — each clip's landing note says `with sound` or `without
sound`, measured from the file — and the join dropped it. An ffmpeg filter
that only crossfades the video streams carries just the first input's audio;
the stitch has to map or crossfade the audio streams too, or concatenate both
streams together. Music is separate either way: a score under the whole cut is
`generate_music` — started early, because it is a background job whose file
arrives as a note — then measured and looped to fit, mixed in at the join.

## Where do the pictures, audio, music and video you make end up?

In one of three places, decided by whose folder the workspace is:

- If the session **owns** its workspace (aforge made it), files land straight in
  it, like anything else the work produced.
- If the workspace is **your repository**, they land in the session's own
  `artifacts/` folder instead, so nothing of aforge's is dropped in your project.
- With no session folder at all, they land under
  `<workspace>/.aforge-v3/images`, `/audio`, `/music` or `/video`.

Either way every generated file gets a row in the deliverables index, so
`/files` finds it again later by name and date, from any directory. Give the
tool an explicit `path` and that decision is yours instead.

## What does a picture, a voiceover, a piece of music or a video cost?

Real money, and more than a message does.

Generation is billed by the provider per image, per stretch of audio, per piece
of music, per render — a video is the expensive one, often more than a whole
conversation of talking. **Music is billed per call**, around $0.08, whatever
length comes back. That spend lands on **the session's total**, not on the turn
that asked for it: no single turn is charged for a render that arrived ten
minutes after it ended. `/cost` shows the total, and a picture you asked for is
inside it.

The permission rules apply as they do to any other tool, so under the default
approval mode you are asked before a generation runs. See the permissions page
for how to change that.
