# App Builder expressions and repairs

## Computed app fields, durations and dates

App computed fields use CEL expressions. Read bare record fields or
`$record.<field>`. Date and datetime values become timestamps for evaluation.
`now` is the current timestamp; `now()` and `$now` also work. `days(n)` returns
a duration for a numeric day count. Integer duration literals include `7d`
(seven days), `24h` (hours), `30m` (minutes), and `2w` (two weeks). A day is
24 hours and a week is seven days. Quoted text keeps its literal bytes.

Examples: `roast_date >= now - 7d` and `due_date < now + days(2)`.
Explicit CEL `duration("168h")` also works. Expressions have a 4096 byte
limit, a parser nesting limit of 64, and an evaluation cost limit of 10000.
Queries use separate field:value tokens.

## Builder Check failed after sample or render

A failed Check returns its issues and repair hints to the Builder's FIX call,
including failures in schemas, kit rules, computed expressions, sample data
or rendering. The Builder uses up to three Check rounds, with two FIX calls.
Expression errors name the field, expression, line and column, and a corrected
example. Canceling a build stops it instead of starting a repair.
