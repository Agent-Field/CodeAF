---
kind: fixed
title: a cost that was really spent never reads $0.00, and money.go is the only place the head spells one
pr: 650
surface: [resident, docs]
invalidates:
  - "The resident's plan read, its one-job result and its finished-window list each spelled a job's cost with a raw `\" | $%.2f\"` under an `impact.Cost > 0` guard, so a leaf journaled at $0.00048 printed `| $0.00 |` beside three turns of real work. All three go through `moneyUSD` now and read `$0.0005` — the same figure, digit for digit, that the spend surface shows for that money."
  - "Sixteen sites across `internal/head` spelled a dollar figure with their own format verb: the plan, result and history rows, the spending read and the status page's money section, the self-work receipts, the window breakdown, the daily-rail lines in both the belt and the turn prompt, the surgery loss and the rail-raise reply. `internal/head/money.go` is the ONLY file in that package that spells one now, and `TestOnlyMoneyGoSpellsADollarFigureInTheHead` fails on the next one written anywhere else in it."
  - "`dimeUSD` lived in `internal/head/toolbelt.go`; it lives in `money.go` beside `moneyUSD`, unchanged. Its dime rounding still renders a few cents as `$0.00` on the live board and in the depth block, deliberately — a figure is only allowed to be as precise as it is stable there — and `internal/tui2/tokens`'s `MoneyDime` still mirrors it. #616 did not reopen that."
  - "`internal/manual/pages/money-and-limits.md` said nothing about how a figure is spelled, so `$0.0005` on a row had no page behind it. It now carries the ladder — cents at a cent and above, four decimals below one, `under $0.0001` at the floor — the law that a cost really spent never reads `$0.00`, the difference between an unpriced row and a free one, and the board's dime."
  - "`internal/head` was in none of the packages `make test-laws` ran, because it held no test that reads the tree with `go/ast`. It holds one now, so the laws gate covers fifteen packages rather than fourteen."
---

#606 made a settled leaf's measured cost durable and put it in reach of the row
that reads it; the row rounded it away. `$0.00` beside real work does not read as
"very small", it reads as free — the failure `internal/head/money.go` and
`internal/tui2/tokens/format.go` were both written about, where a model shown a
rate it had been told was zero reached for one that was not and wrote
`$20.00 a run`. The formatter that gets this right already existed in the
package. The change is reach rather than arithmetic: `moneyUSD`'s ladder, its
zero and its floor are untouched, nothing at or above a cent moved a byte, and
the `impact.Cost > 0` guard stays so an unpriced step still says nothing at all.
