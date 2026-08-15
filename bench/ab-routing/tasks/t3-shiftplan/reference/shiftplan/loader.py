"""Load a roster from JSON. REFERENCE SOLUTION."""

import json

from .model import Shift, ShiftPlanError, Worker


def load_roster(path):
    """Read a roster file and return (shifts, workers).

    A roster that cannot be understood is raised, never answered with an empty
    roster: a payroll run against an empty roster pays nobody and looks like a
    quiet success.
    """
    try:
        with open(path) as handle:
            text = handle.read()
    except OSError as e:
        raise ShiftPlanError(f"cannot read roster {path}: {e}") from e
    return load_roster_text(text)


def load_roster_text(text):
    try:
        raw = json.loads(text)
    except Exception as e:
        raise ShiftPlanError("roster is not valid JSON") from e
    if not isinstance(raw, dict) or "shifts" not in raw or "workers" not in raw:
        raise ShiftPlanError("roster needs both 'shifts' and 'workers'")
    try:
        shifts = [Shift.from_dict(s) for s in raw["shifts"]]
        workers = [Worker.from_dict(w) for w in raw["workers"]]
    except (KeyError, TypeError, ValueError) as e:
        raise ShiftPlanError(f"roster entry is malformed: {e}") from e
    return shifts, workers
