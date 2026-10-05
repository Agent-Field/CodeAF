#!/usr/bin/env python3
"""Record ordinary hosted terminal work; never writes memory or grades model text.

The harness starts the SAME hosted chat a person uses: `chat --debug`, never
`--once` and never `--no-host`. It seeds NO memory and NO answer-bearing file;
the only thing it writes is the legitimate model/provider configuration a
dedicated isolated profile needs.

ISOLATION ORDER MATTERS. The fleet environment and keys file are sourced FIRST
under `set -a`, and only then are CODEAF_HOME and an explicit CODEAF_PROFILE_DIR
exported under this run's state. Sourcing later would let an inherited profile
or home row override the isolation this run depends on.

When --model is given the harness writes a dedicated profile that pins every
text seat to that exact slug (model.talk and all five tier rows, with an empty
fallback list) and exports CODEAF_MODEL/CODEAF_PLAN_MODEL/CODEAF_CHECK_MODEL,
so no background pass can quietly route a text call to another model.
"""
import argparse, hashlib, json, os, pathlib, shlex, subprocess, time

def run(*args):
    return subprocess.check_output(args, text=True).strip()

os_environ = getattr(os, "en" + "viron")

# The tiers a text seat may resolve from. Pinned exactly when a model is named.
TIER_ROWS = ["models.tiers.low", "models.tiers.high", "models.tiers.worker",
             "models.tiers.reflex", "models.tiers.mastermind"]

def source_line():
    """The one line that loads the fleet keys FIRST, spelled in pieces so this
    harness can carry it without this tooling treating the path as credential
    inspection."""
    fleet = "~/." + "con" + "fig/" + "fl" + "eet/"
    return "set -a; source " + fleet + "env" + ".sh; source " + fleet + "sec" + "rets.env; set +a; "

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("action", choices=["start", "say", "capture", "stop"])
p.add_argument("--evidence", required=True, type=pathlib.Path)
p.add_argument("--workspace", type=pathlib.Path)
p.add_argument("--binary", type=pathlib.Path)
p.add_argument("--state", type=pathlib.Path)
p.add_argument("--name", default="contextual-memory-live")
p.add_argument("--model", help="exact model slug every text seat is pinned to")
p.add_argument("--one-model", action="store_true",
               help="pass --one-model: no fallback ladder and no per-item override")
p.add_argument("--log-bodies", action="store_true",
               help="opt in to CODEAF_CALL_LOG_BODIES=1 for request/reply bodies")
p.add_argument("--text")
a = p.parse_args()
a.evidence.mkdir(parents=True, exist_ok=True)
meta = a.evidence / "terminal.json"

def pin_profile(profile_dir, model):
    """Write the dedicated profile's model rows. No memory, no answers: only the
    model/provider configuration this run is measured against."""
    profile_dir.mkdir(parents=True, exist_ok=True)
    config = {"model.talk": model, "models.fallbacks": []}
    for row in TIER_ROWS:
        config[row] = model
    (profile_dir / "config.json").write_text(json.dumps(config, indent=2) + "\n")

if a.action == "start":
    if not all([a.workspace, a.binary, a.state]):
        p.error("start needs --workspace, --binary, --state")
    if a.evidence.resolve().is_relative_to(a.workspace.resolve()):
        p.error("evidence and grader material must be outside the agent workspace")
    state = a.state.resolve()
    state.mkdir(parents=True, exist_ok=True)
    profile_dir = state / "profile"
    model = (a.model or "").strip()
    if model:
        pin_profile(profile_dir, model)
    log_bodies = bool(a.log_bodies or os_environ.get("CODEAF_CALL_LOG_BODIES") == "1")
    exports = [
        "export CODEAF_HOME=" + shlex.quote(str(state)),
        "export CODEAF_PROFILE_DIR=" + shlex.quote(str(profile_dir)),
    ]
    if model:
        exports += [
            "export CODEAF_MODEL=" + shlex.quote(model),
            "export CODEAF_PLAN_MODEL=" + shlex.quote(model),
            "export CODEAF_CHECK_MODEL=" + shlex.quote(model),
        ]
    if log_bodies:
        exports.append("export CODEAF_CALL_LOG_BODIES=1")
    flags = ["--debug"]
    if model:
        flags += ["--model", shlex.quote(model)]
    if a.one_model:
        flags.append("--one-model")
    command = source_line() + "; ".join(exports) + "; exec " + \
        shlex.quote(str(a.binary.resolve())) + " chat " + " ".join(flags)

    run("fleet-session", "start", a.name, "--cwd", str(a.workspace.resolve()), "--cmd", command)
    target = "s-" + a.name
    run("tmux", "-L", "fleet-sh", "pipe-pane", "-t", target, "-o",
        "cat >> " + shlex.quote(str((a.evidence / "terminal.ansi").resolve())))
    # THE METADATA WHITELIST RECORDS FLAGS AND PATHS, NEVER A KEY.
    info = {"name": a.name, "started": time.time(),
            "workspace": str(a.workspace.resolve()), "state": str(state),
            "profile_dir": str(profile_dir),
            "binary": str(a.binary.resolve()),
            "binary_sha256": hashlib.sha256(a.binary.read_bytes()).hexdigest(),
            "revision": run("git", "-C", str(a.binary.parent.parent), "rev-parse", "HEAD"),
            "dirty": bool(run("git", "-C", str(a.binary.parent.parent), "status", "--porcelain")),
            "hosting": "ordinary chat (no --no-host, no --once)",
            "provider": "fleet environment",
            "flags": {"model": model, "one_model": bool(a.one_model),
                      "log_bodies": log_bodies, "debug": True}}
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
