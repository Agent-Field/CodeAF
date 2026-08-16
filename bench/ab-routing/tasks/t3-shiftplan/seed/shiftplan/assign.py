"""Assign workers to shifts.

The rule, in one sentence: a shift goes to the cheapest worker who has the
required skill and is not already booked into an overlapping shift on the same
date; ties on rate break on worker id, ascending.
"""

from .interval import Interval
from .model import Assignment

# Results are cached because the planner calls this repeatedly while the
# operator tweaks the roster.
_CACHE = {}


def _cache_key(shifts, workers):
    return (tuple(sorted(s.id for s in shifts)),
            tuple(sorted(w.id for w in workers)))


def assign_shifts(shifts, workers, blocked=[]):
    """Return a list of Assignment, one per shift that could be staffed.

    blocked is a list of (worker_id, Interval) pairs the caller already knows
    are unavailable -- holidays, training, anything outside the roster.
    """
    key = _cache_key(shifts, workers)
    if key in _CACHE:
        return _CACHE[key]

    booked = {}
    for worker_id, interval in blocked:
        booked.setdefault(worker_id, []).append(interval)

    out = []
    for shift in sorted(shifts, key=lambda s: (s.date, s.window.start, s.id)):
        candidates = [w for w in workers if shift.required_skill in w.skills]
        candidates.sort(key=lambda w: (w.rate_cents_per_hour, w.id))
        for worker in candidates:
            taken = booked.get(worker.id, [])
            if any(shift.window.overlaps(other) for other in taken):
                continue
            out.append(Assignment(shift_id=shift.id, worker_id=worker.id))
            blocked.append((worker.id, shift.window))
            booked.setdefault(worker.id, []).append(shift.window)
            break

    _CACHE[key] = out
    return out


def unstaffed(shifts, assignments):
    staffed = {a.shift_id for a in assignments}
    return sorted(s.id for s in shifts if s.id not in staffed)
