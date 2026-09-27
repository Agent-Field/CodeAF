# Live TUI acceptance: strict reimbursement export and safe import

Recorded on Spark from application source `18a6ec5f21386c1a2e1d5c6165b55a4804086c2d`. This is a real continuation of the expense-report conversation, using its existing journal and generated project. It is not a scripted model-response replay. The operator adapted follow-ups to the actual implementation and handoff. No harness wrote implementation results into the workspace during this run.

## Recording

- `full-session.cast`: original uninterrupted terminal recording, 254.792 seconds through the final frame, sampled at 4 Hz with original elapsed timing.
- `excerpt-180s.gif` and `excerpt-180s.cast`: contiguous 180-second excerpt, original interval **74.792–254.792 seconds**, normal speed. No time stretching or synthesized activity.
- `gif-timing-audit.json`: PIL verifies 221 frames totaling exactly 180,000 ms. Each frame delay matches its original cast interval within GIF centisecond quantization (under 11 ms). Renderer idle compression is disabled for this interval; the final 15.768-second real pause is explicitly preserved because the renderer otherwise ignores the empty cast endpoint. Corrected render Fleet job: `20260927-181803-001259-pr1607-gif-final-hold`.
- `timeline.json`: timestamped user messages, steering, keys, actual window resize, mouse click and checkpoints.
- `models.json`: safe request audit. All 41 completed new model requests used OpenRouter `deepseek/deepseek-v4.1-flash`; every recorded start/completion was checked, including auxiliary calls. Credentials and private model transcripts are excluded.
- `run-summary.json`: exact source, binary checksum, host, duration and model count.

The terminal was actually resized from 140×42 to 90×32 and restored. The recording retains a fixed 140×42 canvas, so the narrow interval occupies its real smaller dimensions with empty surrounding space.

## Genuine user goal and observed behavior

The existing October report had a valid subtotal but also a rejected row. The user asked for opt-in strict reporting so finance could not accidentally import a partial result. While the model was still working, the user added the practical constraint that a failed rerun must preserve the existing destination CSV. The model implemented `--strict`, an atomic `run_report.sh` wrapper, tests and handoff documentation.

The user then questioned whether an old preserved file could still be imported. The model initially gave a confusing pipeline as well as a correct conditional. The next natural follow-up requested only the tested conditional, preserving the exporter failure status. The model demonstrated both failure and success using a harmless local importer marker, corrected the handoff, and identified the mixed-month input as a test fixture rather than presenting it as a bank export.

`generated-project/` contains the actual resulting program, executable wrapper, fixtures, tests and final handoff. Independent verification completed successfully (Fleet job 001254, exit 0 at 17:53:36 UTC). All 39 tests passed. Separate consumer checks confirmed strict mode produces no partial stdout, default non-strict behavior remains unchanged, failed exports preserve the existing target, successful exports replace it, the importer runs only on success, failure status remains 2, and the original input checksum is unchanged. Full results are in `independent-verification.json`.

## UX observations

- A human-facing model update remained visible before the steering message; resumed tools did not erase either message. See `16-human-update-before-steering.gif` and `19-narrow-active-steering.gif`.
- Internal work remained collapsed by default. `ux-audit.json` found zero frames with raw `[update]`, the historical interruption marker, or expanded reading-task briefs.
- The original conversation was resumed and the historical interruption marker stayed folded: `13-history-marker-folded.gif`.
- The operator explicitly opened work with Ctrl+E, scrolled and clicked a nested test command, then closed it. Details in `29-explicit-tool-details.gif` are intentional disclosure; `32-details-closed-handoff-preserved.gif` shows the restored clean view.
- Completed task notifications were dismissed with `/dismiss`; the final tested human handoff remained visible in `36-final-tested-import-policy.gif`.

Snapshots are actual captured frames at or immediately before the recorded checkpoint, with no edited text. The broader four-scenario live suite and repository checks are reported separately by the coordinating task; this artifact alone does not certify every possible UX state.

## Provenance and scope

Live capture/build Fleet job: `20260927-172320-001252-pr1607-reviewed-acceptance`.
Independent generated-program verification Fleet job: `20260927-172939-001254-pr1607-strict-import-outcome`.
Application binary SHA-256: `c8befc8f669d358f7b099bda1ddd1a7718cfb529eb4a1d8c547069d9cd6c3af4`.

The combined acceptance job's recording and four live TUI scenarios passed. Its repository gate found a documentation search-heading regression, subsequently corrected in the docs-only follow-up `d606fa413`; the final gate subsequently passed on `561e1c5f1` after reusing the already-reviewed #1619/#1622 test teardown fixes. See [validation](../validation/README.md). A subsequent test-only follow-up `ed8fed2bc` corrected an old assertion for consumed steering with no replacement work. Application code is unchanged from the recorded revision. This recording is labeled with the exact application revision actually used, not the later documentation/test commits.

Earlier captures in adjacent evidence directories discovered bugs and are explicitly labeled as failure/discovery evidence. They are not substituted for this final clean run.
