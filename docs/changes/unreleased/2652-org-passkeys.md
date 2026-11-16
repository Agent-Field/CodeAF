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
  - "A passkey poll with the verifier alone answered the session. It now also needs the one-time code the page hands to the starting engine's 127.0.0.1 listener (return_port, return_path and return_key in start, code in poll; the code carries an HMAC under return_key so the listener takes only its own flow's); a finished ceremony without the code polls as \"elsewhere\" and never completes, so a sign-in address opened on another device signs nobody in. Joins and adds write the account only at that poll."
  - "A wrong verifier on poll or cancel ended the flow. It is now counted, and the fifth ends it; cancel also refuses plain HTTP off loopback."
  - "Adding a passkey, setting a first password and removing a passkey needed only a session. They now each need a confirmation made for that one action (purpose \"confirm\" with action add_passkey, remove_passkey + passkey, or set_password; or POST /v1/me/password/confirm with the same action, which is factory.password_confirm); it lets the action happen once within five minutes. A sign-in is never one, and there is no session-wide window. Without it the server answers 403 step_up_required with action, and the engine step_up with detail {action, passkey?}."
  - "A ceremony page's JSON answered any client with the flow id. A flow is now bound to the first browser that asks it for a ceremony, by that browser's id in the __Host-codeaf_pkb cookie (HttpOnly, Secure, SameSite=Strict, Path=/, no Domain, so no sibling or plain-HTTP host can plant it); options, finish and the hand-over refuse any other with reason other_browser. The finish answer carries no code: it names /passkey/<flow>/go, which the bound browser follows once to a 303 onto the engine's loopback."
  - "Fetching a ceremony page marked a sign-in flow opened. Only a page-origin options call does now, and opened sign-ins are capped per IPv4 /24 or IPv6 /48 (the oldest dropped)."
  - "Passkey starts were limited only in total. Pending flows are now capped per address (IPv6 /64) and starts per /48, sign-ins get at most three quarters of the room, and an unopened sign-in page ends after two minutes."
  - "HSTS never carried includeSubDomains. It now does when the operator sets hsts_include_subdomains: true in the host config (off by default; the passkey cookie is __Host- either way), and never for a shared wildcard name (sslip.io, nip.io and the like), an IP address or localhost. Such a shared name is for demos only."
  - "A finished flow's code worked until the flow expired. It now works for 60 s after the passkey was used, and the engine's loopback answers it with a 303 to a clean /done."
  - "A full table of opened sign-ins could not be made room in. It now drops one from the network (/24, or /32 for IPv6) holding the most, oldest flow first, but only when that network holds more than 4 and more than the newcomer's own network plus one; otherwise the newcomer is refused with 429. A flood from a few networks yields room; a flood spread one per network never drains an office's few."
  - "A confirmation's page showed the engine's computer name bare (\"requested by codeaf on …\"). It now reads \"requested by a computer named “…”, signed in <date> from <address>\": the name is quoted as a claim, beside the session's sign-in date and the address the server saw."
  - "Any confirmation was kept, up to a table of 4096, and a full table dropped new ones. A session now holds at most 3, a fourth replacing its oldest, and a full table drops the oldest anyone holds. A password confirm for removing a passkey the person does not have is refused with 404, as the passkey confirm is."
  - "An add or confirm that carried a link, username or name answered 404 \"no passkeys\". It now answers 422 (bad_input), which is what it is. localhost and *.localhost are documented as one-machine trial addresses only, since browsers let a plain-HTTP page on them set cookies."
  - "A display name, a passkey's name and the computer named on a confirm page could carry bidi overrides, isolates, zero-width or C1 characters, which could reorder or hide the server's own words. Format characters are now dropped and a control character refuses a display name; the session's device label is cleaned the same way, and the confirm page sets each name in its own <bdi>."
  - "Confirmations are also capped at 3 per account across its sessions, the oldest giving way. When the busiest network in a full table is an IPv6 /32, the sign-in it gives up comes from its busiest /48, so a flood from the same provider drains itself first. The manual states that a sustained flood from hundreds of networks can refuse new passkey sign-ins while it lasts; password sign-in is unaffected."
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
