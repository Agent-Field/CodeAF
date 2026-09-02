---
kind: fixed
title: the mint gate is taken under a deadline, so a mint refuses rather than waits
pr: 310
surface: [engine]
invalidates:
  - "`substore.Store.Mint` took its `.mint.lock` gate with a bare blocking `flock(LOCK_EX)` and could wait without bound — no longer true. The acquire is non-blocking, retried under a two-second `mintGateBound`, and refuses with the new `ErrMintBusy` sentinel."
  - "A mint had two outcomes to handle: it minted, or the claim was refused with `ErrExists` / a moved-on parent. There is now a third — `errors.Is(err, substore.ErrMintBusy)` means another mint held the gate and this one touched nothing, so it is safe to say so or to mint again."
---

#306 gave `Mint` a gate so two writers who read the store a moment apart cannot fork a
lineage, and took it with a blocking file lock that had no deadline — the shape that
silenced the wire for twenty-nine minutes in #264. `Mint` has no caller on a turn path
yet; the bound is here so the first one cannot inherit the freeze. Two seconds comes from
the store's own timing: twenty-four writers contending for one name drain in single-digit
milliseconds per round, and `TestConcurrentMintsNeverShareAVersion` now asserts that no
writer was ever refused by the bound. A gate that cannot be taken at all is unchanged —
the claim still runs with the exclusive create as its floor — but a gate somebody else is
holding is refused rather than run unlocked, which would reopen the fork #306 closed.
