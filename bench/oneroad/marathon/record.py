#!/usr/bin/env python3
"""record.py — one SWE-Marathon cell's record.json, from the container's leavings.

Nothing here is read from a harness's own account of itself. The verdict is the
verifier's metrics.json; the road columns come from the v3 store the profile bind
mount left on the host; and the money is TOKENS × THE ONE OPENROUTER LIST TABLE
for all three arms, never a harness's self-reported dollars. A harness's own
figure is kept beside it as `native_usd` — for the record, not for the comparison.

Usage: record.py <cell> <arm> <task> <seed> <wall_s> <harness_exit> <verify_wall_s> <verify_exit>
Environment: MODEL IMAGE_REF IMAGE_ID TASK_COMMIT WORKDIR NEW_BIN CELL_SECONDS
"""
import csv, glob, hashlib, json, os, re, subprocess, sys

CELL, ARM, TASK, SEED, WALL, CODE, VWALL, VCODE = sys.argv[1:9]
ONEROAD = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LIB = os.path.join(ONEROAD, "lib")
sys.path.insert(0, LIB)

MODEL = os.environ.get("MODEL", "deepseek/deepseek-v4-flash")
WORKDIR = os.environ.get("WORKDIR", "/workspace/rust-java-lsp")
V = os.path.join(CELL, "logs", "verifier")


def load(path, default=None):
    try:
        return json.load(open(path))
    except (OSError, ValueError):
        return default


def read(path, default=""):
    try:
        return open(path).read().strip()
    except OSError:
        return default


# ── the verdict, in the verifier's own words ────────────────────────────────
metrics = load(os.path.join(V, "metrics.json"), {}) or {}
# test.sh's holdout merge REPLACES metrics.json with a summary that has no
# per_method in it, so verify.sh keeps the main corpus's complete file aside
# before the merge lands. Where both exist, both are recorded: the merged file is
# the score, the kept one is the breakdown.
main_metrics = load(os.path.join(V, "metrics_main.json"), {}) or {}
holdout_metrics = load(os.path.join(V, "metrics_holdout.json"), {}) or {}
reward_txt = read(os.path.join(V, "reward.txt"))

per_method = main_metrics.get("per_method") or metrics.get("per_method") or {}
if not per_method:
    # LAST RESORT, AND IT IS THE VERIFIER'S OWN OUTPUT EITHER WAY. score_golden.py
    # prints the per-method table to stdout before it writes the file, so a run
    # whose kept copy was missed can still be read out of the log it printed.
    block = read(os.path.join(CELL, "verify.log"))
    for m in re.finditer(r"^\s{2}(\S+)\s+(\d+)/(\d+)\s+\(", block, re.M):
        per_method.setdefault(m.group(1), {"passed": int(m.group(2)), "total": int(m.group(3))})

partial = metrics.get("partial_score")
reward = metrics.get("reward")
if reward is None and reward_txt:
    try:
        reward = float(reward_txt)
    except ValueError:
        reward = None

# A FAILING BUILD LEAVES NO metrics.json AT ALL, and that is the benchmark's own
# behaviour rather than a fault here: tests/test.sh runs under `set -euo pipefail`,
# so `cargo build --release 2>&1 | tail -20` failing exits the script before it
# reaches write_zero_metrics. reward.txt — 0.0, written at the very top — is all
# that survives, and it IS the verifier's verdict, so the score is zero and not
# unavailable. The reason is recorded rather than left for a reader to infer.
score_source = "verifier metrics.json"
if partial is None:
    if reward_txt:
        partial = 0.0
        log_tail = read(os.path.join(CELL, "verify.log"))[-4000:]
        score_source = ("no metrics.json: tests/test.sh exited on its own pipefail when "
                        "cargo build failed, and reward.txt says %s" % reward_txt
                        if "could not compile" in log_tail or "error[E" in log_tail
                        else "no metrics.json: tests/test.sh stopped before scoring; "
                             "reward.txt says %s" % reward_txt)
    else:
        score_source = "no metrics.json and no reward.txt — nothing was judged"
main_block = metrics.get("main") or main_metrics
holdout_block = metrics.get("holdout") or holdout_metrics

# ── did the agent leave anything behind? ────────────────────────────────────
# This task has no git repository, so there is no diff to measure. The image
# ships exactly one file in the working directory (run_tests.sh); anything above
# that is the agent's own work, and verify.sh listed it at the moment of
# judgement.
workspace_files = read(os.path.join(CELL, "logs", "oneroad_workspace_count.txt"), "0")
try:
    workspace_files = int(workspace_files)
except ValueError:
    workspace_files = 0
landed_work = workspace_files > 1

# ── the outcome, and the wall is not a verdict ──────────────────────────────
#
# A cell that hit the wall was recorded DNF, and DNF reads as "did not finish".
# On the SWE track that once hid a pass: the work landed, the verifier scored it,
# and only the runner's clock ran out afterwards. So the wall is recorded as a
# fact about the CLOCK and the verdict is left to the evidence.
raw_outcome = read(os.path.join(CELL, "outcome"), "UNKNOWN")
if not metrics and reward is None:
    # The verifier produced nothing at all — no score exists, and a zero here
    # would read as "the attempt failed" about a judgement that never happened.
    outcome, settle = "INVALID", "verifier produced no metrics.json (exit %s)" % VCODE
elif raw_outcome == "DNF":
    if landed_work:
        outcome = "OK(wall)"
        settle = "wall reached, but work landed — the clock ran out, the attempt did not"
    else:
        outcome, settle = "DNF", "wall reached with nothing landed"
else:
    outcome = raw_outcome
    settle = read(os.path.join(CELL, "settle_reason"), "n/a")

meta = {
    "track": "swe-marathon",
    "task": TASK, "harness": ARM, "seed": SEED, "model": MODEL,
    "image_ref": os.environ.get("IMAGE_REF", ""),
    "image_id": os.environ.get("IMAGE_ID", ""),
    "task_commit": os.environ.get("TASK_COMMIT", ""),
    "workdir": WORKDIR,
    "started": read(os.path.join(CELL, "started-at")),
    "ended": read(os.path.join(CELL, "ended-at")),
    "wall_s": int(WALL), "wall_budget_s": int(os.environ.get("CELL_SECONDS", "0") or 0),
    "verify_wall_s": int(VWALL), "verify_exit": int(VCODE),
    "exit": int(CODE),
    "outcome": outcome, "settle_reason": settle,
    "reward": reward, "reward_txt": reward_txt,
    "partial_score": partial, "score_source": score_source,
    "pass_rate": main_metrics.get("pass_rate", (main_block or {}).get("pass_rate")),
    "passed": (main_block or {}).get("passed"), "total": (main_block or {}).get("total"),
    "holdout": holdout_block or None,
    "per_method": per_method,
    "workspace_files": workspace_files,
    "timer_at_start": read(os.path.join(CELL, "timer-at-start.txt")).replace("\n", " "),
    "verifier_stages": load(os.path.join(V, "oneroad_stages.json"), {}),
    # THE DEVIATION IS WHAT THE CELL DID, NOT WHAT THIS FILE ONCE BELIEVED. It
    # used to be a frozen sentence saying "full egress", which stayed true only
    # as long as nobody built the allowlist. cell.sh computes it from the run it
    # actually performed and exports it; the fallback is the honest reading of a
    # record.py invoked by hand, where nothing is known about the network.
    "network_deviation": os.environ.get(
        "NETWORK_DEVIATION",
        "unknown: this record was written without NETWORK_DEVIATION set, so the "
        "cell's egress policy was not recorded"),
    # NOTHING IS PINNED. The agent may upgrade its compiler — officially it runs
    # as root off /root/.rustup, task.toml allows static.rust-lang.org, and the
    # verifier inherits whatever it chose because it runs in the same container.
    # This block records which compiler that turned out to be and which store it
    # came from, and flags the cells whose store our own split HOME misplaced.
    "toolchain": {
        "image_toolchain": os.environ.get("IMAGE_TOOLCHAIN", ""),
        "image_rustc": os.environ.get("IMAGE_RUSTC", ""),
        "agent_toolchain": os.environ.get("AGENT_TOOLCHAIN", ""),
        "rustup_home": os.environ.get("AGENT_RUSTUP_HOME", ""),
        "cargo_home": os.environ.get("AGENT_CARGO_HOME", ""),
        "pinned": False,
        "split_home": os.environ.get("AGENT_RUSTUP_HOME", "").startswith("/chome"),
        "note": (
            "cell run before the toolchain fix: HOME=/chome sent the agent's rustup store to "
            "/chome/.rustup, where tests/test.sh could not see it. The agent upgraded there, "
            "which is the official-equivalent state (officially the upgrade lands in /root/.rustup "
            "and the verifier inherits it), so the verifier was pointed at that store."
            if os.environ.get("AGENT_RUSTUP_HOME", "").startswith("/chome") else
            "agent and verifier shared one rustup state on the image's own paths, as officially"),
    },
    "loadavg_before": read(os.path.join(CELL, "loadavg-before")),
    "loadavg_after": read(os.path.join(CELL, "loadavg-after")),
}

# ── which build of which harness was measured ───────────────────────────────
def harness_version():
    if ARM.startswith("aforge"):
        binary = os.environ.get("NEW_BIN", "")
        sha = ""
        if binary and os.path.exists(binary):
            h = hashlib.sha256()
            with open(binary, "rb") as fh:
                for chunk in iter(lambda: fh.read(1 << 20), b""):
                    h.update(chunk)
            sha = h.hexdigest()
        commit = subprocess.run(["git", "-C", os.path.dirname(ONEROAD), "rev-parse", "HEAD"],
                                capture_output=True, text=True).stdout.strip()
        # THE COMMIT THE BINARY WAS BUILT FROM IS NOT THE COMMIT THE TREE IS ON.
        # Several lanes share this checkout, so HEAD moves under a ten-hour cell
        # and the repo commit read at record time is only "where the tree was
        # when the row was written". AFORGE_BUILD_COMMIT is the binary's own
        # provenance, passed in by whoever built it; the sha256 is the proof.
        return {"binary": binary, "sha256": sha,
                "build_commit": os.environ.get("AFORGE_BUILD_COMMIT", ""),
                "repo_commit_at_record": commit,
                "config": "all-flash" if ARM.endswith("flash") else "crew (registry tiers)"}
    exe = {"pi": "pi", "opencode": os.path.expanduser("~/.opencode/bin/opencode")}.get(ARM, ARM)
    out = subprocess.run([exe, "--version"], capture_output=True, text=True)
    return {"version": (out.stdout or out.stderr).strip().splitlines()[-1:] and
            (out.stdout or out.stderr).strip().splitlines()[-1] or ""}

try:
    meta["harness_version"] = harness_version()
except Exception as exc:
    meta["harness_version"] = {"error": str(exc)}

# ── the money, on the one price table ───────────────────────────────────────
from competitor_cost import list_prices, pi_usage, opencode_usage  # noqa: E402
from aforge_list_cost import usage as aforge_usage  # noqa: E402

def spend():
    prices = list_prices(MODEL)
    if ARM.startswith("aforge"):
        tok, n = aforge_usage(CELL)
        return {"input": tok["in"], "output": tok["out"], "cache_read": tok["cache"]}, None, n
    # lib/competitor_cost.py finds pi's and opencode's stores under the HOME it is
    # running as. The cell ran them with HOME=/peer inside the container, which is
    # the bind mount $CELL/peer on this side, so HOME is pointed at it rather than
    # the reader being forked into a second copy.
    os.environ["HOME"] = os.path.join(CELL, "peer")
    started = os.path.getmtime(os.path.join(CELL, "prompt.txt")) - 120
    ended = os.path.getmtime(os.path.join(CELL, "ended-at")) + 120 \
        if os.path.exists(os.path.join(CELL, "ended-at")) else started + 4 * 36000
    win = (started, ended)
    return (pi_usage if ARM == "pi" else opencode_usage)(WORKDIR, win)

try:
    tok, native, calls = spend()
    p = list_prices(MODEL)
    # `input` is prompt_tokens in the OpenAI dialect and INCLUDES the cached part
    # in aforge's journal, so only the remainder is billed at the uncached rate.
    uncached = max(0, tok["input"] - tok["cache_read"]) if ARM.startswith("aforge") else tok["input"]
    usd = uncached * p["prompt"] + tok["cache_read"] * p["cache_read"] + tok["output"] * p["completion"]
    meta.update({"tokens_in": tok["input"], "tokens_out": tok["output"],
                 "tokens_cache": tok["cache_read"], "requests": calls,
                 "cost_usd": round(usd, 6), "cost_source": "tokens×openrouter-list-price",
                 "native_usd": None if native is None else round(float(native), 6)})
except Exception as exc:
    meta.update({"cost_usd": None, "cost_source": "unreadable: %s" % exc})

# ── the road columns, for the arms that have a road ─────────────────────────
if ARM.startswith("aforge"):
    # THE SESSION IS TWO LEVELS UNDER projects/, NOT ONE: projects/<project>/<session>/.
    sessions = sorted(
        d for d in glob.glob(os.path.join(CELL, "profile", "v3", "projects", "*", "*"))
        if os.path.isdir(d) and os.path.exists(os.path.join(d, "transcript.jsonl")))
    if sessions:
        out = subprocess.run(
            [sys.executable, os.path.join(LIB, "road.py"), sessions[0],
             "--timeline", os.path.join(CELL, "timeline.json")],
            capture_output=True, text=True)
        if out.stdout.strip():
            cols = json.loads(out.stdout)
            # road.py's `cost_usd` is what the harness itself was billed. The
            # record's `cost_usd` is the one price table, so the self-reported
            # figure is kept under its own name and never overwrites it.
            meta["native_usd"] = cols.pop("cost_usd", None)
            cols.pop("cost_source", None)
            meta["road_summary"] = {k: cols[k] for k in (
                "road", "route", "armed", "marks", "mark_decisions", "ceiling_decision",
                "division", "division_admitted", "division_why", "division_refused",
                "parts", "peak_workers", "forks", "calls", "escalated") if k in cols}
            meta["road_columns"] = cols
    else:
        meta["road_summary"] = "unreadable(no session folder)"
else:
    meta["road_summary"] = "n/a"

# ── the progress curve, folded into the record ──────────────────────────────
curve_path = os.path.join(CELL, "curve.csv")
if os.path.exists(curve_path):
    with open(curve_path, newline="") as fh:
        meta["curve"] = list(csv.DictReader(fh))

json.dump(meta, open(os.path.join(CELL, "record.json"), "w"), indent=2)
print(json.dumps({k: meta.get(k) for k in
                  ("task", "harness", "outcome", "reward", "partial_score", "passed",
                   "total", "wall_s", "cost_usd", "workspace_files")}))
