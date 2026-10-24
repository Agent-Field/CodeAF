import unittest

import bill
from model import PRICE

MANIFEST = {"url": "u", "script": "s", "start": "2026-01-01T00:00:00Z", "end": "2026-01-01T00:30:00Z",
            "phases": [{"name": "idle_hold", "seconds": 100}, {"name": "warm_moves", "count": 10},
                       {"name": "cold_moves", "count": 2}]}


def vec(**kw):
    return {k: kw.get(k, 0) for k in bill.KEYS}


def billed(scale=1):
    return {"script": "heavy", "phases": {
        "idle_hold": vec(do_requests=50 * scale, do_gb_s=2 * scale, worker_requests=60),
        "warm_moves": vec(do_requests=100 * scale, do_gb_s=1 * scale, r2_class_a=20),
        "cold_moves": vec(do_requests=40 * scale, do_gb_s=4 * scale, r2_class_b=30)}}


class BillTest(unittest.TestCase):
    def test_extrapolation(self):
        u = bill.month_usage(MANIFEST, billed(), bill.SHAPE)
        bg = 8 * 3600 * 22 / 100            # 6336 times the 100 s phase
        self.assertAlmostEqual(u["do_requests"], 50 * bg + 100 / 10 * 40 + 40 / 2 * 4)
        self.assertAlmostEqual(u["do_gb_s"], 2 * bg + 1 / 10 * 40 + 4 / 2 * 4)
        self.assertAlmostEqual(u["worker_requests"], 60 * bg)
        self.assertAlmostEqual(u["r2_class_a"], 20 / 10 * 40)
        self.assertAlmostEqual(u["r2_class_b"], 30 / 2 * 4)

    def test_shape_override(self):
        shape = bill.shape_for({"shape": {"warm_per_month": 1}}, {"cold_per_month": 0, "days_per_month": None})
        self.assertEqual((shape["warm_per_month"], shape["cold_per_month"], shape["days_per_month"]), (1, 0, 22))

    def test_charge(self):
        u = vec(do_requests=2e6, r2_class_a=3e6)
        self.assertAlmostEqual(bill.charge(u, 0, 0), 2 * PRICE["do_req_per_m"] + 3 * PRICE["r2_a_per_m"])
        self.assertAlmostEqual(bill.charge(u, 1, 1), PRICE["workers_base"] + 1 * PRICE["do_req_per_m"]
                               + 2 * PRICE["r2_a_per_m"])

    def test_rows_priced_apart(self):
        self.assertAlmostEqual(bill.charge(vec(do_rows_read=1e6, do_rows_written=1e6), 0, 0), bill.ROWS_READ["per_m"] + bill.ROWS_WRITTEN["per_m"])

    def test_render_one_column(self):
        out = bill.render([dict(title="first", manifest=MANIFEST, billed=billed())], bill.SHAPE)
        self.assertTrue(out.startswith(bill.HEADING + "\n"))
        self.assertIn("| Usage | first |", out)
        self.assertIn("100,000 users, 100% active", out)
        self.assertIn("**Total**", out)

    def test_render_many_columns_and_ratios(self):
        cols = [dict(title=t, manifest=MANIFEST, billed=billed(k)) for t, k in (("one", 1), ("two", 0.5), ("three", 0.25))]
        out = bill.render(cols, bill.SHAPE)
        self.assertIn("| Usage | one | two | three |", out)
        self.assertIn("Ratio two / one per user-month: DO GB-s 0.50, DO requests 0.50", out)
        self.assertIn("Ratio three / one per user-month: DO GB-s 0.25", out)

    def test_class_a_is_at_least_the_client_frame_puts(self):
        phase = {"client_requests": {"POST /v1/store/frames": 12}}
        self.assertEqual(bill.class_a_floor({"r2_class_a": 0}, phase)["r2_class_a"], 12)
        self.assertEqual(bill.class_a_floor({"r2_class_a": 30}, phase)["r2_class_a"], 30)

    def test_splice_replace_and_append(self):
        sec = bill.HEADING + "\n\nnew\n"
        doc = "# T\n\n## 8. x\n\nbody\n\n## 9. Billed heavy-user run\n\nold\n\n## 10. y\n\nz\n"
        self.assertEqual(bill.splice(doc, sec),
                         "# T\n\n## 8. x\n\nbody\n\n## 9. Billed heavy-user run\n\nnew\n\n## 10. y\n\nz\n")
        last = "# T\n\n## 9. Billed heavy-user run\n\nold\n"
        self.assertEqual(bill.splice(last, sec), "# T\n\n" + sec)
        self.assertEqual(bill.splice("# T\n\nbody\n", sec), "# T\n\nbody\n\n" + sec)
        again = bill.splice(bill.splice("# T\n", sec), sec)
        self.assertEqual(again, bill.splice("# T\n", sec))


if __name__ == "__main__":
    unittest.main()
