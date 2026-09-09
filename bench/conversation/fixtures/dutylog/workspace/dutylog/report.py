"""Render weekly totals as a report that depends only on the totals.

Two runs over the same entries in a different order are the same report, byte
for byte: the ordering is fully specified, so nothing is left to the order rows
happened to arrive in.
"""
from decimal import Decimal, ROUND_HALF_UP

ROW = "%-9s %-25s %5s"
HEADER = ROW % ("week", "site", "hours")
TOTAL_LABEL = "TOTAL"


def hours(minutes):
    """Minutes as hours to one decimal place, halves rounded up."""
    return (Decimal(minutes) / Decimal(60)).quantize(Decimal("0.1"),
                                                     rounding=ROUND_HALF_UP)


def render(totals):
    """The whole report, ending in a newline."""
    lines = [HEADER]
    grand = 0
    for week in sorted(totals):
        for site, minutes in sorted(totals[week].items(),
                                    key=lambda item: -item[1]):
            lines.append(ROW % (week, site, hours(minutes)))
            grand += minutes
    lines.append(ROW % (TOTAL_LABEL, "", hours(grand)))
    return "\n".join(lines) + "\n"
