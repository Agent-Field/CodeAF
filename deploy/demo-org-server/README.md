# The teams-demo organization server

One `codeaf factory serve` on a small GCP VM, for recording two desks working
together as two people. It is a demo server, not a product deployment: one
e2-small VM, Debian 12, the server's state on the VM's 20 GB disk, Caddy in
front serving HTTPS on `<static-ip>.sslip.io` with a Let's Encrypt certificate.
Only 80, 443 and 22 are open; the server itself listens on `127.0.0.1:8787`.

```sh
scripts/demo-org-server-deploy.sh                 # first time: creates everything, then deploys origin/work/teams
scripts/demo-org-server-deploy.sh origin/<branch> # any later update: rebuilds from that ref and restarts
scripts/demo-org-server-teardown.sh               # after the demo: deletes the VM, disk, address and firewall rules
```

`GCP_PROJECT` (default `openaf-505800`) and `GCP_ZONE` (default `us-central1-a`)
choose where. A redeploy keeps the server's state and its access token, so
nobody is signed out by an update.

## Canvases

The organization's canvases are kept by canvas-core's `canvas` program, installed
beside codeaf as `/usr/local/bin/canvas`. The deploy builds it as a static
linux/amd64 binary from the desk repository (`DESK_REPO`, default `~/af-desk-rail`,
at `DESK_REF`, default `origin/work/rail-personal`), through `cargo zigbuild` on a
machine that is not linux/amd64 (`cargo install cargo-zigbuild` and a `zig` on PATH),
or takes one already built from `CANVAS_BIN`. Without it the server starts, logs
`this organization keeps no canvases`, and leaves the canvas routes off.

## Claiming it and joining it

1. The operator link is printed in the service log at each start until somebody
   claims it (each restart replaces the unclaimed one, so take the last):
   `gcloud --project openaf-505800 compute ssh codeaf-teams-demo --zone us-central1-a --command "sudo journalctl -u codeaf-org --no-pager | grep 'operator access link' | tail -1"`
2. The operator opens that `codeaf://factory/join?operator=true…` link on their
   Mac (or pastes it into the desk's **Join an organization…** box) and signs in
   with GitHub in the browser. That claims the server.
3. The operator invites the second person from the organization's People page;
   that person pastes the invite link into **Join an organization…** on their
   own Mac and signs in with GitHub.

Sign-in is GitHub's device flow through the CodeAF OAuth app
(`github_client_id: Ov23liZE8n5kPnDn5RcK`); it needs no client secret.
