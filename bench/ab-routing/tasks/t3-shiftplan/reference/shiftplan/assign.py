"""Assign workers to shifts. REFERENCE SOLUTION.

The rule: a shift goes to the highest-ranked worker who has the required skill
and is not already booked into an overlapping shift *on the same date*; the
default ranking is cheapest first, ties on worker id ascending.
"""

from .model import Assignment
from .strategy import GreedyCheapest

_CACHE = {}


def _shift_key(s):
    return (s.id, s.date, s.window.start, s.window.end, s.required_skill)


def _worker_key(w):
    return (w.id, w.rate_cents_per_hour, tuple(sorted(w.skills)))


def _cache_key(shifts, workers, blocked, strategy):
    """Everything that can change the answer, and nothing that cannot.

    The old key was shift ids and worker ids only, so a second call with a
    different `blocked` list was answered from the first call's cache. A key
    that omits an argument the function reads is not a cache, it is a bug with
    a dictionary attached.
    """
    return (
        tuple(sorted(_shift_key(s) for s in shifts)),
        tuple(sorted(_worker_key(w) for w in workers)),
        tuple(sorted((wid, i.start, i.end) for wid, i in blocked)),
        type(strategy).__name__,
    )


def assign_shifts(shifts, workers, blocked=None, strategy=None):
    """Return a list of Assignment, one per shift that could be staffed.

    blocked is a list of (worker_id, Interval) pairs that are unavailable for
    every date. It defaults to None rather than [] and is never mutated: a
    mutable default is shared by every call that omits the argument, so the
    first call's bookings would silently constrain the second's.
    """
    blocked = list(blocked or [])
    strategy = strategy or GreedyCheapest()

    key = _cache_key(shifts, workers, blocked, strategy)
    if key in _CACHE:
        # A copy, so a caller that mutates the returned list cannot corrupt
        # what the next caller is handed.
        return list(_CACHE[key])

    # Standing unavailability applies on every date; shift bookings are scoped
    # to the date they were made on, because a worker who finished at 17:00 on
    # Wednesday is free at 09:00 on Thursday.
    standing = {}
    for worker_id, interval in blocked:
        standing.setdefault(worker_id, []).append(interval)
    booked = {}

    out = []
    for shift in sorted(shifts, key=lambda s: (s.date, s.window.start, s.id)):
        candidates = [w for w in workers if shift.required_skill in w.skills]
        for worker in strategy.rank(shift, candidates):
            taken = (standing.get(worker.id, [])
                     + booked.get((worker.id, shift.date), []))
            if any(shift.window.overlaps(other) for other in taken):
                continue
            out.append(Assignment(shift_id=shift.id, worker_id=worker.id))
            booked.setdefault((worker.id, shift.date), []).append(shift.window)
            break

    _CACHE[key] = list(out)
    return out


def unstaffed(shifts, assignments):
    staffed = {a.shift_id for a in assignments}
    return sorted(s.id for s in shifts if s.id not in staffed)
