# Models: the eight slots, boost, and naming one yourself

## The eight slots

Aforge does not run on one model. It keeps eight named slots, each filled by a
model that can actually do that job:

| slot | what it is for |
| --- | --- |
| `talk` | the front desk — your replies |
| `work` | the workforce — the model that does jobs |
| `voice` | turning your speech into text |
| `image` | generating images |
| `speech` | generating spoken audio |
| `music` | generating music |
| `video` | generating video |
| `boost` | the heavier model you reach for deliberately |

Open them all from **models ⌄** in the header, or with `/model`. Press `1`–`8`
to jump to a slot, type to filter, enter to choose. `/model work <slug>` sets
one directly.

A slot only ever offers models that can do its job — the filter is the
validation. `boost` is the one exception where empty is meaningful: an empty
boost slot means "follow work".

## Boost: heavier, for one question or until you turn it off

Press **ctrl+b** (or **alt+b**), or click the boost hint in the footer. It
cycles through three states:

1. **off**
2. **armed** — the *next* message goes to the boost model
3. **pinned** — every message goes to the boost model until you turn it off

Armed is spent the moment you press enter, not when a reply succeeds, so a
failed call never leaves an expensive surprise armed for your next message.
While boost is on, the input prompt becomes `»`, and a boosted reply is
attributed with the model that produced it — ordinary replies stay unlabelled.

Boost changes who *answers you*. To change who does the *work*, say so in the
request.

## Model words: naming a model in the request

You can name a model in the sentence itself, and it is heard deterministically —
no model gets a vote on which model runs your job.

- **"use the better model"**, "with the best model", "with the stronger model",
  "use the boost model", "model 2" → the job runs on your boost slot.
- **"use gemini"**, "with the opus model", "use model qwen3" → resolved against
  the live catalog for the work slot.

If a name matches exactly one model, the receipt says `Running on <model>.` If
it matches several, aforge asks which you meant — up to four options — and
splices no work until you answer. If it matches nothing and you clearly said the
word "model", you get one calm line saying so and the job runs on the usual
model. If you just said "use gemini" and nothing matched, it stays quiet: a bare
word that resolves to nothing was probably never a model word.

## "Best" for media

For images, speech, music and video, quality words are heard separately:
**"best quality"**, "highest fidelity", "make it really good", "final version",
"production-quality". These tell the workers to reach for the best model of that
kind for the final artifact, rather than the routine one.

Note the split: "use the **best model**" means your boost slot; "**best
quality**" means the best media model. They are different asks and they are
recognized differently.

## The vision proxy

If the model currently doing the work cannot see images, aforge does not give
up and does not pretend. A vision model looks at the file and reports back, and
the answer is prefixed with **`seen by <model>:`** so the attribution is never
lost. If no vision-capable model is available at all, it says so plainly rather
than inventing a description.
