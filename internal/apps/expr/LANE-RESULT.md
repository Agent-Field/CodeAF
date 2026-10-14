# H2 Builder expressions and Check repairs

Based on held H1 commit `8c6f9b60efcb30506eb08692caa8dfab881abac9` after the
requested fetch and rebase. Held locally on `apps-hold/h2-builder-expr`; no push
or pull request. H1 and H2 are intended to land together.

## Change

`internal/apps/expr` owns the CEL environment, safe literal/reference rewrite,
source-position diagnostics, clock bindings and bounded programs. Its exported
names are `NewEnv`, `Compile`, `Program`, `Activation`, and `Diagnostic`. It
imports neither `svc`, `records` nor `check`; a structural test guards this.
Both Check and records call it. No draft package exists on this held base;
worker drafts can use these exports without depending on either service.

Integer `7d`, `24h`, `30m`, `2w` tokens work outside strings and comments;
`days(n)` supports numeric day counts. `now`, `now()` and `$now` work. Declared
date/datetime fields receive timestamp semantics without changing stored JSON
or converting date-looking text fields. Bare `now` marks computed rules as
clock-dependent, preserving timed recomputation.

The first-round exit was `Sample.Generate` returning an error from Check;
Builder's `checkRound` returned it before `Loop.CheckResult` could select FIX.
Check now reports seam failures with repair hints. Builder also converts an
error or an empty failing report from any Check implementation into a repair
issue, preserving the existing three Check rounds / two FIX calls. Cancellation
still exits immediately. Computed issues name their JSON field path, original
expression, source position and a corrected example. Evaluation errors name
position 1:1 when CEL does not supply a location.

The contract, shared WRITE/FIX expression section and compiled chat manual
explain the grammar. Other prompt wording was shortened to preserve the
existing 16000-character static WRITE limit.

## Deterministic proof and gates

- PASS: unit tables for each duration form; escaped, raw and triple-quoted
  strings, comments, identifier boundaries and overflow; `days(n)`; original
  source positions including multiple lines and Unicode.
- PASS: Check and records evaluate `now - 7d`, all four literals and `days(7)`;
  date and datetime inputs; preserved text; clock dependency detection.
- PASS: real Check sample and render failures enter FIX and pass round 2;
  service errors also enter FIX; persistent errors consume all three rounds.
  A computed compilation failure reaches the model's FIX prompt with field,
  expression, source position, corrected example and full files.
- PASS: `make build` (the tested server is `bin/codeaf`).
- PASS: `go build ./... && go vet ./internal/apps/... && go test ./internal/apps/... ./internal/config/...`.
- PASS: chat manual retrieval/vocabulary tests, naming law, prompt budget and
  `git diff --check`.

## Served zoo proof

`~/af-w2/accel/zoo/zoo pick 2` selected `farm-csa-shares` and
`theatre-production-crew`. The H1 inventory paragraph was added as
`inventory-roastery-h1` to a local copy of the zoo metadata so `zoo record`
could also record this extra regression without changing the shared catalog.

All three paragraphs went through the real desk UI, real bridge, and
`bin/codeaf app serve` with OpenRouter `deepseek/deepseek-v4.1-flash` for every
role. The machine's existing credential was supplied without printing it.
`APPS_E2E_BUILDER=1` was set. The desk's `scripts/apps-e2e.sh` was copied into
this worktree's `.tmp/`: its launcher served this candidate binary instead of
fetching and building the older `origin/apps/v1`, and Vite used the desk root.
The test opened New app, submitted the paragraph, approved Write, waited for
automatic repairs, then clicked Keep. It never clicked Fix it.

Every reached stage below was recorded with `zoo record ... --path served
--run <id>`. Use and agent UI stages were not exercised. These are Builder,
Preview and Keep receipts; asynchronously pending behaviour checks are not
proof of completed scheduled automation.

| App | Plan | Write | Check | Preview | Keep | Done | Final round | Cost |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| farm-csa-shares | PASS | PASS | PASS | PASS | PASS | PASS | 2 | $0.042772 |
| theatre-production-crew | PASS | PASS | PASS | PASS | PASS | PASS | 3 | $0.055732 |
| inventory-roastery-h1 | PASS | PASS | PASS | PASS | PASS | PASS | 2 | $0.017935 |

Run ids:

- `h2-farm-csa-shares-1791939781305`: round 1 schema failure on a stray `gap`
  property; FIX repaired it and round 2 rendered four screens. Behaviour was
  skipped/pending; final `left_out: []`.
- `h2-theatre-production-crew-1791939853189`: round 1 behaviour failure
  (`model reply missing call_time`), then round 2 schema/reference failures;
  two FIX calls led to round 3 and five rendered screens. Final behaviour was
  pending; final `left_out: []`.
- `h2-inventory-roastery-h1-1791939961307`: round 1 kit/render failure on a
  generated relative date query; FIX repaired it and round 2 rendered three
  screens. Final report names run `low-stock`; final `left_out: []`.

The required `TestLiveServeInventoryRoastery` also passed against the final
binary through the real bridge: Check round 2, state `kept`, version 1, six
files, 35.32 seconds, $0.020260. Its first round's date-query failure also
reached FIX automatically. An earlier bridge run passed round 1 in 28.14
seconds at $0.018837.

Local logs: `.tmp/h2-final-gate.log`, `.tmp/h2-build.log`,
`.tmp/live-inventory-h2.log`, `.tmp/served-zoo.log`, and
`.tmp/served-events.jsonl`. The served Playwright run reports **3 passed**.
The `.tmp/` directory predates this lane and is not staged.

## Remaining seam

Relative date query tokens such as `roast_date<-7d` are rejected by the query
package; the live model repaired them through FIX. This lane changes CEL
expressions, not the separate query grammar. A query lane would own any new
relative-date token support. The current WRITE few-shot still contains an older
relative-date query example; it is a separate fixture correction, not evidence
that such tokens execute.
