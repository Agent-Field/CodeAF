---
kind: fixed
title: Provider HTTP clients refuse redirects away from the configured origin
pr: 1896
surface: [chat, engine]
invalidates: [A provider or custom endpoint may answer an API call with a redirect to another port or a subdomain of its own host and the request — configured key included — follows it there.]
---

The HTTP clients that carry the configured provider key were built with no
CheckRedirect, so Go's default policy forwarded Authorization to a redirect
that stayed on the same hostname, including a different port, or moved to a
subdomain of it. Every key-carrying client now shares one redirect rule:
follow a redirect only when the target keeps the original request's scheme,
hostname and effective port, and refuse any other redirect with an error
naming the target host. The key is never sent off-origin, and it is never
silently stripped and sent onward either — a stopped redirect stays visible as
an error instead of surfacing as a confusing 401 at a host the person never
configured. Same-origin redirects keep working.