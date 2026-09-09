"""An alternative correct report module: f-strings and an explicit key."""
from decimal import Decimal, ROUND_HALF_UP

STEP = Decimal("0.1")


def hours(minutes):
    return (Decimal(int(minutes)) / 60).quantize(STEP, rounding=ROUND_HALF_UP)


def _line(week, site, value):
    return f"{week:<9} {site:<25} {value:>5}"


def render(totals):
    out = [_line("week", "site", "hours")]
    everything = 0
    for week in sorted(totals.keys()):
        rows = totals[week]
        for site in sorted(rows, key=lambda name: (-rows[name], name)):
            out.append(_line(week, site, hours(rows[site])))
            everything += rows[site]
    out.append(_line("TOTAL", "", hours(everything)))
    return "\n".join(out) + "\n"
