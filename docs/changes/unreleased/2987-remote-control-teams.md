---
kind: added
title: An organization runs its own relay, and a hosted Team chat reconnects and names who is typing
pr: 2987
surface: [remote, chat, docs, build]
invalidates:
  - "Was true: the self-deployable organization stack in deploy/org started the server, the runner and the agents but no relay, so pairing, presence, Move and remote control between members' computers went through the public hosted relay. Is true now: the stack starts a `relay` service (`codeaf relay --storage off` on 127.0.0.1:8788 by default, its own domain on Railway), and with `CODEAF_ORG_RELAY_URL` set the organization server tells everyone signed in where it is (`relay_url`)."
  - "Was true: a relay either kept chats (`--store`) or kept nothing at all, not even the device list pairing needs. Is true now: `codeaf relay --store <dir> --storage off` keeps the device directory that pairing, presence and remote control need and refuses chat uploads; the organization stack runs it that way because its server already keeps everyone's chats."
  - "Was true: `codeaf pair` always used the relay named by CODEAF_SYNC_URL or the public default. Is true now: `codeaf pair --org <org>` pairs through the relay that organization advertises; an explicit `--via` or CODEAF_SYNC_URL still wins, a computer already paired on another relay is refused as before, and people outside an organization keep the public relay."
  - "Was true: in a shared hosted chat, the `typing from <name>` line came from the joining window's own hello, so a member's window could name itself after anyone. Is true now: the organization server names the window from the person it authenticated (a bot reads as `<creator>'s bot`) and drops any client-supplied remote stamp."
  - "Was true: a `codeaf chat --org` window ended for good when its link to the organization server dropped. Is true now: it reconnects as a returning window and ends at once, with the server's own words, only on 401, 403 or 404; `--once` is never retried, and the desktop's organization window still does not reconnect."
---

Remote control (#2965) was built before Teams reached dev. This makes it work inside an organization: its people's computers find each other through the organization's own relay, and its hosted chats behave like a live view should when a link drops or two people share one.
