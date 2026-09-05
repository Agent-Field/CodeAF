"""An alternative correct aggregate module: a defaultdict and a slice."""
from collections import defaultdict


def in_window(entry, window_start, window_end):
    return not (entry.start.date() < window_start or entry.start.date() > window_end)


def week_key(day):
    return "{:04d}-W{:02d}".format(*day.isocalendar()[:2])


def weekly_totals(entries, window_start, window_end):
    gathered = defaultdict(lambda: defaultdict(int))
    for entry in entries:
        if in_window(entry, window_start, window_end):
            gathered[week_key(entry.start.date())][entry.site] += entry.minutes
    return {week: dict(sites) for week, sites in gathered.items()}
