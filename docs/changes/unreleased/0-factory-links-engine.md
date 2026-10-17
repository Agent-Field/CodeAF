---
kind: added
title: The engine can list, make, retry-copy and revoke a team factory's invitations
pr: 0
surface: [engine]
invalidates:
  - "A factory's invitation could be made only by its operator on the factory's own host or command line; the app could not make one. It still cannot copy an EXISTING invitation: the factory shows an invitation's address once, in the answer that creates it."
  - "An access link's row could not say whether revoking it ends its members' sessions. `counted` now says, for a person's invitation only, and is absent when the factory did not say."
---

A distinct optional feature, `factory.links`, adds `factory.links`,
`factory.link_create`, `factory.link_copy` and `factory.link_revoke`. They work
for the operator of a team factory with the credential held now, and fail
closed for anything old, local, solo or unproven. Creating also needs the
factory to say, in `GET /v1/me`, that it can sign people in
(`github_sign_in_available`, `invite_available`, both new and both false when a
factory omits them) and needs a clipboard.

The invitation's address and secret never cross the app wire. The engine takes
them from the factory's one answer, hands them to the clipboard on standard
input, and answers a row and an outcome (`copied`, `not_copied`,
`unconfirmed`). A confirmed invitation whose copy failed is never deleted and
never made again: it is held in memory for at most ten minutes behind an opaque
`ref`, and `factory.link_copy` retries the same one. An ambiguous create is
`uncertain`, sends no delete and no retry, and refuses the next create until the
list has been read.

The native shell and engine require a trusted direct local bundled launch
before creating or copying invitations. The shell refuses overrides before
RPC; the engine consumes a parent-bound arrangement marker before tools can
inherit it. `internal/clipboard` sends text only through helper stdin, with
fixed errors and bounded execution; all unproven launches stay unavailable.

An account switch during a network wait reveals no earlier caller metadata or
held invitation. An ambiguous stored write stays uncertain even when its later
event recording fails with a 4xx. Only a marked pre-mutation refusal proves
nothing changed; an unexpected success status such as Accepted does not prove
completion. Session credentials stay held on uncertain outcomes. Team reads
without a held credential cannot regain host authority through loopback.
