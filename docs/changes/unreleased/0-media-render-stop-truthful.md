---
kind: fixed
title: stopping a video or music render now either saves nothing or says the file was saved
pr: 0
surface: [engine]
invalidates:
  - "Was true: a render the person stopped could still write its file if the provider answered a moment later, `jobs kill` always said `no video was saved` even then, any number of renders could run at once (each holding up to 512 MB), a render that failed listed as `finished`, and a stop never said the provider was not told to cancel. Is true now: a stop accepted before the bytes are committed writes nothing; a stop that arrives after the render committed is answered `had already finished rendering and is being saved`, and its note still arrives; at most 3 renders run at once in the whole process (not per conversation), a place is held until the provider call actually returns even after a stop, and the next is refused with a reason before anything is spent; a failed render lists as `failed`; and a stop says the provider was not told to cancel, so whether it is charged is unknown. The file is written to a temporary name and renamed, so a crash leaves no partial video. Nothing retries a render."
---
