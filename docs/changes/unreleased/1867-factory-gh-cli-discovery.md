---
kind: fixed
title: Find Homebrew gh when a native app starts with a minimal PATH
pr: 1867
surface: [engine]
invalidates: []
---

The GitHub CLI resolver prefers the inherited PATH, then checks standard Homebrew locations for gh. Factory setup, connection and credential reads share that resolver, without starting a login shell or changing the process PATH. Other programs retain their existing lookup behavior. Discovery alone makes no claim about the saved GitHub sign-in.
