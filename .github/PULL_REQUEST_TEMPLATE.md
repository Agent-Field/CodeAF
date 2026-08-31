<!-- Base this on `dev`. docs/rules/branching.md if that is a surprise. -->

## What changed

## How it was checked

<!-- What you actually ran, and what it said. "Tests pass" is not a report. -->

## Checklist

- [ ] A change entry — `make changelog-new PR=<n> KIND=<kind> SLUG=<slug>`. What
      it carries is not what shipped but what somebody now believes wrongly.
      `kind: internal` with just a title is fine; `no-changelog` is the way out.
- [ ] The manual knows about it — `internal/manual/chat/` updated in this change
      if a slash command, key, tool, default, limit or refusal moved. CLAUDE.md
      states the law; three gates fail the build if it is skipped.
- [ ] No new line in `.github/known-red.txt`, or one with the reason above.
- [ ] Only my own paths are staged — no `git add -A`.
