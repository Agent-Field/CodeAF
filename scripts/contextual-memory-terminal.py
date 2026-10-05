#!/usr/bin/env python3
"""Record ordinary hosted terminal work; never writes memory or grades model text."""
import argparse, hashlib, json, os, pathlib, shlex, subprocess, time

def run(*args):
    return subprocess.check_output(args, text=True).strip()

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("action", choices=["start", "say", "capture", "stop"])
p.add_argument("--evidence", required=True, type=pathlib.Path)
p.add_argument("--workspace", type=pathlib.Path)
p.add_argument("--binary", type=pathlib.Path)
p.add_argument("--state", type=pathlib.Path)
p.add_argument("--name", default="contextual-memory-live")
p.add_argument("--text")
a = p.parse_args()
a.evidence.mkdir(parents=True, exist_ok=True)
meta = a.evidence / "terminal.json"
if a.action == "start":
    if not all([a.workspace, a.binary, a.state]):
        p.error("start needs --workspace, --binary, --state")
    if a.evidence.resolve().is_relative_to(a.workspace.resolve()):
        p.error("evidence and grader material must be outside the agent workspace")
    a.state.mkdir(parents=True, exist_ok=True)
    command = " ".join([
        "export CODEAF_HOME=" + shlex.quote(str(a.state.resolve())),
        "; source ~/.config/fleet/env.sh; source ~/.config/fleet/secrets.env;",
        "exec", shlex.quote(str(a.binary.resolve())), "chat --debug",
    ])
    run("fleet-session", "start", a.name, "--cwd", str(a.workspace.resolve()), "--cmd", command)
    target = "s-" + a.name
    run("tmux", "-L", "fleet-sh", "pipe-pane", "-t", target, "-o",
        "cat >> " + shlex.quote(str((a.evidence / "terminal.ansi").resolve())))
    info = {"name": a.name, "started": time.time(), "workspace": str(a.workspace.resolve()),
            "state": str(a.state.resolve()), "binary": str(a.binary.resolve()),
            "binary_sha256": hashlib.sha256(a.binary.read_bytes()).hexdigest(),
            "revision": run("git", "-C", str(a.binary.parent.parent), "rev-parse", "HEAD"),
            "dirty": bool(run("git", "-C", str(a.binary.parent.parent), "status", "--porcelain")),
            "hosting": "ordinary chat (no --no-host, no --once)", "provider": "fleet environment"}
    meta.write_text(json.dumps(info, indent=2) + "\n")
else:
    info = json.loads(meta.read_text())
    target = "s-" + info["name"]
    if a.action == "say":
        if not a.text:
            p.error("say needs --text")
        run("tmux", "-L", "fleet-sh", "send-keys", "-t", target, "-l", a.text)
        run("tmux", "-L", "fleet-sh", "send-keys", "-t", target, "Enter")
        with (a.evidence / "user-inputs.jsonl").open("a") as f:
            f.write(json.dumps({"time": time.time(), "text": a.text}) + "\n")
    elif a.action == "capture":
        screen = run("tmux", "-L", "fleet-sh", "capture-pane", "-t", target, "-p", "-S", "-2000")
        (a.evidence / (str(time.time_ns()) + ".screen.txt")).write_text(screen + "\n")
        print(screen)
    else:
        run("tmux", "-L", "fleet-sh", "send-keys", "-t", target, "C-c")
        info["stopped"] = time.time()
        meta.write_text(json.dumps(info, indent=2) + "\n")
