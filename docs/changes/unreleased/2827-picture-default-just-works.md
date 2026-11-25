---
kind: fixed
title: Pictures work on a fresh profile with no image model set, and an edit keeps its picture's shape
pr: 2827
surface: [engine, chat, docs]
invalidates:
  - "Every picture ask on a profile with no image model set was refused in seconds with 'there is no default image model with a published price'. The curated defaults were listed and priced; the catalog dropped the per-token pricing.image_output that is the only load-time price a drawing model publishes. That price is now kept, and a picture default is chosen."
  - "The default drawing model was bytedance-seed/seedream-5-0-pro, then krea/krea-2-medium-turbo. It is now google/gemini-3.1-flash-image, then bytedance-seed/seedream-5-0-pro, black-forest-labs/flux.2-pro and krea/krea-2-medium-turbo."
  - "When no remembered drawing model was listed and priced, the picture was refused even if other priced drawing models were listed. The best priced listed drawing model (highest published Design Arena score, then lowest id) is now taken. A refusal remains only when nothing listed has a price, or nothing draws at all. Filming keeps its refusal."
  - "generate_image passed aspect_ratio to the model untouched, so a shape the model does not draw (8:5 on Gemini) refused the whole call. The nearest published shape is now sent and the result centre-cropped back to the asked shape, scaled to the edited picture's own size when it has that shape. The result says so."
  - "codeaf image on a home with no catalog cache refused every picture, because it chose a default before the catalog had loaded. It now waits for the catalog, bounded by one fetch."
---

A catalog cache that an older build wrote has no image prices. The lazy warm refetches it once, behind the first frame. The eager `Load` keeps its promise that a fresh cache costs no request.
