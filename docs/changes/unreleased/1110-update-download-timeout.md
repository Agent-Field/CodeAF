---
kind: fixed
title: a release download finishes over a real network, on its own clock
pr: 1110
surface: [chat, build]
invalidates:
  - "#1060's `/update` and `codeaf update` were believed to install a release over any working network. They could not finish one at all: both doors built the release client with the three-second budget meant for the launch check, and Go's `http.Client.Timeout` covers the whole exchange including every body byte, so the 57,532,578-byte `codeaf-linux-amd64` — about eight seconds on a healthy link — was cut off three seconds in with `read the release response: context deadline exceeded`. `--check` passed everywhere because its answer is a few KB, and every hand test served the asset from a local mirror in milliseconds."
  - "The release client no longer sets `http.Client.Timeout` at all, and there is no longer one clock over the whole road. Its transport bounds only what has made no progress — a five-second dial, a five-second TLS handshake, a ten-second wait for response headers — and every call takes its deadline from its context: the launch check and `codeaf update --check` keep a three-second whole-exchange deadline from the single `update.CheckTimeout` constant that both doors read, the release metadata an install reads gets ten seconds per request, and the asset download gets a thirty-second no-byte stall window plus a fifteen-minute ceiling. A dead link still fails in seconds; a slow one now succeeds."
  - "A failed update could hand a person Go's own deadline text. It cannot now: a stall says `downloading codeaf-linux-amd64 stalled — no bytes for 30 s`, the ceiling says `downloading codeaf-linux-amd64 took longer than 15 minutes`, late headers say `codeaf-linux-amd64 did not start arriving within 10 s`, and a release request that does not answer names the release API and its window. The terminal road still ends with the curl line and the chat still offers it."
---

A contract item about time has to be proved with a slow server. Every hand test
behind #1060 downloaded the asset from a local mirror, which serves 57 MB in
milliseconds, so the one clock that mattered was the one nothing ever ran. The
tests here inject the stall window and the ceiling and then use real slow,
silent and trickling servers, and the two roads were walked again against the
published v0.2.0 release.
