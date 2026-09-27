"""Tests for durations.parse and durations.humanize (stdlib unittest only)."""

import unittest

import durations


class ParseTests(unittest.TestCase):
    def test_basic_examples(self):
        self.assertEqual(durations.parse("1h30m"), 5400)
        self.assertEqual(durations.parse("5400s"), 5400)
        self.assertEqual(durations.parse("1d"), 86400)
        self.assertEqual(durations.parse("1d1h1m1s"), 90061)

    def test_reordering_sums(self):
        self.assertEqual(durations.parse("30m1h"), 5400)
        self.assertEqual(durations.parse("1h30m"), durations.parse("30m1h"))
        self.assertEqual(durations.parse("1m1m"), 120)

    def test_whitespace(self):
        self.assertEqual(durations.parse("1h 30m"), 5400)
        self.assertEqual(durations.parse(" 1h30m "), 5400)
        self.assertEqual(durations.parse("1h  30m"), 5400)

    def test_returns_plain_int(self):
        result = durations.parse("1h30m")
        self.assertIsInstance(result, int)
        self.assertNotIsInstance(result, float)

    def test_malformed(self):
        malformed = [
            "",
            "   ",
            "h",
            "30",
            "1x",
            "1.5h",
            "-1h",
            "1h!",
            "1H",
            "garbage",
            "1h30",
            "1h 30",
        ]
        for text in malformed:
            with self.assertRaises(ValueError, msg=repr(text)):
                durations.parse(text)


class HumanizeTests(unittest.TestCase):
    def test_examples(self):
        self.assertEqual(durations.humanize(5400), "1h30m")
        self.assertEqual(durations.humanize(86400), "1d")
        self.assertEqual(durations.humanize(90061), "1d1h1m1s")
        self.assertEqual(durations.humanize(60), "1m")
        self.assertEqual(durations.humanize(45), "45s")

    def test_zero(self):
        self.assertEqual(durations.humanize(0), "0s")

    def test_omits_zero_components(self):
        self.assertEqual(durations.humanize(3630), "1h30s")
        self.assertEqual(durations.humanize(3600), "1h")

    def test_float_rounds_down(self):
        self.assertEqual(durations.humanize(5400.9), "1h30m")

    def test_negative_raises(self):
        with self.assertRaises(ValueError):
            durations.humanize(-1)
        with self.assertRaises(ValueError):
            durations.humanize(-1.5)

    def test_round_trip(self):
        values = [0, 1, 59, 60, 61, 3599, 3600, 5400, 86399, 86400, 90061, 1000000]
        for value in values:
            with self.subTest(value=value):
                self.assertEqual(durations.parse(durations.humanize(value)), value)


if __name__ == "__main__":
    unittest.main()
