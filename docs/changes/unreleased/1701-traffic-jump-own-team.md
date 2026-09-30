---
kind: fixed
title: a Traffic row opens its own team's message, even when its manager is also in All teams
pr: 1701
surface: [chat, docs]
invalidates:
  - "Pressing the words of a manager's Traffic row said `that message is older than this chat's history` when the manager also sat in the global All teams group and the rail showed All, because the jump took its team from the front conversation. The jump now carries the row's own team and resolves the message there."
  - "A same-numbered message in another team, an omitted-team post, a clipped delivery after a rename, or a renamed team's reused old name could make a jump open the wrong message or none. Ownership is now proven in the row's own team; when it cannot be, the row still says the history line rather than open another team's message."
---
