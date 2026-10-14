# Builder plan line length

Merged `origin/apps-hold/h2-builder-expr` before the change. No branch was
pushed; the runner owns integration into `apps/v1`.

PLAN now asks for at most 120 characters per line: one short clause, with
detail in the files. FIX receives explicit file/JSON path, limit and current
length hints for any schema maxLength failure. Before every Check, Builder
shortens overlong data, automation and screen summaries against the embedded
plan schema's limit. It tries semicolon, em dash, comma, ` and `, then a word
boundary, counts Unicode characters and adds no ellipsis. A single unbroken
word falls back to a Unicode character boundary. Each change is saved and
published as a plan build event with its path and before/after lengths.

WRITE uses the full plan before shortening; the original ask, ids and other
JSON fields survive. Unrelated schema failures still reach Check. The manual
documents the new behaviour in the same change.

## Local proof

- `make build`: passed; the served proof used this candidate's `bin/codeaf`.
- `go build ./... && go vet ./internal/apps/... && go test ./internal/apps/...`:
  passed. Changed Builder and prompt suites ran fresh.
- Focused tests: 259- and 215-character lines shortened at a clause boundary
  in all three capped plan groups, logged and saved before the real Check
  service passes. Tests also cover Unicode, delimiter preference, word
  boundaries, idempotence and preservation of unrelated errors.
- FIX prompt tests cover plan lines and non-plan schema maxLength errors,
  asserting exact path, limit and current length.
- PLAN instruction and existing static prompt budget tests passed.
- `make test-focus PKGS=./internal/manual RUN=Chat`: passed.
- `git diff --check`: passed.

## Served proof

Real desk UI, real `codeaf app serve`, OpenRouter
`deepseek/deepseek-v4.1-flash` for Builder and the app agent.
`APPS_E2E_BUILDER=1` was set. The desk's `scripts/apps-e2e.sh` was copied into
a disposable proof harness and pointed at this candidate binary rather than
building `origin/apps/v1`. The five paragraphs are preserved in `apps.json`.
The UI drove Create, Write, Preview and Keep; the WebSocket event stream supplied
rounds and spend. Playwright: **5 passed (11.9 minutes)**.

| App | Stage reached | Check rounds | Builder cost USD | Safety net fired |
| --- | --- | ---: | ---: | --- |
| P1: YouTuber sponsor deals | Preview, then Keep | 1 | 0.046866638 | No |
| P2: freelance developer clients | Preview, then Keep | 3 | 0.073702698 | No |
| P3: enterprise vendor approvals | Preview, then Keep | 3 | 0.097539876 | No |
| Zoo: multi-approval-purchase | Preview, then Keep | 2 | 0.066271476 | No |
| Zoo: yt-sponsor-deals | Preview, then Keep | 2 | 0.036402510 | No |

Total Builder spend: **$0.320783198**. P3 needed two desk “Fix it” retries
after WRITE response-shape failures, before reaching Check. Its cost includes
those attempts. P3's approver-chasing run was left out after its final behaviour
failure; this is a Preview pass with that omission. P2's automations were repaired
on round 3, and its final result has no omissions. No live build hit a plan-line
length failure; the deterministic cases explicitly exercise the safety net.

Plan, Write, Check, Preview and Keep were recorded with `zoo record --path
served` for every app. P1/P2/P3 use the existing `yt-sponsor-deals`,
`dev-freelance-clients` and `ent-vendor-onboarding` catalog ids respectively;
their exact task paragraphs remain in each receipt. The two picks are the
literal output of `zoo pick 2`.

Run ids:

- `h3-plan-lines-P1-20261013`
- `h3-plan-lines-P2-20261013`
- `h3-plan-lines-P3-20261013`
- `h3-plan-lines-pick-multi-approval-purchase-20261013`
- `h3-plan-lines-pick-yt-sponsor-deals-20261013`

Each complete kept app folder, including `definition/app.json`, `plan.json`,
screens, runs, agent, `sample.db` and `records.db`, is copied under
`~/af-w2/accel/zoo/kept/<run-id>/`. Each run also has build events, screenshots
and a receipt. The shared proof directory
`~/af-w2/accel/zoo/kept/h3-plan-lines-proof-20261013/` holds paragraphs, harness,
receipts and gate/live logs. The named `rules-bin.md` was not present on this
host; the task's served-path, model, recording and kept-file requirements were
applied directly.
