---
kind: fixed
title: Factory setup names an owner only when every visible repository has it
pr: 0
surface: [engine]
invalidates:
  - "Was true: factory setup showed the first discovered repository's owner as the account's organization. Is true now: it names an owner only when every discovered repository has that owner."
---

Setup took the owner of the first discovered repository as the footer's
`organization`, so an account whose repositories span several organizations and
personal repositories read as a member of one of them, and an empty rediscovery
kept the previous label. Now `github.organization` is set only when every valid
discovered repository has the same owner (compared ignoring case, one spelling
kept), and is empty otherwise, for zero repositories, and on every rediscovery
until the new rows agree. It is a fact about the rows, not organization
membership, and the verified connected profile is separate and unchanged.
