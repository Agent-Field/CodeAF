# scriptbench: does the script tool help where it should?

The one-road benchmark (`bench/oneroad`) asks for code changes, and its
pilots found that neither model reached for the `script` tool: a weak model
misread it as Node.js, and a strong one already chains commands inside one
`bash` call. This benchmark asks the questions a script is FOR: read many
files, keep a little from each, answer with a number or a list.

## What it runs

Three questions about click 8.1.7 (pinned in `run.sh`), each in
`tasks/<name>.md`:

- `public-defs`: public top-level functions and classes in each module.
- `raisers`: every function whose own body raises one of click's exceptions.
- `longest`: the ten longest functions and methods, with their lengths.

And two that need many calls through a tool the shell cannot stand in for,
codeaf's own `manual` (the packed chat manual; `codeaf manual` on the command
line reads a different corpus, and run.sh keeps codeaf off the cells' PATH):

- `manual-last`: the title and last section heading of 29 named pages.
- `manual-find`: which of 40 named pages have a heading with a word starting "delet".

These are the questions the script tool should win: one script can fetch every
page and return a small table, where calling the tool once per page brings
every page into the conversation. The click questions are the control: a
shell can answer them in one command.

Each is asked of `codeaf-new-crew` and `codeaf-chatscript-crew` in the same
wave. The pair differs by `CODEAF_CHAT_SCRIPT=on` alone. Every cell is
`bench/oneroad/cell.sh`: the real chat over tmux, an empty environment, the
same counters.

```sh
NEW_BIN=$PWD/bin/codeaf bench/scriptbench/run.sh            # all tasks
NEW_BIN=... SEEDS="s1 s2 s3" bench/scriptbench/run.sh raisers                  # one task, three seeds
NEW_BIN=... MODEL=deepseek/deepseek-v4-flash bench/scriptbench/run.sh
```

The default model is `anthropic/claude-sonnet-5.5`. A run calls live models
on `OPENROUTER_API_KEY` and is billed.

## What it found

Sonnet 5.5 (`anthropic/claude-sonnet-5.5`), a binary built from this change with both switches as they ship, 2026-11-18.
Cost is the cell's whole bill from its usage ledger.

| question | arm | seeds | mean cost | mean input tokens | score |
|---|---|---|---|---|---|
| manual-last | new-crew | 3 | $0.626 | 449k | 1.00 1.00 1.00 |
| manual-last | chatscript-crew | 3 | $0.140 | 171k | 1.00 1.00 1.00 |
| manual-find | new-crew | 3 | $0.968 | 533k | 1.00 1.00 1.00 |
| manual-find | chatscript-crew | 3 | $0.111 | 204k | 0.93 1.00 0.93 |

The script arm cost 4.5x less on manual-last and 8.7x less on manual-find, the
ranges did not overlap, and wall time was the same (about 110 s). Every script
cell wrote 3 to 7 scripts making 43 to 145 calls; the baseline made one call per
page, several per response. The saving is what the scripts kept out of the
conversation: input tokens fell to about 38%.

The two 0.93s are one page, `home`, whose heading says "deleting". Those runs
asked for headings containing "delete", "as part of a longer word too", which
invites that match; the question now asks for words starting with "delet", and
the checker agrees. The table scores those runs by the rule they were asked
under. `score.py` uses today's checker, so re-scoring the same replies reads
1.00, 0.93 and 1.00 for the script arm and 0.93 three times for the baseline,
which never matched "deleting" either way. The costs do not change.

The click questions (one seed each) showed no difference: Sonnet answered each
with one to four `bash` calls running Python's `ast` over the package, and
neither arm wrote a script. Where the shell can do the whole job in one
command, the tool has nothing to add. The one-road pilots (`bench/oneroad`)
found the same on code changes, and a weaker model (DeepSeek v4 flash) did not
use the tool at all.

## How it is scored

`check.py` computes every true answer from the source with Python's `ast`, or
from the binary's manual corpus through `manualtruth/`, never from a model, and scores the reply's last ```json block: the share of
modules right, the F1 of the set, the share of the true ten. A reply without
a block scores 0 and says so. Run `check.py truth <task> <repo>` to see an
answer.

`score.py` writes `results/results.csv` and prints one line per task and
arm. Read `scripts` first: an arm whose chat wrote no script measured nothing
about the tool. Then compare `score`, `usd` and the token columns. `usd`
and the tokens are the cell's whole bill from its own usage ledger, the
conversation and any task together.
