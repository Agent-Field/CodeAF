# Running the grid in parallel

`../run.sh` walks its issue list one cell at a time. That is the right shape for
a recorded comparison — a cell that shares a machine with three others is not
being timed under the same conditions as the rows it is compared against — and
the wrong shape for iterating, where a four-issue grid costs two and a half
hours of waiting to learn something in the first ten minutes.

These two launchers run one `run.sh` per issue, concurrently, each with its own
`RESULTS` directory:

```bash
bash bench/parallel/bench-par.sh      "$(pwd)/bench-results/do-$(date +%s)"       # aforge do
bash bench/parallel/bench-par-chat.sh "$(pwd)/bench-results/chat-$(date +%s)"     # aforge chat --once
python3 bench/parallel/bench-rank.py  # merge the CSVs, rank, audit the models
```

`AFORGE_BIN` defaults to `./bin/aforge`; export it to measure another build.

**Wall clock from a parallel grid is not comparable to a recorded sequential
row.** Four cells contend for the machine during `pip install` and `pytest`.
The work is API-bound so the distortion is small, but it is real and it is
always in the same direction: parallel cells look slower than they are. Cost and
test counts are unaffected — quote those freely, and re-run sequentially before
publishing a time.

`bench-rank.py` also audits which models actually served each cell, read out of
the kept store (`do`) or the session transcript (`chat`). A cell that names one
model and spends on three is the failure this exists to catch — see
`--one-model` in `docs/HEADLESS.md`.
