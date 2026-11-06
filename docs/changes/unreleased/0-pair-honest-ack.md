---
kind: fixed
title: Pairing never says Paired on one computer while the other refused
pr: 0
surface: [engine, chat, app, docs]
invalidates:
  - "Was true: the computer that typed a code told the other one it had the answer as soon as the answer opened, before deciding whether to keep it, so a computer that then refused (chats of its own, a vault key from before identities) left the computer that showed the code saying Paired with it. Is true now: it says so only once it has decided to keep the grant, and before it writes any of it; a computer that refuses says nothing, and the computer that showed the code fails with pairing was not confirmed by <name>; check that computer to see whether it joined."
  - "Was true: a mailbox that vanished while the computer that showed the code waited for that word counted as a finished pairing, and a word that never came said the pairing did not finish; nothing was changed. Is true now: both are the not confirmed sentence, which never says nothing was shared, because the grant had already gone out."
  - "Was true: the app's use_code and the chat's /pair contacted the other computer before checking whether this one was already paired, so an actively paired computer was refused only after the other had approved. Is true now: both refuse it first, as codeaf pair does, with This computer is already paired with <names>. in the app; the app's request_link does the same."
  - "Was true: only a solo identity refused its own chats before the code was used. Is true now: one that no other computer still holds, for example one whose identity was sent after a yes but not kept, or one an earlier build took out of solo when it showed a code, is refused before approval too; a removed computer and a computer on a rotated identity are not refused early."
  - "Was true: making a pairing grant ended this computer's solo state on every door that shows a code (codeaf pair --code, /pair, the app's Pair a computer, codeaf devbox up), before the code even appeared and for good, so a code that was taken back, ran out, was declined or was used up by a wrong code still started sync (chat uploads, device records, teams). Is true now: the computer stays solo and quiet until a person approves the device that typed the code and its identity is about to be sent; from then on it syncs, even if the other computer refuses or never confirms. Showing a code still uses the relay for the code, and /pair and codeaf pair --code still write this computer's device record before the code appears; a computer an earlier build took out of solo keeps syncing."
  - "Was true: the app showed the terminal's refusal, which says to run this again with --replace, a flag the app cannot pass, and the joining computer's receipt said Run codeaf to open your chats. Is true now: the app says This computer has N chats of its own (or local work), so it can't join another computer's chats. Nothing was changed., the receipt says Paired with your other computer. Your chats can continue on both computers., and a refusal of what this computer holds carries the error code cannot_join."
---

An older computer that types a code still says it has the answer at once, so a
newer computer that shows the code keeps today's behaviour with it. A newer
computer that refuses sends nothing to an older one, which then says the
pairing did not finish. A computer on an identity that was rotated is no longer
refused as already paired before the code, so it can follow the rotation.
