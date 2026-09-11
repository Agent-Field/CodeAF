# The call census, nightly

`cmd/aforge-census` reads the model-call log this build always writes
(`~/.aforge/logs/calls.jsonl`, `internal/calllog`) and prints
[docs/design/recovery/DESIGN.md](../../docs/design/recovery/DESIGN.md) §1 as
markdown: finishes by status, cause families, the top error signatures under a
normalised spelling, the 429 depth histogram, retry chains with their same-lane
share and their ten longest, lane health per (model, **served**) over the recent
window, and the checks — every finding from the first census, kept as a
number so a wave that fixes one can prove it and a wave that breaks one again is
told the night it happens.

```sh
make census                                  # this machine's own log
make census LOG=/srv/logs/calls.jsonl        # a log synced from somewhere else
make census LOG=… OUT=/srv/census/$(date +%F).md
make census TOP=40 DAYS=7 MIN=10 CHAINS=20   # widen what it prints
```

It is pure Go with no dependencies, and it never writes to the log it reads.

## Nightly, on the Spark

The measurement is the point: five waves of recovery work each move a number in
that table, and without a run that happens without anybody asking, every one of
them is an argument about anecdotes. So the Spark syncs the laptop's log and
runs the same target once a night, keeping one file per day:

```sh
rsync -a laptop:~/.aforge/logs/calls.jsonl ~/census/calls.jsonl
make -C ~/src/aforge-v2 census LOG=~/census/calls.jsonl OUT=~/census/$(date +%F).md
```

**The cron is not set up here and is not this repository's to own.** What is
here is the target it calls and the one thing the target cannot do for itself:
the log is a *rolling* file that rotates at 32 MB and keeps exactly one
predecessor (`calls.1.jsonl`), so a nightly copy is what turns ten days of
history into a series. A census run against the live file alone measures
whatever has not rotated yet.

## Reading two nights side by side

The output is markdown on purpose, so `diff` between two nights is readable and
a month of them can be pasted into a pull request. The rows that matter most are
the last table's — a share that moved is a wave that landed, and a share that
moved the wrong way is a wave that did not.
