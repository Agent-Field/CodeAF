# REPLAY-2: a DIGEST mark reader, off-policy

Generated 2026-08-25 03:34 | 120 completions | temp 0.0 | 3 samples per cell x cut x model

**What this tests.** In live wave 1e the mastermind mark reader read the RAW transcript at rounds 10/20/40 of the four-issue batch cell (57k / 65k / 91k tokens) and answered CONTINUE all three times, at $0.62. Here the same question is asked of a deterministic DIGEST instead: the person's ask verbatim, one line per tool call with NO results, the last thing said (600 chars), and the files touched - bounded to ~6k tokens.

Answers are parsed exactly as `internal/session/checkpoint.go` parses them: first non-empty line, stripped of `` ` ``/`*`/`_`, split on `|` at bracket depth 0; **>= 2 top-level parts -> SPLIT**; a chain, a fork nested behind a step, or `(done)` -> CONTINUE. AMBIG is recorded when the first line carries no arrow/bar grammar at all (checkpoint.go fails those open to CONTINUE; both readings are given below).

Ground truth: `batch` -> SPLIT at rounds 10 and 20 (four independent issues). `20`/`21`/`22`/`23` -> CONTINUE at every cut. The batch cell at round 40 is **recorded, not scored** - by then a short chain is an honest answer.

Skipped cut points (transcript never reaches them): `aforge-final-crew-20-s1` @10, `aforge-final-crew-20-s1` @20, `aforge-final-crew-20-s1` @40, `aforge-final-crew-22-s1` @40, `aforge-final-crew-23-s1` @40, `aforge-final-flash-20-s1` @10, `aforge-final-flash-20-s1` @20, `aforge-final-flash-20-s1` @40, `aforge-final-flash-22-s1` @40, `aforge-final-flash-23-s1` @40.

## Score table

| model | cut | trap acc (20/21/22/23 -> CONTINUE) | batch decision (truth SPLIT) | ambig | mean digest tokens | mean $/read |
|---|---|---|---|---|---|---|
| kimi-k3 | 10 | 1.00 (18/18) | CONTINUE x4/SPLIT x2 | 0.00 | 876 | $0.01244 |
| kimi-k3 | 20 | 1.00 (18/18) | CONTINUE x6 | 0.00 | 998 | $0.01102 |
| kimi-k3 | 40 | 1.00 (6/6) | CONTINUE x6 *(unscored)* | 0.00 | 1,591 | $0.01210 |
| deepseek-v4-flash | 10 | 1.00 (18/18) | CONTINUE x6 | 0.00 | 876 | $0.00033 |
| deepseek-v4-flash | 20 | 0.89 (16/18) | CONTINUE x6 | 0.08 | 998 | $0.00035 |
| deepseek-v4-flash | 40 | 1.00 (6/6) | CONTINUE x6 *(unscored)* | 0.00 | 1,591 | $0.00048 |

### Combined over the scored cuts (rounds 10 and 20)

| model | trap acc | batch -> SPLIT | ambig | mean $/read |
|---|---|---|---|---|
| kimi-k3 | 1.00 (36/36) | 0.17 (2/12) | 0.00 | $0.01173 |
| deepseek-v4-flash | 0.94 (34/36) | 0.00 (0/12) | 0.04 | $0.00034 |

## Per cell x cut decision distribution

| cell | cut | truth | kimi-k3 | deepseek-v4-flash |
|---|---|---|---|---|
| aforge-final-crew-21-s1 | 10 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-21-s1 | 20 | CONTINUE | CONTINUEx3 | AMBIGx1/CONTINUEx2 |
| aforge-final-crew-21-s1 | 40 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-22-s1 | 10 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-22-s1 | 20 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-23-s1 | 10 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-23-s1 | 20 | CONTINUE | CONTINUEx3 | AMBIGx1/CONTINUEx2 |
| aforge-final-crew-batch-s1 | 10 | SPLIT | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-batch-s1 | 20 | SPLIT | CONTINUEx3 | CONTINUEx3 |
| aforge-final-crew-batch-s1 | 40 | (either) | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-21-s1 | 10 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-21-s1 | 20 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-21-s1 | 40 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-22-s1 | 10 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-22-s1 | 20 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-23-s1 | 10 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-23-s1 | 20 | CONTINUE | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-batch-s1 | 10 | SPLIT | CONTINUEx1/SPLITx2 | CONTINUEx3 |
| aforge-final-flash-batch-s1 | 20 | SPLIT | CONTINUEx3 | CONTINUEx3 |
| aforge-final-flash-batch-s1 | 40 | (either) | CONTINUEx3 | CONTINUEx3 |

## The digest the reader sees: `aforge-final-crew-batch-s1` at round 10 (1655 est. tokens, 6,620 chars)

````
WHAT WAS ASKED

Here are four issues to fix in this repository.

## Issue #20: Validation workflow

**Is your feature request related to a problem? Please describe.**

There is currently no automated validation to ensure that unit tests pass before a pull request is merged into the `main` branch. This can allow regressions or failing tests to be merged.

**Describe the solution you'd like**

Add a GitHub Actions workflow that automatically runs the project's unit test suite whenever a pull request targeting the `main` branch is opened, synchronized, or reopened. The workflow should fail if any test fails, preventing the PR from being merged until all tests pass.

**Describe alternatives you've considered**

Running the unit tests manually before merging. This approach is error-prone and depends on contributors consistently executing the test suite.

**Additional context**

The workflow should be configured as a required status check for pull requests targeting the `main` branch.

## Issue #21: Normalization of arithmetic operations

## Is your feature request related to a problem? Please describe.

The current text normalization pipeline expands numbers, dates, times, and measurements, but arithmetic expressions are left unchanged. As a result, TTS may produce unnatural pronunciations for expressions such as `2+2`, `5×3`, `10/2`, or `8-5`.

## Describe the solution you'd like

Extend the text normalizer to recognize and normalize arithmetic expressions by converting operators into their spoken Bambara equivalents (e.g. `+`, `-`, `×`, `÷`, `=`).

The implementation should follow the existing normalization principles:
- Only rewrite arithmetic expressions.
- Preserve punctuation, casing, and surrounding text.
- Fail open and return the original text if normalization cannot be performed.

## Describe alternatives you've considered

Leaving arithmetic expressions unchanged and relying on the TTS model to infer their pronunciation. While this works in some cases, the output is often inconsistent and less natural.

## Additional context

This feature would complement the existing support for numerals, dates, times, and measurements, making mathematical and educational content sound more natural.

## Issue #22: Currency normalization

## Is your feature request related to a problem? Please describe.

The current text normalization pipeline expands numbers, dates, times, and measurements, but currency amounts remain unchanged. As a result, TTS may produce inconsistent pronunciations for values such as `5 000 FCFA`, `$20`, `€15.50`, or `£100`.

## Describe the solution you'd like

Extend the text normalizer to recognize currency amounts and expand them into their spoken Bambara form while preserving the associated currency unit.

The implementation should:
- Support common currency symbols and currency codes.
- Rewrite only the currency span.
- Preserve punctuation, casing, and surrounding text.
- Fail open and return the original text if normalization cannot be performed.

## Describe alternatives you've considered

Leaving currency amounts unchanged and relying on the TTS model to pronounce them correctly. This leads to inconsistent behavior across different currencies and numeric formats.

## Additional context

This feature would complement the existing normalization support for numerals, dates, times, and measurements, improving the quality of speech synthesis for financial and transactional content.

## Issue #23: Make CLI dependencies optional

## Is your feature request related to a problem? Please describe?

The CLI currently ships as part of the default installation. As a result, installing `bambara-text-normalizer` also installs CLI-specific dependencies such as `typer` and `rich`, even when users only want to use the library as a Python package.

For applications that only import the normalization APIs, these dependencies increase the installation size and dependency footprint without providing any benefit.

## Describe the solution you'd like

Make the CLI an optional dependency by leveraging Python extras.

For example:

```bash
# Core library only
pip install bambara-text-normalizer

# Install with CLI support
pip install "bambara-text-normalizer[cli]"

# Install everything
pip install "bambara-text-normalizer[all]"
```

The base installation should only include the dependencies required for the Python library. CLI-specific dependencies (`typer`, `rich`, etc.) should be installed only when the `cli` extra is requested.

## Describe alternatives you've considered

Keep the current installation model where the CLI is always included. While simpler to maintain, it forces every user to install dependencies they may never use.

Another alternative is to publish the CLI as a separate package, but using optional extras provides a simpler user experience while keeping everything under a single distribution.

## Additional context

This follows a common packaging pattern used by many Python projects, where optional functionality is exposed through extras (e.g. `dev`, `docs`, `cli`, `all`) while keeping the default installation as lightweight as possible.

Work in this repository. Implement the changes and make the existing test suite pass. Do not weaken or delete tests to make them pass.

WHAT HAS BEEN DONE (33 tool calls, results not shown)

ls: .
read: pyproject.toml
ls: src
ls: tests
ls: .github
ls: src/bambara_normalizer
ls: .github/workflows
read: src/bambara_normalizer/__init__.py
read: src/bambara_normalizer/normalizer.py
read: src/bambara_normalizer/spans.py
read: src/bambara_normalizer/cli.py
read: src/bambara_normalizer/numbers.py
read: src/bambara_normalizer/config.py
read: src/bambara_normalizer/utils.py
read: src/bambara_normalizer/numeric.py
read: src/bambara_normalizer/dates.py
read: src/bambara_normalizer/times.py
read: src/bambara_normalizer/measurements.py
read: tests/test_normalizer.py
read: tests/test_cli.py
read: tests/test_numeric.py
read: tests/test_numbers.py
read: tests/test_dates.py
read: tests/test_times.py
read: tests/test_measurements.py
read: .github/workflows/code-check.yml
read: tests/conftest.py
read: tests/test_contractions.py
read: tests/test_disambiguation.py
read: tests/test_evaluation.py
read: tests/test_utilities.py
write: .github/workflows/validation.yml
write: src/bambara_normalizer/arithmetic.py

LAST THING SAID

Now let me create the arithmetic module. I need to understand the Bambara words for arithmetic operators first, then integrate with the span system.

FILES WRITTEN OR EDITED

- .github/workflows/validation.yml
- src/bambara_normalizer/arithmetic.py
````

## Verbatim answers

**batch @10, kimi** - `aforge-final-crew-batch-s1` -> `CONTINUE` (1 top-level parts)

> (A | B | C) > D
> 
> A: finish arithmetic normalization — wire `arithmetic.py` into the span-detection/normalizer pipeline (and package exports) so expressions like `2+2` are rewritten; B: implement currency normalization (new module plus the same pipeline integration) for amounts like `5 000 FCFA`, `$20`, `€15.50`; C: make CLI dependencies optional in `pyproject.toml` by moving `typer`/`rich` into a `cli` extra and adding an `all` extra; D: run the full test suite (and the CLI import behavior) and fix any failures without weakening tests.

**batch @10, flash** - `aforge-final-crew-batch-s1` -> `CONTINUE` (1 top-level parts)

> (R | C | L) > T  
> R: integrate arithmetic normalization into the normalizer and add tests; C: implement currency normalization (module, integration, tests); L: make CLI dependencies optional via extras; T: run the full test suite and fix any failures.

**batch @20, kimi** - `aforge-final-flash-batch-s1` -> `CONTINUE` (1 top-level parts)

> A > (B | C | D) > E
> 
> A is finishing the in-progress `cli.py` edit (indenting the remaining code under the `else:` block so typer/rich imports are optional, completing Issue #23 alongside the pyproject.toml extras); B is adding the GitHub Actions workflow that runs unit tests on pull requests to `main` (Issue #20); C is implementing arithmetic-expression normalization in the normalizer (Issue #21); D is implementing currency-amount normalization (Issue #22); E is running the full test suite to confirm everything passes without weakening tests.

**trap @20, kimi** - `aforge-final-crew-21-s1` -> `CONTINUE` (1 top-level parts)

> A > (B | C) > D
> 
> A is running the full existing test suite to see whether the edits broke anything; B is fixing any regressions in the touched files (spans/config/numeric/normalizer/__init__) if failures appear; C is smoke-testing the new arithmetic normalizer directly (e.g. `2+2`, `5×3`, `10/2`, `8-5`, plus fail-open cases) since no tests cover it yet; D is a final full-suite pass confirming everything is green.

**batch @40, kimi** - `aforge-final-crew-batch-s1` -> `CONTINUE` (1 top-level parts)

> A > B
> 
> A: Fix the arithmetic normalizer so date-like spans such as `45-13-2025` are not rewritten as subtraction — either by making the date pattern fail to match invalid dates at regex time (instead of matching then returning None) or by guarding the arithmetic pattern against date-like contexts. B: Run the full test suite to confirm all tests pass across the four issues (validation workflow, arithmetic, currency, optional CLI extras).

## The bracket rule is what loses the batch, not the digest

`checkpoint.go` counts parts at the TOP level of the whole line, so `(A | B | C) > D` - three jobs that can all start now, joined by a test run at the end - counts as **one** part and reads CONTINUE. That is the shape the readers actually write for the batch cell. The diagnostic column below re-reads each shape by its FIRST STAGE instead: take the segment before the first top-level `>`, strip one enclosing bracket, count inside. `A > (B | C)` still reads CONTINUE (one job stands in front), which is the property the strict rule was adopted for.

| model | cut | trap acc (top-level) | batch (top-level) | trap acc (first-stage) | batch (first-stage) |
|---|---|---|---|---|---|
| kimi-k3 | 10 | 18/18 | CONTINUE x4/SPLIT x2 | 15/18 | SPLIT x6 |
| kimi-k3 | 20 | 18/18 | CONTINUE x6 | 18/18 | CONTINUE x4/SPLIT x2 |
| kimi-k3 | 40 | 6/6 | CONTINUE x6 | 6/6 | CONTINUE x3/SPLIT x3 |
| deepseek-v4-flash | 10 | 18/18 | CONTINUE x6 | 18/18 | CONTINUE x5/SPLIT x1 |
| deepseek-v4-flash | 20 | 16/18 | CONTINUE x6 | 16/18 | CONTINUE x6 |
| deepseek-v4-flash | 40 | 6/6 | CONTINUE x6 | 5/6 | CONTINUE x6 |

### What the first-stage reading costs on the traps

Every trap read where the two rules disagree - the strict rule says CONTINUE (right) and the first-stage rule says SPLIT (wrong):

| cell | cut | model | sample | shape |
|---|---|---|---|---|
| aforge-final-crew-21-s1 | 40 | deepseek-v4-flash | 0 | `(A \| B) > C` |
| aforge-final-crew-23-s1 | 10 | kimi-k3 | 0 | `(A \| B) > C > D` |
| aforge-final-flash-22-s1 | 10 | kimi-k3 | 0 | `(A \| B) > C` |
| aforge-final-flash-23-s1 | 10 | kimi-k3 | 2 | `(A \| B) > C > D` |

## Every batch shape line

| cell | cut | model | sample | shape | top-level parts | decision | first-stage reading |
|---|---|---|---|---|---|---|---|
| crew | 10 | deepseek-v4-flash | 0 | `(R \| C \| L) > T` | 1 | CONTINUE | SPLIT |
| crew | 10 | deepseek-v4-flash | 1 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 10 | deepseek-v4-flash | 2 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 10 | kimi-k3 | 0 | `(A \| B \| C) > D` | 1 | CONTINUE | SPLIT |
| crew | 10 | kimi-k3 | 1 | `(A \| B \| C) > D` | 1 | CONTINUE | SPLIT |
| crew | 10 | kimi-k3 | 2 | `(A \| B \| C) > D` | 1 | CONTINUE | SPLIT |
| flash | 10 | deepseek-v4-flash | 0 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 10 | deepseek-v4-flash | 1 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 10 | deepseek-v4-flash | 2 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 10 | kimi-k3 | 0 | `A \| B > C \| D > E \| F > G` | 4 | SPLIT | SPLIT |
| flash | 10 | kimi-k3 | 1 | `(A \| B \| C \| D) > E` | 1 | CONTINUE | SPLIT |
| flash | 10 | kimi-k3 | 2 | `A \| (B > C) \| (D > E) \| F > G` | 4 | SPLIT | SPLIT |
| crew | 20 | deepseek-v4-flash | 0 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 20 | deepseek-v4-flash | 1 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 20 | deepseek-v4-flash | 2 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 20 | kimi-k3 | 0 | `(A \| B) > C` | 1 | CONTINUE | SPLIT |
| crew | 20 | kimi-k3 | 1 | `(A \| B) > C` | 1 | CONTINUE | SPLIT |
| crew | 20 | kimi-k3 | 2 | `A > B` | 1 | CONTINUE | CONTINUE |
| flash | 20 | deepseek-v4-flash | 0 | `(validation workflow) > (arithmetic normalization \| currency normalization \| CLI optional deps)` | 1 | CONTINUE | CONTINUE |
| flash | 20 | deepseek-v4-flash | 1 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 20 | deepseek-v4-flash | 2 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 20 | kimi-k3 | 0 | `A > (B \| C \| D) > E` | 1 | CONTINUE | CONTINUE |
| flash | 20 | kimi-k3 | 1 | `A > (B \| C \| D) > E` | 1 | CONTINUE | CONTINUE |
| flash | 20 | kimi-k3 | 2 | `A > (B \| C \| D) > E` | 1 | CONTINUE | CONTINUE |
| crew | 40 | deepseek-v4-flash | 0 | `A > B` | 1 | CONTINUE | CONTINUE |
| crew | 40 | deepseek-v4-flash | 1 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 40 | deepseek-v4-flash | 2 | `(done)` | 0 | CONTINUE | CONTINUE |
| crew | 40 | kimi-k3 | 0 | `A > B` | 1 | CONTINUE | CONTINUE |
| crew | 40 | kimi-k3 | 1 | `A > B` | 1 | CONTINUE | CONTINUE |
| crew | 40 | kimi-k3 | 2 | `A > B` | 1 | CONTINUE | CONTINUE |
| flash | 40 | deepseek-v4-flash | 0 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 40 | deepseek-v4-flash | 1 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 40 | deepseek-v4-flash | 2 | `(done)` | 0 | CONTINUE | CONTINUE |
| flash | 40 | kimi-k3 | 0 | `(A \| B) > C` | 1 | CONTINUE | SPLIT |
| flash | 40 | kimi-k3 | 1 | `(A \| B) > C` | 1 | CONTINUE | SPLIT |
| flash | 40 | kimi-k3 | 2 | `(A \| B) > C` | 1 | CONTINUE | SPLIT |

## deepseek-v4-flash's failure mode: it declares the batch finished

On the batch cell the low-tier reader does not draw a narrow shape - it writes `(done)`, on a digest whose FILES list shows two of the four issues barely started. This is not a parser problem and no rule change recovers it.

`aforge-final-crew-batch-s1` @20 sample 0:

> (done)

`(done)` on the batch cell: **deepseek-v4-flash 15/18** reads, kimi-k3 0/18.

## Determinism at temp 0

Shape wording drifts at temp 0 on both models; the DECISION taken off the shape is far steadier, which is the only thing the harness reads.

| model | identical shape line 3/3 | identical decision 3/3 |
|---|---|---|
| kimi-k3 | 5/20 | 19/20 |
| deepseek-v4-flash | 9/20 | 18/20 |

## Spend

- total on the 120 scored reads: **$0.7303** (140,490 prompt tokens, 45,289 completion tokens)
- deepseek/deepseek-v4-flash: $0.0220 over 60 reads ($0.00037 per read)
- moonshotai/kimi-k3: $0.7083 over 60 reads ($0.01180 per read)
- roughly $0.31 more went on probes and two runs that wedged before the deadlock in this script's own progress line was found (their completed reads were resumed, not repaid). Lane total under $1.05 against the $5 cap.

For scale: the live wave-1e reader spent **$0.62 on three raw reads** of one cell. Sixty kimi reads of the digest - every cell, every cut, three samples - cost $0.71.

## Recommendation

**The digest fixes the reading, and `checkpoint.go`'s parser then throws the fix away.** Live, on the raw 57k/65k/91k-token transcript, the reader answered CONTINUE on the batch three times out of three and cost $0.62. On the digest - 876 tokens at round 10, 998 at round 20, 1,591 at round 40 - kimi-k3 draws the fork in **6 of 6** batch reads at round 10 (`(A | B | C) > D`, `(A | B | C | D) > E`, `A | B > C | D > E | F > G`), and its legends name the right four issues. It also never once loses a trap: **36/36** on cells 20/21/22/23 across both cut points, with zero ambiguity. The whole sweep of 120 reads cost $0.73, and one digest read costs **$0.0118** against roughly $0.207 for one live raw read - about an 18x saving with a context 60x smaller. So the digest shape and the question are right and should ship. What must ship WITH them is a change to how the first line is counted: `topLevelParts` reads `(A | B | C) > D` as one part, and that one shape is what the reader writes for a genuinely four-way batch, so the strict rule turns a correct reading into a CONTINUE and the batch recall stays at 2/12. Counting the FIRST STAGE instead - the segment before the first top-level `>`, with one enclosing bracket stripped - takes kimi's batch recall to 6/6 at round 10 and 8/12 over both scored cuts while keeping `A > (B | C)` a CONTINUE, and it costs only 4 trap reads out of 72, every one of them a real two-way `(A | B) > C`. The cheaper alternative, if the parser must stay as it is, is to change the ASK: its own example line `'A > (B | C)'` is what teaches the bracketed form, so dropping that example and saying plainly that parts which can start now go at the top level with no brackets should move the same reads without touching the counting rule. **The reader cannot drop to the low tier.** deepseek-v4-flash matches kimi on the traps (34/36) and costs 32x less ($0.00037 a read), but on the batch it answers `(done)` in **15 of 18** reads - it declares four half-finished issues finished, on a digest whose own FILES list contradicts it. That is a comprehension failure, not a parsing one, and no rule change recovers it: a mark reader that says `(done)` mid-turn is worse than no mark reader. Keep the mastermind tier, ship the digest, and fix the counting.

