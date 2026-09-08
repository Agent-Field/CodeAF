---
kind: changed
title: live conversation work rolls through three compact step descriptions
pr: 653
surface: [chat, docs]
invalidates:
  - "A running conversation exposed reasoning and tool rows by default. It now shows recent caption descriptions in a three-row reading budget, with older lines dimmer and the current step shimmering; opening the work restores its detailed outline."
  - "The caption shimmer used a hard bright strip that restarted immediately. It now feathers a low-contrast highlight across whole character clusters, rests between sweeps, and stays static on terminals that cannot express a gentle colour change."
  - "The work disclosure key only targeted completed turns. It now opens or closes the running conversation's compact steps first, and each expanded caption retains its own tool-detail control."
  - "An expansion made during a turn could remain after completion. The default folded view now clears that live expansion when the turn settles, retaining the question and answer; the explicit ui.work=open preference remains available."
---

The newest caption is kept whole if it wraps beyond the three-row budget on a
narrow screen. Corrections, questions, answers, notices and failed steps retain
their place and existing controls. Task pages and node transcripts keep their
existing detail. No timer suffix, icon collection or background-job badge is
introduced by this change.
