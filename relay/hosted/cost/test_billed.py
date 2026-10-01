import unittest
from datetime import datetime, timezone

import billed


def r2(action, status, n):
    return {"dimensions": {"actionType": action, "actionStatus": status}, "sum": {"requests": n}}


class BilledTest(unittest.TestCase):
    def test_classify(self):
        self.assertEqual(billed.classify("PutObject", "success"), "A")
        self.assertEqual(billed.classify("ListObjects", "success"), "A")
        self.assertEqual(billed.classify("GetObject", "notFound"), "B")
        self.assertEqual(billed.classify("HeadBucket", "success"), "B")
        self.assertIsNone(billed.classify("DeleteObject", "success"))
        self.assertIsNone(billed.classify("AbortMultipartUpload", "success"))
        self.assertIsNone(billed.classify("PutObject", "serverError"))
        self.assertIsNone(billed.classify("SomethingNew", "success"))

    def test_parse_r2(self):
        rows = [r2("PutObject", "success", 27), r2("GetObject", "success", 130),
                r2("GetObject", "notFound", 3), r2("DeleteObject", "success", 9)]
        self.assertEqual(billed.parse_r2(rows), {"r2_class_a": 27, "r2_class_b": 133})
        self.assertEqual(billed.unknown_actions(rows + [r2("Zap", "success", 1)]), ["Zap"])

    def test_parse_workers_and_do(self):
        w = billed.parse_workers([{"sum": {"requests": 1247, "cpuTimeUs": 982911}}])
        self.assertEqual(w, {"worker_requests": 1247, "worker_cpu_ms": 982.911})
        rows = [{"sum": {"activeTime": 14_300_000, "duration": 1.83, "rowsRead": 5, "rowsWritten": 2}},
                {"sum": {"activeTime": 0, "duration": 0.0, "rowsRead": 1, "rowsWritten": 0}}]
        d = billed.parse_do_periodic(rows)
        self.assertEqual((d["do_active_s"], d["do_gb_s"], d["do_rows_read"]), (14.3, 1.83, 6))
        self.assertEqual(billed.parse_do_invocations([{"sum": {"requests": 4}}, {"sum": {"requests": 6}}]), 10)

    def test_minute_window(self):
        s = datetime(2026, 9, 30, 10, 0, 40, tzinfo=timezone.utc)
        e = datetime(2026, 9, 30, 10, 5, 0, tzinfo=timezone.utc)
        lo, hi = billed.minute_window(s, e)
        self.assertEqual((billed.iso(lo), billed.iso(hi)), ("2026-09-30T10:00:00Z", "2026-09-30T10:05:00Z"))
        _, hi2 = billed.minute_window(s, e.replace(second=1))
        self.assertEqual(billed.iso(hi2), "2026-09-30T10:06:00Z")

    def test_sum_phases(self):
        a = dict.fromkeys(billed.METRICS, 1)
        self.assertEqual(billed.sum_phases({"x": a, "y": a})["r2_class_a"], 2)


if __name__ == "__main__":
    unittest.main()
