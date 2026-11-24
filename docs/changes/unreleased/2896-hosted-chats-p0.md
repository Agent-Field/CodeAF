---
kind: added
title: codeaf chat --org runs a chat on the organization's server, in a sessionhost of its own
pr: 2896
surface: [chat, engine, factory, build, docs]
invalidates:
  - "An organization server answered every /v1/hosted/ and /v1/model/ path as it answers any unknown /v1/ path, and a chat could only run on the computer in front of the person or on one machine reached with --host or --at. With CODEAF_HOSTED_URL set the server now hands a person's chat to a sessionhost and proxies it, and codeaf chat --org <url> opens one. With the variable unset nothing changed."
  - "The engine fenced a hello's commands only when the hello was headless, and ran them with the full permissions of the process when no fence could be applied. With CODEAF_FENCE_REQUIRE=1 the engine fences every hello, and a command that cannot be confined is refused with a plain sentence instead of being run open; without the variable the engine is what it was."
  - "deploy/org's codeaf image could only carry the pinned published release, which has no sessionhost command. A local binary can now be placed beside codeaf.Dockerfile and chosen with CODEAF_BINARY_FROM=local; the default build is unchanged."
---

A person can run a whole chat (model, shell, git, a worktree) on a machine the organization keeps and attach to
it from a terminal. The machine gives every chat a Linux user and a closed folder of its own, holds no
organization secret or model key, and refuses to start chats when it cannot keep them apart. It is off unless an
operator sets it up; `deploy/org/README.md` has the steps and `SMOKE_HOSTED=1 ./smoke.sh` the keyless proof.
