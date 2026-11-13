---
kind: added
title: Scoped clients keep sealed slots and a signed queue
pr: 1928
surface: [remote, docs]
invalidates:
  - "The relay manual said the hosted Worker does not support scoped clients. The Go relay and the hosted Worker both register a client device, store its sealed slots, and accept a signed queue."
  - "A device on the relay was a full device. A full device can now register a scoped client that cannot read the directory, the blob store, or another client's slots and queue."
---

Registration happens before the certificate is released. The relay cannot read sealed slots or calls. An executor must verify the retained request proof before it runs one.
