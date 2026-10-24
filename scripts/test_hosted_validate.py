#!/usr/bin/env python3
"""Tests for the pairing step of scripts/hosted-validate.py.

The rig once waited for a six-digit code that `codeaf pair` no longer prints, so these tests feed it
the sentences the command prints now (internal/pair/lines.go, docs/testing-anywhere.md) and check that
the link and the check number come out. Nothing here talks to a relay.
"""
import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

# What a new device prints while it waits, word for word from pair.InviteLines.
INVITE = """Approve this device from one you already use. Open this link there:
  https://codeaf.agentfield.ai/p/k7m2q9xd#Qm9vYmFyX2tleV9ieXRlc18zMl9sb25nXzAxMjM

Or, on a computer with codeaf, run:
  codeaf pair approve k7m2q9xd.Qm9vYmFyX2tleV9ieXRlc18zMl9sb25nXzAxMjM

Check number: 4821 (the other device shows the same number)
Waiting for approval; good for 10 minutes. Press ctrl+c to cancel.
"""


def load():
    spec = importlib.util.spec_from_file_location("hosted_validate", os.path.join(HERE, "hosted-validate.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class ParseInviteTest(unittest.TestCase):
    def test_link_and_check_number_come_out_of_the_current_pair_output(self):
        link, check = load().parse_invite(INVITE)
        self.assertEqual(link, "https://codeaf.agentfield.ai/p/k7m2q9xd#Qm9vYmFyX2tleV9ieXRlc18zMl9sb25nXzAxMjM")
        self.assertEqual(check, "4821")

    def test_a_screen_that_has_not_finished_printing_is_not_an_invite(self):
        half = INVITE.split("Check number")[0]
        self.assertIsNone(load().parse_invite(half))
        self.assertIsNone(load().parse_invite(""))


if __name__ == "__main__":
    unittest.main()
