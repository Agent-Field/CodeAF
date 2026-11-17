---
kind: changed
title: A factory item's size says where it came from, and an issue's body length is no longer a size
pr: 1864
surface: [engine, chat]
invalidates:
  - "Was true: an issue arrived sized by its body's length (S up to 400 characters, M up to 2000, L beyond), and `size:`, `risky` and `when: large` matched that guess; the read's own size was discarded. Is true now: an issue has no size until a size label or a completed read; `size:`, `risky`, `when: large`, the shaping turn and the chat floor read one resolver: the label, else a pull request's measured lines, else the read."
  - "Was true: triage.size on the wire carried no source. Is true now: triage.size_from is `read`, `diff`, `chat` or absent; absent is unknown and is shown as nothing."
  - "Still true: only a size label starts a job. A read or diff size never starts, holds or orders one, and older items are not reread just to fill the size gap."
  - "Limit: items read before this change keep no shown size until their text changes or they are labelled; an unchanged older pull request stays blank until the forge reports a change; a pull request with zero changed lines has no size."
---

A size shown for a factory item now names its source. A size label (`size:S`,
`size/s`, XS to XL) wins. Without one, a pull request's measured changed lines
give S (50 or fewer), M (400 or fewer) or L, shown as `size M (diff)`. Without
that, the completed read's size is shown as `read says M`. A chat card's guess
and the length of an issue's body are not sizes and show nothing.

Items with no recorded source, including older unlabelled issues without a new measured diff, are unknown and draw nothing. Editing a title or body drops the read's
size until the next read. Only an explicit size label lets a job start on its
own; older items are not automatically reread just to fill the gap.

The manual's "how big is a job" section in `internal/manual/chat/factory.md`
says the same, with the limits.
