---
kind: fixed
title: a generated image, speech, music or video reply over its size limit is refused instead of saved cut short
pr: 0
surface: [engine]
invalidates:
  - "Was true: the media client read a reply through a limit reader with no check past the limit, so a video over 512 MB or an image/speech reply over 128 MB came back as a successful, silently truncated file that would not play, and a music stream with no message separator grew its buffer without end. Is true now: a reply one byte over its limit (or declaring a longer length up front) is an error naming the limit, no file is written, and a music event or the clip it adds up to is bounded by the same ceiling."
---
