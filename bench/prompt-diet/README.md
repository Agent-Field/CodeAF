# The prompt-diet parity bench

What this measures: whether a build with a smaller prefix **does the same things**
as the build before it, and how many prompt tokens per turn it pays to do them.

The owner's acceptance for the whole wave, in their own words: *"real e2e with
dev and our branch, same other changes, just this diff alone, and see if we are
on parity but efficient."* That is two claims, and they are decided separately:

| claim | decided by | what would sink it |
| --- | --- | --- |
| **parity** | every subtest and every cell ends equal or better | one subtest that passed on `dev` and fails on the diet |
| **efficiency** | median prompt tokens per **turn** | a page that got shorter while the conversation got longer |

`docs/design/prompt-diet/BENCH.md` is the report: the recipes, the baseline
table, what is not covered and why. This file says what the scripts are.

**It spends real money and is on demand.** Nothing here is wired into `make
check` and nothing here may be. Layer A is free and is worth running while
editing; layers B, C and D call a real model.

## The four layers

| layer | what it is | costs | proves |
| --- | --- | --- | --- |
| **A** prefix | `TestTheFixedPrefixStaysUnderItsBudget`, run with `-v` and its one logged line read back | nothing | the static bill, exactly: page bytes and tool-block bytes |
| **B** suites | `TestTUIE2E`, `TestQuestionsE2E`, `TestStandingE2E` — the real binary, a real terminal, a real model | ~20 min, cents | **parity**, which is what these are for |
| **C** cells | `bench/conversation` scenarios and `bench/e2e` cells, unchanged, handed this build's binary | minutes, cents | outcome and cost on ordinary work |
| **D** wire | the per-request ledger, rolled up out of what layers B and C already wrote | nothing extra | **efficiency**: tokens per turn |

Layer D stands up no new proxy. `bench/conversation/lib/guard.py` is already a
loopback forwarder every cell runs in front of OpenRouter — handed to the
harness as `AFORGE_BASE_URL` with a sentinel key — and it already writes an
admitted row carrying the request's shape and a settled row carrying what the
provider said it charged. `internal/calllog` is aforge's own always-on record
and, with `AFORGE_CALL_LOG_BODIES=1`, is the only place the **tool block's own
bytes** can be counted per request. `lib/wire.py` normalises both into one
JSONL, keeping `source` on every row so the two are never added together.

## The rig is this checkout; the subject is the branch

`run.sh` builds `<branch>` in a worktree of its own and points every bench
script **from this checkout** at that binary. That is the only arrangement that
can compare two branches at all: `dev` at `6aa6a946e` has never heard of
`bench/prompt-diet`, so a run using the subject's own rig would have nothing to
run on the baseline side — and a rig that moved with the subject would be
comparing two harnesses rather than two prompts.

## Running it

Everything runs on the Spark. `~/.local/bin/go`, the key in
`~/.config/fleet/secrets.env`, and `tmux` are all there; the laptop has the
first two and the owner's standing order says the suites do not run on it.

```sh
bench/prompt-diet/run.sh 6aa6a946e dev --layers a         # free, seconds
bench/prompt-diet/run.sh 6aa6a946e dev                    # A + C + D
bench/prompt-diet/run.sh 6aa6a946e dev --layers a,b,c,d   # everything, ~40 min
bench/prompt-diet/run.sh prompt-diet/integrate diet --layers a,b,c,d
bench/prompt-diet/compare.py dev diet
```

`compare.py` exits non-zero when an outcome got worse, so it is usable as a
gate. Evidence lands in `bench/prompt-diet/out/<label>/`, which is gitignored:

```
out/<label>/meta.json                       label, revision, model, layers, clock
out/<label>/prefix.log                      layer A, whole
out/<label>/suites/<suite>.log              layer B, whole — the screens are too wide to pipe
out/<label>/suites/outcomes.json            one word per subtest
out/<label>/cells/conversation/…            bench/conversation's own evidence tree
out/<label>/cells/e2e.csv                   bench/e2e's append-only row per cell
out/<label>/wire.jsonl                      one normalised row per request
out/<label>/summary.md                      one page about this run alone
```

### Knobs

| flag | default | meaning |
| --- | --- | --- |
| `--layers` | `a,c,d` | which layers to run |
| `--model` | `deepseek/deepseek-v4.1-flash` | the catalog id every cell is pinned to |
| `--scenarios` | `research-brief,code-fix,followup-while-working,work-result-recalled` | `bench/conversation` scenarios, or `none` |
| `--cells` | `lookup,bundle3` | `bench/e2e` cells, or `none` |
| `--suites` | `TestTUIE2E,TestQuestionsE2E,TestStandingE2E` | layer B, or `none` |
| `--allowlist` | — | extra ids the run may legitimately have billed (a fallback model) |
| `--out` | `out/<label>` | where evidence lands |
| `--keep-worktree` | off | leave the build for a follow-up run |
| `--dry-run` | off | compose everything, spend nothing |

**`TestTUIE2E` pins its own model** — `e2eModel` in
`internal/e2e/harness_test.go` is the constant `deepseek/deepseek-v4-flash`, not
this bench's `--model`. That is fine and deliberate: both sides of a comparison
run the same constant, so the comparison is fair. It does mean layer B and layer
C are not on the same model, and the tables say which is which.

## The ablation

`ablate.sh <branch> <unit-id>` renders the page with one law unit removed and
runs the cell set without it. `units.tsv` is the list and says, for each unit,
where it lives and whether a substring test pins it. **It is prepared, not run**
— BENCH.md §5 says what it would cost and what it could and could not settle.

## Files

| file | what it is |
| --- | --- |
| `run.sh` | one branch's whole measurement, four layers |
| `compare.py` | two labels side by side, and the parity/efficiency ruling |
| `ablate.sh` | one law unit removed, then the cells |
| `units.tsv` | the law units, largest first, with their key sentences |
| `lib/outcomes.py` | `go test -v` logs → one word per subtest |
| `lib/wire.py` | the guard's ledger and the call log → one row per request |
| `lib/summary.py` | one page about one run |
