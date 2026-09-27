# Inventory supplier export: late admission and restart acceptance

Exact tested combined candidate: `221e7e866` (binary SHA256 in receipts.json), containing the final PR #1604 admission fix. The earlier inventory quarantine/negative-input repair happened on `41cf9cd0f`; this recording continues that useful project on the newer build. It does not attribute the earlier repair to the newer recording.

Goal: give a parts-store supplier a usable reorder CSV. A natural correction changed the requested columns from item/quantity to item/available/needed, requiring cancellation of the first queued attempt. The corrected extension was completed, run and independently tested.

1. After starting the new engine, the isolated profile's minimum-free-memory setting was raised externally before creating task4. The lazily created admission gate honored that setting: task4 stayed queued and project file SHA256s matched the baseline.
2. The user cancelled task4 to correct the CSV format. It ended stopped, with no startedAt, no changed files and no pendingRun; project SHA256s were still unchanged. The chat says its branch was kept, but persisted evidence shows no work began—do not interpret the model's branch wording as proof a worktree was created.
3. Corrected task5 was queued under the same hold. Only this fixture's frontend and engine were terminated and restarted. Task5 restored queued with its pending request intact, and task4 stayed stopped.
4. The profile minimum was set to0 externally without another restart. Task5 began and completed in59.377seconds. The final supplier artifact was actually run and read: item,available,needed followed by Nuts,7,3 only. Available stock19, returned4; quarantined Clips5 and returned Washers4 were excluded from the export.
5. Independent `python3 -m unittest -v` passed24tests. A separate actual CLI run reproduced the CSV, and negative input failed without changing the last good report bytes.

All48new completed OpenRouter calls and48usage rows used `deepseek/deepseek-v4.1-flash`; completed statuses were200. Receipts exclude previous-session calls and contain no key/profile data.

Screenshots are unmodified real terminal frames, visually inspected for readability:
- held-before-creation.png: task4 queued after the externally changed setting was honored at admission-gate creation.
- cancelled.png: natural cancellation and unchanged-workspace reply.
- restored-held.png: restarted conversation, task5 still queued.
- handoff.png: real final command, supplier CSV, totals and24passing tests.

workflow.mp4 is the finite recording of actual VHS text frames at3fps, with one blank edge pixel padded only for H.264 even-width encoding. No simulated UI or reconstructed terminal frames are used. workflow.cast is the original event stream and authoritative real timing; sampled video timing can differ slightly under host load. The tape inherits terminal geometry rather than forcing incompatible dimensions. Only owned recording encoders and the isolated fixture frontend/engine were stopped after verification; the project and evidence are preserved.

inventory-supplier-project.tar.gz contains finished source, input, tests, README and output only—no .git, API keys, profiles or runtime logs. lifecycle.json contains only the task state facts needed for cancellation/restart acceptance, not full private task journals.

UI vocabulary note: the restored rollup uses “tasks landed” for terminal task cards, including stopped/read-only cards, with their individual state icons and stopped text. Acceptance uses the persisted queued/stopped/done states in lifecycle.json; that rollup is not evidence every listed task successfully changed files.
