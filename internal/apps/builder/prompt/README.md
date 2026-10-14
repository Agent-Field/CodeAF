# Builder prompt pack

Pure logic, no I/O. The wiring lane calls the model and the server; this package renders prompts and judges replies.

1. `Render(kind, Inputs)` fills one of three embedded templates (`templates/*.tmpl`): **plan**, **write**, **fix**. Missing inputs are an error naming them.
2. **plan**: paragraph + space + budget + kit catalog → `plan.json` (Data · Automation · Screens · Agent · Needs). Lines are short; ids are stable and become the record, run and screen file names. `needs` is the literal `"computed"`; codeaf fills it.
3. **write**: `plan.json` + catalog + file schemas → one JSON object `{path: content}` (`app.json`, `screens/*.json`, `runs/*.yaml`, `agent.md`). JSON files may be given as objects, yaml and md as strings.
4. **fix**: full current files + check report with fix hints + the shared run rules → the changed files only, same shape.
5. Every prompt carries the ten rules (`templates/rules.tmpl`) and demands strict JSON with no fence or prose.
6. The few-shot is the CRM of board 08, kept as real files in `example/`. A test proves it parses with `ParseFiles` and `ParsePlan`.
7. Size: the fixed text of WRITE is about 10k characters (~2.5k tokens), tested under 16k, so the catalog and schemas fit inside the 12k-token target for deepseek/deepseek-v4.1-flash.
8. `ParseFiles(output)` prefers an object under `files`, then the first object whose keys are all valid app paths (`app.json`, `plan.json`, `agent.md`, `screens/<id>.json`, `runs/<id>.yaml`). It skips unrelated objects past prose and code fences. The selected object's contents must still parse as JSON or YAML where required. If no object qualifies, `FilesObjectError` names the observed keys and asks for a path-to-content map; the Builder's repair loop sends that correction back with the rejected reply for one retry.
9. `ParsePlan(output)` does the same for a plan and checks ids (lowercase, unique per group) and non-empty lines.
10. `ApplyPlanEdits(plan, edits)` applies plan-card edits (`set`, `add`, `remove` on data, automation, screens; `set` on agent) to a copy. One bad edit fails the batch and changes nothing. Needs cannot be edited.
11. `Loop` is the policy: plan → write → check → fix → check … The caller reports each step (`PlanWritten`, `FilesWritten`, `CheckResult`, `FixApplied`) and reads `Phase()`.
12. A passing check ends in `preview`. A failing check goes to `fix` up to `MaxFixRounds` (3) times.
13. The fourth failure ends in `check_failed`; `LastReport()` holds that report for the Fix it · Cancel card.
14. Out-of-order steps are errors, so a wiring bug cannot silently skip a check.
15. Catalog and schemas are plain text inputs; when `internal/apps/spec` lands, pass its embedded files in.

Runs use `$params.name` for typed values and `{{params.name}}` in text. Gate and action share one step. `templates/runs.txt` supplies the same rule text and example to WRITE and FIX. Agent tools are derived from the app capabilities: readable records get query/get and writable records also get create/update/delete.
