---
kind: fixed
title: map origin metadata stays off display entries and resumed turn working sets
pr: 0
surface: [engine]
invalidates:
  - "Was true: V87b stored JournalLine on every DisplayEntry and retained a physical-line map in the timestamp index, growing entries from 304 to 312 bytes and making resumed scroll-back differ from the live fold. Is true now: physical origins are private, sparse metadata on display records only; DisplayEntry and the live timestamp index have their original size and resumed agents build no origin map."
---

The graph resolves physical lines through Record.JournalLine without changing its
wire fields. Record-cache leases share the immutable origin vector; its full
allocated capacity is charged to the existing cache budget. No budget was raised.
The focused entry-size, long-turn fold, origin and record-cache tests cover the
boundary.
