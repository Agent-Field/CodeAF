# PR #1562: a maintenance helper built and used through the actual chat UI

**Candidate:** `41cf9cd0f`, Spark, composed of #1562, #1603, #1604 and the #1610 one-model fix. This is combined-candidate acceptance, not evidence that the original #1562 head alone contains every fix. Preflight records the executable hash and isolated profile/database bindings. Every one of the **87 recorded model calls** used OpenRouter `deepseek/deepseek-v4.1-flash`, including auxiliary calls. No fallback model occurred in this accepted workflow.

## What the person did

1. Asked for a useful offline home-maintenance CLI in a named project while the conversation started at home. The agent read project notes, tracked work in the explicit local PlanDB, built Python code, tests, README, and ran the sample (31 seconds).
2. Corrected the desired behavior to include in-progress chores and requested JSON output (18 seconds).
3. Started an actual `/task` to add safe exact-title completion. The worker and checker used the assigned project copy and finished in56seconds. The branch was retained, preserving the earlier uncommitted work.
4. Used `/land` and `/land now`; the UI honestly reported the merge could not be applied. A normal follow-up asked to resolve it preserving the new features, exclude the local database from Git, and actually mark `Clean dryer vent` done. The agent did so and ran25tests (77seconds).
5. Asked for a command usable from home. An unnecessary cache deletion was denied through the real approval card; the agent retried the read-only command and returned the correct JSON from `/home/santosh` (68seconds including the human approval wait).

The resulting CSV persists the dryer-vent chore as done. Text and JSON both show two remaining chores: the smoke-alarm batteries and in-progress front-step repainting. The generated helper and tests are included in `maintenance-helper/`. Independent verification passed all25tests and both CLI output modes; Git status was clean. All5local PlanDB tasks are done.

## Media and receipts

- `workflow-final.cast`: the complete original6minute2second native terminal source recording at160columns/45rows, containing build, correction, task, attempted landing, recovery, and actual use. Replay at its recorded geometry. The corresponding VHS render was excluded because the outer browser used a mismatched terminal size; its overlapping text was a capture problem, not fabricated UI evidence.
- `final-screen.png` and `final-screen.gif`: clean live capture of the completed artifact, tests and actual two-chore output.
- `use-from-home.mp4` and `use-from-home.cast`: clean live follow-up capture; video is61.6seconds of actual finite captured frames and includes the approval interaction. `home-result.png`, `.gif`, and `.cast` separately capture the completed home-directory result. The terminal continues between these segments; the short video alone does not claim to contain the entire build.
- Tapes record the capture commands. No frames were generated from invented output. The short MP4 was encoded from the captured PNG sequences with an explicit frame limit, avoiding the recorder's stalled unbounded background encoder.
- `model-receipts.json`, `preflight.json`, `local-plan.json`, `task-ground-receipts.json`, and `artifact-verification.json` supply model, isolation, folder, PlanDB and test evidence.

## Observed limits

The first landing attempt was not seamless because previous edits were uncommitted; the task branch was kept and the conversation resolved the actual project. The UI still displayed the retained-change hint after the agent manually integrated the code, even though Git was clean. This residual hint is visible rather than edited out. An earlier separate diagnostic take touched the installed external PlanDB's default database; it is excluded entirely from this publication, and this accepted run used a verified explicit fixture database before its first call.
