---
kind: added
title: the model catalog reads OpenRouter's dedicated image and video listings
pr: 0
surface: [engine]
invalidates:
  - "Was true: the catalog was GET /models?output_modalities=all and nothing else, so it held no video-output model at all and 16 of the 59 image generators OpenRouter serves. Is true now: on the OpenRouter source, on its own base only, a load also reads GET /images/models and GET /videos/models (two bounded requests, read after the general listing lands, 4 MiB and 500 rows each) and merges them in; their modalities and parameters win, a chat model keeps its text side and token prices, and a pure generator never gains text. A listing that fails keeps its previous rows and is reported stale by `Catalog.MediaState`; its failure is never proof that no service offers a model."
  - "Was true: a catalog price was always dollars per token. Is true now: a video model's per-second rate (`cents_per_*` skus) rides `Model.MediaPrices` in its own unit and `PriceUnknown` still says there is no token price; every other sku, and every image price, stays unknown until `catalog.ImageEndpointPrices` is asked for one model."
  - "Was true: `ResolveVisionModel` took the first model that accepted image input. Is true now: it must also answer in text alone, because an image editor takes pictures and returns one. An explicit vision model is still used as written."
---
