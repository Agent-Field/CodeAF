---
kind: changed
title: Access-link checks and polls go by POST when the factory advertises it
pr: 0
surface: [engine]
invalidates:
  - "Every access-link check was GET /v1/factory-access?link=FULL_INVITE, so the invitation secret sat in the request URL and in proxy and access logs. A factory that advertises access_check_post:true in /v1/about is now checked and polled with POST {link, verb:\"check\"}, and the engine never falls back to GET after that."
  - "POST /v1/factory-access took only sign_in and cancel. It now also takes check, which only reads or polls and never starts a sign-in, mints a cookie or resets a flow."
  - "A factory whose valid /v1/about lacks access_check_post is still checked by GET with the link in the URL. That is the old risk, kept for older factories and not made secure; an unreadable or failed about is an error, not a reason to use GET."
---

Provisional entry; the pull request number is filled when it opens. The
capability is optional and `api.Contract` does not move.
