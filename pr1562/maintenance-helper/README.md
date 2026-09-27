# Home maintenance helper

Shows the home maintenance chores you still owe, up to a date you pick, and
lets you tick one off by its exact title from the command line.
Standard library only — no install step.

## Use it

```sh
python3 maintenance.py --due 2026-09-25
```

Output (sample data):

```
1 unfinished chore(s) due by 2026-09-25:
  2026-09-20  Replace smoke alarm batteries  [open]
```

Options:

| Flag | Meaning |
| --- | --- |
| `--due YYYY-MM-DD` | Show chores due on or before this date. |
| `--done TITLE` | Mark the chore with this exact title as `done`. |
| `--csv PATH` | CSV to read, and with `--done` to write. Default: `maintenance.csv` beside `maintenance.py`. |
| `--json` | Print the result as JSON for another script instead of text. |

Give exactly one of `--due` or `--done`.

A chore counts as unfinished when its status is not `done` (also accepts
`complete`, `completed`, `closed`) — so both `open` and `in-progress` chores
are listed. Results are soonest-first. If nothing is due, it prints
`No unfinished chores due by <date>.`

## Mark a chore done

`--done` sets the matching row's `status` to `done` and rewrites the CSV:

```sh
python3 maintenance.py --done "Replace smoke alarm batteries"
```

The title must match exactly — case-sensitive, with surrounding whitespace
ignored. If no row matches, or more than one row shares the title, the
command prints an error and exits `2` **without touching the CSV**. The
rewrite goes to a temp file in the same directory and is renamed over the
original, so an interrupted run never leaves a half-written file; other
columns and the rows that did not match are preserved.

## JSON output

Add `--json` to get the same result for another script:

```sh
python3 maintenance.py --due 2026-10-01 --json
```

```json
{
  "due_by": "2026-10-01",
  "count": 3,
  "chores": [
    {"title": "Replace smoke alarm batteries", "due": "2026-09-20", "status": "open"},
    {"title": "Repaint front step", "due": "2026-09-25", "status": "in-progress"},
    {"title": "Clean dryer vent", "due": "2026-10-01", "status": "open"}
  ]
}
```

## CSV format

```
title,due,status
Replace smoke alarm batteries,2026-09-20,open
Change HVAC filter,2026-09-01,done
```

`due` is an ISO date (`YYYY-MM-DD`). `status` is free text; `done` (any case)
means finished.

## Tests

```sh
python3 -m unittest -v
```
