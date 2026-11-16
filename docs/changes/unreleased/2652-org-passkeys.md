---
kind: added
title: an organization's people sign in with a passkey first, in their own browser
pr: 2652
surface: [factory, engine, docs]
invalidates:
  - "An organization's sign_in was [\"password\",\"github\"]. On a server reached over HTTPS at a DNS name it is now [\"passkey\",\"password\",\"github\"]; a server on plain HTTP or a bare IP address still lists no passkey, and its passkey routes answer 404."
  - "An account on the server always had a password. A person who joins with a passkey has none (accounts.json hash is empty) until they set one, and factory.password_change takes an empty current for that first password."
  - "An operator's reset link set a new password and left everything else. A reset (by password or by passkey) now replaces every way in: the old password and every passkey stop working."
  - "A link made for one GitHub login offered [\"github\"] after E9; it still does, and a passkey join through it is refused with reason github_link."
  - "Removing a passkey moves the account's generation, as a password change does: every other session of the person ends, and a ceremony finished before the removal signs nobody in at the poll."
  - "factory.me's sign_in_kind was \"password\" or \"github\". It is now \"passkey\" for a session made by a passkey sign-in or join, with github_signed_in false; factory.passkey_list's password says whether the person also has a password."
  - "A passkey poll with the verifier alone answered the session. It now also needs the one-time code the page hands to the starting engine's 127.0.0.1 listener (return_port and return_path in start, code in poll); a finished ceremony without the code polls as \"elsewhere\" and never completes, so a sign-in address opened on another device signs nobody in. Joins and adds write the account only at that poll."
  - "A wrong verifier on poll or cancel ended the flow. It is now counted, and the fifth ends it; cancel also refuses plain HTTP off loopback."
  - "Adding a passkey, setting a first password and removing a passkey needed only a session. They now need a proof in the last five minutes (a sign-in, purpose \"confirm\", or POST /v1/me/password/confirm, which is factory.password_confirm); without one the server answers 403 step_up_required and the engine step_up. An add by a person with passkeys asks for one of them first on the page."
  - "Passkey starts were limited only in total. Pending flows are now capped per address (IPv6 /64) and starts per /48, sign-ins get at most three quarters of the room, and an unopened sign-in page ends after two minutes."
  - "The relying party id kept an explicit default port (https://host:443); it is now stripped, so such an address still works."
---

Passkeys are WebAuthn via github.com/go-webauthn/webauthn v0.18.2 (BSD-3-Clause):
relying party id = the server's host, one origin, attestation none, resident key
and user verification required, authenticatorAttachment left open. The
ceremony runs on a page the server serves (/passkey/<flow>, strict CSP, no
third-party script) in the person's default browser; the engine starts a flow
with the SHA-256 of a verifier it keeps and polls with the verifier for the
session (/v1/auth/passkey/start, /poll, /cancel; /v1/me/passkeys). The engine
serves factory.passkey_start, passkey_poll, passkey_cancel, passkey_list and
passkey_remove behind the hello word factory_passkey.
