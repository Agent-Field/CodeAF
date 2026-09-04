#!/usr/bin/env python3
"""A loopback forwarding guard: it refuses a non-allowlisted model, and it is
the run's own meter for what was actually billed.

Why it exists. `--one-model` and `--smol/--slow/--plan` are configuration, not
enforcement: a role, a fallback chain, a reused profile setting or a generated
task can still name another model. So the harness never gets the real key. The
guard holds it, the harness gets a sentinel and a loopback base URL, and two
things become true rather than hoped for: a call naming a model off the
allowlist is refused before any socket upstream is opened, and a call that goes
around the guard can buy nothing.

Why it also meters. A harness's self-reported cost is its own arithmetic over
its own price table, and a custom provider config can put zeroes in that table —
which is exactly how a paid pilot run reported $0.00. What the provider says it
charged is upstream's `usage`, and this is the only place that sees it for every
call including the auxiliary ones.

  guard.py --allow <id> [--allow <id>] --audit <path> --usage <path>
           --sentinel <token> --scope <name>

It prints `PORT <n>` when listening. The real key comes from GUARD_UPSTREAM_KEY
in its own environment and is never logged.
"""
import argparse
import http.server
import json
import os
import sys
import threading
import urllib.error
import urllib.request

UPSTREAM = "https://openrouter.ai/api/v1"

HOP_BY_HOP = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
              "te", "trailers", "transfer-encoding", "upgrade", "content-length",
              "host", "authorization"}

# Only the paths this suite actually needs. Anything else is refused rather than
# forwarded: a guard that relays whatever path it is handed is an open proxy to
# the upstream, whatever it does about models.
POST_PATHS = {"/chat/completions", "/completions"}
GET_PREFIXES = ("/models", "/key", "/credits")

# How much of a response is kept to read `usage` out of. Usage sits at the end
# of both shapes (the last SSE chunk, the last key of a JSON body), so the tail
# is what is kept, and it is bounded so a long generation cannot grow memory.
TAIL_BYTES = 128 * 1024


def normalise(model):
    """Strip the prefixes the same id wears in different mouths. A variant
    suffix like `:batch` is kept: different queue, different price."""
    model = (model or "").strip()
    if model.startswith("~"):
        model = model[1:]
    for prefix in ("openrouter/", "guard/"):
        if model.startswith(prefix):
            model = model[len(prefix):]
    return model


def requested_models(payload):
    """Every model id a request could route to.

    `model` is not the whole story: OpenRouter also takes a `models` fallback
    array, and a request naming an allowlisted model with a commercial fallback
    would otherwise pass a check that only read the top-level field."""
    found = []
    if not isinstance(payload, dict):
        return found
    if isinstance(payload.get("model"), str):
        found.append(payload["model"])
    fallbacks = payload.get("models")
    if isinstance(fallbacks, list):
        for entry in fallbacks:
            if isinstance(entry, str):
                found.append(entry)
            elif isinstance(entry, dict):
                for key in ("model", "id", "name"):
                    if isinstance(entry.get(key), str):
                        found.append(entry[key])
                        break
            else:
                found.append("")   # unreadable entry: cannot be cleared
    elif fallbacks is not None:
        found.append("")
    return found


def usage_from(blob):
    if isinstance(blob, dict) and isinstance(blob.get("usage"), dict):
        return blob["usage"]
    return None


def usage_in_tail(tail):
    """Find the last usage block in a response tail, SSE or plain JSON."""
    text = tail.decode("utf-8", "replace")
    found = None
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("data:"):
            line = line[5:].strip()
            if not line or line == "[DONE]":
                continue
        if not line.startswith("{"):
            continue
        try:
            found = usage_from(json.loads(line)) or found
        except ValueError:
            continue
    if found is None:
        # A non-streamed body arrives as one object, possibly across lines.
        start = text.find("{")
        if start >= 0:
            try:
                found = usage_from(json.loads(text[start:]))
            except ValueError:
                found = None
    return found


class Guard(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    allow = frozenset()
    upstream = UPSTREAM
    sentinel = ""
    audit_path = ""
    usage_path = ""
    scope = ""
    timeout = 300.0
    measure = True
    lock = threading.Lock()

    def log_message(self, *_args):
        """Silence the default logger: it prints request lines, and nothing
        derived from a credential may be written."""

    def write_line(self, path, row):
        if not path:
            return
        with self.lock:
            with open(path, "a") as handle:
                handle.write(json.dumps(row, sort_keys=True) + "\n")

    def audit(self, **fields):
        self.write_line(self.audit_path, dict(fields, scope=self.scope))

    def refuse(self, status, reason, **fields):
        body = json.dumps({"error": {"message": reason, "type": "guard_refused"}}).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        self.audit(decision="deny", reason=reason, **fields)

    def api_path(self):
        path = self.path
        if path.startswith("/v1/"):
            path = path[3:]
        return path.split("?", 1)[0]

    def do_GET(self):
        path = self.api_path()
        if not path.startswith(GET_PREFIXES):
            self.refuse(403, "path %s is not one this guard forwards" % path,
                        path=self.path, model=None)
            return
        self.relay(b"", None)

    def do_POST(self):
        path = self.api_path()
        if path not in POST_PATHS:
            self.refuse(403, "path %s is not an inference path this guard forwards" % path,
                        path=self.path, model=None)
            return

        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        try:
            payload = json.loads(body or b"{}")
        except ValueError:
            self.refuse(400, "body is not JSON, so its models cannot be checked",
                        path=self.path, model=None)
            return

        wanted = requested_models(payload)
        if not wanted:
            self.refuse(400, "request names no model", path=self.path, model=None)
            return
        for model in wanted:
            if normalise(model) not in self.allow:
                self.refuse(403, "model %s is not on the open-model allowlist" % (model or "<unreadable>"),
                            path=self.path, model=model, considered=wanted)
                return

        # Ask the provider to account for the call. This changes the REQUEST,
        # never the response bytes: without it a streamed call reports no usage
        # at all and the only cost left would be the harness's own arithmetic,
        # which is what this exists to stop trusting.
        added_usage = False
        if self.measure and isinstance(payload, dict) and "usage" not in payload:
            payload["usage"] = {"include": True}
            body = json.dumps(payload).encode()
            added_usage = True

        self.relay(body, payload.get("model"), added_usage=added_usage)

    def relay(self, body, model, added_usage=False):
        if self.sentinel:
            presented = (self.headers.get("Authorization") or "").removeprefix("Bearer ").strip()
            if presented != self.sentinel:
                self.refuse(401, "caller did not present this run's sentinel token",
                            path=self.path, model=model)
                return

        url = self.upstream.rstrip("/") + "/" + self.path.lstrip("/").removeprefix("v1/")
        headers = {name: value for name, value in self.headers.items()
                   if name.lower() not in HOP_BY_HOP}
        key = os.environ.get("GUARD_UPSTREAM_KEY", "")
        if key:
            headers["Authorization"] = "Bearer " + key

        request = urllib.request.Request(url, data=body or None, headers=headers,
                                         method=self.command)
        tail = bytearray()
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                self.send_response(response.status)
                for name, value in response.headers.items():
                    if name.lower() not in HOP_BY_HOP:
                        self.send_header(name, value)
                self.send_header("Transfer-Encoding", "chunked")
                self.end_headers()
                # read1 returns what has arrived rather than waiting for a full
                # buffer: with read(1024) a token stream is held back until 1 KiB
                # exists, which turns a live conversation into a batch and makes
                # every time-to-first-token measurement wrong.
                reader = getattr(response, "read1", None)
                while True:
                    chunk = reader(65536) if reader else response.read(1)
                    if not chunk:
                        break
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(chunk), chunk))
                    self.wfile.flush()
                    tail.extend(chunk)
                    if len(tail) > TAIL_BYTES:
                        del tail[:len(tail) - TAIL_BYTES]
                self.wfile.write(b"0\r\n\r\n")
                self.record_usage(model, bytes(tail))
                self.audit(decision="allow", path=self.path, model=model,
                           upstream_status=response.status, usage_include_added=added_usage)
        except urllib.error.HTTPError as error:
            payload = error.read()
            self.send_response(error.code)
            self.send_header("Content-Type", error.headers.get("Content-Type", "application/json"))
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            self.audit(decision="allow", path=self.path, model=model, upstream_status=error.code)
        except Exception as error:
            self.refuse(502, "upstream failed: %s" % type(error).__name__,
                        path=self.path, model=model)

    def record_usage(self, model, tail):
        """Write what the provider said it charged. No usage block means no
        figure — never a zero, which would read as a free call."""
        if not self.usage_path:
            return
        usage = usage_in_tail(tail)
        if usage is None:
            self.write_line(self.usage_path, {
                "scope": self.scope, "model": model, "path": self.path,
                "cost_usd": None, "note": "upstream returned no usage block",
            })
            return
        cost = usage.get("cost")
        self.write_line(self.usage_path, {
            "scope": self.scope,
            "model": model,
            "path": self.path,
            "prompt_tokens": usage.get("prompt_tokens"),
            "completion_tokens": usage.get("completion_tokens"),
            "total_tokens": usage.get("total_tokens"),
            "cost_usd": float(cost) if isinstance(cost, (int, float)) else None,
            "note": "" if isinstance(cost, (int, float)) else "upstream usage carried no cost",
        })


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--allow", action="append", required=True)
    parser.add_argument("--audit", default="")
    parser.add_argument("--usage", default="", help="JSON Lines of what upstream charged")
    parser.add_argument("--scope", default="", help="the cell these calls belong to")
    parser.add_argument("--sentinel", default="")
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--timeout", type=float, default=300.0)
    parser.add_argument("--no-measure-usage", action="store_true",
                        help="do not add usage accounting to forwarded requests")
    # Fixed upstream. Movable only under GUARD_TEST=1, which is how the
    # deterministic test proves a refused request reaches no upstream at all.
    parser.add_argument("--upstream", default=UPSTREAM)
    args = parser.parse_args()

    upstream = UPSTREAM
    if args.upstream != UPSTREAM:
        if os.environ.get("GUARD_TEST") != "1":
            sys.exit("refusing a non-default upstream outside GUARD_TEST=1")
        upstream = args.upstream

    Guard.allow = frozenset(normalise(model) for model in args.allow)
    Guard.upstream = upstream
    Guard.sentinel = args.sentinel
    Guard.audit_path = args.audit
    Guard.usage_path = args.usage
    Guard.scope = args.scope
    Guard.timeout = args.timeout
    Guard.measure = not args.no_measure_usage

    server = http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Guard)
    print("PORT %d" % server.server_address[1], flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
