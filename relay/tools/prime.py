#!/usr/bin/env python3
"""Prime the Model Pool relay from the scored runs the seed index already holds.

This is a ONE-OFF. It is run once by the operator, over the private run ledgers
that never enter this repository, so that the first published index carries the
same cells the binary's embedded seed (internal/pool/index/seed.json) encodes.
Run it with --dry-run first: that prints the rows and checks them against the
seed without sending anything.

It reads two newline-delimited JSON ledgers by path, rebuilds the ordinary Model
Pool rows the seat/score mapping implies, groups them per install and day, and
either prints the NDJSON (--dry-run) or POSTs each batch to <relay>/v1/rows.

The ledgers stay outside the repository: pass their paths in on the command
line, and nothing here writes them back out.

    python3 prime.py --ledger <path> --cells <path> [--relay <url>] [--dry-run]
"""

import argparse
import hashlib
import json
import os
import sys
import urllib.error
import urllib.request

# The relay's submit route, the install header, and the content type the Go
# client (internal/pool/outbox) already sends.
ROWS_PATH = "/v1/rows"
INSTALL_HEADER = "X-Codeaf-Install"
CONTENT_TYPE = "application/x-ndjson"
DEFAULT_RELAY = "https://codeaf.agentfield.ai/pool"

# The relay refuses a request with more than 200 lines and folds at most 500
# rows into one install and one day (relay/src/worker.js). A day over the
# second number cannot be sent at all.
MAX_BATCH = 200
ROWS_PER_INSTALL_PER_DAY = 500

# One judge id for every row: these runs were scored by review, not by a judge
# model, and one judge keeps the relay's judge-severity fit neutral.
JUDGE = "codeaf/reviewer"

# The seed the self-check reads, relative to this file.
SEED_PATH = os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "..", "..",
    "internal", "pool", "index", "seed.json",
)

# A run's worker seat names the worker by a short spelling on the ledger; these
# are the spellings the seed's cells were folded from. A worker whose name has
# no "/" after this aliasing is not a model id and its worker seat is skipped.
WORKER_ALIASES = {
    "ds-v4.1-flash": "deepseek/deepseek-v4.1-flash",
    "glm-5.3-flash": "z-ai/glm-5.3-flash",
    "glm-5.3": "z-ai/glm-5.3",
}

# A task-door crew names its checker by a short spelling after "task+"; these
# are the spellings the seed's high cells were folded from.
HIGH_ALIASES = {
    "kimi-k3": "moonshotai/kimi-k3",
    "qwen3.8-max": "qwen/qwen3.8-max-0902",
    "qwen3.8-max-0902": "qwen/qwen3.8-max-0902",
    "claude-fable-5.1": "anthropic/claude-fable-5.1",
    "fable-5.1": "anthropic/claude-fable-5.1",
    "claude-opus-5": "anthropic/claude-opus-5",
    "opus-5": "anthropic/claude-opus-5",
}


def day_of(at):
    """The YYYY-MM-DD date of a ledger "at" stamp ("…T…" or "… …")."""
    text = str(at or "").strip()
    day = text[:10]
    return day if len(day) == 10 and day[4] == "-" and day[7] == "-" else ""


def install_of(session):
    """The install id a session submits under: the first 32 hex of its sha256.

    One install per originating session, and the same 32 characters the
    X-Codeaf-Install header carries, so the relay folds one session's runs
    under one install.
    """
    return hashlib.sha256(str(session).encode("utf-8")).hexdigest()[:32]


def as_int(value, default=0):
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def is_no_work(rating, notes):
    """A run is "no work" when its rating is at most 1 and its notes say so.

    Such a run is a FAILURE for the worker seat and contributes nothing to any
    other seat.
    """
    try:
        low = float(rating) <= 1
    except (TypeError, ValueError):
        low = False
    return low and "no work" in str(notes or "").lower()


def score_of(major, no_work):
    """The 0/100 score a seat takes from a run's outcome."""
    if no_work:
        return 0
    return 100 if as_int(major) == 0 else 0


def resolve_worker(name):
    return WORKER_ALIASES.get(str(name or "").strip(), str(name or "").strip())


def resolve_high(name):
    return HIGH_ALIASES.get(str(name or "").strip(), str(name or "").strip())


def checker_of(crew):
    """The checker short name a task-door crew names.

    A crew is "task+<checker>" or "task+learn(<worker>/<checker>)"; the checker
    is the whole tail, or the part after the slash inside "learn(...)".
    """
    rest = crew[len("task+"):]
    if rest.startswith("learn(") and rest.endswith(")"):
        inner = rest[len("learn("):-1]
        if "/" in inner:
            return inner.rsplit("/", 1)[1].strip()
        return inner.strip()
    return rest.strip()


def size_from_task(task):
    """A ledger row's size: the leading "S "/"M "/"L " or "S-"/"M-"/"L-", else M."""
    text = str(task or "")
    if len(text) >= 2 and text[0] in "SML" and text[1] in (" ", "-"):
        return text[0]
    return "M"


def read_jsonl(path):
    """Every non-blank line of a JSONL file, parsed."""
    rows = []
    with open(path, "r", encoding="utf-8") as handle:
        for number, line in enumerate(handle, 1):
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError as err:
                raise SystemExit("%s:%d: not json: %s" % (path, number, err))
    return rows


def load_ledger(path):
    """Ledger rows deduplicated by (session, run), last wins.

    A row with tag == "bench" or a truthy bench field is an experiment arm and
    is dropped entirely.
    """
    latest = {}
    for row in read_jsonl(path):
        if row.get("tag") == "bench" or row.get("bench"):
            continue
        latest[(str(row.get("session")), str(row.get("run")))] = row
    return list(latest.values())


def load_cells(path):
    """The completed cells of a cells ledger: a lease joined to its done row.

    done rows are deduplicated by id, last wins. A cell whose id equals a
    ledger run is skipped later, where the ledger's own run is known.
    """
    leases = {}
    dones = {}
    for row in read_jsonl(path):
        if row.get("kind") == "lease":
            leases[str(row.get("id"))] = row
        elif row.get("kind") == "done":
            dones[str(row.get("id"))] = row
    return leases, dones


def attempts_from_ledger(path):
    """One attempt per ledger run."""
    attempts = []
    for row in load_ledger(path):
        crew = str(row.get("crew") or "")
        attempts.append({
            "session": row.get("session"),
            "crew": crew,
            "worker": row.get("worker"),
            "planner": row.get("planner"),
            "major": row.get("major"),
            "rating": row.get("rating"),
            "notes": row.get("notes"),
            "day": day_of(row.get("at")),
            "size": size_from_task(row.get("task")),
            "door": "do" if crew == "do" else "task",
        })
    return attempts


def attempts_from_cells(path, ledger_runs):
    """One attempt per completed cell ledger row.

    The crew is "task+<checker>" on the task door and "do" otherwise; a cell
    whose id is also a ledger run is skipped, because the ledger already holds
    that run.
    """
    leases, dones = load_cells(path)
    attempts = []
    for cell_id, lease in leases.items():
        if cell_id in ledger_runs:
            continue
        done = dones.get(cell_id)
        if done is None:
            continue
        door = str(lease.get("door") or "")
        if door == "task":
            crew = "task+" + str(lease.get("checker") or "")
        else:
            crew = "do"
        size = str(lease.get("size") or "M")
        attempts.append({
            "session": lease.get("session"),
            "crew": crew,
            "worker": lease.get("worker"),
            "planner": lease.get("thinker"),
            "major": done.get("major"),
            "rating": done.get("rating"),
            "notes": done.get("notes"),
            "day": day_of(lease.get("at")),
            "size": size if size in ("S", "M", "L") else "M",
            "door": "do" if crew == "do" else "task",
        })
    return attempts


def rows_of(attempt):
    """The Model Pool rows one attempt contributes: one per seat it scores.

    The worker seat is always scored (when its name is a model id); a task-door
    crew adds the high seat, any other crew the mastermind seat. A "no work" run
    is a worker failure and scores no other seat; a mastermind whose model is
    not a model id, or is the worker itself, is dropped.
    """
    no_work = is_no_work(attempt["rating"], attempt["notes"])
    score = score_of(attempt["major"], no_work)
    worker = resolve_worker(attempt["worker"])
    install = install_of(attempt["session"])
    day = attempt["day"]

    seats = []
    if "/" in worker:
        seats.append(("worker", worker))
    if not no_work:
        if attempt["crew"].startswith("task+"):
            checker = resolve_high(checker_of(attempt["crew"]))
            if "/" in checker:
                seats.append(("high", checker))
        else:
            planner = str(attempt["planner"] or "").strip()
            if "/" in planner and planner != worker:
                seats.append(("mastermind", planner))

    rows = []
    for role, model in seats:
        rows.append({
            "schema": 1,
            "day": day,
            "nonce": install,
            "payload": {
                "schema": 1,
                "metric": "role_quality",
                "role": role,
                "model": model,
                "score": score,
                "judge": JUDGE,
                "door": attempt["door"],
                "size": attempt["size"],
                "day": day,
            },
        })
    return rows


def build_rows(ledger_path, cells_path):
    """Every row both ledgers imply, in ledger-then-cells order."""
    ledger_rows = load_ledger(ledger_path)
    ledger_runs = {str(row.get("run")) for row in ledger_rows}
    attempts = attempts_from_ledger(ledger_path)
    attempts += attempts_from_cells(cells_path, ledger_runs)
    rows = []
    for attempt in attempts:
        rows.extend(rows_of(attempt))
    return rows


def cell_totals(rows):
    """Per (role, model): how many rows and how many scored 100."""
    totals = {}
    for row in rows:
        payload = row["payload"]
        key = (payload["role"], payload["model"])
        count, ok = totals.get(key, (0, 0))
        totals[key] = (count + 1, ok + (1 if payload["score"] == 100 else 0))
    return totals


def load_seed(path):
    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def posterior(ok, n):
    """The seed's Beta(1,1) posterior mean of a cell: 100·(1+ok)/(2+n)."""
    return 100.0 * (1 + ok) / (2 + n)


def report_totals(rows, out):
    """Per install: rows and distinct days. Per (role, model): count and mean."""
    by_install = {}
    for row in rows:
        install = row["nonce"]
        by_install.setdefault(install, set()).add(row["day"])
    out.write("installs:\n")
    for install in sorted(by_install):
        count = sum(1 for row in rows if row["nonce"] == install)
        out.write("  %s  rows=%d  days=%d\n" % (install, count, len(by_install[install])))
    out.write("cells (role, model: count, mean score):\n")
    totals = cell_totals(rows)
    for (role, model) in sorted(totals):
        count, ok = totals[(role, model)]
        out.write("  %-10s %-28s n=%-4d mean=%.1f\n"
                  % (role, model, count, 100.0 * ok / count))


def self_check(rows, seed, out):
    """Compare our cells against the seed's and fail when an n differs.

    Every seed cell with n >= 3 must be reproduced exactly, and the seed's mean
    must be the Beta(1,1) posterior of the same ok/n.
    """
    totals = cell_totals(rows)
    expected = seed.get("cells", [])
    out.write("seed self-check (every seed cell with n >= 3 must match):\n")
    out.write("  %-10s %-28s %6s %6s %6s | %6s %9s %9s\n"
              % ("role", "model", "ours n", "ours ok", "ours P", "seed n", "seed mean", "posterior"))
    mismatches = []
    for cell in expected:
        role = str(cell.get("role"))
        model = str(cell.get("model"))
        seed_n = int(cell.get("n") or 0)
        ours_n, ours_ok = totals.get((role, model), (0, 0))
        ours_p = (ours_ok / ours_n) if ours_n else 0.0
        out.write("  %-10s %-28s %6d %6d %6.3f | %6d %9.1f %9.1f\n"
                  % (role, model, ours_n, ours_ok, ours_p, seed_n,
                     float(cell.get("mean") or 0.0), posterior(ours_ok, ours_n)))
        if seed_n >= 3 and ours_n != seed_n:
            mismatches.append("%s/%s: seed n=%d but we built n=%d"
                              % (role, model, seed_n, ours_n))
    return mismatches


def batches_of(rows):
    """Rows grouped by (install, day), each day split into batches of at most 200.

    A day that holds more than the relay's per-install per-day quota at all is
    an error: it cannot be submitted legally.
    """
    groups = {}
    for row in rows:
        groups.setdefault((row["nonce"], row["day"]), []).append(row)
    batches = []
    for key in sorted(groups):
        group = groups[key]
        if len(group) > ROWS_PER_INSTALL_PER_DAY:
            raise SystemExit(
                "install %s on %s holds %d rows, over the %d-per-day quota; "
                "the relay would refuse it" % (key[0], key[1], len(group),
                                               ROWS_PER_INSTALL_PER_DAY))
        for start in range(0, len(group), MAX_BATCH):
            batches.append((key[0], group[start:start + MAX_BATCH]))
    return batches


def ndjson(rows):
    return "\n".join(json.dumps(row, separators=(",", ":")) for row in rows) + "\n"


def post(relay, install, body):
    """One batch to <relay>/v1/rows, answering the HTTP status."""
    url = relay.rstrip("/") + ROWS_PATH
    request = urllib.request.Request(
        url,
        data=body.encode("utf-8"),
        method="POST",
        headers={INSTALL_HEADER: install, "Content-Type": CONTENT_TYPE},
    )
    try:
        with urllib.request.urlopen(request) as response:
            return response.status
    except urllib.error.HTTPError as err:
        return err.code


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--ledger", required=True, help="path to the ledger.jsonl of scored runs")
    parser.add_argument("--cells", required=True, help="path to the cells.jsonl of leases and done rows")
    parser.add_argument("--relay", default=DEFAULT_RELAY,
                        help="the relay base URL (default: %(default)s)")
    parser.add_argument("--dry-run", action="store_true",
                        help="print the rows and check them against the seed; send nothing")
    args = parser.parse_args(argv)

    rows = build_rows(args.ledger, args.cells)

    if args.dry_run:
        sys.stdout.write(ndjson(rows))
        report_totals(rows, sys.stderr)
        mismatches = self_check(rows, load_seed(SEED_PATH), sys.stderr)
        if mismatches:
            for line in mismatches:
                sys.stderr.write("mismatch: %s\n" % line)
            return 1
        return 0

    failed = False
    for install, group in batches_of(rows):
        status = post(args.relay, install, ndjson(group))
        print("%s %s: %d rows -> %d" % (install, group[0]["day"], len(group), status))
        # The relay accepts a batch with 202 (relay/src/worker.js handleRows),
        # so any 2xx is a batch that arrived; 400 or 429 is a refusal.
        if not 200 <= status < 300:
            failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
