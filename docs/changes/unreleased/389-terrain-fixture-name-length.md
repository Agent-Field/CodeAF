---
kind: fixed
title: the terrain fixture asks for a name Linux can hold, and leaves the known-red ledger
pr: 389
surface: [engine, build]
invalidates:
  - "`internal/plan TestRenderTerrainStaysUnderTheCap` was recorded in `.github/known-red.txt` as environment-shaped — red on the Linux runner, green on a clean macOS tree. That reading was wrong: the fixture built a 541-byte path component, which ext4 and tmpfs refuse outright, so it could never have passed on Linux on any machine or under any `TMPDIR`. It is fixed and out of the ledger."
---

The fixture built each of fourteen directory names from ninety repeats of a
three-byte rune. APFS caps a path component at 255 characters and took it; ext4
and tmpfs cap it at 255 bytes and refused, so the test died in `mkdir` before
reaching an assertion. Forty repeats now — 241 bytes a name, 3374 across the
fourteen, still well past the 2048-byte cap, so the clipping the test exists for
is still exercised. `RenderTerrain` and `terrainClip` were always correct; what
the known-red line hid was a fixture bug wearing an operating system's clothes.
