---
kind: fixed
title: Provider keys stay on their configured origin
pr: 1904
surface: [chat, engine]
invalidates:
  - "Video polling formerly sent the provider bearer to a foreign job URL. Foreign polling and downloads now omit it."
  - "Provider redirects formerly inherited the HTTP client's default forwarding rules. Redirects now stay on the original scheme, hostname and effective port."
---

Cross-origin redirects stop before the destination is contacted. The error names
the host and asks the user to check the service address; it does not expose
signed URL secrets or mislabel the refusal as an expired key. Same-origin
redirects, polling and downloads retain authentication.
