"""Bucket accepted entries into ISO weeks inside a date window.

The window is inclusive at both ends. A week is an ISO week, identified by its
ISO year and its ISO week number together, and totals are kept per site.
"""


def in_window(entry, window_start, window_end):
    """True when the entry's start date falls inside the inclusive window."""
    return window_start <= entry.start.date() <= window_end


def week_key(day):
    """The ISO year and week of a date, as YYYY-Www."""
    iso_year, iso_week, _ = day.isocalendar()
    return "%04d-W%02d" % (iso_year, iso_week)


def weekly_totals(entries, window_start, window_end):
    """Minutes per site, per ISO week, for the entries inside the window."""
    totals = {}
    for entry in entries:
        if not in_window(entry, window_start, window_end):
            continue
        sites = totals.setdefault(week_key(entry.start.date()), {})
        sites[entry.site] = sites.get(entry.site, 0) + entry.minutes
    return totals
