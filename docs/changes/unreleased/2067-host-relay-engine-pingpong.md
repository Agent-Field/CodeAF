---
kind: fixed
title: codeaf chat --host no longer takes the engine away from a window that set CODEAF_SYNC_URL, and back
pr: 2067
surface: [engine, remote]
invalidates:
  - >-
    A window attaching to a running session host compared its whole resolved
    relay with the host's, so a launch with CODEAF_SYNC_URL unset (the ssh
    child of --host, or any shell that never exported it) counted as a
    different sync setting and retired an idle host or was refused by a busy
    one. Now only a relay the launch names itself is compared whole; a launch
    with the variable unset is compared only on the pairing's saved address
    (else the hosted default), which the host records as sync-unnamed.
  - >-
    The host's provider `environment` fingerprint included the resolved relay.
    It no longer does; the relay is judged only by SyncDiffers, and
    EnvironmentMAC (the automations clock) still includes it.
---

On a machine where a person ran codeaf with `CODEAF_SYNC_URL` pointing at a
self-hosted relay, `codeaf chat --host` from another machine ran `codeaf engine`
under an ssh shell without that variable. The engine read the silence as a
different relay and retired the host or refused to attach, and the local
window's next launch did the same in reverse. A terminal that explicitly names
a different address or `off` is still refused or restarts the engine exactly as
before.
