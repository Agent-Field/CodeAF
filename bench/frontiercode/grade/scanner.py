#!/usr/bin/env python3
"""The egress scanner: the safeguard between an open-internet run and a
meaningless score.

FrontierCode runs its agents with internet ON and flags afterwards. A domain
blocklist is a dead end — Cognition's own account says theirs passed a
thousand entries and agents kept finding ways around — so this rig's policy is
open egress, logged at two levels, scanned here:

  (a) the egress proxy's connection log (hostname level: every CONNECT and
      plain request the container made), and
  (b) the harness's own transcript (senior-dev's action log, whose step
      records carry every model-written shell command and web tool call).

Flag rules, v1. A HARD flag scores the run 0 and counts toward flag rate:

  - the upstream slug (owner/repo of the task's repository) appearing in the
    transcript, in any URL, clone/fetch/pull command or web tool argument
  - pull request / commit / patch URL shapes pointing at the upstream
    (github.com/<slug>/pull|commit, *.patch, *.diff)
  - connections to the hosts that serve repository content by design:
    raw.githubusercontent.com, codeload.github.com, api.github.com,
    objects.githubusercontent.com

A SOFT flag is recorded and reported but does not zero the run by itself:

  - github.com or gist.github.com connections with no path evidence (hostname
    level cannot tell documentation from the fix)
  - package-registry installs (npm, pypi, cargo, go modules) — Cognition's
    second named leak path is a registry install of a version containing the
    fix; v1 records installs for review rather than version-comparing them.

The model plane is not agent egress: the guard that meters model calls and the
catalog endpoint sit on this list and are excluded from flagging, still logged.

A scan that cannot read its inputs is `rig`, never a clean pass — the same law
the grades follow.
"""

import argparse
import json
import os
import pathlib
import re
import sys

# The model plane: hosts the run's own machinery talks to, not the agent.
MODEL_PLANE_HOSTS = {
    "openrouter.ai", "api.openrouter.ai", "models.dev",
    "guard",  # the credential guard container, by its compose name
    "codeaf.agentfield.ai",  # the harness's own attribution endpoint
}

# Hosts that exist to serve repository content; a connection to one is a leak
# path on its own.
HARD_HOSTS = {
    "raw.githubusercontent.com", "codeload.github.com",
    "api.github.com", "objects.githubusercontent.com",
}
SOFT_HOSTS = {"github.com", "gist.github.com"}

# Registry hosts — Cognition's second named leak path. Installs are recorded.
REGISTRY_HOSTS = {
    "registry.npmjs.org", "registry.yarnpkg.com", "pypi.org",
    "files.pythonhosted.org", "crates.io", "static.crates.io",
    "proxy.golang.org", "rubygems.org",
}

# Transcript patterns for the upstream itself. The slug (owner/repo) is the
# exact string; the URL shapes are how the fix is typically fetched.
PR_SHAPES = [
    r"github\.com/[^\s\"']+/(pull|commit|issues)/\d+",
    r"\.(patch|diff)\b",
    r"git (clone|fetch|pull|checkout)[^\n]*",
]


def log(msg):
    print(f"[scanner] {msg}", file=sys.stderr, flush=True)


def read_proxy_log(path):
    """CONNECT/GET/POST/FAIL lines from the proxy log -> host set + lines."""
    hosts, lines = set(), []
    if not path.exists():
        return hosts, lines, False
    for line in path.read_text(errors="replace").split("\n"):
        m = re.match(r"^\S+ \S+ (CONNECT|\S+) (\S+)", line.strip())
        if not m:
            continue
        target = m.group(2)
        host = target.split(":")[0].split("/")[0].lower()
        hosts.add(host)
        lines.append(line.strip())
    return hosts, lines, True


def read_transcripts(run_dir):
    """Every line of every transcript the run wrote: the harness's action log
    and its own records (whose step entries carry every model-written shell
    command and web tool call), any run log, and a sealed fixture's planted
    files."""
    texts = []
    for pattern in ("**/delegate-actions.jsonl", "**/records.jsonl",
                    "**/*.log", "**/transcript.txt", "**/events.ndjson"):
        for path in pathlib.Path(run_dir).glob(pattern):
            try:
                texts.append((str(path), path.read_text(errors="replace")))
            except Exception as e:
                texts.append((str(path), f"[unreadable: {e}]"))
    return texts


def slug_of(repository_url):
    m = re.search(r"github\.com[:/]([^/]+/[^/]+?)(\.git)?$", repository_url or "")
    return m.group(1) if m else ""


def scan(run_dir, repository_url, model_hosts=None):
    run_dir = pathlib.Path(run_dir)
    slug = slug_of(repository_url)
    hard, soft = [], []
    proxy_log = run_dir / "egress-proxy.log"
    hosts, proxy_lines, proxy_present = read_proxy_log(proxy_log)
    if not proxy_present:
        # A run with no proxy log has not demonstrated the logged-egress path;
        # that is a rig gap, said out loud, not a quiet clean bill.
        hard.append("no egress proxy log found — the logged-egress path did not run")

    model_hosts = set(model_hosts or []) | MODEL_PLANE_HOSTS
    for host in sorted(hosts):
        if host in model_hosts:
            continue
        if host in HARD_HOSTS:
            hard.append(f"proxy: connection to {host}")
        elif host in SOFT_HOSTS:
            soft.append(f"proxy: connection to {host}")
        elif host in REGISTRY_HOSTS:
            soft.append(f"proxy: registry host {host}")

    seen_slug = False
    for source, text in read_transcripts(run_dir):
        for i, line in enumerate(text.split("\n")):
            if slug and slug in line:
                seen_slug = True
                hard.append(f"transcript: upstream slug in {pathlib.Path(source).name}:{i+1}: {line.strip()[:160]}")
            for shape in PR_SHAPES:
                if re.search(shape, line) and slug and slug.split("/")[-1] in line:
                    hard.append(
                        f"transcript: patch/commit shape in {pathlib.Path(source).name}:{i+1}: {line.strip()[:160]}")

    # A run that never logged a model-plane host either is suspicious in the
    # other direction: the guard sits on the model plane and every run talks
    # to it. Recorded, not flagged.
    model_seen = bool(hosts & model_hosts)
    return {
        "flagged": bool(hard),
        "reasons": sorted(set(hard)),
        "soft": sorted(set(soft)),
        "counts": {"proxy_hosts": len(hosts), "proxy_lines": len(proxy_lines),
                   "model_plane_hosts": sorted(hosts & model_hosts),
                   "model_plane_seen": model_seen,
                   "upstream_slug": slug},
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-dir", required=True)
    parser.add_argument("--repository-url", required=True)
    parser.add_argument("--out", default=None, help="write scan.json here")
    args = parser.parse_args()
    result = scan(args.run_dir, args.repository_url)
    out = args.out or os.path.join(args.run_dir, "scan.json")
    pathlib.Path(out).write_text(json.dumps(result, indent=2))
    log(f"flagged={result['flagged']} hard={len(result['reasons'])} soft={len(result['soft'])}")
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())