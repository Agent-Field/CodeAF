---
kind: fixed
title: a conversation you put away with ctrl+e leaves home on the keystroke, not a few seconds later
pr: 1039
surface: [chat]
invalidates:
  - "`tui3.Options.Archive` was wired to a bare `client.Archive`
    (cmd/aforge's [hostOptions]) — a round trip that landed the write on the
    engine's disk and told the surface's own cache nothing. Home's `ctrl+e`
    writes and then re-reads the world ON THE SPOT, expecting the put-away row
    to be gone from the rebuild, but that re-read goes through [hostWorld.world],
    whose first law is that it ANSWERS FROM WHAT IS HELD and starts a refresh
    behind itself. So the rebuild redrew the row exactly as it was, and the row
    only went when a later beat's fetch had returned AND a later beat rebuilt:
    [hostWorldEvery]'s two seconds of allowed staleness plus up to two of
    tui3's own three-second [homeEvery] beats. The seam is [hostWorld.archive]
    now — the write, then the held copy corrected and the entry aged out, which
    is [hostStanding.save]'s correction applied to the one write home makes
    against the world. A refused write still changes nothing here."
  - "This was NEVER only a `--host` fault, which is the part worth remembering:
    an ordinary local `aforge` has taken the engine road since
    cmd/aforge's [v3TakeHostRoad] stopped requiring a host that was already
    answering, so `chatv3_local.go` builds its surface through the same
    [hostOptions] and `localDoors` overrides neither `World` nor `Archive`. A
    fault in the far-machine caching layer was reaching every launch on the
    laptop. Anything that reasons about the places seam as remote-only is wrong."
---

The put-away was always correct on disk and instant; what lagged was the screen,
which is the worse of the two to meet — `put away · type its name to find it
again` printed under a row still sitting there, because `h.say` does not go
through the world and the panel does.
