---
kind: added
title: A Docker Compose to host a codeaf organization yourself, in deploy/org
pr: 2894
surface: [factory, build, docs]
invalidates:
  - "Was true: the only deployment recipe in the repository for an organization server was deploy/demo-org-server, one VM with a systemd unit, and the AgentField control plane, SWE-AF and PR-AF had to be started by hand beside it. Is true: deploy/org holds a Docker Compose that runs the organization server, the control plane, a factory runner, SWE-AF and PR-AF together, each with its own volume and health check, from pinned artifacts."
---

`deploy/org/README.md` is the walkthrough: `./setup.sh`, `docker compose up -d --build`, `./operator-link.sh`, and a keyless
`./smoke.sh` that proves the stack against a scripted model. The same Dockerfiles back a Railway project.
