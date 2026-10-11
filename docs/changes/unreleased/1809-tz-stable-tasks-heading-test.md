---
kind: internal
title: the tasks-heading manual test pins its clock to the local zone, so it no longer fails east of UTC
pr: 1809
surface: [chat]
invalidates:
  - >-
    TestTheManualQuotesBothTasksOpeningHeadingsExactly was believed to be a
    deterministic, host-independent test. It built its fixture clock in a fixed
    -04:00 zone while the tasks window buckets days in the viewer's LOCAL zone
    (usageBucketStart, and the "THE DAY IS THE WRITER'S" law on SpendToday), so
    the rendered `since aug 11` the chat manual quotes rendered as `aug 12` on a
    host far enough east that the foreign offset crossed midnight — green on a
    UTC runner, red on a UTC+8 box (#1808). The fixture clock is now built in
    time.Local, so the test reads the same date under every $TZ.
---

No product behavior changed: the window following the viewer's local calendar
day is deliberate and stays. Only the test was host-dependent, because it
injected a fixed -04:00 clock and then asserted a manual string that only
matched in the author's timezone band. Verified identical green from
Pacific/Pago_Pago (UTC-11) to Pacific/Kiritimati (UTC+14).
