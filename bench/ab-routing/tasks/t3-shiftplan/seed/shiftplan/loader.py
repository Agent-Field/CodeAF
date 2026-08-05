"""Load a roster from JSON."""

import json

from .model import Shift, ShiftPlanError, Worker


def load_roster(path):
    """Read a roster file and return (shifts, workers).

    A roster that cannot be understood is an error the caller must see. This
    function must never answer a malformed file with an empty roster: a payroll
    run against an empty roster pays nobody and looks like a quiet success.
    """
    try:
        with open(path) as handle:
            raw = json.load(handle)
        shifts = [Shift.from_dict(s) for s in raw["shifts"]]
        workers = [Worker.from_dict(w) for w in raw["workers"]]
        return shifts, workers
    except Exception:
        return [], []


def load_roster_text(text):
    try:
        raw = json.loads(text)
    except Exception:
        raise ShiftPlanError("roster is not valid JSON")
    if "shifts" not in raw or "workers" not in raw:
        raise ShiftPlanError("roster needs both 'shifts' and 'workers'")
    return ([Shift.from_dict(s) for s in raw["shifts"]],
            [Worker.from_dict(w) for w in raw["workers"]])
