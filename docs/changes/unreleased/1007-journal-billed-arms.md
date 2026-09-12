---
kind: fixed
title: billed arms journal their call lines, and a hopped turn seals one usage line per model
pr: 1007
surface: [chat, engine]
invalidates:
  - "A transcript's `call` lines were written only for requests whose usage block arrived on the stream. A billed request reconciled from a late receipt — a hedge arm or a cut retry — now writes one too, marked `arm: hedge` or `arm: reconciled`; it is evidence like every call line and moves no total."
  - "A turn's `usage` seal was one line naming the model latched at the seal, so a mid-turn model hop attributed the whole turn to the last model. The seal now writes one line per model that answered, each with that model's own tokens, calls and cost; the turn's wall time rides the alphabetically first line only."
  - "`session.Usage` was a plain comparable struct. It now carries a `*modelShares` breakdown field — still comparable and still nil on every value but a live turn's — so `usage == (Usage{})` keeps compiling."
---

The measured planning session's journal summed to $0.789 of a $1.412 bill, and
its one turn seal named deepseek for 31 requests that z-ai/glm-5.3 had served
thirteen of. Both gaps were the same shape: the expensive requests left no
evidence. The meter was always right; only the journal was silent, which is why
neither fix touches a total.
