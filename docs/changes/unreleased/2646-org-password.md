---
kind: added
title: an organization's people sign in with a username and password, and GitHub becomes the second way in
pr: 2646
surface: [factory, engine, docs]
invalidates:
  - "/v1/me's github_signed_in meant any signed-in person. It is now true only for a GitHub sign-in; a password person reads false with sign_in_kind password, and the engine's can_sign_out covers both."
  - "Joining a team server meant signing in with GitHub through an access link's device code. Now the operator link and invitations make a username + password account by default (POST /v1/factory-access verb sign_up); GitHub stays available where the server has a GitHub app. /v1/about's sign_in lists the ways, password first."
  - "factory.me's invite_available was true only when the server had GitHub. It is now true on any team server with a token store."
  - "A GitHub login could be any name. A GitHub sign-in whose login equals a password person's handle is now refused with the sentence \"That name belongs to someone who signs in here with a password\"."
  - "A session lasted 30 days from its last use with no ceiling. It now also ends 90 days after it was made."
  - "There was no way back in for a person who lost their credential short of a new invitation. The operator now makes a one-hour, single-use reset link (POST /v1/access/links kind reset, engine factory.link_create kind reset) for a password person."
---

Passwords are argon2id (OWASP parameters) in PHC strings, 12 characters to 256
bytes after NFKC, checked against about 486,000 common passwords compared by
their letters and digits. Every try is paid for before it is verified:
throttled per username and address with decaying backoff, per username
overall and per address. A machine that signed up, reset or signed in before
holds a device token (kept hashed on the server, revoked by a password change
or reset, surviving a restart) and is exempt from the per-username ceiling, so
a stranger guessing at a name cannot lock out the person's own machines; a new
machine during such an attack uses the operator's reset link, which is never
throttled. A device token is made only for a client that asks (want_device;
the engine asks only after the app's core says factory.sessions_store
can_keep true), a person holds at most 16 and none in use is pushed out. A
signed-in password change counts in one bucket per session, at any address,
not against the username's ceiling; a device token has its own ceiling of 100
across every address, past which its tries count like any other. POST /v1/me/password now answers 200 with the machine's new
device_token. Each
session is tied to the password it was made with. A username already named
anywhere on the server is taken, and an invitation made for one GitHub login
takes no password. The engine serves factory.access_sign_up, access_reset,
password_sign_in and password_change behind the hello word factory_password.
