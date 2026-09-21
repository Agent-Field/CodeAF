#!/usr/bin/env python3
"""A local stub endpoint: identical bytes to every CLI, no model, no network.

Serves the OpenAI and Anthropic streaming shapes from one process, stdlib only.
Replays one canned answer of TOKENS deltas, DELAY_MS apart, so every CLI is fed
the same bytes at the same rate. Any model name and any key are accepted, so no
CLI ever retries for a reason of ours.

Every request is logged as one line to STUB_LOG with a monotonic timestamp, and
the timestamp of the FIRST delta written is logged separately: that is the wire
side of the first-delta-to-paint figure.

Run inside a loopback-only network namespace (`unshare -rn`). A CLI that ignores
the redirect then fails loudly instead of quietly measuring a real provider.
"""
import json, os, sys, time, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT     = int(os.environ.get("STUB_PORT", "8099"))
TOKENS   = int(os.environ.get("STUB_TOKENS", "1000"))
DELAY_MS = float(os.environ.get("STUB_DELAY_MS", "20"))
LOGPATH  = os.environ.get("STUB_LOG", "/tmp/stub.log")
SENTINEL = os.environ.get("STUB_SENTINEL", "Zarquon")
MODEL    = "stub-1"

_lock = threading.Lock()

def log(*parts):
    line = "t=%.6f %s\n" % (time.monotonic(), " ".join(str(p) for p in parts))
    with _lock:
        with open(LOGPATH, "a") as fh:
            fh.write(line)
            fh.flush()

# The answer is one sentinel word followed by short filler words. The sentinel is
# first so first paint is detectable from a single short token that no CLI will
# reflow or split, and the filler carries no markdown so a CLI is not charged for
# syntax highlighting it did not ask for.
WORDS = [SENTINEL] + ["alpha", "bravo", "charlie", "delta", "echo"] * ((TOKENS // 5) + 1)
WORDS = WORDS[:TOKENS]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass  # our own log only

    def _body(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b""
        try:
            return json.loads(raw or b"{}")
        except Exception:
            return {}

    def _sse_open(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        # Connection: close, deliberately. With keep-alive and no content length a
        # client can sit waiting after the last delta, which would land in both the
        # CPU and the latency figures as time the CLI never actually spent working.
        # The self-test caught exactly that: curl hung ten seconds after [DONE].
        self.send_header("Connection", "close")
        self.close_connection = True
        self.end_headers()

    def _emit(self, payload, event=None):
        buf = b""
        if event:
            buf += ("event: %s\n" % event).encode()
        buf += ("data: %s\n\n" % json.dumps(payload)).encode()
        self.wfile.write(buf)
        self.wfile.flush()

    def do_GET(self):
        log("GET", self.path)
        if self.path.startswith("/v1/models"):
            one = {"id": MODEL, "object": "model", "owned_by": "stub",
                   "created": 0, "context_length": 200000}
            if self.path.rstrip("/").endswith("/models"):
                self._json({"object": "list", "data": [one]})
            else:
                self._json(one)
            return
        self._json({"ok": True})

    def _json(self, obj, code=200):
        raw = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_POST(self):
        body = self._body()
        path = self.path.split("?")[0].rstrip("/")
        log("POST", path, "model=%s" % body.get("model"),
            "stream=%s" % body.get("stream"))
        if path.endswith("/messages"):
            self._stream_anthropic()
        elif path.endswith("/chat/completions") or path.endswith("/completions"):
            self._stream_openai()
        elif path.endswith("/responses"):
            self._stream_openai()
        else:
            log("UNHANDLED", path)
            self._json({"error": {"message": "stub: unhandled path " + path}}, 404)

    def _pace(self, i):
        if i == 0:
            log("FIRST_DELTA")
        time.sleep(DELAY_MS / 1000.0)

    def _stream_openai(self):
        self._sse_open()
        base = {"id": "stub", "object": "chat.completion.chunk",
                "created": int(time.time()), "model": MODEL}
        try:
            for i, w in enumerate(WORDS):
                self._pace(i)
                d = dict(base)
                d["choices"] = [{"index": 0, "delta": {"content": ("" if i == 0 else " ") + w},
                                 "finish_reason": None}]
                self._emit(d)
            d = dict(base)
            d["choices"] = [{"index": 0, "delta": {}, "finish_reason": "stop"}]
            self._emit(d)
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
            log("DONE openai tokens=%d" % len(WORDS))
        except (BrokenPipeError, ConnectionResetError):
            log("CLIENT_CLOSED openai")

    def _stream_anthropic(self):
        self._sse_open()
        try:
            self._emit({"type": "message_start", "message": {
                "id": "stub", "type": "message", "role": "assistant",
                "model": MODEL, "content": [], "stop_reason": None,
                "usage": {"input_tokens": 1, "output_tokens": 0}}}, "message_start")
            self._emit({"type": "content_block_start", "index": 0,
                        "content_block": {"type": "text", "text": ""}},
                       "content_block_start")
            for i, w in enumerate(WORDS):
                self._pace(i)
                self._emit({"type": "content_block_delta", "index": 0,
                            "delta": {"type": "text_delta",
                                      "text": ("" if i == 0 else " ") + w}},
                           "content_block_delta")
            self._emit({"type": "content_block_stop", "index": 0}, "content_block_stop")
            self._emit({"type": "message_delta",
                        "delta": {"stop_reason": "end_turn"},
                        "usage": {"output_tokens": len(WORDS)}}, "message_delta")
            self._emit({"type": "message_stop"}, "message_stop")
            log("DONE anthropic tokens=%d" % len(WORDS))
        except (BrokenPipeError, ConnectionResetError):
            log("CLIENT_CLOSED anthropic")


if __name__ == "__main__":
    log("STUB_START port=%d tokens=%d delay_ms=%.1f sentinel=%s"
        % (PORT, TOKENS, DELAY_MS, SENTINEL))
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print("stub on 127.0.0.1:%d tokens=%d delay_ms=%.1f sentinel=%s"
          % (PORT, TOKENS, DELAY_MS, SENTINEL), flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
