---
kind: fixed
title: Reuse confirmed hosted authority and prepare new identity peers asynchronously
pr: 2162
surface: [remote, chat]
invalidates:
  - Hosted reads previously repeated quorum work and synchronous presence updates; they now reuse confirmed authority while failing closed when it expires.
  - New empty identities previously waited for three voting replicas; their first change now commits on one voter before background joint-consensus formation.
---

Directory mutations remain consensus commits. Committed revocations close device
watches and held store polls immediately. Authorization is checked once before
blob work and repeated after I/O only when the applied index changed. Advisory
presence is coalesced outside the request path.

Internal quorum reads preserve an in-progress idle transition; ordinary reads
still wake the group. Link approvals validate certificates and grants before
reserving a pairing request. Exact retries of a committed decision remain valid
after its original pending deadline; expired undecided requests remain refused.

Existing identities retain their voting configuration. New singleton births
persist immutable provenance that older relay code cannot serve; upgrades must
preserve compatible recovery rather than restore a local directory writer.

Copied chats use bounded destination names when a source workspace path exceeds
the filesystem component limit, including paths nested by earlier device moves.
