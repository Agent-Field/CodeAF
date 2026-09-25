---
kind: fixed
title: worker commits carry attribution when a run ends
pr: 1495
surface: [chat, engine]
invalidates:
  - "On the worker harness, a commit a worker made itself carried no attribution unless the worker added it. A run now signs its private worker commits at landing with the bare Assisted-by line, and codeaf do signs its workers' commits in place when the run ends."
  - "The harness signed its own landing commit even when CONTRIBUTING forbade AI trailers. It now reads regular CONTRIBUTING files in the repository root, .github and docs, and adds no lines to its landing or worker commits when one forbids them."
  - "A run whose worker committed everything answered nothing to land while its work merged home. It now names the branch and files the worker committed; a read-only run still answers nothing to land."
---

Worker commits keep their original author and committer. Commits signed with Git's own signature, already pointed at by another branch, tag or remote-tracking ref, or fetched from elsewhere and merged are left as the worker wrote them. In a repository with no prior commits, `codeaf do` signs the worker's first commits too. A run that commits and then reverts its work still reports the touched files.
