---
kind: changed
title: Organization stack gaps closed, with .env kept out of git and Compose building codeaf from source
pr: 2910
surface: [build, factory, docs, chat]
invalidates:
  - "`deploy/org/.env`, which `./setup.sh` fills with the AgentField key, the organization token, the hosted-chats secret, the server key and the model keys, was described as ignored by git, but no ignore rule named it, so a `git add` of the folder would have committed every secret in it. Now `deploy/org/.gitignore` ignores `.env` and any other `.env.*` copy, and `.env.example`, `.env.stub` and `.env.stub.hosted` stay tracked. The image builds already left the file out."
  - "A plain `docker compose up --build` fetched the pinned release for the codeaf image, because `CODEAF_BINARY_FROM` defaulted to `fetch`, and that release has no `codeaf sessionhost`, so the stack could not run hosted chats. Now the `codeaf`, `runner` and `sessionhost` services build codeaf from this checkout by default (`CODEAF_BINARY_FROM=source`, with the repository root as the build context), the first build compiles it and takes several minutes, and `CODEAF_BINARY_FROM=fetch` is opt-in until a release ships the sessionhost. `compose.yaml` names the check that says when the default can go back: the pinned release's `codeaf sessionhost --help`."
  - "Only a chat's owner could move it, and no setting could change that. Now the organization's policy has `chats.move_by`: `owner`, which an absent value also means and which is the behaviour and the refusal sentence from before, or `manage`, which lets anyone who holds the manage permission on the chat, such as a person it is shared with at `full`, move it. An `edit` holder and a bot still cannot, and a person who is not the owner can never move the chat into their own My space."
  - "The entry for #2900 said stopping a hosted chat stays the owner's whatever the role. The code lets the owner and anyone holding the manage permission, which the `full` role does, stop or delete a hosted chat; approving a command, settings and files are what stay the owner's on the chat's wire. The entry now says so, and the code is unchanged."
---

Each of these was a gap between what the owner decided and what the code did. The move rule keeps today's default, so an
organization that changes nothing sees no difference.
