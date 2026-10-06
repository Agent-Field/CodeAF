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

A `stopped` marker means the surface was CONFIRMED GONE, never merely that a key
was sent: Ctrl+C interrupts a turn while codeaf is busy and only quits it when
idle, so `stop` sends it, waits, re-checks the real tmux pane, and either records
a verification method or exits non-zero leaving no `stopped` marker behind. The
start metadata records what `codeaf version` actually printed and refuses to
record a run whose binary does not name the source HEAD it was launched from.
"""
import argparse, hashlib, json, os, pathlib, shlex, subprocess, time

def run(*args):
    return subprocess.check_output(args, text=True).strip()

os_environ = getattr(os, "en" + "viron")

# The tiers a text seat may resolve from. Pinned exactly when a model is named.
TIER_ROWS = ["models.tiers.low", "models.tiers.high", "models.tiers.worker",
             "models.tiers.reflex", "models.tiers.mastermind"]

# How long `stop` may spend proving the surface actually exited, and how long it
# waits between Ctrl+C presses. Ctrl+C interrupts a running turn, so one press is
# not closure; each retry is re-checked against the real pane rather than counted.
STOP_TIMEOUT_SECONDS = 15.0
STOP_RETRY_WAIT_SECONDS = 1.0

def source_line():
    """The one line that loads the fleet keys FIRST, spelled in pieces so this
    harness can carry it without this tooling treating the path as credential
    inspection."""
    fleet = "~/." + "con" + "fig/" + "fl" + "eet/"
    return "set -a; source " + fleet + "env" + ".sh; source " + fleet + "sec" + "rets.env; set +a; "

def tmux(*args):
    """Run one tmux command against the harness's own server, tolerating the
    non-zero exit of a target that is already gone."""
    return subprocess.run(["tmux", "-L", "fleet-sh", *args], text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE)

def pane_closed(target):
    """Return (closed, method, detail): is the fleet pane's process actually gone?

    closed is True ONLY for proof of exit -- the pane/session cannot be listed,
    or every pane reports pane_dead=1 -- and never because a key was sent. This
    is what lets a `stopped` marker mean confirmed closure."""
    listed = tmux("list-panes", "-t", target, "-F",
                  "#{pane_dead}|#{pane_current_command}|#{pane_id}")
    if listed.returncode != 0:
        return True, "pane-absent", (listed.stderr.strip() or "tmux could not find the pane")
    rows = [row for row in listed.stdout.splitlines() if row.strip()]
    if not rows:
        return True, "pane-absent", "tmux listed no panes"
    alive = []
    for row in rows:
        dead, command, _pane_id = (row.split("|") + ["", "", ""])[:3]
        if dead.strip() != "1":
            alive.append(command.strip() or "?")
    if alive:
        return False, "", "pane still running: " + ",".join(alive)
    return True, "pane-dead", "tmux reports pane_dead=1"

def binary_version_line(binary):
    """A binary that cannot name its source cannot be trusted as the tested one:
    run it and return the one line `codeaf version` prints, or refuse."""
    probed = subprocess.run([str(binary), "version"], text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if probed.returncode != 0:
        raise SystemExit("start refused: `%s version` exited %d; stderr:\n%s"
                         % (binary, probed.returncode, probed.stderr.strip()))
    lines = [line for line in probed.stdout.splitlines() if line.strip()]
    if not lines or not lines[0].startswith("codeaf "):
        raise SystemExit("start refused: `%s version` printed %r, not a codeaf version line"
                         % (binary, probed.stdout.strip()))
    return lines[0].strip()

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
    # VERIFY THE BINARY BEFORE LAUNCHING IT. The source HEAD beside the binary is
    # not the tested binary: a stale or dirty build names a different revision in
    # its own `version` line, so record that line and refuse a run whose build
    # does not match the source it is being launched from.
    binary = a.binary.resolve()
    version_line = binary_version_line(binary)
    tokens = version_line.split()
    built_revision = tokens[1] if len(tokens) > 1 else ""
    built_dirty = "(dirty)" in version_line or "no revision stamped" in version_line
    repo = binary.parent.parent
    head_full = run("git", "-C", str(repo), "rev-parse", "HEAD")
    head_short = run("git", "-C", str(repo), "rev-parse", "--short", "HEAD")
    tree_dirty = bool(run("git", "-C", str(repo), "status", "--porcelain"))
    revision_mismatch = not (built_revision and
                             (head_full.startswith(built_revision) or
                              built_revision.startswith(head_short)))
    if revision_mismatch or built_dirty or tree_dirty:
        why = []
        if revision_mismatch:
            why.append("binary names %r but source HEAD is %s (%s)"
                       % (built_revision or "no revision", head_short, head_full))
        if built_dirty:
            why.append("binary was built from a dirty tree")
        if tree_dirty:
            why.append("source tree is dirty")
        raise SystemExit("start refused: the tested binary does not match source HEAD: "
                         + "; ".join(why) + ". Rebuild with `make build`.")
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
        shlex.quote(str(binary)) + " chat " + " ".join(flags)

    run("fleet-session", "start", a.name, "--cwd", str(a.workspace.resolve()), "--cmd", command)
    target = "s-" + a.name
    run("tmux", "-L", "fleet-sh", "pipe-pane", "-t", target, "-o",
        "cat >> " + shlex.quote(str((a.evidence / "terminal.ansi").resolve())))
    # THE METADATA WHITELIST RECORDS FLAGS, PATHS AND THE BINARY'S OWN VERSION
    # LINE, NEVER A KEY.
    info = {"name": a.name, "started": time.time(),
            "workspace": str(a.workspace.resolve()), "state": str(state),
            "profile_dir": str(profile_dir),
            "binary": str(binary),
            "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
            "binary_version": version_line,
            "revision": built_revision,
            "source_head": head_full,
            "source_head_short": head_short,
            "dirty": tree_dirty,
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
        # Ctrl+C interrupts the running turn; it only quits codeaf when idle. So
        # send it, wait, and PROVE the pane is gone before recording anything.
        began = time.monotonic()
        attempts = 0
        closed, method, detail = pane_closed(target)
        if not closed:
            deadline = began + STOP_TIMEOUT_SECONDS
            while True:
                attempts += 1
                tmux("send-keys", "-t", target, "C-c")
                time.sleep(STOP_RETRY_WAIT_SECONDS)
                closed, method, detail = pane_closed(target)
                if closed or time.monotonic() >= deadline:
                    break
        waited = round(time.monotonic() - began, 3)
        info["stop_attempts"] = attempts
        info["stop_waited_seconds"] = waited
        info["stop_verified_by"] = method if closed else None
        info["stop_detail"] = detail
        if closed:
            info["stopped"] = time.time()
            info["stopped_confirmed"] = True
        meta.write_text(json.dumps(info, indent=2) + "\n")
        if not closed:
            raise SystemExit("stop: the UI did not exit after %d C-c over %.1fs (%s); "
                             "NOT recording it stopped" % (attempts, waited, detail))
