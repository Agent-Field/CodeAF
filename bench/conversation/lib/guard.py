#!/usr/bin/env python3
"""A loopback forwarding guard that refuses to send a non-allowlisted model.

Why this exists. Flags like aforge's `--one-model` and omp's `--smol/--slow/
--plan` are *configuration*: they say what a harness should do. They are not
enforcement, because a harness has other roles (omp alone carries
`providers.tinyModel`, `memoryModel`, `autoThinkingModel` and
`unexpectedStopModel`), because a reused profile can carry settings this run
never wrote, and because a task the model itself generates can name a model.
Checking the receipts afterwards finds all of that — after the money is spent.

So the run does not hand a harness the real key at all. The guard holds it, the
harness gets a sentinel, and the harness's base URL points here. That makes two
things true rather than hoped for:

  a call with a model outside the allowlist is refused BEFORE any socket to
  the upstream is opened, and

  a call that bypasses the guard carries only the sentinel, so it cannot buy
  anything from anybody.

The guard is deliberately small and dull. One fixed upstream, loopback only,
no credential ever logged, and streaming bytes passed through untouched so that
what the harness sees is what the provider sent.

  guard.py --allow deepseek/deepseek-v4-flash-0731 --audit <path>

It prints one line, `PORT <n>`, when it is listening, and serves until killed.
The real key comes from GUARD_UPSTREAM_KEY in its own environment.
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

# Hop-by-hop headers are connection-scoped and must not be relayed.
HOP_BY_HOP = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
              "te", "trailers", "transfer-encoding", "upgrade", "content-length",
              "host", "authorization"}


def normalise(model):
    """Strip the prefixes the same id wears in different mouths. A variant
    suffix such as `:batch` is kept: it is a different queue and a different
    price, so it has to be allowlisted on purpose."""
    model = (model or "").strip()
    if model.startswith("~"):
        model = model[1:]
    for prefix in ("openrouter/", "guard/"):
        if model.startswith(prefix):
            model = model[len(prefix):]
    return model


class Guard(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    # Configured by main().
    allow = frozenset()
    upstream = UPSTREAM
    sentinel = ""
    audit_path = ""
    audit_lock = threading.Lock()

    def log_message(self, *_args):
        """Silence the default logger: it prints request lines, and this server
        must never write anything derived from a credential."""

    def audit(self, **fields):
        if not self.audit_path:
            return
        with self.audit_lock:
            with open(self.audit_path, "a") as handle:
                handle.write(json.dumps(fields, sort_keys=True) + "\n")

    def refuse(self, status, reason, **fields):
        body = json.dumps({"error": {"message": reason, "type": "guard_refused"}}).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        self.audit(decision="deny", reason=reason, **fields)

    def do_GET(self):
        self.relay(b"", None)

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""

        # The model is read out of the body and nothing else is inspected. A
        # body that is not JSON, or that names no model, cannot be checked and
        # therefore cannot be forwarded.
        try:
            payload = json.loads(body or b"{}")
        except ValueError:
            self.refuse(400, "body is not JSON, so its model cannot be checked",
                        path=self.path, model=None)
            return
        model = payload.get("model") if isinstance(payload, dict) else None
        if not model:
            self.refuse(400, "request names no model", path=self.path, model=None)
            return
        if normalise(model) not in self.allow:
            # Nothing is opened upstream. This is the whole point of the guard.
            self.refuse(403, "model %s is not on the open-model allowlist" % model,
                        path=self.path, model=model)
            return
        self.relay(body, model)

    def relay(self, body, model):
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
        try:
            with urllib.request.urlopen(request) as response:
                self.send_response(response.status)
                for name, value in response.headers.items():
                    if name.lower() not in HOP_BY_HOP:
                        self.send_header(name, value)
                self.send_header("Transfer-Encoding", "chunked")
                self.end_headers()
                # Streaming is relayed chunk by chunk and flushed, so a token
                # arrives at the harness when the provider sent it rather than
                # when the response ends.
                while True:
                    chunk = response.read(1024)
                    if not chunk:
                        break
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(chunk), chunk))
                    self.wfile.flush()
                self.wfile.write(b"0\r\n\r\n")
                self.audit(decision="allow", path=self.path, model=model,
                           upstream_status=response.status)
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


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--allow", action="append", required=True,
                        help="an exact catalog id that may be forwarded (repeatable)")
    parser.add_argument("--audit", default="", help="JSON Lines decision log")
    parser.add_argument("--sentinel", default="", help="token callers must present")
    parser.add_argument("--port", type=int, default=0)
    # The upstream is fixed. It can be moved only under GUARD_TEST=1, which is
    # how the deterministic test proves that a refused request never reaches an
    # upstream at all — the stand-in upstream records everything it receives.
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

    server = http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Guard)
    print("PORT %d" % server.server_address[1], flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
