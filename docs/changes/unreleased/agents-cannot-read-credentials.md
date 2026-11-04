---
kind: security
title: Agents cannot read credentials in any approval mode
surface: [engine, app]
invalidates:
  - "The conversation could run gh auth token and read credential files under the default allow; only a task was refused. Every agent is now refused, in every approval mode, and only a person's own ! command is exempt."
  - "Agent shells inherited GH_TOKEN, GITHUB_TOKEN and other variables named like credentials, though provider keys were stripped. They are now removed too, and CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL keeps both."
  - "The watch tool's commands and principal audit checks inherited the whole environment. They now get the same environment as any other agent shell."
---
