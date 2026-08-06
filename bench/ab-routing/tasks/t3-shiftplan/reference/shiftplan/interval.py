"""Half-open time intervals, measured in whole minutes from midnight.

REFERENCE SOLUTION -- never shipped to an agent. Its only job is to prove the
hidden suite is satisfiable, so that a failing grade is evidence about the
model rather than about the grader.
"""


class Interval:
    """A half-open interval [start, end): start is included, end is not."""

    __slots__ = ("start", "end")

    def __init__(self, start, end):
        if end < start:
            raise ValueError(f"end {end} precedes start {start}")
        self.start = start
        self.end = end

    def __repr__(self):
        return f"Interval({self.start}, {self.end})"

    def __eq__(self, other):
        return (isinstance(other, Interval)
                and self.start == other.start and self.end == other.end)

    def __hash__(self):
        return hash((self.start, self.end))

    @property
    def minutes(self):
        return self.end - self.start

    @property
    def empty(self):
        return self.end <= self.start

    def overlaps(self, other):
        if self.empty or other.empty:
            return False
        return self.start < other.end and other.start < self.end

    def contains(self, minute):
        return self.start <= minute < self.end


def any_overlap(intervals):
    """True if any two intervals in the list overlap.

    Empty intervals are dropped first: sorting by start makes the adjacent-pair
    check sufficient for non-empty intervals, but an empty interval sitting
    between two that do overlap would hide them.
    """
    ordered = sorted((i for i in intervals if not i.empty),
                     key=lambda i: (i.start, i.end))
    return any(left.overlaps(right) for left, right in zip(ordered, ordered[1:]))


def total_minutes(intervals):
    return sum(i.minutes for i in intervals)
