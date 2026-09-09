#!/usr/bin/env python3
"""A stand-in upstream, so a refusal can be proved rather than asserted.

The guard's promise is that a non-allowlisted model never reaches the provider.
The only way to show that is to put something where the provider would be and
demonstrate it was never contacted. This records every request it receives to a
file and streams a small chunked response, so the pass-through path can be
checked at the same time.

  upstream.py --hits <path>     prints `PORT <n>` and serves until killed
"""
import argparse
import http.server
import json
import time


class Upstream(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    hits_path = ""

    def log_message(self, *_args):
        pass

    def record(self, body):
        model = None
        try:
            model = json.loads(body or b"{}").get("model")
        except ValueError:
            pass
        with open(self.hits_path, "a") as handle:
            handle.write(json.dumps({"path": self.path, "model": model}) + "\n")

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        self.record(body)
        # `stream_delay` seconds between the two events, which is how a client
        # can tell a relay from a buffer: with a gap upstream, a client that
        # sees the first event only when the second arrives was waiting on a
        # proxy holding the whole response.
        try:
            asked = json.loads(body or b"{}")
        except ValueError:
            asked = {}
        try:
            delay = float(asked.get("stream_delay") or 0)
        except (ValueError, TypeError):
            delay = 0
        delay = max(0.0, min(delay, 10.0))
        # A priced call, when the caller asked for accounting — which the guard
        # does on every request it forwards. The figure arrives in the LAST
        # event, which is why a stream that is cut short cannot be priced.
        events = [b"data: UPSTREAM-CHUNK-1\n\n", b"data: UPSTREAM-CHUNK-2\n\n"]
        if isinstance(asked.get("usage"), dict) and asked["usage"].get("include"):
            events.append(b'data: {"usage": {"cost": 0.00025, "prompt_tokens": 11, '
                          b'"completion_tokens": 7, "total_tokens": 18}}\n\n')
        events.append(b"data: [DONE]\n\n")
        # Two chunks with a marker in each, so a proxy that buffers the whole
        # response instead of relaying it is still visible in the bytes.
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        first = True
        try:
            for part in events:
                if not first and delay:
                    time.sleep(delay)
                first = False
                self.wfile.write(b"%x\r\n%s\r\n" % (len(part), part))
                self.wfile.flush()
            self.wfile.write(b"0\r\n\r\n")
        except (BrokenPipeError, ConnectionResetError):
            # The guard hung up because its own client did. Nothing to say.
            self.close_connection = True

    def do_GET(self):
        self.record(b"")
        body = json.dumps({"data": [{"id": "stand-in"}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hits", required=True)
    args = parser.parse_args()
    Upstream.hits_path = args.hits
    open(args.hits, "w").close()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
    print("PORT %d" % server.server_address[1], flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
