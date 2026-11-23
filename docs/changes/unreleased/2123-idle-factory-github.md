---
kind: fixed
title: An idle factory no longer spends its GitHub hourly limit asking the same questions
pr: 2123
surface: [engine]
invalidates:
  - "The factory asked GitHub who its token is (GET /user) on almost every request. The answer now stands for ten minutes per token, and a 401 for that token forgets it."
  - "GitHub reads with no ETag of their own were always asked in full. They are now asked with the last ETag seen, and a 304 answers with the kept body."
  - "A missing .codeaf/factory.yaml, factory.md or CODEOWNERS was asked for on every intake tick. A missing file is now believed for five minutes, so a newly added one is read within five minutes; edits to a file that exists still reach the next start."
  - "A branch protection that the plan cannot read was asked for every minute. It is now asked once every ten minutes."
---

One idle team factory with a runner made about 6,500 GitHub calls an hour, more than one token's 5,000/h limit.
