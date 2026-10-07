# Verification

All builds, full suites and acceptance runs used Spark. No full suite or model call ran on the laptop. Parallel review and implementation used the native subagents; no design or review was delegated to an external model.

## Revisions and evidence

- Baseline screenshots: `4fd9cfaa0fb63b1c78db5aa6ebe9479e512d1640`.
- Initial screenshot source: `24d8d436f5003fd8a8a0eec098943df0d130802c`.
- Full `make pr-ready` passed on `ec65c91ccc3f2ba87f189c4842abb9f4672f7538`, Spark job `20261007-185348-001853-settings-final-proof`. Build, vet, packed/manual checks, 139 layout/law files across 30 packages, and complete affected packages (`cmd/codeaf`, `internal/config`, `internal/manual`, `internal/tui3`) passed. All selected package tests passed on their first run.
- After that run, the final editor adjustment moved validation beside the input and explained empty optional counts. Its rendered-frame, preservation, valid-save, reopen, and clear tests passed at both 80 and 120 columns: `20261007-185636-001854`.
- Everyday task controls were then placed first, with task analysis and shell timing under Advanced. Focused settings/configuration tests and all 67 final captures passed in `20261007-190236-001856`. Both widths verify the task order, adjacent validation, valid save/reopen and blank clear/reopen.
- The complete source plus gallery at `569cf41e6368e847ad28ba29063cf8551fb8815b` also passed `make pr-ready` in `20261007-190655-001857-settings-pr1792-acceptance`, with every selected test passing on its first run.
- The branch was subsequently rebased onto dev `6246d7154` (new built-in review/security programs). The sole conflict was manual wording; both the new program badges and Activity terminology were retained. Settings rendering and presentation source are identical to the screenshot revision.
- The final full acceptance result, including the gallery commit and its exact tested revision, is recorded in [draft PR #1792](https://github.com/Agent-Field/CodeAF/pull/1792). This file is written before that run to keep its source frozen during testing.

Raw logs and screenshot provenance remain on Spark under `/home/santosh/settings-redesign-evidence-20261007/`; fleet logs retain each job's command and exit status.

## Examples checked

| Example | Evidence |
| --- | --- |
| Open an old profile with memory off, hints off, a task limit, team preferences and an unknown future field | Config integration tests verify unchanged bytes on read and preserve other fields on a setting write |
| Find a moved setting by its old name or config key | Global search tests and before/after old-label captures |
| Find an account while browsing another category | Global catalog search and asynchronous account-refresh regression tests |
| Inspect memories without pretending to change a preference | Real Memory frame and action-hint test; Enter opens the existing memory view |
| Use the keyboard or hover a row for the same explanation | Stable geometry, shared focused-help and pointer target tests; hover capture |
| Enter `invalid` for concurrent tasks | Deliberate error fixture; rejection is adjacent to the draft and the stored limit stays unchanged |
| Correct the draft to `4`, save, close settings, and reopen | Real CLI capture asserts the persisted JSON and shows the reopened value |
| Clear the task limit | Real CLI capture asserts persisted zero and shows the reopened no-limit state |
| Edit a credential | Masked value/caret tests; modal pointer input cannot activate controls behind the editor |
| Choose local/free/OCR document reading | Four-choice real launch-boundary test verifies the preference reaches the session configuration |
| Use a task cap and the default task model | Spark `20261007-185331-001852-settings-task-wiring` passes task-slot, model fallback, machine admission and queuing checks |
| Use plain/narrow terminals | Navigation laws preserve active labels, More, hit targets and text-only navigation; 80-column real frames accompany all category captures |

## Failure analysis retained

The first complete run caught stale assumptions about categories, labels and Advanced disclosure, plus real defects: omitted custom-provider controls, account refresh lost while searching another category, lost Advanced state on returning from the crew panel, and a legacy practice command pointing at a hidden row. These were corrected while retaining behavior assertions.

Credit-warning tests failed identically on the base and changed revisions when host credentials overrode their fixture. Both passed with those environment keys unset. The fixture now isolates its own credentials; all 18 credit tests passed even with deliberately conflicting dummy environment keys (`20261007-183854-001835-credits-fixture-fix`). Runtime credential precedence is unchanged.

## Limits

This verifies the settings surface, persistence and named runtime boundaries. It does not claim live end-to-end coverage of every external provider, OAuth service or billing account. Screenshots use dummy data and a loopback endpoint. Settings captured at process startup still require restarting already-open CLI chats, as their help states. The shared resident registry and CLI configuration vocabulary remain compatible; this is a redesign of the live chat surface.

The PR remains a draft for owner review. These changes are not installed or merged by this task.

## Interaction refinement

Source revision: `746af81a7b9af9ba3839d8674dacb508678d6f13`.
Final clean build and refreshed real CLI screenshots: Spark job
`20261007-202643-001862-settings-interaction-final`.
An earlier successful capture (`001861`) prompted one final correction: an editor
opened through global search highlights its setting's category while preserving
the search on return.

Focused Spark checks cover explicit choice preview/cancel/save/reopen, mouse
save/cancel, unchanged invalid drafts, successful-save receipts, pending remote
Close wording, 24-column controls and short terminals, approval-gate updates,
masked editors, and paste/wheel isolation. The real CLI capture sends terminal
mouse events to Save and Cancel and asserts the stored profile values.

Full acceptance for the final source and refreshed gallery is recorded in the
PR with its exact revision and fleet job; no local full suite was run.

## Chat settings integration

Chat tools now use the presentation metadata and shared choice labels. Focused
tool tests verify new and legacy search names, exact accepted values, real
registry writes/readback, persisted hints/work/memory values, protected/invalid
write preservation, hidden controls and restart/project-override receipts.

Spark `20261007-204140-001865-settings-live-ui` passed the live interface refresh
regressions and related choice/hints/word tests. Full acceptance for the final
revision, including the affected session package, is recorded in the draft PR.
These are direct tool and runtime-boundary tests, not a claim that a live model
conversation was used for this validation.

Chat-written routing, speed guard and pinned-host preferences explicitly recommend
a restart to ensure activation in already-open chats. This wording is specific
to the tool path; the panel retains its live transport update hooks.
