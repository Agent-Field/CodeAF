---
kind: fixed
title: the frame law sees a function that takes the surface as a parameter, and the two reads it found are gone
pr: 898
surface: [chat, build]
invalidates:
  - "The frame law in internal/tui3 stopped at a package function taking the surface as a parameter. `placeFrameWithBar(a, …)` was walked as a function but its `a.…` calls were dropped, so `app.View → app.placeDraw → placeFrameWithBar` ⊘ `a.composerRows → … → a.errandPlace → errandHomeDir → os.Getwd` was invisible to a law whose forbidden set already contained os.Getwd. Every parameter typed `*app` is now a second spelling of the surface (surfaceNames), and the planted shape in TestTheLawSeesEveryShapeOfIndirectionItClaims turns red if that branch is deleted."
  - "`.errandHomeDir` was believed to reach os.Getwd from the frame. It did, on every paint, through composerOpensAt. The fallback is deleted: the function answers `os.UserHomeDir()` and then `.`, so the frame makes no syscall there at all, and this is a deletion rather than an allowlist entry."
---
