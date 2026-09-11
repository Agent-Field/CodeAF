---
kind: fixed
title: the model catalog cache belongs to the base it was fetched from
pr: 759
surface: [chat, engine]
invalidates:
  - "There was one model catalog cache, `<state root>/model-catalog.json`, shared by every base URL. There is one per base now: the default base keeps that file name, any other base gets `model-catalog-<16 hex of sha256 of the normalised base>.json`, and a cache stamped for one base is refused for another."
  - "A cold or offline machine got `catalog.hardcodedFallbacks`'s eleven rows whatever `AFORGE_BASE_URL` said, so `generate_image`, `speak`, `generate_music` and `generate_video` were on the belt for any vendor. Those rows are OpenRouter's and are served only for the default base; any other base with no usable cache and a failed fetch has an empty catalog and none of the four verbs."
  - "`Config.ResolveMusicModel` fell back to `google/lyria-3-clip-preview` when the catalog advertised no music model. It now returns nothing, the way `ResolveImageModel`, `ResolveSpeechModel` and `ResolveVideoModel` already did."
  - "The default base URL was spelled in `internal/config`. It is `catalog.DefaultBaseURL` now, and `config.DefaultBaseURL` reads it — the package that ships the built-in rows owns the constant."
  - "`PERF.md`'s launch-path pin said the whole launch asks sixteen blocking catalog questions. It says eleven: the pinned fixture's refusing endpoint is a custom base, whose catalog is now correctly empty, so the five media hands are never armed and the figure no longer covers the reads a catalog that does advertise those models pays."
---

Nothing changes for a person on the default base — same picker, same prices, same startup,
no extra network round trip, and a `model-catalog.json` written by an older build is still
read as the default base's cache, so the upgrade costs nobody a cold refetch.
