---
kind: fixed
title: A chat can always be written on the computer in front of you, even on an older sync service
pr: 2075
surface: [chat, remote, build, docs]
invalidates:
  - "A chat whose hold had lapsed could only be written after a conditional take, so on a sync service without codeaf-conditional-takes it opened read-only and Enter showed Continue this chat here? again; it is now taken and written on every service, and only taking a chat that is open on another computer waits for the update."
  - "Turns this computer saved on top of the chat's last uploaded turn made a reopened chat read-only; they are now published when the chat opens."
  - "A chat this computer held last and let lapse read as off and asked Continue this chat here? with this chat was last held here; it now reads as free and opens without the question."
  - "Home offered Continue <device>'s latest chat here? for a device removed with codeaf devices revoke; a removed device is no longer offered."
  - "codeaf devices revoke <name> --all looked only at devices that can use this machine and missed computers that sync chats; it now removes every device of that name of either kind."
  - "The older-service refusal said the sync service must be updated before chats can move; it now says that taking an open chat from another computer waits for a sync service update and that everything else works."
---

A chat whose hold had lapsed needed a conditional take, which the client made
only when the service promised one. The deployed hosted service does not, so
every lapsed chat was refused, turns stayed local, and the next open found this
computer's own newer turns and opened the chat read-only. An older service
already refuses a live holder atomically and raises the fence, and the caller
checks the head the take answers, so a lapsed take is now made on every service.

`codeaf-release service` asks the live hosted service, as a throwaway identity,
for every promise this build needs and fails when one is missing. The engine's
different-environment refusal now offers `codeaf chat --no-host`, and the
`.moving` folder that holds older chats' copies carries a `README.txt`.
