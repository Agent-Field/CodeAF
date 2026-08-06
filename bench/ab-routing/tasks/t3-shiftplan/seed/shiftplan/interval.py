"""Half-open time intervals, measured in whole minutes from midnight."""


class Interval:
    """A half-open interval [start, end): start is included, end is not.

    Two shifts that merely touch -- one ending exactly when the next begins --
    do not overlap and may be worked back to back by the same person.
    """

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

    def overlaps(self, other):
        return self.start <= other.end and other.start <= self.end

    def contains(self, minute):
        return self.start <= minute <= self.end


def any_overlap(intervals):
    """True if any two intervals in the list overlap."""
    ordered = sorted(intervals, key=lambda i: (i.start, i.end))
    for left, right in zip(ordered, ordered[1:]):
        if left.overlaps(right):
            return True
    return False


def total_minutes(intervals):
    return sum(i.minutes for i in intervals)
