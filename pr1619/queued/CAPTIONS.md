# Focused PR #1619: supplier order through hold, cancellation and restart

Tested source: **25893534bfa9d9077e50b3d23c563f072b9a156c**, built 2026-09-27 13:01 on Spark. Binary SHA-256 `f2446383a7a768420a12a7dc5b9ad5f0d1e16b8d50f94486c1521ceaf9e730f3`. This is the focused restart/cancel/admission candidate, distinct from the earlier broad PR #1604 integration. Independent source review found its required recovery, live profile governor, joined lifecycle and queued-stop dependencies retained; unrelated auth, daily-spend, team and standing changes were excluded.

## Useful goal and observed result

Using the existing tested inventory CLI, prepare a supplier order. The first request used target 10; the person corrected it to target 15 before execution. The final CSV contains `Bolts,12,3` and `Nuts,7,8`, excludes returned/quarantined stock and is usable in the checkout. All **24 existing tests** pass independently. Original inventory, report, old supplier CSV, application and test files remain byte-identical.

1. Set an external memory admission hold after starting the engine and before task 1 was created. It remained queued with durable exact folder/brief and no working copy. Cancelled it through ordinary chat; persisted stopped/aborted, no files changed.
2. Created corrected task 2 while held. Opened `/new`; that independent conversation wrote and committed only `SUPPLIER_NOTE.md` in the same folder while task 2 stayed queued. The note commit is also the eventual task copy's home revision. This proves the held task did not block another conversation editing the folder.
3. Stopped only this fixture's frontend and engine, reopened the original transcript with the same binary, and verified task 2 returned queued with pending admission while task 1 remained stopped. Released the external memory setting to zero without another restart. Task 2 completed.
4. The checkout is on protected `main`, so the application intentionally kept the completed task branch instead of auto-merging it. An initial checkout-file check therefore failed. Chat correctly explained that location; a natural follow-up explicitly requested the three completed supplier files in the checkout. The app brought them in, ran the CLI with a separate verification report and passed all 24 tests. Independent verification confirmed the CSV, unchanged original files and retained note. The supplied source archive contains the usable result, without Git history, runtime data or credentials.

## Recording captions

- **held.png**: task 1 stays queued after a hold written before creation.
- **cancelled.png**: natural target correction stops the queued task. The model's generic “branch is kept” sentence is inaccurate for this never-started task; durable evidence confirms no copy/changes and merge=aborted.
- **second-conversation.png**: separate conversation commits the supplier note while the first conversation still has queued work.
- **restored-held.png**: original conversation after restarting the fixture; corrected task is queued and cancelled task remains stopped.
- **handoff.png**: completed supplier files now in the checkout, actual CSV rows and passing tests, previous artifacts preserved.
- **hold-cancel-second-conversation.mp4** / **workflow.cast**: first real interaction segment, 120.33 seconds of captured frames.
- **restart-release-handoff.mp4** / **resumed.cast**: resumed queued state, external release, task completion and explicit usable-file handoff, 228.33 seconds of captured frames.

Both videos are encoded from actual VHS terminal capture frames at 3 fps, with one blank pixel of padding for the encoder. Source casts retain native terminal geometry and real event times. The frontend restart closed the first recording's tmux attachment, so the two segments are deliberately separate; no fabricated continuity or synthetic terminal frames. Screenshots were visually inspected. The fixture engine/frontend were stopped after verification; project and evidence remain.

## Model receipt and limits

All **53 completed transport calls** and **52 usage rows** name exactly `deepseek/deepseek-v4.1-flash`; all completed transport statuses are 200. The profile pinned all text roles/fallbacks and the live UI used OpenRouter with `--one-model`. Counts are reported separately: one worker transport receipt has no separate usage row; this is not a billing parity assertion. There were no substitutions to another model.

This live sequence covers ordinary held-task admission/cancellation, queued restart recovery, external admission release and another conversation editing the same folder. It does not claim every issue #1554 recovery variant, senior-dev program replay or unrelated broad PR #1604 behavior is verified. It does not claim protected-main auto-landing. Agent1562 supplies independent active-copy restart acceptance.

Follow-up accounting audit: the missing worker usage amount is the first paid empty-at-ceiling attempt, $0.0005994 (718 input / 320 completion tokens). Its retry is recorded, but the first attempt was omitted. This separate pre-existing provider/session accounting bug is tracked in [issue #1624](https://github.com/Agent-Field/CodeAF/issues/1624). This recording proves the recovery workflow and model choice, not a corrected billing total.
