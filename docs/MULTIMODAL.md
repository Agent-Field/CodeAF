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
