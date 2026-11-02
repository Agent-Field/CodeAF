---
kind: fixed
title: A team factory answers at once while GitHub's rate limit is spent, and says until when
pr: 2120
surface: [engine]
invalidates:
  - "`codeaf factory serve` looked up the host's GitHub login for the operator link before its listener served, and a spent rate limit made that lookup sleep in backoff, so the factory answered nothing and logged nothing for minutes. The listener now serves first; the operator link's row is written before the address is printed and its GitHub login is named after the reset, then the link is printed; the limit is logged once when met and once when cleared, and `/v1/about` carries `github_limited` and `github_resumes_at` while it is spent."
  - "A GitHub read whose rate limit resets later than one pause (ninety seconds) is answered at once with the limit instead of being slept on and tried again."
---
