---
kind: fixed
title: Pairing names the failing sync service, an unused computer can join, and removal asks first
pr: 2087
surface: [chat, engine, remote, docs]
invalidates:
  - "A computer that had only opened codeaf once was refused by codeaf pair and told to use --replace; now a home whose chats never had a turn sent can join, and a home with real chats is told how many and what --replace does."
  - "r on /devices removed the selected computer at once; now it asks `remove <name>? it must pair again to come back  ·  y removes  ·  any other key keeps it` and only y removes it."
  - "Quitting while the sync service was down printed could not save and exited 1 even though the chat was saved on this computer; now it says the chat is saved here and reaches the other devices when the service is back, and exits 0. A failed local write still says could not save. Mixed old and new builds keep the old words and exit codes."
  - "A removed computer that rejoined with a current device's code stayed removed (adopt returned Already); now --replace gives it a fresh device key and it comes back."
  - "Printed codeaf pair commands left out --via on a computer with its own sync service; now every one carries it, and a link waiting on another service is refused with the command to run instead."
  - "chatlist.Unreachable, ReadTimedOut and SyncPaused blamed other machines; now they name the sync service. Running on <device> on Home now reads held on <device>, because a hold does not mean work is running."
  - "/devices, /pair and /settings typed on Home started a new chat; now they open over Home."
  - "internal/syncwords' HostedURL is no longer a constant; internal/syncsetup hands it this build's channel's hosted service at startup."
---

Terminal link approval stays `a / d`, the same deliberate key as the chat card,
and `codeaf --help` does not list `codeaf relay` because the front page is at its
line cap.
