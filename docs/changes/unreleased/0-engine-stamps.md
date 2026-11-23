---
kind: fixed
title: engine readers pay once per file stamp
pr: 0
surface: [engine]
invalidates:
  - "Was true: crew lookups repeatedly parsed the router diary, project settings and team files were read on every lookup, traffic queries reopened unchanged logs, and launches rediscovered unchanged imported skills. Is true now: parsed crew learning and daily totals, project settings, team files, traffic queries and team spend are memoized by file size, modification time and identity. Traffic resumes at the last complete line on append and resets on truncation or replacement; initial bounded pages retain their bounded reads. Crew midnight totals and route expiry still use the caller's clock. Skill import stats remembered files and directories before discovery, skips unchanged scans, and serializes scan plus publication per shelf. Nested edits, directory additions, symlink targets and plugin control files invalidate the snapshot; unsuccessful passes retry. Caches retain at most 64 paths, queries or import scopes (the existing content-digest cache remains capped at 512 folders), without a time-based expiry. Platforms without skill change times continue scanning."
---
