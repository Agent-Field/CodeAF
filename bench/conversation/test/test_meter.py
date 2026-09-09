#!/usr/bin/env python3
"""The guard's meter, tested against a stand-in provider on loopback.

The guard is the only thing standing between a run's receipt and a harness's
own arithmetic, so the two things it must never do are both quiet failures:
invent a figure (a zero that reads as a free call) and lose one (a billed call
that disappears from the total). The first is refused by construction — no
usage block means no figure — and the second is what the request-id
reconciliation in receipts.py exists for: counting admissions against
settlements can agree while a settlement is written twice and a different
call goes missing, so the reader has to pair ids exactly.

Everything here is offline: a stub upstream serves the streams, and the guard
runs in-process. No model is called, nothing is spent.

  python3 bench/conversation/test/test_meter.py
"""
import gzip
import http.client
import http.server
import json
import os
import sys
import tempfile
import threading
import struct
import socket
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "lib"))

import guard  # noqa: E402
import receipts  # noqa: E402

MODEL = "deepseek/deepseek-v4-flash-0731"


class SettlementValidationTests(unittest.TestCase):
    def settle(self, usage):
        meter = object.__new__(guard.Guard)
        meter.usage_path, meter.path = 'unused', '/chat/completions'
        rows = []
        meter.write_line = lambda path, row: rows.append(row)
        meter.settle('r', MODEL, json.dumps({'id':'g','usage':usage}).encode(), '')
        return rows[0]

    def test_invalid_price_is_not_coerced_into_real_dollars(self):
        for cost in (True, False, -1, float('nan'), float('inf'), '0.1'):
            with self.subTest(cost=cost):
                self.assertIsNone(self.settle({'cost':cost})['cost_usd'])
        self.assertEqual(self.settle({'cost':0})['cost_usd'], 0)

    def test_cache_and_reasoning_counts_survive_without_arbitrary_content(self):
        got = self.settle({'cost':.1, 'prompt_tokens_details':{'cached_tokens':10,
                           'cache_write_tokens':3, 'unexpected':'private text'},
                           'completion_tokens_details':{'reasoning_tokens':5}})
        self.assertEqual(got['prompt_tokens_details'], {'cached_tokens':10,'cache_write_tokens':3})
        self.assertEqual(got['completion_tokens_details'], {'reasoning_tokens':5})
        self.assertNotIn('prompt_tokens_details', self.settle({'cost':.1}))


class Upstream(http.server.BaseHTTPRequestHandler):
    """A stand-in provider. It streams two chunks, then a usage chunk, then
    [DONE] — and the usage chunk is sent ONLY when the request actually asked
    for accounting (`usage: {"include": true}` or
    `stream_options: {"include_usage": true}`), which is how a guard that
    forgot to ask shows up as an unpriced stream. A `stream_delay` body field
    spaces the chunks out so a client can walk away mid-stream."""
    protocol_version = "HTTP/1.1"
    seen = []
    lock = threading.Lock()

    def log_message(self, *_args):
        pass

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        try:
            asked = json.loads(body or b"{}")
        except ValueError:
            asked = {}
        with Upstream.lock:
            Upstream.seen.append(asked)
        asked_usage = (isinstance(asked.get("usage"), dict)
                       and asked["usage"].get("include") is True)
        asked_stream_usage = (isinstance(asked.get("stream_options"), dict)
                              and asked["stream_options"].get("include_usage") is True)
        wants_accounting = asked_usage or asked_stream_usage
        streaming = asked.get("stream") is True
        try:
            delay = float(asked.get("stream_delay") or 0)
        except (TypeError, ValueError):
            delay = 0.0
        delay = max(0.0, min(delay, 5.0))
        if streaming:
            events = [
                b'data: {"id": "gen-test-123", "choices": [{"delta": {"content": "one"}}]}\n\n',
                b'data: {"id": "gen-test-123", "choices": [{"delta": {"content": "two"}}]}\n\n',
            ]
            if wants_accounting:
                events.append(b'data: {"id": "gen-test-123", "usage": {"cost": 0.00025, '
                              b'"prompt_tokens": 11, "completion_tokens": 7, '
                              b'"total_tokens": 18}}\n\n')
            events.append(b"data: [DONE]\n\n")
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            try:
                first = True
                for part in events:
                    if not first and delay:
                        time.sleep(delay)
                    first = False
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(part), part))
                    self.wfile.flush()
                self.wfile.write(b"0\r\n\r\n")
            except (BrokenPipeError, ConnectionResetError):
                self.close_connection = True
        else:
            payload = {"id": "gen-test-123", "choices": [{"message": {"content": "hi"}}]}
            if wants_accounting:
                payload["usage"] = {"cost": 0.00025, "prompt_tokens": 11,
                                    "completion_tokens": 7, "total_tokens": 18}
            blob = json.dumps(payload).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(blob)))
            self.end_headers()
            self.wfile.write(blob)


class MeterCase(unittest.TestCase):
    """A stub upstream and a guard, both in-process, both on loopback."""
    maxDiff = None

    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory(prefix="afmeter-test.")
        cls.usage_path = os.path.join(cls.tmp.name, "usage.jsonl")
        Upstream.seen = []
        cls.upstream_server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
        cls.upstream_port = cls.upstream_server.server_address[1]
        cls.upstream_thread = threading.Thread(
            target=cls.upstream_server.serve_forever, daemon=True)
        cls.upstream_thread.start()

        guard.Guard.allow = frozenset([MODEL])
        guard.Guard.upstream = "http://127.0.0.1:%d/v1" % cls.upstream_port
        guard.Guard.sentinel = ""
        guard.Guard.audit_path = ""
        guard.Guard.usage_path = cls.usage_path
        guard.Guard.scope = "meter-test"
        guard.Guard.measure = True
        guard.Guard.calls_admitted = 0
        cls.guard_server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), guard.Guard)
        cls.guard_port = cls.guard_server.server_address[1]
        cls.guard_thread = threading.Thread(
            target=cls.guard_server.serve_forever, daemon=True)
        cls.guard_thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.guard_server.shutdown()
        cls.upstream_server.shutdown()
        cls.guard_server.server_close()
        cls.upstream_server.server_close()
        cls.tmp.cleanup()

    def setUp(self):
        open(self.usage_path, "w").close()
        Upstream.seen = []
        # calls_admitted is deliberately NOT reset: request ids keep counting
        # up for the process's whole life, so a settlement that a finished
        # test's relay thread writes a moment too late can never be mistaken
        # for this test's own.

    def test_compressed_provider_refusal_keeps_its_encoding(self):
        body = gzip.compress(json.dumps({'error': {'message': 'No route available', 'code': 404}}).encode())
        def refuse(upstream):
            upstream.rfile.read(int(upstream.headers.get('Content-Length', 0)))
            upstream.send_response(404)
            upstream.send_header('Content-Type', 'application/json')
            upstream.send_header('Content-Encoding', 'gzip')
            upstream.send_header('Content-Length', str(len(body)))
            upstream.send_header('Retry-After', '2')
            upstream.end_headers()
            upstream.wfile.write(body)
        with patch.object(Upstream, 'do_POST', refuse):
            connection = http.client.HTTPConnection('127.0.0.1', self.guard_port, timeout=5)
            connection.request('POST', '/v1/chat/completions', json.dumps({'model': MODEL}), {'Content-Type':'application/json'})
            response = connection.getresponse()
            raw = response.read()
            connection.close()
            self.assertEqual(response.status, 404)
            self.assertEqual(response.getheader('Content-Encoding'), 'gzip')
            self.assertEqual(response.getheader('Retry-After'), '2')
            self.assertEqual(json.loads(gzip.decompress(raw))['error']['message'], 'No route available')

    # -- helpers -----------------------------------------------------------

    def ask(self, payload, read_to_end=True, hang_up_after_first=False):
        """POST one request through the guard and return the raw body bytes
        (a chunked stream) plus the response status."""
        conn = http.client.HTTPConnection("127.0.0.1", self.guard_port, timeout=30)
        conn.request("POST", "/v1/chat/completions", body=json.dumps(payload),
                     headers={"Content-Type": "application/json"})
        response = conn.getresponse()
        chunks = []
        if hang_up_after_first:
            response.read1(64)
            # A clean FIN lets a peer keep writing for a while, so the walk
            # is made abrupt the way a killed process is: RST on close.
            conn.sock.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER,
                                 struct.pack("ii", 1, 0))
            conn.close()
            return response.status, b""
        if read_to_end:
            while True:
                piece = response.read1(65536)
                if not piece:
                    break
                chunks.append(piece)
        conn.close()
        return response.status, b"".join(chunks)

    def rows(self, phase=None):
        with open(self.usage_path) as handle:
            out = [json.loads(line) for line in handle if line.strip()]
        if phase:
            out = [row for row in out if row.get("phase") == phase]
        return out

    def settlement_of(self, request_id):
        """The settlement row for one admission. Matched by request id, not
        by position: a relay thread from an earlier test can still append a
        late settlement while this one runs."""
        for row in self.rows("settled"):
            if row.get("request_id") == request_id:
                return row
        return None

    def latest_settlement(self):
        admissions = self.rows("admitted")
        self.assertTrue(admissions, "no admission was booked")
        return self.settlement_of(admissions[-1]["request_id"])

    def wait_for(self, predicate, seconds=15):
        """The disconnect settlements land from the guard's own relay thread,
        slightly after the client has already walked away."""
        deadline = time.time() + seconds
        while time.time() < deadline:
            if predicate():
                return True
            time.sleep(0.1)
        return False

    # -- the guard's request rewriting ---------------------------------------

    def test_streaming_request_gains_include_usage(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}],
                  "stream": True})
        saw = Upstream.seen[-1]
        self.assertEqual(saw.get("stream_options"), {"include_usage": True})
        self.assertEqual(saw.get("usage"), {"include": True})

    def test_existing_stream_options_are_preserved(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}],
                  "stream": True,
                  "stream_options": {"include_usage": False, "other": 1}})
        saw = Upstream.seen[-1]
        self.assertEqual(saw.get("stream_options"),
                         {"include_usage": True, "other": 1})

    def test_present_include_usage_is_not_rewritten(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}],
                  "stream": True, "stream_options": {"include_usage": True}})
        self.assertEqual(Upstream.seen[-1].get("stream_options"), {"include_usage": True})

    def test_non_streaming_request_gains_no_stream_options(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}]})
        saw = Upstream.seen[-1]
        self.assertNotIn("stream_options", saw)
        self.assertEqual(saw.get("usage"), {"include": True})

    def test_caller_usage_key_is_never_clobbered(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}],
                  "stream": True, "usage": {"include": True}})
        self.assertEqual(Upstream.seen[-1].get("usage"), {"include": True})

    # -- the relay -----------------------------------------------------------

    def test_stream_chunks_arrive_in_order_and_are_priced(self):
        status, body = self.ask({"model": MODEL,
                                 "messages": [{"role": "user", "content": "hi"}],
                                 "stream": True})
        self.assertEqual(status, 200)
        self.assertIn(b'"content": "one"', body)
        self.assertLess(body.find(b'"content": "one"'), body.find(b'"content": "two"'))
        self.assertIn(b'"cost": 0.00025', body)
        settled = self.latest_settlement()
        self.assertIsNotNone(settled)
        self.assertEqual(settled["cost_usd"], 0.00025)
        self.assertEqual(settled["outcome"], "completed")
        self.assertEqual(settled["generation_id"], "gen-test-123")

    def test_non_streaming_call_settles_with_usage(self):
        status, _ = self.ask({"model": MODEL,
                              "messages": [{"role": "user", "content": "hi"}]})
        self.assertEqual(status, 200)
        settled = self.latest_settlement()
        self.assertIsNotNone(settled)
        self.assertEqual(settled["cost_usd"], 0.00025)
        self.assertEqual(settled["outcome"], "completed")

    def test_timings_are_recorded(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}],
                  "stream": True, "stream_delay": 0.2})
        settled = self.latest_settlement()
        self.assertIsNotNone(settled["ttfb_ms"])
        self.assertIsNotNone(settled["elapsed_ms"])
        # The first chunk is held back by the upstream's delay only between
        # chunks, so the first byte arrives fast; the whole call takes at
        # least the two gaps the stub slept.
        self.assertLess(settled["ttfb_ms"], 500.0)
        self.assertGreaterEqual(settled["elapsed_ms"], 400.0)

    def test_no_prompt_or_secret_reaches_the_ledger(self):
        self.ask({"model": MODEL,
                  "messages": [{"role": "user", "content": "SECRET-PROMPT-MARKER"}],
                  "stream": True})
        with open(self.usage_path) as handle:
            ledger = handle.read()
        self.assertNotIn("SECRET-PROMPT-MARKER", ledger)

    def test_client_disconnect_settles_unknown_with_generation_id(self):
        self.ask({"model": MODEL, "messages": [{"role": "user", "content": "hi"}],
                  "stream": True, "stream_delay": 1.0},
                 hang_up_after_first=True)
        self.assertTrue(self.wait_for(lambda: self.latest_settlement()))
        settled = self.latest_settlement()
        # The call was made; the client left; the usage chunk never arrived.
        # The row must say unknown — never zero — and carry enough for a
        # later reconciliation against the provider's records.
        self.assertIsNone(settled["cost_usd"])
        self.assertIn("disconnect", settled["note"])
        self.assertEqual(settled["outcome"], "client-disconnect")
        self.assertEqual(settled["generation_id"], "gen-test-123")

    # -- the reader's reconciliation -----------------------------------------

    def write_rows(self, rows):
        with open(self.usage_path, "w") as handle:
            for row in rows:
                handle.write(json.dumps(row, sort_keys=True) + "\n")

    def admitted(self, request_id):
        return {"phase": "admitted", "request_id": request_id, "model": MODEL,
                "path": "/v1/chat/completions", "scope": "meter-test"}

    def settled(self, request_id, cost=0.00025, **extra):
        row = {"phase": "settled", "request_id": request_id, "model": MODEL,
               "path": "/v1/chat/completions", "scope": "meter-test",
               "cost_usd": cost, "prompt_tokens": 11, "completion_tokens": 7,
               "total_tokens": 18, "note": ""}
        row.update(extra)
        return row

    def read(self):
        return receipts.read_guard_usage(self.usage_path)

    def test_healthy_ledger_sums(self):
        self.write_rows([self.admitted("c-1"), self.admitted("c-2"),
                         self.settled("c-1"), self.settled("c-2", 0.0005)])
        got = self.read()
        self.assertEqual(got["cost_source"], "guard-upstream")
        self.assertEqual(got["cost_usd"], 0.00075)
        self.assertEqual(got["calls"], 2)
        self.assertEqual(got["tokens_in"], 22)

    def test_out_of_order_settlements_reconcile(self):
        self.write_rows([self.admitted("c-1"), self.admitted("c-2"),
                         self.settled("c-2"), self.settled("c-1")])
        self.assertEqual(self.read()["cost_usd"], 0.0005)

    def test_missing_settlement_is_unknown(self):
        self.write_rows([self.admitted("c-1"), self.admitted("c-2"),
                         self.settled("c-1")])
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        self.assertIn("never settled", " ".join(got["notes"]))
        self.assertEqual(got["calls"], 2)

    def test_duplicate_settlement_cannot_conceal_a_missing_one(self):
        # Three admissions, one settlement written twice, one call never
        # accounted for. The counts agree (2 and 2 beside the duplicate
        # admission's own row) — only the ids tell the truth.
        self.write_rows([self.admitted("c-1"), self.admitted("c-2"), self.admitted("c-3"),
                         self.settled("c-1"), self.settled("c-1"), self.settled("c-2")])
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        joined = " ".join(got["notes"])
        self.assertIn("repeated", joined)
        self.assertIn("never settled", joined)

    def test_duplicate_admission_is_unknown(self):
        self.write_rows([self.admitted("c-1"), self.admitted("c-1"),
                         self.settled("c-1")])
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        self.assertIn("repeated", " ".join(got["notes"]))

    def test_settlement_without_admission_is_unknown(self):
        self.write_rows([self.admitted("c-1"), self.settled("c-1"),
                         self.settled("c-9")])
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        self.assertIn("no admission booked", " ".join(got["notes"]))

    def test_negative_cost_is_unknown(self):
        self.write_rows([self.admitted("c-1"), self.settled("c-1", cost=-0.00025)])
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        self.assertIn("cannot be a price", " ".join(got["notes"]))

    def test_nonfinite_cost_is_unknown(self):
        # json.dumps writes NaN/Infinity as bare words and json.loads reads
        # them back — a NaN sums as a number and would pass a plain
        # isinstance check, so the reader has to refuse it by name.
        for bad in (float("nan"), float("inf")):
            with open(self.usage_path, "w") as handle:
                handle.write(json.dumps(self.admitted("c-1")) + "\n")
                handle.write(json.dumps(self.settled("c-1", cost=bad)) + "\n")
            got = self.read()
            self.assertIsNone(got["cost_usd"], "a %r cost became a total" % bad)
            self.assertIn("cannot be a price", " ".join(got["notes"]))

    def test_settlement_without_usage_is_unknown_not_zero(self):
        self.write_rows([self.admitted("c-1"),
                         self.settled("c-1", cost=None,
                                       note="client disconnected mid-stream; "
                                            "upstream returned no usage block")])
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        self.assertIn("priced 0 of 1", " ".join(got["notes"]))

    def test_legacy_phaseless_rows_still_read(self):
        # The shape the guard wrote before admissions were recorded: a bare
        # settlement with no phase and no request id. Old evidence must
        # keep reading, and it is the only id-less shape that does.
        self.write_rows([{"model": MODEL, "path": "/v1/chat/completions",
                          "cost_usd": 0.00025, "prompt_tokens": 11,
                          "completion_tokens": 7, "total_tokens": 18}])
        got = self.read()
        self.assertEqual(got["cost_source"], "guard-upstream")
        self.assertEqual(got["cost_usd"], 0.00025)
        self.assertEqual(got["calls"], 1)

    def test_empty_ledger_reads_as_nothing(self):
        self.write_rows([])
        self.assertIsNone(self.read())

    def test_missing_identity_or_mixed_ledger_is_unknown(self):
        for rows in ([self.admitted(None), self.settled(None, cost=0.1)],
                     [self.admitted('r'), self.settled('r', cost=0.1),
                      dict(path='/chat/completions',cost_usd=0.1)]):
            self.write_rows(rows)
            self.assertIsNone(self.read()['cost_usd'])

    def test_torn_row_cannot_disappear_from_accounting(self):
        self.write_rows([self.admitted('r'), self.settled('r', cost=0.1)])
        with open(self.usage_path, 'a') as handle:
            handle.write('{"phase":"admitted"')
        self.assertIsNone(self.read()['cost_usd'])

    def test_live_evidence_shape_reads_unknown(self):
        # The shape the 2026-09-03 followup run actually wrote: sixteen
        # matched pairs, three of them settled with no usage because the
        # client hung up mid-stream. The count-based reader got this one
        # right by luck; the id-based one has to get it right by pairing.
        rows = []
        for number in range(1, 17):
            request_id = "followup-while-working-aforge-%d" % number
            rows.append(self.admitted(request_id))
            if number in (13, 15, 16):
                rows.append(self.settled(request_id, cost=None,
                                         note="client disconnected mid-stream; "
                                              "upstream returned no usage block"))
            else:
                rows.append(self.settled(request_id, cost=0.0001 * number))
        self.write_rows(rows)
        got = self.read()
        self.assertIsNone(got["cost_usd"])
        self.assertIn("priced 13 of 16", " ".join(got["notes"]))
        self.assertEqual(got["calls"], 16)


if __name__ == "__main__":
    unittest.main(verbosity=2)
