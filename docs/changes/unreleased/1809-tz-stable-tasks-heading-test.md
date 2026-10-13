---
kind: internal
title: the tasks-page test fixtures pin their clocks to the local zone, so they no longer fail east of UTC
pr: 1809
surface: [chat]
invalidates:
  - >-
    The tasks-page tests were believed to be deterministic and host-independent.
    They built their fixture clocks in a fixed -04:00 zone while the tasks window
    buckets days in the viewer's LOCAL zone (usageBucketStart, and the "THE DAY
    IS THE WRITER'S" law on SpendToday), so a date the tests assert — the chat
    manual's `since aug 11`, and the `shift+← aug 2 – aug 25 →` window control —
    shifted by a day once the foreign offset was read as local on a host far
    enough east: green on a UTC runner, red on a UTC+8 box (#1808). The
    affected fixture clocks now use time.Local, fixing the fixed-offset
    mismatch; a pre-existing bug at skipped-midnight DST transitions, such as
    America/Santiago on 2026-09-06, remains unresolved.
---

No product behavior changed: the window following the viewer's local calendar
day is deliberate and stays. The tests were host-dependent because each fixture
injected a fixed -04:00 clock and then asserted dates that only matched in the
author's timezone band — one shared cause across tallyrows_test.go,
tasksplace_test.go, tasksparked_test.go, tasksconversation_test.go and
taskstable_test.go, two of whose tests were already red on a UTC+8 box.
Verified identical green from Pacific/Pago_Pago (UTC-11) to Pacific/Kiritimati
(UTC+14).
