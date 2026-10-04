#!/usr/bin/env python3
"""Read what Cloudflare billed a Worker and its R2 bucket for a time window.

The numbers come from the GraphQL analytics API, which is the same source the invoice uses, so a
load run can be priced from measured usage instead of estimates. Standard library only.

Units of the API fields this file reads (checked with __type introspection on 2026-09-30):
  workersInvocationsAdaptive.sum.cpuTimeUs            microseconds of CPU      -> worker_cpu_ms = /1000
  durableObjectsPeriodicGroups.sum.activeTime         microseconds active      -> do_active_s   = /1e6
  durableObjectsPeriodicGroups.sum.duration           GB*s, already as billed  -> do_gb_s
  (check: STAGE-1H-DECISION.md 8.8 shows 1.83 GB-s over 14.3 s active, i.e. 128 MB x 14.3 s)
  durableObjectsPeriodicGroups.sum.rowsRead/rowsWritten   rows (SQLite-backed objects)

Exit codes: 0 ok, 2 bad usage or no data, 3 token lacks the Analytics scope, 4 API or network failure.
"""
import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone

ENDPOINT = "https://api.cloudflare.com/client/v4/graphql"
DEFAULT_ACCOUNT = "190bd4aa38c975a1ae33f2521f80d01a"
DEFAULT_SCRIPT = "caf-relay-staging"
DEFAULT_BUCKET = "caf-relay-staging-frames"
WRANGLER_CONFIG = "~/.config/.wrangler/config/default.toml"
WRANGLER_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")

# The one classification table, from Cloudflare's published R2 pricing categories
# (developers.cloudflare.com/r2/pricing, fetched 2026-09-30). Free operations map to None.
R2_CLASS = {
    **dict.fromkeys(
        ["ListBuckets", "PutBucket", "ListObjects", "PutObject", "CopyObject",
         "CompleteMultipartUpload", "CreateMultipartUpload", "LifecycleStorageTierTransition",
         "ListMultipartUploads", "UploadPart", "UploadPartCopy", "ListParts",
         "PutBucketEncryption", "PutBucketCors", "PutBucketLifecycleConfiguration"], "A"),
    **dict.fromkeys(
        ["HeadBucket", "HeadObject", "GetObject", "UsageSummary", "GetBucketEncryption",
         "GetBucketLocation", "GetBucketCors", "GetBucketLifecycleConfiguration"], "B"),
    **dict.fromkeys(["DeleteObject", "DeleteBucket", "AbortMultipartUpload"], None),
}
# Cloudflare bills every request except unauthorized ones, so a miss (notFound) is a billed
# Class B read. We count success and notFound and leave out error statuses, which are rare here.
BILLED_STATUSES = {"success", "notFound"}

METRICS = ["worker_requests", "worker_cpu_ms", "do_requests", "do_active_s", "do_gb_s",
           "do_rows_read", "do_rows_written", "r2_class_a", "r2_class_b"]

SCOPE_FIX = ("the token lacks the Account Analytics Read scope.\n"
             "Fix: create an API token with permission 'Account Analytics:Read' "
             "(dash.cloudflare.com/profile/api-tokens) and run: export CLOUDFLARE_API_TOKEN=<token>")


class ScopeError(Exception):
    """The API refused the query for lack of the analytics scope."""


class AuthExpired(Exception):
    """The API answered 401, so the token is expired or wrong."""


# ---------- time helpers ----------

def parse_time(text):
    return datetime.fromisoformat(text.replace("Z", "+00:00")).astimezone(timezone.utc)


def iso(moment):
    return moment.strftime("%Y-%m-%dT%H:%M:%SZ")


def minute_window(start, end):
    """Widen a window to whole minutes, because analytics are bucketed per minute and a
    partial minute could not be separated from its neighbours anyway."""
    lo = start.replace(second=0, microsecond=0)
    hi = end.replace(second=0, microsecond=0)
    if hi < end:
        hi += timedelta(minutes=1)
    return lo, hi


# ---------- token and transport ----------

def token_from_wrangler():
    text = open(os.path.expanduser(WRANGLER_CONFIG)).read()
    return re.search(r'oauth_token\s*=\s*"([^"]+)"', text).group(1)


def load_token():
    """An explicit API token wins, because it is the owner's deliberate grant of the scope."""
    return os.environ.get("CLOUDFLARE_API_TOKEN") or token_from_wrangler()


def refresh_wrangler_token():
    """`wrangler whoami` renews an expired OAuth token as a side effect; nothing else is changed."""
    subprocess.run(["npx", "wrangler", "whoami"], cwd=WRANGLER_DIR, check=False,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=180)


def post(query, token):
    body = json.dumps({"query": query}).encode()
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json"}
    try:
        with urllib.request.urlopen(urllib.request.Request(ENDPOINT, body, headers), timeout=60) as r:
            return json.load(r)
    except urllib.error.HTTPError as err:
        if err.code == 401:
            raise AuthExpired()
        if err.code == 403:
            raise ScopeError()
        raise


def raise_on_errors(answer):
    errors = answer.get("errors") or []
    text = json.dumps(errors).lower()
    if errors and ("authz" in text or "not authorized" in text or "does not have access" in text):
        raise ScopeError()
    if errors:
        raise RuntimeError("analytics API error: " + json.dumps(errors)[:400])


class Client:
    """Runs queries, refreshing the wrangler token once when it has expired."""

    def __init__(self, account):
        self.account = account
        self.token = load_token()
        self.refreshed = False

    def run(self, inner):
        query = '{ viewer { accounts(filter:{accountTag:"%s"}) { %s } } }' % (self.account, inner)
        try:
            answer = post(query, self.token)
        except AuthExpired:
            if self.refreshed or os.environ.get("CLOUDFLARE_API_TOKEN"):
                raise
            self.refreshed = True
            refresh_wrangler_token()
            self.token = load_token()
            return self.run(inner)
        raise_on_errors(answer)
        return answer["data"]["viewer"]["accounts"][0]


# ---------- queries ----------

def q_workers(script, lo, hi):
    return ('workersInvocationsAdaptive(limit:10, filter:{scriptName:"%s", datetime_geq:"%s", '
            'datetime_leq:"%s"}) { sum { requests cpuTimeUs } }' % (script, iso(lo), iso(hi)))


def q_do_groups(script, lo, hi):
    return ('durableObjectsInvocationsAdaptiveGroups(limit:100, filter:{scriptName:"%s", '
            'datetime_geq:"%s", datetime_leq:"%s"}) { dimensions { namespaceId } sum { requests } }'
            % (script, iso(lo), iso(hi)))


def q_do_periodic(namespaces, lo, last_minute):
    ids = json.dumps(sorted(namespaces))
    return ('durableObjectsPeriodicGroups(limit:1000, filter:{namespaceId_in:%s, '
            'datetimeMinute_geq:"%s", datetimeMinute_leq:"%s"}) { dimensions { datetimeMinute } '
            'sum { activeTime duration rowsRead rowsWritten } }' % (ids, iso(lo), iso(last_minute)))


def q_r2(bucket, lo, hi):
    return ('r2OperationsAdaptiveGroups(limit:200, filter:{bucketName:"%s", datetime_geq:"%s", '
            'datetime_leq:"%s"}) { dimensions { actionType actionStatus } sum { requests } }'
            % (bucket, iso(lo), iso(hi)))


# ---------- parsing (pure) ----------

def parse_workers(rows):
    requests = sum(r["sum"]["requests"] for r in rows)
    cpu_us = sum(r["sum"]["cpuTimeUs"] for r in rows)
    return {"worker_requests": int(requests), "worker_cpu_ms": cpu_us / 1000.0}


def parse_do_invocations(rows):
    return int(sum(r["sum"]["requests"] for r in rows))


def parse_do_periodic(rows):
    def total(key):
        return sum(r["sum"][key] for r in rows)
    return {"do_active_s": total("activeTime") / 1e6, "do_gb_s": float(total("duration")),
            "do_rows_read": int(total("rowsRead")), "do_rows_written": int(total("rowsWritten"))}


def classify(action_type, status):
    """Return 'A', 'B' or None (free, unbilled or unknown) for one R2 operation row."""
    if status not in BILLED_STATUSES:
        return None
    return R2_CLASS.get(action_type)


def parse_r2(rows):
    counts = {"A": 0, "B": 0}
    for r in rows:
        kind = classify(r["dimensions"]["actionType"], r["dimensions"]["actionStatus"])
        if kind:
            counts[kind] += int(r["sum"]["requests"])
    return {"r2_class_a": counts["A"], "r2_class_b": counts["B"]}


def unknown_actions(rows):
    return sorted({r["dimensions"]["actionType"] for r in rows} - set(R2_CLASS))


def sum_phases(phases):
    return {m: sum(p[m] for p in phases.values()) for m in METRICS}


# ---------- reading one phase ----------

def periodic_ranges(phases, steady=()):
    """The minutes of the periodic Durable Object dataset that belong to each phase.

    That dataset is stamped when the runtime reports, not when work ran, and was seen up to a minute
    off (a move at 21:10:04 showed its rows in the 21:09 bucket). So a phase owns every minute up to
    the midpoint of the quiet gap that follows it, which is wide enough (two minutes or more) that
    the shift cannot reach the next phase's work. A steady phase (an idle hold, whose rate is what is
    wanted) owns exactly its own minutes: the quiet after it still holds the lease and would be counted
    in rows that its seconds do not include."""
    order = sorted(phases.items(), key=lambda kv: kv[1][0])
    ranges, first = {}, None
    for i, (name, (start, end)) in enumerate(order):
        first = first or start.replace(second=0, microsecond=0)
        if i + 1 < len(order):
            mid = end + (order[i + 1][1][0] - end) / 2
            last = mid.replace(second=0, microsecond=0)
        else:
            last = (end + timedelta(minutes=1)).replace(second=0, microsecond=0)
        if name in steady:
            last = min(last, minute_window(start, end)[1] - timedelta(minutes=1))
            first = max(first, minute_window(start, end)[0])
        ranges[name] = (first, last)
        first = last + timedelta(minutes=1)
    return ranges


def read_phase(client, script, bucket, namespaces, start, end, minutes=None):
    lo, hi = minute_window(start, end)
    hi_event = hi - timedelta(seconds=1)
    first_minute, last_minute = minutes or (lo, hi - timedelta(minutes=1))
    out = parse_workers(client.run(q_workers(script, lo, hi_event))["workersInvocationsAdaptive"])
    groups = client.run(q_do_groups(script, lo, hi_event))["durableObjectsInvocationsAdaptiveGroups"]
    out["do_requests"] = parse_do_invocations(groups)
    # The periodic dataset has no script name, so it is read per namespace found above and in
    # the run-wide discovery (an idle namespace still bills duration in a phase without requests).
    ids = set(namespaces) | {g["dimensions"]["namespaceId"] for g in groups}
    periodic = []
    if ids:
        periodic = client.run(q_do_periodic(ids, first_minute, last_minute))["durableObjectsPeriodicGroups"]
    out.update(parse_do_periodic(periodic))
    r2_rows = client.run(q_r2(bucket, lo, hi_event))["r2OperationsAdaptiveGroups"]
    bad = unknown_actions(r2_rows)
    if bad:
        print("warning: unclassified R2 actions ignored: " + ", ".join(bad), file=sys.stderr)
    out.update(parse_r2(r2_rows))
    return out


def discover_namespaces(client, script, start, end):
    lo, hi = minute_window(start, end)
    groups = client.run(q_do_groups(script, lo, hi - timedelta(seconds=1)))
    return {g["dimensions"]["namespaceId"] for g in groups["durableObjectsInvocationsAdaptiveGroups"]}


def read_all(client, args, phases, window):
    namespaces = discover_namespaces(client, args.script, *window)
    minutes = periodic_ranges(phases, getattr(args, 'steady', ()))
    return {name: read_phase(client, args.script, args.bucket, namespaces, s, e, minutes[name])
            for name, (s, e) in phases.items()}


# ---------- orchestration ----------

def load_phases(args):
    """Return ({name: (start, end)}, (window_start, window_end), script)."""
    if args.manifest:
        m = json.load(open(args.manifest))
        phases = {p["name"]: (parse_time(p["start"]), parse_time(p["end"])) for p in m["phases"]}
        args.steady = {p["name"] for p in m["phases"] if p.get("steady")}
        window = (parse_time(m["start"]), parse_time(m["end"]))
        return phases, window, m.get("script")
    if not (args.time_from and args.time_to):
        sys.exit("usage: give --manifest FILE, or both --from and --to (or --check)")
    window = (parse_time(args.time_from), parse_time(args.time_to))
    return {"total": window}, window, None


def wait_until(target):
    while True:
        left = (target - datetime.now(timezone.utc)).total_seconds()
        if left <= 0:
            return
        print("waiting %ds for analytics to settle (until %s)" % (left, iso(target)), flush=True)
        time.sleep(min(left, 60))


def read_stable(client, args, phases, window):
    """Analytics lag the events, so re-read each minute until two reads agree (max 20 minutes)."""
    deadline = time.monotonic() + 20 * 60
    prev = read_all(client, args, phases, window)
    while time.monotonic() < deadline:
        time.sleep(60)
        cur = read_all(client, args, phases, window)
        if cur == prev:
            return cur
        print("numbers still moving, re-reading in 60s", flush=True)
        prev = cur
    print("warning: numbers never settled within 20 minutes", file=sys.stderr)
    return prev


def build_result(args, phases_read, window):
    now = datetime.now(timezone.utc)
    return {"script": args.script, "bucket": args.bucket, "read_at": iso(now),
            "lag_seconds": max(0, int((now - window[1]).total_seconds())),
            "phases": phases_read, "total": sum_phases(phases_read)}


def check(client, args):
    end = datetime.now(timezone.utc)
    client.run(q_workers(args.script, end - timedelta(hours=1), end))
    print("OK: analytics API reachable and the token has the Analytics scope")


def parse_args(argv):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    p.add_argument("--manifest")
    p.add_argument("--from", dest="time_from")
    p.add_argument("--to", dest="time_to")
    p.add_argument("--script", default=None)
    p.add_argument("--bucket", default=DEFAULT_BUCKET)
    p.add_argument("--account", default=DEFAULT_ACCOUNT)
    p.add_argument("--out")
    p.add_argument("--settle-seconds", type=int, default=360)
    p.add_argument("--stable", action=argparse.BooleanOptionalAction, default=True)
    p.add_argument("--check", action="store_true")
    return p.parse_args(argv)


def run(args):
    client = Client(args.account)
    if args.check:
        return check(client, args)
    phases, window, manifest_script = load_phases(args)
    args.script = args.script or manifest_script or DEFAULT_SCRIPT
    if args.settle_seconds > 0:  # zero means the caller wants to read right now, even a future-ending window
        wait_until(window[1] + timedelta(seconds=args.settle_seconds))
    read = read_stable if args.stable else read_all
    result = build_result(args, read(client, args, phases, window), window)
    text = json.dumps(result, indent=2)
    if args.out:
        open(args.out, "w").write(text + "\n")
    print(text)


def main(argv=None):
    args = parse_args(argv)
    args.script = args.script or (None if args.manifest else DEFAULT_SCRIPT)
    try:
        run(args)
    except ScopeError:
        print("error: " + SCOPE_FIX, file=sys.stderr)
        return 3
    except (AuthExpired, urllib.error.URLError, RuntimeError) as err:
        print("error: %s" % (err or "token rejected (401) even after refresh"), file=sys.stderr)
        return 4
    return 0


if __name__ == "__main__":
    sys.exit(main())
