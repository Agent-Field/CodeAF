# Multimodal — capability for the graph, presence for the chat

Voice, images, and speech enter aforge through two doors that must not be
confused: *generation is a graph capability* (tools any leaf can use),
*perception is a surface affordance* (mic and attachments where you talk).
The governing decision, per the emergent-capability principle: we ship
modality **primitives**, never modality *features* — what to generate and
when is the system's judgment inside ordinary tasks, not a button.

## Decision 1 — Generation is a tool, never a chat feature

`generate_image`, `speak`, and `view_image` are leaf tools, registered
beside `recall`, journaled like every tool call, costed through the dollar
rail. Chat reaches them the same way it reaches everything: a reflex or
task. This means a future job — "diagram the architecture for the README",
"produce the episode intro" — simply *has* the capability, with no new
surface built. Artifacts land in the task workspace under `media/` with
readable names, and render in the thread as one quiet OSC8 line
(`⌾ sunset-over-harbor-1.png`, `♪ intro.mp3`) that opens in the OS viewer.

**Rejected:** an image command, inline terminal image protocols, any
generation path that bypasses the journal or the rail.

## Decision 2 — One catalog, one question: "what can this model do?"

A single cached seam over OpenRouter's models API answers every modality
question — `ModelsWithOutput("image")`, `Supports(model, "input",
"image")` — with ~24h TTL and graceful offline decay (stale beats empty
beats hardcoded). Every picker, gate, and tool consults this one seam.
Endpoints, for the record: `/api/v1/images` (b64 out, `input_references`
for img2img), `/api/v1/audio/speech` (raw bytes out), `/api/v1/audio/
transcriptions` (text out), all filtered from `/api/v1/models` by
`input_modalities` / `output_modalities`.

## Decision 3 — The model palette: five slots, one dropdown

The header stops naming models separately. One `models ⌄` affordance opens
a single calm panel — the palette:

    talk      claude-sonnet-5            ⌄
    work      claude-opus-5              ⌄
    voice     qwen3-asr-flash            ⌄
    image     gemini-3-pro-image         ⌄
    speech    gpt-4o-mini-tts            ⌄

Each row opens the same type-to-search list, pre-filtered by that slot's
required capability from the catalog (a slot can only ever be set to a
model that can do the job — the filter *is* the validation). Arrows,
number keys, click, esc — the standard grammar. Defaults resolve at use
time against the catalog with a documented preference order, so a renamed
slug degrades to the best available model instead of a 404.

## Decision 4 — Perception: the draft is sacred, in every modality

- **Voice in** (mic, `docs/` — the voice-input build): transcription
  *appends* to the typed draft, provisional text is typographically
  unmistakable, esc discards voice and never the draft.
- **Images in**: a path dragged into the input becomes a dim chip
  `⌾ name.png ⟨×⟩` above the bar; on send it rides as a content part if
  the talk model has vision, else one calm hint and the message goes as
  text. Tasks perceive through `view_image`, gated by the same catalog
  check, refusing with the model's name so the executor can adapt.

## What this is not

- Not a media app. No galleries, no previews, no progress bars — a
  generation is a tool call that ends in a clickable line.
- Not per-message model switching. The palette sets standing slots; the
  router still owns escalation within a job.
- Not free. Every byte generated is a journaled, railed dollar.

---

## The v3 revision (Aug 2026) — one knob, a use-time resolver, and a full belt

Decisions 1–4 above were written for the graph-era product. v3 keeps their
spirit and re-lands them against the session engine, with the defects the
first wiring left behind named and closed. Where this section disagrees with
the text above, this section wins.

### Decision 5 — One knob per modality: the settings slot IS the truth

The media slots in the v3 settings sheet — looking, drawing, speaking,
composing, filming, voice — become the keys the ENGINE actually reads. The
double-knob era (a `vision_model` row v3 ignored beside a `roles.vision` pin
it obeyed) is over: the roles free-text pins remain as the second rung for
operators, but the sheet's slot rows are the front door, their writes are
ACCEPTED, and the picker each opens is pre-filtered by the slot's required
capability from the catalog — the filter is the validation, as Decision 3
promised and the wiring never delivered.

Resolution happens AT USE TIME, in one resolver per modality, with one
documented ladder: the slot key → the role pin → the best catalog candidate
publishing the capability → the curated fallback. Every rung is
capability-checked against the catalog (`Supports`, `ModelsWithOutput`): a
slot or pin naming a model that cannot do the job is passed over with one
log line, never sent to a provider to fail. A renamed slug degrades to the
best available model instead of a 404, and a machine that has never opened
settings still draws, speaks, and films out of the box.

### Decision 6 — The door stops starving the pickers

`v3Models` no longer drops non-text-out rows; the full catalog reaches the
surface and the on-disk cache, and each list applies its OWN filter at the
moment it is drawn (talk lists stay chat-only; the drawing slot sees image
models). Silence gets ONE law everywhere: an unpublished modality list means
text-in/text-out and NOTHING more — a media capability is never assumed,
only published, with the id-word marks as the last resort for rows that
publish nothing. Picker rows and `aforge models` grow a dim modality tail
("sees · draws") so a filtered list is explicable, and `/model <slug>` warns
when a slug cannot hold a conversation instead of silently accepting a
music model as the talk model.

### Decision 7 — The belt carries every verb, and each verb carries its features

`session.Config` gains `Media` (the provider media client) and `MediaModel`
(the use-time resolver, one string in: "image", "speech", "video",
"vision"). The v3 belt gains, each absent-not-broken but PRESENT by default
because the resolver has catalog fallbacks:

- **`generate_image`** grows the features the wire already has:
  `reference_paths` (image-to-image — edit, restyle, combine), and
  `aspect_ratio`/`size` passthrough. The description TEACHES the leverage:
  a model that knows it can pass its own last render back as a reference
  can iterate a diagram; one that only knows "prompt in, png out" cannot.
- **`speak`** — text → audio via `/audio/speech` (`voice` optional,
  provider default when omitted; mp3 out), landed and cited like an image.
- **`generate_video`** — async by nature (`/videos` is submit-then-poll),
  so it is a BACKGROUND JOB in the existing registry: the tool returns the
  job id in seconds, the poll runs where jobs run, and completion reaches
  the model on the steering lane like any job's exit. A ten-minute render
  never holds a turn hostage. `frame_images` (first/last frame) and
  `input_references` (style) ride through.
- **`view_image`** — look at a file on disk through the LOOKING slot's
  model, one shot, answer in the tool result. This is how a task node
  finally gets eyes, and how a blind chat model examines a screenshot
  without a person attaching it.

Every output lands by the Decision-26 law (owned → `work/`, borrowed → the
session's `artifacts/`), earns a row in the deliverables index (`/files`),
and its spend folds into the session's auxiliary pocket. Generation stays
behind the approval gate: a dollar a tool spends is a dollar the person
posture'd. The manual pages land in the same change, because the build
gates make a tool the manual does not know a build that does not exist.

### Decision 8 — The model always knows what it can do

Capability is not a surprise the model discovers by failing. The tool
descriptions are the teaching surface — each names its features, its
landing law, and one clever use — and the vision fallback, the frames
rung, and `read_document`'s image rung all resolve through the same
looking slot, so "can I see" has one answer wherever it is asked. The
transcript guard closes the last hole: switching a conversation with
attached images onto a blind model turns the image parts into their text
placeholders instead of sending base64 to a model that cannot read it.

**Deliberately deferred:** mic/voice-input in tui3 (the `internal/voice`
machinery is real and waits on a surface wave), and `generate_music` as a
separate verb (`speak` carries the endpoint until a music model earns a
schema of its own).

### Decision 9 — Perception is `read`'s job: a file is a file, in every modality

Decision 20 of CHAT-V3.md refused an `extract_pdf` tool because a belt with
two hands for one intention makes the model choose, and what it chooses is
wrong. That law generalizes, and it is the whole answer to "how does the
model know it can look and listen": the model's most natural instinct — "I
need to look at this image, I need to hear this recording → read it" —
must simply work. So `read` sniffs and routes: an image goes to the looking
model as a full extraction; audio climbs a ladder that tries TRANSCRIPTION
first and falls to an audio-understanding chat model when the sound is not
speech (the ladder picks the sense, so "what style is this track" needs no
endpoint knowledge from the model); video goes to a video-input chat model.
No new input verbs, no belt growth, no prompt real estate beyond one
unconditional line on `read` itself — unconditional because the senses ride
the session's own credentials, exactly as `read_document` does.

The two-door precedent holds: bare `read` is the undirected extraction;
`view_image` (and `read_document`'s ladder) stay as the directed doors a
question rides through. Production stays explicit — three verbs, always
taught by their own descriptions — because making something is a decision
and sensing something is a reflex.
