---
kind: added
title: the /pair code is copyable, and /pair <code> --replace joins behind a confirmation
pr: 1745
surface: [chat]
invalidates:
  - "A pairing code shown by /pair could not be selected or copied with the mouse: the panel took every press and acted on none, so the drag never reached it. A drag over the panel now selects and copies what it crosses — the code, its lines, and the link-approval card's rows — over the same clipboard road with the same `copied · N chars` word, and `c` while the code is shown copies the code alone, named in the panel's hint line."
  - "Replacing a computer's own chats was a flag of the terminal command and never a keystroke in a chat. It is both now: `/pair <code> --replace` in the chat asks first, states that this machine's own chats are gone for good and that chats sealed under the old identity cannot be read afterwards, and joins only on an explicit yes — a no, or esc, leaves everything untouched. A home with nothing of its own is joined without the question. The plain refusal now names the in-chat way, and the terminal's `--replace` behaviour is unchanged."
---

The copy bug was the pairing panel's own modal law taken one row too far: it
swallowed the press that arms every sweep on this surface. The replace keeps the
deliberateness the comment asked for — typed whole, asked out loud, default no —
so a stray keystroke still cannot give a machine's chats away.
