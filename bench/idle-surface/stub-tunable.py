#!/usr/bin/env python3
"""stub-tunable.py - ~/src/stub.py with ONE change: the per-token delay is read
from a file on every token, so a harness can slow the stream down mid-flight
without restarting the endpoint. Identical bytes on the wire otherwise; see
the original for the contract. STUB_DELAY_FILE is the path to a file holding a
delay in milliseconds (missing file or unparsable content falls back to
STUB_DELAY_MS).
"""
import os, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)) + "/../..")

PORT     = int(os.environ.get("STUB_PORT", "8099"))
TOKENS   = int(os.environ.get("STUB_TOKENS", "40"))
DELAY_MS = float(os.environ.get("STUB_DELAY_MS", "5"))
DELAY_FILE = os.environ.get("STUB_DELAY_FILE", "")
LOGPATH  = os.environ.get("STUB_LOG", "/tmp/stub.log")

def _delay_ms():
    try:
        return float(open(DELAY_FILE).read().strip())
    except Exception:
        return DELAY_MS

# pull in everything else from the pinned instrument, then patch the two names
# the streaming loops read
_src = os.environ.get("STUB_SRC", os.path.expanduser("~/src/stub.py"))
_g = {"__name__": "stubsrc"}
exec(compile(open(_src).read(), _src, "exec"), _g)
WORDS = _g["WORDS"]
log = _g["log"]
Handler = type("Handler", (_g["Handler"],), {})
class TunableHandler(Handler):
    def _pace(self, i):
        if i == 0:
            log("FIRST_DELTA")
        time.sleep(_delay_ms() / 1000.0)

if __name__ == "__main__":
    from http.server import ThreadingHTTPServer
    log("STUB_START tunable port=%d tokens=%d delay_ms=%.1f file=%s"
        % (PORT, TOKENS, DELAY_MS, DELAY_FILE))
    TunableHandler.log_message = lambda *a: None
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), TunableHandler)
    srv.serve_forever()
