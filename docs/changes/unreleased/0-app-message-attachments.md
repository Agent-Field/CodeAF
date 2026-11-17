---
kind: added
title: codeaf app carries attachments on a new agent's first message and on crew messages
pr: 0
surface: [engine]
invalidates:
  - "agents.create took words only; a first message with files, links or a paste had to create first and send second. It now takes attachments [{kind file|paste|link, path?, text?, url?}], checked whole before anything is made, and attachments alone start the agent."
  - "crews.message took words only. It now takes attachments (ids staged on the target), and an attached message to a busy target is refused busy rather than queued — the queue would have sent the words and lost the attachments. Words alone still queue."
  - "agents.send consumed its staged attachments before the turn started, so a busy or failed send lost every chip. Chips are now consumed only by a turn that started; a refused send leaves them all staged."
  - "agents.send with attachments to an engine without staging sent the words and silently dropped the attachments; it now answers unknown_method. agents.send and crews.message no longer need text when they carry attachments."
  - "A paste had no size bound. One paste holds at most 200,000 characters, one message at most 10 attachments and 400,000 characters of paste, at every door; an id named twice is bad_params."
---

`agents.create {workspace, text?, attachments: [{kind: "file", path: "notes.md"},
{kind: "paste", text: "…"}, {kind: "link", url: "https://…"}]}` answers at once
as before. A file or folder must exist (relative to the workspace, or
absolute); any refusal is `bad_params` with no agent, folder or event behind
it. The first user entry carries the attachments with ids; the model is sent
the same words `agents.send` writes for staged ones, a file as its path only.
A boot failure's retry sends the same words and shows the same attachments.

`crews.message {crew, to?, text?, attachments: ["at3"]}` sends to the manager
by default or to `to` exactly; ids staged on another agent are `not_found` and
stay where they were.
