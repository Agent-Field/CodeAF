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

How the meter cannot quietly under-count. Every request that is ADMITTED gets a
`phase: "admitted"` row with a request id before a socket upstream is opened,
and a `phase: "settled"` row when it ends — priced, or explicitly unknown. A
call that is billed and then lost (the client hangs up mid-stream, the upstream
fails after generating, this process is killed) leaves an admission nothing
closed, and the reader turns that into an unknown cost for the whole cell rather
than a total that silently omits it.

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
import time
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
                continue
            named = ""
            if isinstance(entry, dict):
                for key in ("model", "id", "name"):
                    if isinstance(entry.get(key), str):
                        named = entry[key]
                        break
            # An entry whose model cannot be read — an empty object, a number,
            # a nested shape this does not know — is recorded as unreadable and
            # therefore unclearable. Skipping it would let a request route
            # somewhere no check ever saw.
            found.append(named)
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


def generation_id_in_tail(tail):
    """The generation id the response chunks wore, so an unpriced call can be
    matched against the provider's own records later. Only the id is read —
    the tail is never written anywhere, so no content travels with it."""
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
            blob = json.loads(line)
        except ValueError:
            continue
        if isinstance(blob, dict) and isinstance(blob.get("id"), str):
            found = blob["id"]
    if found is None:
        start = text.find("{")
        if start >= 0:
            try:
                blob = json.loads(text[start:])
                if isinstance(blob, dict) and isinstance(blob.get("id"), str):
                    found = blob["id"]
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
    calls_admitted = 0

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

    def sentinel_ok(self, model=None):
        """Checked before anything is admitted, so a refused caller never opens
        an accounting row that nothing would ever close."""
        if not self.sentinel:
            return True
        presented = (self.headers.get("Authorization") or "").removeprefix("Bearer ").strip()
        if presented == self.sentinel:
            return True
        self.refuse(401, "caller did not present this run's sentinel token",
                    path=self.path, model=model)
        return False

    def do_GET(self):
        path = self.api_path()
        if not path.startswith(GET_PREFIXES):
            self.refuse(403, "path %s is not one this guard forwards" % path,
                        path=self.path, model=None)
            return
        if not self.sentinel_ok():
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
        if not self.sentinel_ok(payload.get("model")):
            return

        # The id the request was checked under is the id that goes upstream. A
        # harness may address the guard through a provider alias (`guard/…`),
        # and forwarding that alias asks OpenRouter for a model it has never
        # heard of; the checked form is the real one.
        rewritten = False
        if isinstance(payload.get("model"), str) and normalise(payload["model"]) != payload["model"]:
            payload["model"] = normalise(payload["model"])
            rewritten = True
        if isinstance(payload.get("models"), list):
            fallbacks = [normalise(entry) if isinstance(entry, str) else entry
                         for entry in payload["models"]]
            rewritten = rewritten or fallbacks != payload["models"]
            payload["models"] = fallbacks

        # Ask the provider to account for the call. This changes the REQUEST,
        # never the response bytes: without it a streamed call reports no usage
        # at all and the only cost left would be the harness's own arithmetic,
        # which is what this exists to stop trusting.
        added_usage = False
        if self.measure and isinstance(payload, dict) and "usage" not in payload:
            payload["usage"] = {"include": True}
            added_usage = True
        # Request standard streaming accounting as well as OpenRouter's usage
        # extension. Other options survive; this does not cure cancelled streams,
        # which may end before any usage arrives.
        stream_usage_added = False
        if self.measure and payload.get("stream") is True:
            options = payload.get("stream_options")
            if not isinstance(options, dict):
                options = {}
            if options.get("include_usage") is not True:
                options["include_usage"] = True
                payload["stream_options"] = options
                stream_usage_added = True
        if added_usage or stream_usage_added or rewritten:
            body = json.dumps(payload).encode()

        # ADMISSION IS THE ACCOUNTING EVENT, not completion. From here the call
        # may be billed whatever happens next — the client can hang up, the
        # upstream can fail after streaming half a reply, this process can be
        # killed — so the row is opened now and closed later. An admission with
        # no settlement is what an unknown cost looks like, and the reader
        # treats it as one.
        request_id = self.open_account(payload.get("model"),
                                       normalised=rewritten, usage_include_added=added_usage,
                                       stream_usage_added=stream_usage_added)
        self.relay(body, payload.get("model"), added_usage=added_usage, request_id=request_id)

    def open_account(self, model, **fields):
        with self.lock:
            Guard.calls_admitted += 1
            number = Guard.calls_admitted
        request_id = "%s-%d" % (self.scope or "call", number)
        self.write_line(self.usage_path, dict(
            fields, scope=self.scope, request_id=request_id, phase="admitted",
            model=model, path=self.path))
        self.audit(decision="allow", phase="admitted", request_id=request_id,
                   path=self.path, model=model, **fields)
        return request_id

    def relay(self, body, model, added_usage=False, request_id=None):
        url = self.upstream.rstrip("/") + "/" + self.path.lstrip("/").removeprefix("v1/")
        headers = {name: value for name, value in self.headers.items()
                   if name.lower() not in HOP_BY_HOP}
        key = os.environ.get("GUARD_UPSTREAM_KEY", "")
        if key:
            headers["Authorization"] = "Bearer " + key

        request = urllib.request.Request(url, data=body or None, headers=headers,
                                         method=self.command)
        tail = bytearray()
        sent_headers = False
        # Timings are taken here rather than reconstructed later, because an
        # unknown settlement is only reconcilable against what the provider
        # knows: when the call happened and which generation it was. Nothing
        # derived from the request body is recorded — no prompt, no key.
        started = time.monotonic()
        first_byte = None
        generation_id = None
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                self.send_response(response.status)
                for name, value in response.headers.items():
                    if name.lower() not in HOP_BY_HOP:
                        self.send_header(name, value)
                self.send_header("Transfer-Encoding", "chunked")
                self.end_headers()
                sent_headers = True
                # read1 returns what has arrived rather than waiting for a full
                # buffer: with read(1024) a token stream is held back until 1 KiB
                # exists, which turns a live conversation into a batch and makes
                # every time-to-first-token measurement wrong.
                reader = getattr(response, "read1", None)
                while True:
                    chunk = reader(65536) if reader else response.read(1)
                    if not chunk:
                        break
                    if first_byte is None:
                        first_byte = time.monotonic()
                    tail.extend(chunk)
                    if len(tail) > TAIL_BYTES:
                        del tail[:len(tail) - TAIL_BYTES]
                    if request_id and generation_id is None:
                        generation_id = generation_id_in_tail(bytes(tail))
                        if generation_id:
                            # Save identity before forwarding so a killed process
                            # leaves enough evidence for post-run billing lookup.
                            self.write_line(self.usage_path, dict(phase="generation",
                                request_id=request_id, generation_id=generation_id,
                                model=model, path=self.path, scope=self.scope))
                    try:
                        self.wfile.write(b"%x\r\n%s\r\n" % (len(chunk), chunk))
                        self.wfile.flush()
                    except (BrokenPipeError, ConnectionResetError):
                        # The caller hung up. Nothing more can be said to it —
                        # a second response after headers is a protocol error —
                        # but the call was still made, and may still be billed.
                        # The upstream is closed with it, so the usage chunk
                        # that ends the stream never arrives; the settlement
                        # carries the generation id and timings instead, which
                        # is what a later reconciliation needs. No billing
                        # recovery call runs on the measured path. A separate
                        # post-run metadata reader can reconcile this generation.
                        self.close_connection = True
                        self.settle(request_id, model, bytes(tail),
                                    "client disconnected mid-stream",
                                    outcome="client-disconnect",
                                    started=started, first_byte=first_byte)
                        self.audit(decision="allow", phase="ended", request_id=request_id,
                                   path=self.path, model=model, ended="client-disconnect")
                        return
                self.wfile.write(b"0\r\n\r\n")
                self.settle(request_id, model, bytes(tail), "",
                            outcome="completed", started=started, first_byte=first_byte)
                self.audit(decision="allow", phase="ended", request_id=request_id,
                           path=self.path, model=model,
                           upstream_status=response.status, usage_include_added=added_usage)
        except urllib.error.HTTPError as error:
            payload = error.read()
            if not sent_headers:
                self.send_response(error.code)
                self.send_header("Content-Type", error.headers.get("Content-Type", "application/json"))
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)
            else:
                self.close_connection = True
            # An error status is not proof of a free call: a provider can fail
            # after generating. Whatever usage came back is read; otherwise the
            # row settles unknown.
            self.settle(request_id, model, payload,
                        "upstream returned HTTP %d" % error.code,
                        outcome="upstream-error", started=started, first_byte=first_byte)
            self.audit(decision="allow", phase="ended", request_id=request_id,
                       path=self.path, model=model, upstream_status=error.code)
        except (BrokenPipeError, ConnectionResetError):
            self.close_connection = True
            self.settle(request_id, model, bytes(tail), "connection lost",
                        outcome="connection-lost", started=started, first_byte=first_byte)
            self.audit(decision="allow", phase="ended", request_id=request_id,
                       path=self.path, model=model, ended="connection-lost")
        except Exception as error:
            reason = "upstream failed: %s" % type(error).__name__
            if not sent_headers:
                self.refuse(502, reason, path=self.path, model=model)
            else:
                self.close_connection = True
                self.audit(decision="allow", phase="ended", request_id=request_id,
                           path=self.path, model=model, ended=type(error).__name__)
            self.settle(request_id, model, bytes(tail), reason,
                        outcome="upstream-failed", started=started, first_byte=first_byte)

    def settle(self, request_id, model, tail, note, outcome=None,
               started=None, first_byte=None):
        """Close an admitted call's accounting row with what the provider said
        it charged — or with an explicit unknown. No usage block means no
        figure; never a zero, which would read as a free call, and never
        silence, which would read as a call that did not happen.

        Beyond the price, the row carries what a reconciliation against the
        provider's own records needs — the generation id the chunks wore, when
        the first byte arrived, how long the call ran, how it ended — and
        nothing from the request itself: no prompt, no credential."""
        if not request_id or not self.usage_path:
            return
        usage = usage_in_tail(tail) if tail else None
        now = time.monotonic()
        row = {"scope": self.scope, "request_id": request_id, "phase": "settled",
               "model": model, "path": self.path, "cost_usd": None, "note": note,
               "generation_id": generation_id_in_tail(tail) if tail else None,
               "outcome": outcome,
               "ttfb_ms": round((first_byte - started) * 1000.0, 3)
               if started is not None and first_byte is not None else None,
               "elapsed_ms": round((now - started) * 1000.0, 3)
               if started is not None else None}
        if usage is None:
            row["note"] = "; ".join(part for part in
                                    (note, "upstream returned no usage block") if part)
            self.write_line(self.usage_path, row)
            return
        cost = usage.get("cost")
        row.update({
            "prompt_tokens": usage.get("prompt_tokens"),
            "completion_tokens": usage.get("completion_tokens"),
            "total_tokens": usage.get("total_tokens"),
            "cost_usd": float(cost) if isinstance(cost, (int, float)) else None,
        })
        if not isinstance(cost, (int, float)):
            row["note"] = "; ".join(part for part in
                                    (note, "upstream usage carried no cost") if part)
        self.write_line(self.usage_path, row)


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
