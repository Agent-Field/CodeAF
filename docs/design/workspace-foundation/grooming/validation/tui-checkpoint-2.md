# TUI checkpoint 2 — compact, sanitized evidence

Durable summary of what was checked for [BUILD-TUI-02.md](../BUILD-TUI-02.md), so a fresh
agent does not depend on `/tmp`. It holds no model reasoning, no transcript bodies, no
secrets and no raw JSONL. The full working logs stay on Spark as reference only:
`/tmp/af-pai-exp-logs/cp2-{models,diagnosis,accept1,accept2,demo2,tests}` and profiles
`/tmp/af-pai-cp2-{explore1,explore2,accept1,accept2}`.

## Jobs, revisions and what each phase proved

| Phase | Fleet job | Revision | Proof |
| --- | --- | --- | --- |
| Implementation, first exploration (DeepSeek, Haiku) | `20260914-203238-000447` | `0ac146768`-dirty | Exploration only; Haiku never qualifies (C28) |
| Model diagnosis, fixture pinning, review 1, acceptance 1–2, review 2, push `b777ed109` | `20260914-211634-000450` | see rows below | — |
| Source commit | 450 | `9687f15c5` | Focused tests; independent Opus review 1 (no data-corrupting defect, 11 findings) |
| Acceptance 1 (setup only, then stopped for fixes) | 450 | binary `5417b4b71` (source = `9687f15c5`) | Setup through chat PASS; retained, not the accepting run |
| Review-1 fixes | 450 | `061ffdc2f` | Focused tests |
| Acceptance 2 — setup through chat | 450 | binary `061ffdc2f` | Setup PASS (below) |
| Acceptance fix (named-file warning on inputs) | 450 | `9d9beebc3` | Deterministic test |
| Acceptance 2 — inspect, run, pause, resume, edit, stop | 450 | binary `9d9beebc3` | All PASS (below) |
| Review-2 fixes (regression under `**` watches, lead stacking, wording) | 450 | `92ba84022` | Deterministic regression tests + re-read of the accepted, stopped record through the real engine with no model call. **Not** a live acceptance rerun |
| Docs reconciliation with the journey checklist | `20260914-220626-000455` | merge of `b99b784f7` | Docs only; runtime diff against `92ba84022` empty; `make build` receipt in BUILD-TUI-02 |

No single binary ran the whole acceptance: setup ran on `061ffdc2f` and the remaining steps
on `9d9beebc3`. Between them only `session.StandingNamedFiles` changed, which only the
inspector reads; the setup path (`stand`, the card, placement) is identical.

## Models (C28)

Permitted in the fixture profile (`config.json` written by `scripts/demo-personal.sh`):
`model.talk` and worker/mastermind tiers `z-ai/glm-5.3-flash`; `models.fallbacks`
`deepseek/deepseek-v4.1-flash`; reflex `mistralai/mistral-nemo`; low
`deepseek/deepseek-v4-flash-0731`; high and `vision_model` `qwen/qwen3.8-27b`.

Checked 2026-09-14T21:17Z against the live OpenRouter catalog and Hugging Face API:

| ID | HF repo | Licence tag | Tools |
| --- | --- | --- | --- |
| `z-ai/glm-5.3-flash` | `zai-org/GLM-5.3-Flash` | mit | all 27 listed endpoints |
| `deepseek/deepseek-v4.1-flash` | `deepseek-ai/DeepSeek-V4.1-Flash` | mit | yes |
| `deepseek/deepseek-v4-flash-0731` | `deepseek-ai/DeepSeek-V4-Flash-0731` | mit | yes |
| `qwen/qwen3.8-27b` | `Qwen/Qwen3.8-27B` | apache-2.0 | yes |
| `mistralai/mistral-nemo` | `mistralai/Mistral-Nemo-Instruct-2407` | apache-2.0 | yes |
| `z-ai/glm-5.3` (not used) | `zai-org/GLM-5.3` | other | — |

**Guarantee scope.** The acceptance profile's call log (`logs/calls.jsonl`) and usage
ledger (`v3/usage.jsonl`) contain only allowlisted IDs — checked by a script comparing
every requested model against the table above (`NON-OPEN MODELS: none`). That is a receipt
for this journey's calls, not a product-wide guarantee: no gate enforces a profile
allowlist, and image/speech/music/video making and listening/watching still resolve from
the catalog (some closed). Nothing in the journey invoked them.

Acceptance 2 calls by model (usage ledger, USD): `z-ai/glm-5.3-flash` chat 0.02351 and
standing runs 0.00237; `qwen/qwen3.8-27b` rules check 0.00212; `deepseek/deepseek-v4-flash-0731`
titles/captions 0.00306. Total **$0.03105**. Acceptance 1 total $0.01159. Exploration
≈ $0.14 (Haiku $0.108). Checkpoint-2 cumulative ≈ **$0.18**.

## DeepSeek exploration diagnosis (summary)

Requested `~deepseek/deepseek-v4-flash-latest` → resolved `deepseek/deepseek-v4-flash-0731`
(V4 Flash, not V4.1). Every setup turn carried 27 tools including `stand`; the standing
section of the prompt was rendered. `stand` was never called in 30+ calls; the same folders
were listed 5–6 times; told to propose, it called `propose_task` for an older fixture line.
Classified as a model limitation. Method: allowlist projection of user messages, tool names
and arguments, tool results and call metadata only.

## Acceptance 2 checks (tmux on Spark, 200×52, isolated profile, serial, no retries)

| Check | Observed |
| --- | --- |
| Before yes, nothing stands | `aforge standing list`: only the fixture rule |
| Card inherits the folder and rule | `folder · Product, where this conversation is placed — its rules reach every run`; `rule · (fixture) Product reports never quote customer contact details.` ([card](tui02-screen-setup-card.txt)) |
| After yes | item `60acae90a7652958` active |
| Inspector before any run | `state active`, `wakes when product/** changes`, three `checks` lines, no named-file warning ([screen](tui02-screen-inspect-before-run.txt)) |
| Change 1 + `aforge standing check` | `1 checked · 1 ran` (31 s); `last run … landed · v1`, `why modified product/spec.md`, `held to 1 kept · rule 659dac6d`, `on disk 385 bytes · put there by aforge … · sha 2254fc05`, selection unmoved ([screen](tui02-screen-inspect-after-run1.txt)); report contains no `@` or `555` |
| `p` pause, change 2, check | store `paused`; `nothing was due`; runs `{000001}` ([screen](tui02-screen-inspect-paused-after-change.txt)) |
| `p` start again, check | `going again`; `1 checked · 1 ran`; run `000002` `modified product/spec.md`, 611 bytes, report reflects change 2 ([screen](tui02-screen-inspect-after-resume-run2.txt)) |
| `e` edit in its chat | box `Change the ongoing work “product spec digest”: `; `stand op: edit` on the same id, report unchanged; edit card ([card](tui02-screen-edit-card.txt)); yes → `specRevision 2` |
| `s` stop, change 3, check | `stopped`, strip offers no `p`/`s`/`e`; `nothing was due`; `aforge standing resume` → `a stopped item must be set up afresh` ([screen](tui02-screen-inspect-stopped.txt)) |
| Final binary re-read | `92ba84022` shows the same stopped record, `calls.jsonl` 46 → 46 ([screen](tui02-screen-final-binary-92ba84022-reread.txt)) |

## Setup latency: 7m42s of interactive journey, decomposed

From the call log and the driver timeline (21:42:10Z sentence sent → 21:49:52Z turn ended):

| Part | Seconds | Source |
| --- | --- | --- |
| Chat model streaming, 6 turn calls | ≈ 447 | `ms` per call |
| · of which one stalled stream cut at 2m30s and re-asked by the harness (Sail Research) | 160 | `the reply ran past 2m30s without finishing and was cut` |
| · of which one slow first call (Reka) | 139 | |
| · of which time to first token on two calls | ≈ 43 | `ttft_ms` 21535, 21767 |
| Approval and card answers by the driver (allow, allow, yes) | ≈ 20 | ask visible → key sent: 7 s, 8 s, 5 s |
| Tool execution (`ls` ×2, `stand` ×2 local) | < 20 | result timestamps |

So the elapsed setup was provider streaming latency and one stalled endpoint, not the
operator; one `stand` refusal (`rails.per_run_usd 0.05 came with no cost_words`) cost one
extra model round.

## Builds and checks

- `make build` on Spark: `5417b4b71`, `061ffdc2f`, `9d9beebc3`, `92ba84022` all exit 0.
- Focused, Spark: `go vet` touched packages, `gofmt`, `make test-laws`, `internal/iconlaw`,
  untagged `internal/e2e`, `internal/manual`, `internal/standing`, `internal/workspaceview`,
  `internal/tui3 -run 'Folder|Ongoing|Collection|Manual|Draft|Standing|Home'`,
  `internal/session -run 'Standing|Manual|Belt'`, `cmd/aforge -run 'Standing|Organization|Collection|Host'`,
  personal fixture test — pass on `92ba84022`.
- `cmd/aforge-demo-home`: 12 failures (`deps-weekly … {{evidence}}`), identical at base
  `0ac146768`, not in `.github/known-red.txt`.
- Not run: broad tui3/cmd suites, tagged E2E (C21).
- Coordinator integration builds (docs/runtime-unchanged, not acceptance): Fleet448
  `20260914-203328-000448` at `0ac146768`, ended 20:33:32Z; Fleet454
  `20260914-214321-000454` at `b99b784f7`, ended 2026-09-14T21:43:25Z.

## Reviews

- Opus review 1 of `9687f15c5`: status writes cannot write stale specs back; 11 findings,
  all addressed in `061ffdc2f`; finding 6 (inputs warned as rival reports) reappeared live
  and was fixed in `9d9beebc3`.
- Opus review 2 of `9687f15c5..9d9beebc3`: 9 fixed, 3 partial, 1 new regression (under a
  `**` watch every relative name was hidden, silencing real rival reports). All resolved in
  `92ba84022`: only a bare name is placed inside the watched folder; `e` replaces an older
  lead; adopted wording; `checked` dot; manual; STAGE check order; comments.

## Demo 2 launch (verified from the laptop form, no message sent)

[tui02-launch-command.txt](tui02-launch-command.txt). The key is read at launch from the
Spark account's `~/.aforge/config.json`; the verification checked only that
`OPENROUTER_API_KEY` reached both the TUI and its engine with an `sk-or-` shape, and that no
first-run setup appeared. No model call was logged; the engine was stopped afterwards.
