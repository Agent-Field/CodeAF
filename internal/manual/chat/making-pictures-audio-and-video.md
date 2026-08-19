# Making pictures, audio and video

aforge can produce media as well as read it: a picture from a description, a
spoken audio file from text, a short video from a prompt. Each one is a tool on
the list, each one costs money, and each one saves a file and tells you where it
went.

**These tools are only there when a model behind them is.** aforge resolves one
model per kind of media — drawing, speaking, filming — from your settings, and a
kind with no model available simply has no tool, rather than a tool that refuses.
So `generate_image`, `speak` and `generate_video` may each be present or absent
independently, and asking for one that is absent gets you an honest "I do not
have that here" rather than a failed attempt.

## Can you draw me a picture or make an image?

Yes, with `generate_image`, when a drawing model is available.

Arguments: `prompt` (required), `reference_paths`, `aspect_ratio`, `size`, `path`.

The picture is written to a file, and the result aforge reads is one line naming
it, like:

```
.aforge-v3/images/20260817-142201-sunset-over-the-harbour.png — 1024×1024 png, 1.4MB, generated on <model>
```

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

You see it, in colour, without leaving the terminal.

The line above is what aforge itself reads — a path is all that goes into the
conversation — but the screen does more with it. **Open the `generate_image` or
`view_image` row** (click it, or select it with `↑`/`↓` and press `enter`) and
the picture is drawn in the expansion, with one dim line under it giving the
file's whole absolute path, its pixel size and its size on disk.

It is drawn from half-block characters, two stacked pixels to a cell, so it needs
a terminal with **256 colours or better** and a UTF-8 locale — iTerm2,
Terminal.app, kitty, Alacritty, WezTerm, Ghostty, GNOME Terminal, and tmux or ssh
over any of them all qualify. Where those are missing, or the file is gone, the
row shows its result line exactly as before and never an error. png, jpeg, gif
and webp are drawn.

The "what is on the screen" page has the sizes, the wrapping rule for the path,
and the full list of cases where no picture is drawn.

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

## Can you read this out loud, or make a voiceover?

Yes, with `speak`, when a speech model is available.

Arguments: `text` (required), `voice`, `path`. It writes an **mp3** and answers
with the path, the file size and the model, e.g.

```
.aforge-v3/audio/20260817-142433-good-morning-harbour-road.mp3 — 84.2KB of mp3 audio, spoken by <model>
```

**Leave `voice` out and the provider's default voice speaks.** Name one only if
you asked for a particular voice, because a voice the model does not have is a
failed generation rather than a near miss.

There is no duration in the result: nothing here opens the mp3 to measure it, and
a guessed length would be worse than none. Play the file to hear it — aforge
cannot listen to audio.

## Can you make a video?

Yes, with `generate_video`, when a video model is available — and this one
behaves differently from every other tool, because a render takes **minutes**.

**The call returns immediately with a background job**, like `bash` with
`background: true`:

```
job 3 started; filming on <model> — the finished video arrives as a note naming the file. Log at /path/to/.aforge-v3/jobs/3.log
```

It keeps working while you and aforge carry on talking. When it lands, aforge is
told in a note at the next step:
`job 3 finished: .aforge-v3/video/20260817-143001-a-ferry-at-dawn.mp4 — 4.2MB of mp4 video, filmed on <model>`.
A render that fails says so the same way: `job 3 failed: video generation timed
out (<model>); no video was saved`. Nothing waits for it and nothing polls it.

Arguments: `prompt` (required), `duration` in seconds, `aspect_ratio`,
`frame_paths`, `reference_paths`, `path`.

- `frame_paths` pins the motion: the first picture is the opening frame, a second
  is the closing one. **More than two is refused** — the wire has no third slot.
- `reference_paths` sets the look — style, palette, a face — and not the motion.
- An image `generate_image` just made is a valid frame or reference.

The render **is** a job: it shows in `jobs list` as
`job 3 · video · running · 42.1s · a ferry at dawn`, and `jobs kill 3` stops it —
`video (job 3) stopped; no video was saved`. Like every job, it dies when the
conversation ends. The provider gives up after **10 minutes** and no file is
saved.

## Where do the pictures, audio and video you make end up?

In one of three places, decided by whose folder the workspace is:

- If the session **owns** its workspace (aforge made it), files land straight in
  it, like anything else the work produced.
- If the workspace is **your repository**, they land in the session's own
  `artifacts/` folder instead, so nothing of aforge's is dropped in your project.
- With no session folder at all, they land under
  `<workspace>/.aforge-v3/images`, `/audio` or `/video`.

Either way every generated file gets a row in the deliverables index, so
`/files` finds it again later by name and date, from any directory. Give the
tool an explicit `path` and that decision is yours instead.

## What does a picture, a voiceover or a video cost?

Real money, and more than a message does.

Generation is billed by the provider per image, per stretch of audio, per
render — a video is the expensive one, often more than a whole conversation of
talking. That spend lands on **the session's total**, not on the turn that asked
for it: no single turn is charged for a render that arrived ten minutes after it
ended. `/cost` shows the total, and a picture you asked for is inside it.

The permission rules apply as they do to any other tool, so under the default
approval mode you are asked before a generation runs. See the permissions page
for how to change that.
