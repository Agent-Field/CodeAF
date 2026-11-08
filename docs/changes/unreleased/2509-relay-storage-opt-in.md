---
kind: changed
title: Hosted sync storage is off until an account is approved
pr: 2509
surface: [chat, engine, remote, docs]
invalidates:
  - "Pairing two computers through the hosted relay made it store chats, files, saved keys, memories, teams and task copies automatically; it now stores nothing for an account until that account is approved, while pairing, the device list, presence and live --at keep working."
  - "Every hosted relay write was accepted up to the account's 5 GiB limit; a write from an account that is not approved is now refused with 403 storage_off before anything is stored, and frames stored before this change stay readable."
  - "There was no way to grant or withdraw hosted storage per account; codeaf-relay-admin approve and revoke send an operator-signed request to POST /v1/admin/storage, checked against CAF_ADMIN_KEYS, and CAF_LIMITS storageOpen opens storage for everyone."
  - "Rotating an identity needed nothing from the hosted relay beyond the freeze; the freeze bytes are unchanged, and a relay that reports storage also takes a separate POST /v1/store/handoff from the freezing device so an approved account keeps storage."
  - "On the default engine road, sync refusal sentences for storage full, removed, replaced, clock skew and displacement stayed in the engine process and never reached the window; they now reach every attached window of that chat."
  - "Reopening local turns under this device's expired hold with an empty recorded head opened a viewer; it now reacquires the chat and publishes those turns when storage is on, including after approval."
---
The privacy terms for the hosted relay are not finished, so until they are it
keeps nobody's chats or files. A person asks for storage from Settings →
Devices → sync storage, which opens a pre-filled GitHub issue carrying their
request code, and an operator approves the account with `codeaf-relay-admin`.
The app learns of the approval by itself within about a minute of its next check
and starts uploading without a restart. While storage is off, `/status` says
`sync storage  off`, chats keep saving on the computer, and the app makes no store
writes. A self-hosted relay is unaffected and needs no request.
