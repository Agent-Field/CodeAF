---
kind: added
title: The team factory serves HTTPS, with an automatic certificate or behind a proxy
pr: 1800
surface: [engine, docs]
invalidates:
  - "codeaf factory serve spoke plain HTTP only, so a team factory was safe only on a private network. It now serves HTTPS off loopback: tls: auto (a Let's Encrypt certificate for domain), tls: files (your own PEM pair, reloaded on change) or tls: off (behind a reverse proxy)."
  - "A host config with a listen address that is not loopback and no tls or domain key started on plain HTTP. It now refuses to start, naming the fix: set domain, set tls: files with cert_file and key_file, or set tls: off to keep plain HTTP behind a proxy."
  - "Any request from a loopback peer was the owner's open floor, whatever headers it carried. A loopback request with X-Forwarded-For, X-Forwarded-Proto, X-Real-IP or Forwarded from a peer outside trusted_proxies now needs a token or sign-in, so a proxy on the same machine cannot open the floor."
  - "When the factory serves HTTPS itself, serve.json names a separate plain-HTTP loopback listener rather than the public one, because the certificate names the domain and never 127.0.0.1."
---

New host config keys `tls`, `domain`, `cert_file`, `key_file`, `cache_dir` and
`trusted_proxies`. Over HTTPS every response carries HSTS, the session cookie is
Secure (it is always HttpOnly and SameSite=Strict), port 80 redirects to HTTPS
except the certificate challenge, and `GET /v1/about` gains `scheme`. Forwarded
headers are believed only from `trusted_proxies`. `/v1/events` sends
`X-Accel-Buffering: no` so proxies pass events straight through.
`codeaf factory serve` prints the `https://` address, and warns when it serves
plain HTTP off loopback. docs/design/factory/HTTPS.md has Caddy and nginx examples.
