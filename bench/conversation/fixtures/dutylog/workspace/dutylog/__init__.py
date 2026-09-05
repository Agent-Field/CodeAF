"""dutylog — weekly duty-minute reporting for a small depot.

Read a duty log CSV (parsing), check it against the rules (validation), total
it per ISO week and site inside a date window (aggregate), and render it
(report). SPEC.md is the contract; cli.py joins the four together.
"""
