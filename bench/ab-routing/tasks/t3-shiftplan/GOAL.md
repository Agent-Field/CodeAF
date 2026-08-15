Audit, repair and refactor the `shiftplan` Python package in this workspace.

`shiftplan` assigns workers to shifts and computes payroll. It is small, it
imports cleanly, and most of its shipped test suite passes — but it is wrong in
several independent ways, and only some of them are visible from the tests that
shipped with it. `python3 -m pytest tests/ -q` is your starting signal, not your
finishing line: the failures you can see point at two areas, and there are more
defects than that in the package. The specification each module must satisfy is
written in its own docstrings; where a docstring and the code disagree, the
docstring is right and the code is the defect.

Read every module. Assume the shipped tests under-test the package.

## What must be true when you are done

1. **Every defect is fixed.** Look for wrong boundary semantics, state that
   leaks between calls, caching that returns an answer for a question that was
   not asked, arithmetic that loses exactness, and error handling that turns a
   failure into a quiet success. Each module's docstring states the behaviour
   that module is supposed to have.

2. **The refactor is done.** Assignment must accept a pluggable ranking
   strategy. Add a module `shiftplan/strategy.py` exporting exactly:

   - `class Strategy` with one method `rank(self, shift, candidates)`, which
     receives the shift and the list of workers holding its required skill, and
     returns them in preference order (best first). `Strategy.rank` is abstract:
     the base class must not implement a ranking.
   - `class GreedyCheapest(Strategy)` implementing the behaviour `shiftplan`
     has today: cheapest `rate_cents_per_hour` first, ties broken on worker id
     ascending.

   And change `assign_shifts` to the signature

   ```python
   assign_shifts(shifts, workers, blocked=None, strategy=None)
   ```

   where `strategy=None` means `GreedyCheapest()`. `rank` must be given only
   the workers who hold the shift's required skill, and must **not** be asked
   to filter for availability — the assigner walks the ranked list and skips
   anyone already booked. Both names must be importable as
   `from shiftplan.strategy import Strategy, GreedyCheapest`, and `Strategy`
   must be usable as a base class by code outside the package.

3. **`tests/test_visible.py` is byte-for-byte unchanged.** It is the contract,
   not your workspace. Do not edit it, do not delete it, do not weaken an
   assertion, do not mark anything skip or xfail. Every test in it must pass
   when you are done. You may add new test files of your own; they are not
   graded and will not be read.

4. **The public API keeps working.** Everything currently exported from
   `shiftplan/__init__.py` must still be importable under the same name, and
   `import shiftplan` must succeed. Add `Strategy` and `GreedyCheapest` to the
   exports.

## How this is graded

By a hidden test suite you will not see, grouped by defect family. Your score is
the fraction of families in which **every** test passes — partial repair of a
family scores nothing for that family, so finish what you start. Editing
`tests/test_visible.py`, or leaving the package unimportable, scores zero
overall regardless of anything else.

Work in this directory. Leave the repaired package on disk; there is nothing to
submit and no report to write.
