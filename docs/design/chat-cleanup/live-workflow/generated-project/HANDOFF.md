# September reimbursement handoff

Finance-ready category totals for September, produced from
`september-reimbursements.csv` with the `report.py` CLI (this round it gained a
`--csv` output and a `--month YYYY-MM` filter; see *Source and
reproducibility*).

## What to hand finance

- `september-reimbursement-summary.csv` — **the finance deliverable.** A CSV
  with `category,amount` columns and an explicit `TOTAL` row, ready for a CSV
  import:

  ```csv
  category,amount
  books,25.00
  food,15.00
  travel,120.00
  TOTAL,160.00
  ```

  It round-trips: importing it yields books 25.00, food 15.00, travel 120.00,
  and TOTAL 160.00. Header is exactly `category,amount`.

- `september-reimbursement-report.json` — machine-readable category summary,
  exact cents as decimal strings, for an importer or spreadsheet:

  ```json
  {
    "categories": {
      "books": "25.00",
      "food": "15.00",
      "travel": "120.00"
    },
    "total": "160.00"
  }
  ```

- `september-reimbursement-report.txt` — the same figures as a plain-text
  report.
- `september-reimbursement-report.warnings.txt` — the skipped-row warnings from
  the run (see *Data problems* below).

## How to run it

From this directory:

```sh
python3 report.py september-reimbursements.csv            # text report
python3 report.py september-reimbursements.csv --csv      # CSV summary (finance import)
python3 report.py september-reimbursements.csv --json     # machine-readable
python3 report.py september-reimbursements.csv --category FOOD   # one category
python3 report.py september-reimbursements.csv --month 2026-09 --csv   # one month only
python3 report.py september-reimbursements.csv --month 2026-09 --category FOOD --csv --strict   # safe import (stdout)
./run_report.sh TARGET.csv EXPORT.csv --csv --strict   # safe import onto a file finance uses
```

`--month YYYY-MM` restricts the report to one calendar month. Use it whenever
the export spans more than one month (a bank export can cover a whole quarter),
so a mixed-month file is reported a month at a time instead of summed together.
It combines with `--category` and with every output format.

`--json` and `--csv` are **mutually exclusive** — choose one output format.
Passing both exits `2` with argparse's `not allowed with` error:

```sh
python3 report.py september-reimbursements.csv --json --csv
# error: argument --csv: not allowed with argument --json
```

Next month: drop the new CSV into this directory and pass its filename as the
first argument. The filename is **always** required — the tool's built-in
default is `expenses.csv`, so never run it without the file argument.

### Reporting one month from a mixed-month export

`mixed-month-sample.csv` is a self-contained fixture (September rows plus
October rows) used to prove the filter separates months. Verified output:

```sh
python3 report.py mixed-month-sample.csv --month 2026-09 --csv   # September
# category,amount / books,25.00 / food,15.00 / travel,120.00 / TOTAL,160.00
python3 report.py mixed-month-sample.csv --month 2026-10 --csv   # October (excludes September)
# category,amount / books,10.00 / food,42.50 / travel,80.00 / TOTAL,132.50
```

The September figures from the mixed file match `september-reimbursements.csv`
exactly (TOTAL 160.00); October contains no September rows. The original
`september-reimbursements.csv` is only ever read, never filtered or rewritten by
these runs.

### Safe import for automation (`--strict` + `run_report.sh`)

`--strict` is the switch for an automated import, so a partial report can never
look complete. If **any** row would be skipped (an unreadable amount, or an
unreadable date under `--month`), the tool prints the warnings to stderr, writes
**nothing** to stdout, and exits `2`. With clean data it behaves exactly like a
normal run and exits `0`.

When the report is written straight onto a CSV finance already uses, go through
`run_report.sh`. It runs `report.py` into a temporary file beside the target and
replaces the target **only on a fully successful run (exit 0)**; a failed run
leaves the existing target byte-for-byte intact. (Proved against a pre-existing
target file: a `--strict` run over a source with a rejected row left the target
unchanged and left no stray temp file; a clean run then replaced it.)

For the next import, `TARGET.csv` is the file finance reads and `EXPORT.csv` is
the bank export. Gate the import on the exit code so neither partial nor stale
data gets in:

```sh
if ./run_report.sh TARGET.csv EXPORT.csv --month 2026-10 --category FOOD --csv --strict; then
    import TARGET.csv          # exit 0 => written this run, complete for the month
else
    rc=$?                       # capture before anything else
    echo "no import: report refused (exit $rc)" >&2
    exit "$rc"                  # preserve status so cron reports the failure
fi
```

On exit 0 the target was written this run; any non-zero exit leaves the previous
target in place, so skip the import.

`EXPORT.csv` is whatever file the bank produces (the September source was
`september-reimbursements.csv`). Do **not** point this at the test fixture
`mixed-month-sample.csv` — that file exists only to exercise the mechanics.
Read the target only on exit `0`; any non-zero exit means the run was refused and
the previous target file is still in place.

### Exit codes and warnings

- Exit `0` — every row was read and summed.
- Exit `2` — at least one row was skipped. Each skipped row prints a warning to
  **stderr**:
  - an unreadable amount prints
    `september-reimbursements.csv:8: skipping invalid amount 'typo'`;
  - under `--month`, a row whose date cannot be read prints
    `september-reimbursements.csv:3: skipping row with unreadable date 'not-a-date' (needed for --month)`.

  The JSON/CSV/text report on stdout stays clean and covers only the valid rows,
  so a malformed or impossible date (`2026-02-30`) is reported with its source
  line rather than silently vanishing from the filtered report. With `--strict`,
  exit `2` also means **no report was written to stdout at all**.
- Exit `1` — the file is missing, or a required column/date/category is absent.

Because the report goes to stdout and warnings to stderr, `--csv` and `--json`
can be piped straight into another tool.

## Data problems — clear these before the numbers are final

**The grand total of 160.00 is provisional.** Three rows were excluded because
their amount is not a finite number. They are **not** in any total:

| CSV line | date | category | amount (as written) | reason |
|---------:|------------|----------|---------------------|--------|
| 6 | 2026-09-14 | food | `NaN` | not a number |
| 7 | 2026-09-15 | travel | `Infinity` | not a finite amount |
| 8 | 2026-09-16 | food | `typo` | not a number |

The totals above cover only the four valid rows:

| CSV line | date | category | amount |
|---------:|------------|----------|--------|
| 2 | 2026-09-10 | travel | 120.00 |
| 3 | 2026-09-11 | food | 18.40 |
| 4 | 2026-09-12 | food | -3.40 (refund) |
| 5 | 2026-09-13 | books | 25.00 |

Category totals sorted by name: books 25.00, food 18.40 − 3.40 = 15.00,
travel 120.00; grand total 160.00.

**Nothing questionable was silently included.** `NaN` and `Infinity` are
deliberately rejected rather than coerced to numbers; the run exits 2 so an
unnoticed skip is visible to any automated caller.

**Action needed:** supply the real amounts for lines 6–8, or confirm they should
stay excluded. Until then the report is not final. Do not edit the totals by
hand — fix the CSV and re-run so the numbers stay reproducible.

## Source and reproducibility

- `september-reimbursements.csv` is unchanged (input only; never written by the
  tool).
- Amounts use `decimal.Decimal`, so cents are exact (no floating-point drift).
- A negative amount is a refund and reduces its category total.
- A bad `--month` value is rejected before any report: only `YYYY-MM` is
  accepted (`2026-13`, `2026-9` and `202609` are refused), with argparse's
  `error: argument --month: expected a month as YYYY-MM (e.g. 2026-09)` and
  exit `2`.
- Improvements this round: added the `--csv` output flag (mutually exclusive
  with `--json`) via a `format_csv` helper, the `--month YYYY-MM` filter
  (`--month` combines with `--category` and all output formats), the `--strict`
  mode that withholds the report and exits `2` when any row is skipped, and
  `run_report.sh`, which replaces a target CSV only on a fully successful run.
  The test suite is now 39/39 green with `python3 -m unittest` (`test_report.py`
  plus `test_run_report.py` for the safe-export wrapper), covering month
  separation, `--month` with `--category`, malformed `--month` rejection, a
  malformed/impossible date warned rather than dropped, `--strict` withholding
  the report on skips, and the wrapper leaving an existing target intact on
  failure. The three fixture-dependent tests no longer depend on a missing
  `expenses.csv`.
