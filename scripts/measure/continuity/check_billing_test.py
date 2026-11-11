"""Continuity snapshots retain missing receipts as labeled accounting records."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("continuity", Path(__file__).with_name("check.py"))
continuity = importlib.util.module_from_spec(spec)
spec.loader.exec_module(continuity)


class BillingSnapshotTests(unittest.TestCase):
    def test_snapshot_labels_marker_without_inventing_a_call(self):
        with tempfile.TemporaryDirectory() as home:
            ledger = Path(home) / "v3/usage.jsonl"
            ledger.parent.mkdir()
            ledger.write_text('{"session":"chat","model":"m","kind":"unbilled-marker","unbilled":true}\n')
            out = Path(home) / "snapshot.json"
            continuity.snap(home, "chat", "-", str(out))
            got = json.loads(out.read_text())
            self.assertEqual(got["usage_rows"], [["m", 0, 0, 0, 0, "unbilled-marker"]])
            self.assertEqual(got["usage_usd"], 0)


if __name__ == "__main__":
    unittest.main()
