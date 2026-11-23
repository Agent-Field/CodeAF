"""Missing receipts are accounting markers, not measured model calls."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("receipts", Path(__file__).parents[1] / "lib/receipts.py")
receipts = importlib.util.module_from_spec(spec)
spec.loader.exec_module(receipts)


class BillingMarkerTests(unittest.TestCase):
    def test_home_reader_skips_missing_receipt_markers(self):
        with tempfile.TemporaryDirectory() as home:
            ledger = Path(home) / "v3/usage.jsonl"
            ledger.parent.mkdir()
            rows = [{"kind": "unbilled-marker", "model": "missing", "unbilled": True},
                    {"model": "measured", "calls": 1, "in": 10, "usd": 0.001}]
            ledger.write_text("".join(json.dumps(row) + "\n" for row in rows))
            got = receipts.read_codeaf_home(home, str(Path(home) / "stdout"))
            self.assertEqual(got["calls"], 1)
            self.assertEqual(got["models"], ["measured"])
            self.assertEqual(got["cost_usd"], 0.001)

    def test_marker_only_home_has_no_measured_calls_or_price(self):
        with tempfile.TemporaryDirectory() as home:
            ledger = Path(home) / "v3/usage.jsonl"
            ledger.parent.mkdir()
            ledger.write_text('{"kind":"unbilled-marker","model":"missing","unbilled":true}\n')
            got = receipts.read_codeaf_home(home, "")
            self.assertEqual(got["calls"], 0)
            self.assertIsNone(got["cost_usd"])
            self.assertIn("no measured calls", got["notes"][0])


if __name__ == "__main__":
    unittest.main()
