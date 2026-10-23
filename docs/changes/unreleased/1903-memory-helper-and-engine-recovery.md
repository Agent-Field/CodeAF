---
kind: fixed
title: recover unavailable memory helpers and explain background engine upgrades
pr: 1903
surface: [chat, engine]
invalidates:
  - >-
    An unavailable memory helper and unavailable small-work fallback could end
    automatic capture. Recovery now also tries the current chat model within
    the same 45-second budget, skips unavailable models for that chat, and
    discloses a validated substitution without rewriting model preferences.
  - >-
    Running engines could keep model settings changed by another process until
    restart. Helper resolution now refreshes after profile or project settings
    change, so the next call uses the saved configuration.
  - >-
    Reopening an updated window does not necessarily update its background
    engine. Startup guidance explains closing all workspace windows after work
    finishes or explicitly stopping that workspace; /status shows both builds
    and follows the engine after reconnection.
---

Busy engines keep their work. Targeted restart instructions use the installed
executable and quote paths correctly, with an explicit interruption warning.
Authentication, payment, quota and policy refusals do not advance the recovery
chain. Background retries remain bounded to the open chat's lifetime.
