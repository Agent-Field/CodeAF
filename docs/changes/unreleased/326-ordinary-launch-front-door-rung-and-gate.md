---
kind: fixed
title: the ordinary launch is shown its front door, its rung and its own gate
pr: 326
surface: [chat]
invalidates:
  - "A fresh install launched the ordinary way was shown the first-run and provider-connection screen — it was not, since openSetup returned on an empty profileDir, and AFORGE_PROFILE_DIR is unset on very nearly every launch. That guard shipped with the setup screen itself on 2026-08-24, so the screen had never once opened on an ordinary launch. It opens now, and a hosted (--host) window is the only one still asked nothing."
  - "The status line's absent YOLO segment meant the tool gate would ask before running anything — an absent segment is that claim, and it was not true. readApproval answered \"\" on an empty profileDir while cmd/aforge's v3Policy read the same profile, so a person with tools.approvalMode set to allow had the gate open, every tool running without asking, and nothing on the status line saying so, from 2026-08-15 until now. The segment reports the posture in force for the process's own profile."
  - "The `--yolo` flag raises the YOLO segment. It does not: the flag replaces the gate's default for that session without writing the profile row, so a --yolo run has the gate open and the segment empty. That is issue #325 and is not fixed here; the manual's YOLO row now says so."
  - "The install's effort rung was drawn on the card's facts line. It was drawn only where AFORGE_PROFILE_DIR was exported, because effortProfile read an empty profile directory as no profile; the `thinking high` clause is on the card for everybody now."
  - "internal/tui3's emptyprofile_test.go carries an allow-list of sites that may read an empty profile directory as no profile. The list is EMPTY: the go/ast test is now a guarantee over the whole package, and any new comparison against \"\" fails by name."
  - "internal/e2e's start() passed the suite's key through to every rig. It still does; startFresh is the new door for a fresh-install run, taking every *_API_KEY and AFORGE_PROFILE_DIR OUT of the environment, and emptyHome is a state root with nothing in it at all — newHome writes a config.json and cannot be used for that."
---

The three sites #320 named and left standing. Each decided more than a status
segment, and the sharpest of them was the front door: the product was unusable on
a fresh install, silently, for everybody without a key in their shell. The third
is a safety claim rather than a display bug and is written up as a disclosure on
the pull request.
