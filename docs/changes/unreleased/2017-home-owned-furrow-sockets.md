---
kind: fixed
title: Each codeaf home has its own private saving connection
pr: 2017
surface: [chat, engine, docs]
invalidates:
  - "Different codeaf homes on Windows could use the same saving daemon because its socket named only the engine build. The address now identifies both the home and build."
  - "A long home on Unix trusted a predictable temporary socket without checking its directory or owner. Short endpoints now use validated private directories and reject preclaimed or foreign endpoints before dialing or pruning."
  - "Windows could repeatedly fail every seal when no secure endpoint fit the address limit. Saved file history now turns off with one plain sentence and tools keep working."
  - "An unset HOME or the original ancestor of a symlinked home could bypass the home-folder policy. Both now seal only the chat's own folder."
---

Same-home clients keep sharing their saving daemon, also across an upgrade.
Unix keeps a fitting in-home address; a long home keeps the short temporary
address earlier engines used while it is this user's private directory, and only
a preclaimed one falls to the user's private runtime directory or other
validated private temporary directories. Windows falls back only under the user's own temporary
folder with a private ACL; final addresses stay below the platform byte limit.
Stale endpoints are pruned only while holding their daemon's lifetime lock,
inside the existing per-socket start gate. A lost answer is never replayed and
the next seal reconnects as before. The home-folder policy and its sentence stay
the same; ordinary project folders retain saved history and rewind.
