---
kind: fixed
title: the installer says why GitHub's API refused, and retries once without a token GitHub refused
pr: 1890
surface: [build, docs]
invalidates:
  - >-
    One sentence was believed to cover every refusal from GitHub's API:
    `GitHub's API could not be reached or refused (a rate limit, or a repository
    you cannot read?); pin VERSION=<tag>, or export GITHUB_TOKEN`. It is retired.
    The installer now names a used-up rate limit with the local time it lifts, a
    rejected token, refused access, a repository it cannot find, a server fault or
    an unreachable host, and never tells someone who exported a token to export one.
  - >-
    A token GitHub refused ended the install. A 401, 403 or 429 on a token-bearing
    lookup now sets the token aside and asks the same lookup once without it,
    because the anonymous quota is a separate bucket; the install goes on with the
    note `GitHub refused GITHUB_TOKEN (…); continuing without it`.
---

`http_get` used to keep only the status code, so the reason GitHub gives in the
same response (`x-ratelimit-remaining`, `x-ratelimit-reset`, the body's
`message`) was discarded. It now keeps the headers and body, and a pure function
of the three picks the sentence. Downloads still never carry the token.
