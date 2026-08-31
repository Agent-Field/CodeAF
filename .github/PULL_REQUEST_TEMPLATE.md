<!-- Base this on `dev`. docs/rules/branching.md if that is a surprise. -->

## What changed

## How it was checked

<!-- What you actually ran, and what it said. "Tests pass" is not a report. -->

## Checklist

- [ ] The manual knows about it — `internal/manual/chat/` updated in this change
      if a slash command, key, tool, default, limit or refusal moved. CLAUDE.md
      states the law; three gates fail the build if it is skipped.
- [ ] No new line in `.github/known-red.txt`, or one with the reason above.
- [ ] Only my own paths are staged — no `git add -A`.
