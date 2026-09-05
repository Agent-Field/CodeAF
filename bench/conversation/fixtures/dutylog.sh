#!/usr/bin/env bash
# dutylog.sh — the multi-defect coding workload's fixture: one small Python
# project, standard library only, with four independent defects in four
# modules and a command-line pipeline that needs all four repaired.
#
# Deterministic and offline. Nothing here is generated at random and nothing is
# downloaded: the project, its own failing suite and the written contract are
# copies of files kept beside this script, so a run on Tuesday is the run on
# Monday. The judge is NOT copied into the workspace — it goes to the cell's
# judge directory, a sibling of it, and it exercises inputs this workspace never
# contained. That is a LOCATION and not a sandbox: the judge is an ordinary file
# on the same disk, reachable by anything with the path. What the scenario has
# is a checksum taken before the run, and a rule that a changed instrument is
# reported and not used.
#
# The four defects, one per module, each with its own cause:
#
#   parsing.py     a hand-rolled split on commas, so a quoted field containing
#                  a comma and a leading byte order mark both break the file
#   validation.py  a repeated identifier is only noticed when the repeat is on
#                  the very next record
#   aggregate.py   an ISO week number pasted onto the calendar year, so the
#                  last days of December land in the wrong year's week
#   report.py      ties left in whatever order the totals were built in, so the
#                  report depends on the order records arrived in
#
# They are independent: each is found and fixed on its own, and no one of them
# is a prerequisite for another. The pipeline is where they meet — its output
# is only right when all four are.

# fixture_dutylog <workspace> <judge-dir>. The project goes where the harness
# can read it; the judge and the guard manifest go beside it, not inside it.
fixture_dutylog() {
  local work="$1" judge="${2:-$1}"
  local template="$CONV_ROOT/fixtures/dutylog"
  mkdir -p "$work" "$judge" || return 1
  cp -R "$template/workspace/." "$work/" || return 1
  find "$work" -name '__pycache__' -type d -prune -exec rm -rf {} + 2>/dev/null

  # SPEC.md is written here rather than kept as a file beside the modules so
  # that campaign.py's rig hash — which covers .sh and .py — covers the contract
  # too. A judged contract that can change without starting a new experiment is
  # not a frozen fixture.
  cat > "$work/SPEC.md" <<'MD'
# dutylog — the contract

A depot logs duty in a CSV and wants minutes per site per ISO week, for a date
window, as text a person reads and a script diffs. Four modules do it:
`parsing`, `validation`, `aggregate`, `report`; `cli` joins them together.
Standard library only.

## The file

Six columns, in this order, and the header must say exactly this:

```
entry_id,start,worker,site,minutes,status
```

* It is CSV, in the ordinary sense — the RFC 4180 shape minus one thing, named
  below: a field may be quoted, a quoted field may contain commas, and a doubled
  quote inside a quoted field is one quote.
  `"Ward ""B"", North"` is the single value `Ward "B", North`.
* A byte order mark at the start of the file is not part of the first header
  name.
* A blank line is not a record. Line numbers count every line in the file, so
  the record after a blank line knows which line it was on. `line_no` is
  1-based, and the header is line 1 of a file that starts with it.
* Surrounding whitespace is not part of a value.
* A record with a newline inside a quoted field is out of scope.
* `parsing.read_entries(path)` and `parsing.parse_entries(text)` return
  `RawEntry` records — still text — in file order. A file whose header is wrong,
  whose record has the wrong number of fields, or which has no header at all
  raises `parsing.ParseError`; a refusal about a record names its line.

## The rules

`validation.validate(raw_entries)` returns `Validated(entries, errors)`. It
raises nothing: every rejected record becomes one message, in file order.

* `start` is `YYYY-MM-DDTHH:MM`, read with Python's `%Y-%m-%dT%H:%M`. That
  accepts an unpadded month, day or hour — `2027-3-1T8:00` is a valid start —
  and rejects anything else, including a date with no time.
* `minutes` is a whole number, and `1 <= minutes <= 1440`. Both ends are inside
  the range.
* `status` is `logged` or `void`. A `void` record is not an error and is not an
  entry: it is a record that was cancelled.
* `entry_id` is unique across the whole file, wherever the repeat is — the next
  line or the last one.

A record breaks at most one rule, the first of these it breaks, and the message
is `line <n>: <reason>`:

| order | reason |
|---|---|
| 1 | `missing entry_id` |
| 2 | `bad start timestamp` |
| 3 | `minutes is not a whole number` |
| 4 | `minutes out of range` |
| 5 | `unknown status` |
| 6 | `duplicate entry_id` |

An identifier is remembered from the records that were accepted, including
`void` ones. An identifier on a record that was rejected is not remembered, so a
later record may use it.

## The window and the week

`aggregate.weekly_totals(entries, window_start, window_end)` returns minutes per
site, per week: `{week: {site: minutes}}`.

* An entry is in the window when its start **date** is on or after
  `window_start` and on or before `window_end`. Both ends are included; the time
  of day does not matter.
* A week is an **ISO** week, written `YYYY-Www` — `2025-W01`. It is identified
  by its ISO year and its ISO week number together, and those are not always the
  calendar year and week of the dates in it: Monday 2024-12-30 is in `2025-W01`,
  and Friday 2027-01-01 is in `2026-W53`.

## The report

`report.render(totals)` returns the whole report, ending in a newline. Every
line is `"%-9s %-25s %5s"` of a week, a site and hours; the first line is that
format applied to `week`, `site`, `hours`, and the last is that format applied
to `TOTAL`, an empty site, and the hours of every minute in the report.

* Weeks ascend by their `YYYY-Www` key.
* Inside a week, the bigger total comes first; sites with equal totals are in
  ascending order of their name, compared by code point — `Byre` before `annex`.
* Hours are minutes / 60 to one decimal place, halves rounded up: 15 minutes is
  `0.3`, 45 is `0.8`. `report.hours(minutes)` returns exactly that.
* The total line is the hours of the summed minutes, not the sum of the rounded
  hours: 15 and 15 minutes total `0.5`, never `0.6`.
* The report is a function of the totals alone. The same records in a different
  order are the same bytes.

## The command

```
python3 -m dutylog.cli --input LOG.csv --from YYYY-MM-DD --to YYYY-MM-DD
```

| exit | when | stdout | stderr |
|---|---|---|---|
| 0 | a report | the report | empty |
| 1 | records were rejected | empty | one message per rejected record, in file order |
| 2 | the arguments are unusable, including `--from` after `--to` | empty | the usage error |
| 3 | the file could not be parsed or read at all | empty | what was wrong |
MD

  cp "$template/judge/judge_dutylog.py" "$judge/judge_dutylog.py" || return 1
  python3 "$template/manifest.py" create "$work" "$judge/workspace-guard.json" \
    --path tests --path SPEC.md >/dev/null || return 1
}

# fixture_dutylog_judge_sum and fixture_dutylog_guard_sum are the guards against
# a harness that answers the question by editing the marker. They are read once,
# when the fixture is built, and held by the runner — not by anything the
# harness can reach.
fixture_dutylog_judge_sum() {
  shasum -a 256 "$1/judge_dutylog.py" 2>/dev/null | cut -d' ' -f1
}

fixture_dutylog_guard_sum() {
  shasum -a 256 "$1/workspace-guard.json" 2>/dev/null | cut -d' ' -f1
}
