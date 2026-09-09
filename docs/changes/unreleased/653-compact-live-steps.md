---
kind: changed
title: live conversation work rolls through three compact step descriptions
pr: 653
surface: [chat, docs]
invalidates:
  - "A running conversation exposed reasoning and tool rows by default. It now shows recent caption descriptions in a three-row reading budget, with older lines dimmer and the current step shimmering; opening the work restores its detailed outline."
  - "The caption shimmer was too faint and its speed depended on painted frames. A two-second sweep now follows elapsed time with a broad cosine feather reaching ordinary reading ink, keeps character clusters intact, and stays static on terminals that cannot express a gentle colour change."
  - "Between finished calls the compact view could stop moving, and hidden reasoning had no clickable door before a caption arrived. Before any caption exists, a Working door carries the shimmer and disclosure. Between completed steps, only a separate dot animates beside the latest readable caption; no extra Working row is added."
  - "The work disclosure key only targeted completed turns. It now opens or closes the running conversation's compact steps first, and each expanded caption retains its own tool-detail control."
  - "An expansion made during a turn could remain after completion. The default folded view now clears that live expansion when the turn settles, retaining the question and answer; the explicit ui.work=open preference remains available."
---

The reading budget is three wrapped rows, admitting whole captions newest first.
The newest caption stays whole even if it alone exceeds that budget, including
between calls. Corrections, questions, answers, notices and failed steps remain
visible with their controls.

Running task pages use the same compact display while retaining independent
settled-phase disclosures. A node transcript opened inside a run's page stays
detailed. Clicking the compact block or pressing ctrl+e restores the outline;
its captions expand their own tools. Completion clears live expansion, and
reopening keeps completed work folded by default. The explicit ui.work=open
preference still opens work.

The final display includes still action icons, an elapsed timer after ten seconds
of a running tool step, and a separate inline response-wait indicator. Their
categories, fallback behavior, clocks and persistence are described in
653-step-action-icons.md, 653-compact-step-time.md and 653-inline-wait.md. No
background-job badge or tool count is added to the compact step lines.
