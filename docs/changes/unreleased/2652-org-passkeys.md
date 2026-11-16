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
