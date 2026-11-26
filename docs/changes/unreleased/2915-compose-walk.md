---
kind: changed
title: the Compose path of a self-hosted organization, walked as a stranger and corrected
pr: 2915
surface: [docs, build, chat]
invalidates:
  - "The operator README said the operator link's http://[::]:8787 works from the machine running Compose. It does not: the published port listens on 127.0.0.1 only, so [::] does not connect, and over 127.0.0.1 the server refuses a password because Docker presents the request from its bridge, not from loopback. A password sign-in, even on the same machine, needs a public name and an HTTPS proxy in front; the README now has a one-machine recipe with Caddy."
  - "CODEAF_PUBLIC_DOMAIN=localhost looked like the way to name a one-machine trial. The Compose stack always runs the server with tls off, which refuses localhost, so the container restarted in a loop; a name with a dot that resolves to 127.0.0.1 is what a trial uses."
  - "./operator-link.sh printed the newest operator link in the server's log even after the organization was claimed, so it handed out a spent link and never reached its own message. It now asks the server to check the link and reports a used, expired or retired one instead of printing it. A link also expires after 24 hours."
  - "The scripted git host (gitstub) was in the stub profile, where it refused to start without a token that only smoke.sh makes, so turning the scripted model on by hand left a container restarting forever. It is profile gitstub now, and smoke.sh asks for it with the hosted part."
  - "Joining an organization, inviting a person, sharing a chat and getting the sign-in token for codeaf chat --org were written down only for the desktop app. docs/organizations.md and two manual pages now give each as the request the app sends, so a terminal can do all of it."
  - "The operator README's backup was a bare tar into a bind mount: root-owned, readable by everyone on a default host, holding password hashes, sign-in sessions and the organization's access token, with no restore and no word on stopping the services or on the hosted chats' own volume. It is rewritten, and backup and restore were run against a live stack."
  - "codeaf --help and the guide did not name the chat flag --org. Both do now."
---

Found by following only the published docs, as a stranger, to stand up an organization with
Docker Compose on one machine, invite a second person and run a first hosted chat on the
scripted model. One product fault is written down as a known limit and not fixed here: a
guest's refused `--once` on a chat that is not running starts it for the owner in one-shot
mode, and the owner's window is then refused until the chat is stopped.
