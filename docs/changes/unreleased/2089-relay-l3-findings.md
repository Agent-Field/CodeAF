---
kind: fixed
title: Continuing opens the chat you picked, a refused continue keeps its card, and a slow relay says so
pr: 2089
surface: [chat, remote, docs]
invalidates:
  - "A bare codeaf chat that was continuing the newest chat another computer let go of answered every explicit continue on Home with a chat is already continuing here, and the older chat opened; now a confirmed different chat waits, the automatic move finishes and is let go without opening, and the chosen chat opens. The busy line now answers only a second pick during a continue the person started."
  - "A continue refused because the chat's holder changed showed another device continued this chat first in the footer only; every such refusal now keeps the 1 retry / 2 leave it there card and the draft, and when the holder named on the card simply reopened the chat the card asks again instead of blaming another device."
  - "Home gave up on the chat listing after 5 seconds and said other machines did not answer in time; it now waits up to 25 seconds, says waiting for <relay host>; still checking after 5, keeps its rows, and still shows a real outage at once."
  - "The first message sent right after a move could be left in the box under Home's timeout sentence; a send right after this window's own move now waits up to 30 seconds for the arriving chat to be ready and sends once, and a send that cannot leave says the message is still in the box and names the relay."
  - "A self-hosted codeaf relay counted the first X-Forwarded-For address for its registration and dial limits whenever a request sent one, so a relay facing the internet could be given any address; forwarding headers now count only with --trust-proxy, for registration, dial and pairing limits alike, and a relay behind a proxy must pass it or every caller shares one allowance."
  - "A chat this computer had never held came over serial object batches whenever the shared store had exchanged any other chat with the relay; it now downloads its frames, and a chat this computer already holds still fetches only what changed."
---

Found by the two-network test of the relay (lane L3). Each continue also writes
one `chat.log` line with the time spent in each phase, so a slow move can be read
instead of guessed. Consent to continue still binds the holder and its ownership
number shown on the card; nothing about ownership rules, wire or stored formats,
or the hosted relay changed.
