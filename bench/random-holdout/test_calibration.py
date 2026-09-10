"""Calibration accepts missing APIs without accepting unusable references."""
import copy
import unittest

from prepare import validate_controls


class CalibrationTests(unittest.TestCase):
    def controls(self):
        return {"base": {"exit": 2, "tests": 1, "failures": 0, "errors": 1, "skipped": 0},
                "gold": {"exit": 0, "tests": 9, "failures": 0, "errors": 0, "skipped": 0}}

    def test_missing_api_can_prevent_base_collection(self):
        validate_controls(self.controls())

    def test_assertion_failure_is_also_a_negative_control(self):
        pair = self.controls()
        pair["base"].update(exit=1, tests=9, failures=1, errors=0)
        validate_controls(pair)

    def test_unusable_or_non_discriminating_controls_are_rejected(self):
        for side, changes in [
            ("base", {"exit": 0, "failures": 0, "errors": 0}),
            ("base", {"exit": 5, "tests": 0, "errors": 0}),
            ("base", {"exit": 124}),
            ("base", {"exit": 2, "failures": 0, "errors": 0}),
            ("gold", {"exit": 1, "failures": 1}),
            ("gold", {"exit": 2, "errors": 1}),
            ("gold", {"tests": 9, "skipped": 9}),
            ("gold", {"tests": 0, "skipped": 0}),
        ]:
            with self.subTest(side=side, changes=changes):
                pair = copy.deepcopy(self.controls())
                pair[side].update(changes)
                with self.assertRaises(AssertionError):
                    validate_controls(pair)


if __name__ == "__main__":
    unittest.main()
