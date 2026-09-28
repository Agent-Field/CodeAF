# Add a warning logger and route every warning through it

## Task description

The CLI writes its warnings with ad-hoc `std::cerr` statements. Every warning
site spells its own `warning: ` prefix by hand, and multi-line warnings repeat
that prefix per statement, so a warning's later lines print without one.

Add a helper to `src/logger.h`, next to the existing `LOG_VERBOSE` helper:

- `auto LOG_WARNING() -> std::ostream &`
- it must always write to `std::cerr`, regardless of `--verbose` or any other
  option
- it must write the `warning: ` prefix itself, so call sites spell only their
  message

Then convert every warning in the CLI sources to use the helper: replace each
`std::cerr` statement that prints a `warning: <message>` with
`LOG_WARNING() << <message>`. A multi-line warning must be converted down to
its LAST statement, not just its first — every continuation statement of the
message goes through the helper too, so the whole warning carries the one
prefix.

The warning sites in the sources today are in `src/command_lint.cc`,
`src/command_validate.cc` (two sites), `src/resolver.h` and
`src/command_bundle.cc`. Search the tree yourself; do not assume the list is
exhaustive.

The repository to work in is `~/repos/jsonschema`, already checked out for
you. Work from the commit that is checked out; do not rebase and do not fetch
newer history.

## Test guidelines

The project builds with CMake and its own Makefile:

- configure and compile with `make configure compile` from the repository root
- the test suite is POSIX-shell tests registered in `test/CMakeLists.txt`, run
  through ctest; the tests that cover this change are the `validate` suite's
  `pass_jsonl_empty` tests, which run the built CLI against an empty JSONL
  file and compare its stderr
- run them with `ctest --test-dir build -R 'pass_jsonl_empty' --output-on-failure`

Tests that cover your change must keep passing, and if you add or change a
test it must be registered the way the other shell tests are.

## Lint guidelines

- the compile target runs the project's clang-format over `src/*.h` and
  `src/*.cc`; your diff should be clang-format clean under the project's own
  style file
- shell tests are checked with shellcheck; if you touch a `.sh` file keep it
  shellcheck clean

## Style guidelines

- keep the diff focused on the change: no drive-by reformatting, no edits to
  vendored code, nothing outside the sources and their tests
- follow the file's existing conventions rather than importing new ones —
  log helpers here are free functions returning `std::ostream &`, declared
  `inline auto ... -> std::ostream &`
- new includes go with the existing include block, grouped the way the file
  already groups them

---

## Internet use

This task runs with internet access. What it is for: reading documentation,
API references, error messages and background concepts — the material any
engineer would consult while doing this work on their own machine.

What it is not for, and what we scan for after every run: anything that could
hand you the fix for this specific task. That means the repository this task
is cut from, and any mirror, fork, vendored copy, issue tracker, pull request,
commit, patch, diff, changelog or CI log of it; and searching for phrasings
likely to surface the bug or the patch itself. Finding this task's own
upstream change is a flagged run: it scores zero regardless of the rest.

If a page you land on turns out to be from the task's own upstream project,
stop using it, say so in your final answer, and carry on from what you already
had.