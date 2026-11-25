---
kind: fixed
title: A shared hosted chat's tool approvals stay with its owner, and two policy settings loosen that
pr: 2927
surface: [factory, chat, docs]
invalidates:
  - "A guest of a shared hosted chat who held the answer permission, which the `edit` role does, could approve a tool call by sending the chat's approval question an answer, and the chat then ran the command as its owner, although the documentation said a command that needs approval waits for the owner. Now both doors an approval has stay with the owner by default, and the guest reads the same sentence as before: `This chat is shared with you. Only its owner can do that.` Answering the chat's other questions is unchanged."
  - "Who approves a tool call in a shared hosted chat could not be set. Now the organization's policy has `chats.approvals_by`: `owner`, which an absent value also means and which is the behaviour above, or `answer`, which lets a person who holds the answer permission allow one tool call once or refuse it. Allowing a tool for the rest of the chat, putting the approval away and the chat's own settings stay with the owner."
  - "Anyone who could see a shared hosted chat that was not running started it for its owner by opening it, and no setting could change that. Now the policy has `chats.wake_by`: `view`, which an absent value also means and which is the behaviour from before, `run`, which lets only a person who may send messages in the chat start it, or `owner`. Under the last two a guest is told the chat is not running and to ask its owner, and nothing is started."
---

Both settings are the operator's to set with `PUT /v1/org/policy`, are refused with a sentence naming the allowed words
when they are not one of them, and show in `GET /v1/org/policy` once set. An organization that sets neither sees no
difference except the first line above. The keyboard rule for a guest's message while the owner's window is open, and
how a new share reaches an open desktop window, are not settings here.
